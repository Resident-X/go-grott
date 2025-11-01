// Package pubsub provides implementations of message publishers.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/rs/zerolog/log"
)

// NoopPublisher is a no-operation implementation of the MessagePublisher interface.
type NoopPublisher struct{}

// NewNoopPublisher creates a new no-operation publisher.
func NewNoopPublisher() *NoopPublisher {
	return &NoopPublisher{}
}

// Connect is a no-op for the NoopPublisher.
func (p *NoopPublisher) Connect(_ context.Context) error {
	return nil
}

// Publish is a no-op for the NoopPublisher.
func (p *NoopPublisher) Publish(_ context.Context, _ string, _ interface{}) error {
	return nil
}

// Close is a no-op for the NoopPublisher.
func (p *NoopPublisher) Close() error {
	return nil
}

// MQTTPublisher implements the MessagePublisher interface for MQTT.
type MQTTPublisher struct {
	config        *config.Config
	client        mqtt.Client
	connected     bool
	clientFactory func(*config.Config) mqtt.Client
	mu            sync.RWMutex  // Protects connected flag
	reconnectDone chan struct{} // Signals reconnection loop to stop
}

// NewMQTTPublisher creates a new MQTT publisher.
func NewMQTTPublisher(cfg *config.Config) *MQTTPublisher {
	return &MQTTPublisher{
		config:        cfg,
		connected:     false,
		clientFactory: createMQTTClient,
		reconnectDone: make(chan struct{}),
	}
}

// NewMQTTPublisherWithClient creates a new MQTT publisher with a custom client (for testing).
func NewMQTTPublisherWithClient(cfg *config.Config, client mqtt.Client) *MQTTPublisher {
	return &MQTTPublisher{
		config:        cfg,
		client:        client,
		connected:     false,
		reconnectDone: make(chan struct{}),
	}
}

// createMQTTClient is the default factory function for creating MQTT clients.
func createMQTTClient(cfg *config.Config) mqtt.Client {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.MQTT.Host, cfg.MQTT.Port)).
		SetClientID(fmt.Sprintf("go-grott-%d", time.Now().Unix())).
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetWriteTimeout(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetCleanSession(false)

	// Set credentials if provided
	if cfg.MQTT.Username != "" {
		opts.SetUsername(cfg.MQTT.Username)
		opts.SetPassword(cfg.MQTT.Password)
	}

	return mqtt.NewClient(opts)
}

// Connect establishes a connection to the MQTT broker.
func (p *MQTTPublisher) Connect(ctx context.Context) error {
	// If MQTT is disabled, do nothing
	if !p.config.MQTT.Enabled {
		return nil
	}

	// Create client if not already set (for testing)
	if p.client == nil {
		p.client = p.clientFactory(p.config)
	}

	// Connect with context for timeout
	connectCtx, cancel := context.WithTimeout(ctx, time.Duration(p.config.MQTT.ConnectionTimeout)*time.Second)
	defer cancel()

	connToken := p.client.Connect()

	// Wait for connection or context timeout
	select {
	case <-connectCtx.Done():
		p.setConnected(false)
		return fmt.Errorf("failed to connect to MQTT broker: timeout after %d seconds", p.config.MQTT.ConnectionTimeout)
	case <-connToken.Done():
		if connToken.Error() != nil {
			p.setConnected(false)
			return fmt.Errorf("failed to connect to MQTT broker: %w", connToken.Error())
		}
	}

	p.setConnected(true)
	return nil
}

// IsConnected returns the current connection status.
func (p *MQTTPublisher) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connected && p.client != nil && p.client.IsConnected()
}

// setConnected sets the connection status in a thread-safe manner.
func (p *MQTTPublisher) setConnected(status bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = status
}

// Publish sends data to the specified topic.
func (p *MQTTPublisher) Publish(ctx context.Context, topic string, data interface{}) error {
	if !p.config.MQTT.Enabled {
		return nil
	}

	// Check if connected before attempting publish
	// If not connected, silently skip (reconnection loop will restore connection)
	if !p.IsConnected() {
		return nil
	}

	// Convert data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data to JSON: %w", err)
	}

	// Publish with context for timeout
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	token := p.client.Publish(topic, 0, p.config.MQTT.Retain, jsonData)

	// Wait for publication or context timeout
	select {
	case <-publishCtx.Done():
		// Mark as disconnected on timeout so reconnection loop can restore
		p.setConnected(false)
		return fmt.Errorf("publish timeout after 5 seconds")
	case <-token.Done():
		if token.Error() != nil {
			// Mark as disconnected on error so reconnection loop can restore
			p.setConnected(false)
			return fmt.Errorf("failed to publish message: %w", token.Error())
		}
	}

	return nil
}

// PublishInverterData publishes inverter data to the configured MQTT topic.
func (p *MQTTPublisher) PublishInverterData(ctx context.Context, data *domain.InverterData) error {
	if !p.config.MQTT.Enabled {
		return nil
	}

	// Determine topic based on configuration
	baseTopic := p.config.MQTT.Topic

	// Add inverter ID to topic if configured
	if p.config.MQTT.IncludeInverterID && data.PVSerial != "" {
		baseTopic = fmt.Sprintf("%s/%s", baseTopic, data.PVSerial)
	}

	return p.Publish(ctx, baseTopic, data)
}

// StartReconnectionLoop starts a background goroutine that continuously monitors
// and attempts to restore MQTT connection if it's lost. This ensures the system
// can heal automatically from any MQTT broker outages, no matter how long.
func (p *MQTTPublisher) StartReconnectionLoop(ctx context.Context) {
	if !p.config.MQTT.Enabled {
		return
	}

	go func() {
		// Check every 10 seconds
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		logger := log.With().Str("component", "mqtt-reconnect").Logger()
		logger.Info().Msg("MQTT reconnection loop started")

		for {
			select {
			case <-ctx.Done():
				logger.Info().Msg("MQTT reconnection loop stopped (context cancelled)")
				return
			case <-p.reconnectDone:
				logger.Info().Msg("MQTT reconnection loop stopped (close requested)")
				return
			case <-ticker.C:
				// Check if we're not connected
				if !p.IsConnected() {
					logger.Info().
						Str("broker", fmt.Sprintf("%s:%d", p.config.MQTT.Host, p.config.MQTT.Port)).
						Msg("MQTT disconnected, attempting reconnection")

					// Attempt reconnection
					if err := p.Connect(ctx); err != nil {
						logger.Warn().
							Err(err).
							Msg("MQTT reconnection attempt failed, will retry")
					} else {
						logger.Info().Msg("MQTT reconnection successful")
					}
				}
			}
		}
	}()
}

// Close terminates the connection to the MQTT broker and stops the reconnection loop.
func (p *MQTTPublisher) Close() error {
	// Signal reconnection loop to stop
	close(p.reconnectDone)

	// Disconnect from MQTT broker
	if p.client != nil && p.IsConnected() {
		p.client.Disconnect(250) // Disconnect with 250ms timeout
		p.setConnected(false)
	}
	return nil
}
