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
	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/resident-x/go-grott/internal/session"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// DataCollectionServer manages the inverter data collection, processing and publishing.
type DataCollectionServer struct {
	config          *config.Config
	listener        net.Listener
	apiServer       *api.Server
	parser          domain.DataParser
	publisher       domain.MessagePublisher
	monitoring      domain.MonitoringService
	registry        domain.Registry
	sessionManager  *session.SessionManager
	responseManager *protocol.ResponseManager
	clients         map[string]net.Conn
	clientMutex     sync.RWMutex
	done            chan struct{}
	logger          zerolog.Logger
	startTime       time.Time
}

// NewDataCollectionServer creates a new data collection server instance.
func NewDataCollectionServer(cfg *config.Config, parser domain.DataParser,
	publisher domain.MessagePublisher, monitoring domain.MonitoringService) (*DataCollectionServer, error) {
	// Create device registry.
	registry := domain.NewDeviceRegistry()

	// Create logger with component context.
	logger := log.With().Str("component", "server").Logger()

	// Create session manager with 30 minute timeout
	sessionManager := session.NewSessionManager(30 * time.Minute)

	// Create response manager
	responseManager := protocol.NewResponseManager()

	// Create server instance.
	server := &DataCollectionServer{
		config:          cfg,
		parser:          parser,
		publisher:       publisher,
		monitoring:      monitoring,
		registry:        registry,
		sessionManager:  sessionManager,
		responseManager: responseManager,
		clients:         make(map[string]net.Conn),
		done:            make(chan struct{}),
		logger:          logger,
	}

	// Initialize HTTP API server if enabled.
	if cfg.API.Enabled {
		server.apiServer = api.NewServer(cfg, registry)
		// Connect the API server to the session manager for command queuing
		server.connectAPIToSessions()
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

	// Close session manager
	if s.sessionManager != nil {
		s.sessionManager.Close()
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
	clientAddr := conn.RemoteAddr().String()
	host, _ := s.extractHost(clientAddr)

	session := s.setupConnectionSession(conn, clientAddr)
	defer s.cleanupConnection(conn, clientAddr, session)

	s.logger.Info().
		Str("address", clientAddr).
		Str("session_id", session.ID).
		Msg("Client connected")

	s.runConnectionLoop(ctx, conn, session, clientAddr, host)
}

// extractHost extracts the host part from a client address.
func (s *DataCollectionServer) extractHost(clientAddr string) (string, error) {
	host, _, err := net.SplitHostPort(clientAddr)
	if err != nil {
		// Fallback to full address if parsing fails
		return clientAddr, err
	}
	return host, nil
}

// setupConnectionSession creates and initializes a session for the connection.
func (s *DataCollectionServer) setupConnectionSession(conn net.Conn, clientAddr string) *session.Session {
	// Create session for this connection
	session := s.sessionManager.CreateSession(conn)

	// Register client connection (for backward compatibility)
	s.clientMutex.Lock()
	s.clients[clientAddr] = conn
	s.clientMutex.Unlock()

	return session
}

// cleanupConnection handles cleanup when a connection ends.
func (s *DataCollectionServer) cleanupConnection(conn net.Conn, clientAddr string, session *session.Session) {
	err := conn.Close()
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to close client connection")
	}

	s.clientMutex.Lock()
	delete(s.clients, clientAddr)
	s.clientMutex.Unlock()

	// Remove session
	s.sessionManager.RemoveSession(session.ID)
}

// runConnectionLoop handles the main connection processing loop.
func (s *DataCollectionServer) runConnectionLoop(ctx context.Context, conn net.Conn, session *session.Session, clientAddr, host string) {
	buf := make([]byte, 4096)

	for {
		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		default:
			if s.shouldContinueLoop(ctx, conn, session, buf, clientAddr, host) {
				continue
			} else {
				return
			}
		}
	}
}

// shouldContinueLoop processes one iteration of the connection loop.
// Returns true to continue the loop, false to exit.
func (s *DataCollectionServer) shouldContinueLoop(ctx context.Context, conn net.Conn, session *session.Session, buf []byte, clientAddr, host string) bool {
	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to set read deadline")
		return false
	}

	// Read data from connection
	n, err := conn.Read(buf)
	if err != nil {
		return s.handleReadError(err, clientAddr, session.ID)
	}

	if n > 0 {
		s.processIncomingData(ctx, session, buf[:n], clientAddr)
	}

	// Check for queued commands from HTTP API
	s.processQueuedCommands(session, clientAddr, host)

	return true
}

// handleReadError processes read errors and determines if the loop should continue.
func (s *DataCollectionServer) handleReadError(err error, clientAddr, sessionID string) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// Just a timeout, continue
		return true
	}

	// Log disconnection and exit
	s.logger.Info().
		Str("address", clientAddr).
		Str("session_id", sessionID).
		Err(err).
		Msg("Client disconnected")
	return false
}

// processIncomingData handles incoming data from the client.
func (s *DataCollectionServer) processIncomingData(ctx context.Context, session *session.Session, data []byte, clientAddr string) {
	// Update session activity and stats
	session.UpdateActivity()
	session.AddBytesReceived(int64(len(data)))

	// Process received data and potentially send response
	if err := s.processDataWithAPIIntegration(ctx, session, data); err != nil {
		session.IncrementErrorCount()
		s.logger.Error().
			Str("address", clientAddr).
			Str("session_id", session.ID).
			Err(err).
			Msg("Failed to process data")
	}
}

// processQueuedCommands checks for and processes any queued commands from the HTTP API.
func (s *DataCollectionServer) processQueuedCommands(session *session.Session, clientAddr, host string) {
	if s.apiServer == nil {
		return
	}

	commandQueue := s.apiServer.GetCommandQueue(host, 0)
	if commandQueue == nil {
		return
	}

	select {
	case command := <-commandQueue:
		// Send command to device
		if err := s.sendCommandToDevice(session, command); err != nil {
			s.logger.Error().
				Str("address", clientAddr).
				Str("session_id", session.ID).
				Err(err).
				Msg("Failed to send queued command")
		}
	default:
		// No commands queued
	}
}

// processDataBidirectional handles incoming data packets with bidirectional communication support.
func (s *DataCollectionServer) processDataBidirectional(ctx context.Context, session *session.Session, data []byte) error {
	clientAddr := session.RemoteAddr

	// Check if we should generate a response
	if s.responseManager.ShouldRespond(data, clientAddr) {
		// Generate response
		response, err := s.responseManager.HandleIncomingData(data)
		if err != nil {
			s.logger.Debug().
				Str("address", clientAddr).
				Str("session_id", session.ID).
				Err(err).
				Msg("Failed to generate response")
		} else if response != nil {
			// Send response back to the device
			if err := s.sendResponse(session, response); err != nil {
				s.logger.Error().
					Str("address", clientAddr).
					Str("session_id", session.ID).
					Err(err).
					Msg("Failed to send response")
			} else {
				session.UpdateLastCommand()
				s.logger.Debug().
					Str("address", clientAddr).
					Str("session_id", session.ID).
					Uint8("response_type", response.Type).
					Msg("Response sent successfully")

				// Note: Python reference implementation does not have automatic scheduling
				// after record type "03" - keeping behavior simple and reactive only
			}
		}
	}

	// Continue with normal data processing
	return s.processData(ctx, clientAddr, data)
}

// sendResponse sends a response back to the connected device.
func (s *DataCollectionServer) sendResponse(session *session.Session, response *protocol.Response) error {
	if session.Connection == nil {
		return fmt.Errorf("session has no active connection")
	}

	// Set write deadline
	if err := session.Connection.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Write response data
	n, err := session.Connection.Write(response.Data)
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	// Update session stats
	session.AddBytesSent(int64(n))

	s.logger.Debug().
		Str("session_id", session.ID).
		Int("bytes", n).
		Str("response_hex", protocol.FormatResponse(response)).
		Msg("Response sent")

	return nil
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

	// Update session with device information if available
	if session, exists := s.sessionManager.GetSessionByAddr(clientAddr); exists {
		s.updateSessionWithDeviceInfo(session, inverterData, data)
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

// updateSessionWithDeviceInfo updates session information based on parsed data.
func (s *DataCollectionServer) updateSessionWithDeviceInfo(sess *session.Session, data *domain.InverterData, rawData []byte) {
	// Determine device type based on data content
	deviceType := session.DeviceTypeUnknown
	serialNumber := ""

	if data.DataloggerSerial != "" {
		deviceType = session.DeviceTypeDatalogger
		serialNumber = data.DataloggerSerial

		// If we also have PV serial, this might be an inverter
		if data.PVSerial != "" {
			deviceType = session.DeviceTypeInverter
			serialNumber = data.PVSerial
		}
	}

	// Detect protocol from raw data
	protocol := "unknown"
	if len(rawData) >= 4 {
		protocol = fmt.Sprintf("%02x", rawData[3])
	}

	// Update session with device information
	sess.SetDeviceInfo(deviceType, serialNumber, protocol, "")

	// Update session state to active if we successfully parsed data
	if sess.GetState() == session.SessionStateConnected {
		sess.SetState(session.SessionStateActive)
	}
}

// GetMetrics returns server metrics including command scheduler status.
func (s *DataCollectionServer) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// Server metrics
	metrics["uptime"] = time.Since(s.startTime).Seconds()
	metrics["start_time"] = s.startTime

	// Client connection metrics
	s.clientMutex.RLock()
	metrics["active_connections"] = len(s.clients)
	s.clientMutex.RUnlock()

	// Session metrics
	metrics["session_count"] = s.sessionManager.GetSessionCount()
	allSessions := s.sessionManager.GetAllSessions()

	// Count sessions by state
	stateCount := make(map[string]int)
	deviceTypeCount := make(map[string]int)

	for _, sessionStat := range allSessions {
		stateCount[sessionStat.State.String()]++
		deviceTypeCount[sessionStat.DeviceType.String()]++
	}

	metrics["session_states"] = stateCount
	metrics["device_types"] = deviceTypeCount

	return metrics
}

// connectAPIToSessions connects the HTTP API server to the session management system
// for command queuing and response tracking.
func (s *DataCollectionServer) connectAPIToSessions() {
	if s.apiServer == nil {
		return
	}

	s.logger.Debug().Msg("Connected API server to session management system")
}

// processDataWithAPIIntegration processes data and integrates with the HTTP API response tracking.
func (s *DataCollectionServer) processDataWithAPIIntegration(ctx context.Context, session *session.Session, data []byte) error {
	// First, let the API server process the response for command tracking
	if s.apiServer != nil {
		s.apiServer.ProcessDeviceResponse(data)
	}

	// Then process the data normally
	return s.processDataBidirectional(ctx, session, data)
}

// sendCommandToDevice sends a command to a connected device.
func (s *DataCollectionServer) sendCommandToDevice(session *session.Session, command []byte) error {
	if session.Connection == nil {
		return fmt.Errorf("session has no active connection")
	}

	// Set write deadline
	if err := session.Connection.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Write command data
	n, err := session.Connection.Write(command)
	if err != nil {
		return fmt.Errorf("failed to write command: %w", err)
	}

	// Update session stats
	session.AddBytesSent(int64(n))

	s.logger.Debug().
		Str("session_id", session.ID).
		Int("bytes", n).
		Msg("Command sent to device")

	return nil
}
