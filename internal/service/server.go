// Package service provides implementation of the core application server.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/resident-x/go-grott/internal/api"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// DataCollectionServer manages the inverter data collection, processing and publishing.
type DataCollectionServer struct {
	config      *config.Config
	listener    net.Listener
	apiServer   *api.Server
	parser      domain.DataParser
	publisher   domain.MessagePublisher
	monitoring  domain.MonitoringService
	registry    domain.Registry
	clients     map[string]net.Conn
	clientMutex sync.RWMutex
	done        chan struct{}
	logger      zerolog.Logger
	startTime   time.Time
}

// NewDataCollectionServer creates a new data collection server instance.
func NewDataCollectionServer(cfg *config.Config, parser domain.DataParser,
	publisher domain.MessagePublisher, monitoring domain.MonitoringService) (*DataCollectionServer, error) {
	// Create device registry.
	registry := domain.NewDeviceRegistry()

	// Create logger with component context.
	logger := log.With().Str("component", "server").Logger()

	// Create server instance.
	server := &DataCollectionServer{
		config:     cfg,
		parser:     parser,
		publisher:  publisher,
		monitoring: monitoring,
		registry:   registry,
		clients:    make(map[string]net.Conn),
		done:       make(chan struct{}),
		logger:     logger,
	}

	// Initialize HTTP API server if enabled.
	if cfg.API.Enabled {
		server.apiServer = api.NewServer(cfg, registry)
	}

	return server, nil
}

// Start initializes and starts all server components.
func (s *DataCollectionServer) Start(ctx context.Context) error {
	// Record start time.
	s.startTime = time.Now()

	// Start listening for TCP connections.
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener on %s: %w", addr, err)
	}
	s.listener = listener

	s.logger.Info().
		Str("address", addr).
		Str("version", "1.0.0").
		Msg("Server started")

	// Start HTTP API server if enabled.
	if s.apiServer != nil {
		if err := s.apiServer.Start(ctx); err != nil {
			return fmt.Errorf("failed to start API server: %w", err)
		}
	}

	// Start accepting connections in a goroutine
	go s.acceptConnections(ctx)

	return nil
}

// Stop gracefully shuts down all server components.
func (s *DataCollectionServer) Stop(ctx context.Context) error {
	s.logger.Info().Msg("Stopping server")

	// Signal shutdown
	close(s.done)

	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.logger.Error().Err(err).Msg("Failed to close listener")
		}
	}

	// Close all client connections
	s.clientMutex.Lock()
	for id, conn := range s.clients {
		if err := conn.Close(); err != nil {
			s.logger.Error().
				Str("client", id).
				Err(err).
				Msg("Failed to close client connection")
		}
	}
	s.clientMutex.Unlock()

	// Stop API server
	if s.apiServer != nil {
		if err := s.apiServer.Stop(ctx); err != nil {
			s.logger.Error().Err(err).Msg("Failed to stop API server")
		}
	}

	// Close message publisher
	if err := s.publisher.Close(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to close message publisher")
	}

	// Close monitoring service
	if err := s.monitoring.Close(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to close monitoring service")
	}

	return nil
}

// acceptConnections handles incoming TCP connections.
func (s *DataCollectionServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		default:
			// Accept new connection
			conn, err := s.listener.Accept()
			if err != nil {
				// Check if server is shutting down
				select {
				case <-s.done:
					return
				case <-ctx.Done():
					return
				default:
					// Check if this is a "use of closed network connection" error
					// which indicates the listener was closed during shutdown
					if isClosedConnError(err) {
						return
					}
					s.logger.Error().Err(err).Msg("Failed to accept connection")
					continue
				}
			}

			// Handle connection in a new goroutine
			go s.handleConnection(ctx, conn)
		}
	}
}

// isClosedConnError checks if the error is due to a closed network connection.
func isClosedConnError(err error) bool {
	return err != nil && (err.Error() == "use of closed network connection" ||
		err.Error() == "accept tcp: use of closed network connection")
}

// handleConnection processes data from a client connection.
func (s *DataCollectionServer) handleConnection(ctx context.Context, conn net.Conn) {
	// Get client IP and port
	clientAddr := conn.RemoteAddr().String()

	// Register client connection
	s.clientMutex.Lock()
	s.clients[clientAddr] = conn
	s.clientMutex.Unlock()

	// Ensure connection is closed when done
	defer func() {
		err := conn.Close()
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to close client connection")
		}
		s.clientMutex.Lock()
		delete(s.clients, clientAddr)
		s.clientMutex.Unlock()
	}()

	s.logger.Info().Str("address", clientAddr).Msg("Client connected")

	// Buffer for reading data
	buf := make([]byte, 4096)

	for {
		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		default:
			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				s.logger.Error().Err(err).Msg("Failed to set read deadline")
				return
			}

			// Read data from connection
			n, err := conn.Read(buf)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					// Just a timeout, continue
					continue
				}

				// Log disconnection and exit
				s.logger.Info().
					Str("address", clientAddr).
					Err(err).
					Msg("Client disconnected")
				return
			}

			if n > 0 {
				// Process received data
				if err := s.processData(ctx, clientAddr, buf[:n]); err != nil {
					s.logger.Error().
						Str("address", clientAddr).
						Err(err).
						Msg("Failed to process data")
				}
			}
		}
	}
}

// processData handles incoming data packets.
func (s *DataCollectionServer) processData(ctx context.Context, clientAddr string, data []byte) error {
	// Parse data
	inverterData, err := s.parser.Parse(ctx, data)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Extract client IP and port
	host, _, err := net.SplitHostPort(clientAddr)
	if err != nil {
		return fmt.Errorf("failed to parse client address: %w", err)
	}

	// Register devices in registry
	if inverterData.DataloggerSerial != "" {
		if err := s.registry.RegisterDatalogger(inverterData.DataloggerSerial, host, 0, ""); err != nil {
			s.logger.Error().Err(err).Msg("Failed to register datalogger")
		}

		if inverterData.PVSerial != "" {
			if err := s.registry.RegisterInverter(inverterData.DataloggerSerial, inverterData.PVSerial, ""); err != nil {
				s.logger.Error().Err(err).Msg("Failed to register inverter")
			}
		}
	}

	// Publish to message broker
	topic := s.config.MQTT.Topic
	if s.config.MQTT.IncludeInverterID && inverterData.PVSerial != "" {
		topic = fmt.Sprintf("%s/%s", topic, inverterData.PVSerial)
	}

	if err := s.publisher.Publish(ctx, topic, inverterData); err != nil {
		s.logger.Error().
			Str("topic", topic).
			Err(err).
			Msg("Failed to publish message")
	}

	// Send to monitoring service
	if err := s.monitoring.Send(ctx, inverterData); err != nil {
		s.logger.Error().Err(err).Msg("Failed to send to monitoring service")
	}

	s.logger.Debug().
		Str("datalogger", inverterData.DataloggerSerial).
		Str("inverter", inverterData.PVSerial).
		Msg("Processed data packet")

	return nil
}
