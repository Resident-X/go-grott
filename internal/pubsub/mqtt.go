// Package pubsub provides implementations of message publishers.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
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
}

// NewMQTTPublisher creates a new MQTT publisher.
func NewMQTTPublisher(cfg *config.Config) *MQTTPublisher {
	return &MQTTPublisher{
		config:        cfg,
		connected:     false,
		clientFactory: createMQTTClient,
	}
}

// NewMQTTPublisherWithClient creates a new MQTT publisher with a custom client (for testing).
func NewMQTTPublisherWithClient(cfg *config.Config, client mqtt.Client) *MQTTPublisher {
	return &MQTTPublisher{
		config:    cfg,
		client:    client,
		connected: false,
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
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	connToken := p.client.Connect()

	// Wait for connection or context timeout
	select {
	case <-connectCtx.Done():
		return fmt.Errorf("failed to connect to MQTT broker: timeout after 10 seconds")
	case <-connToken.Done():
		if connToken.Error() != nil {
			return fmt.Errorf("failed to connect to MQTT broker: %w", connToken.Error())
		}
	}

	p.connected = true
	return nil
}

// Publish sends data to the specified topic.
func (p *MQTTPublisher) Publish(ctx context.Context, topic string, data interface{}) error {
	if !p.config.MQTT.Enabled || !p.connected {
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
		return fmt.Errorf("publish timeout after 5 seconds")
	case <-token.Done():
		if token.Error() != nil {
			return fmt.Errorf("failed to publish message: %w", token.Error())
		}
	}

	return nil
}

// PublishInverterData publishes inverter data to the configured MQTT topic.
func (p *MQTTPublisher) PublishInverterData(ctx context.Context, data *domain.InverterData) error {
	if !p.config.MQTT.Enabled || !p.connected {
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

// Close terminates the connection to the MQTT broker.
func (p *MQTTPublisher) Close() error {
	if p.client != nil && p.connected {
		p.client.Disconnect(250) // Disconnect with 250ms timeout
		p.connected = false
	}
	return nil
}
