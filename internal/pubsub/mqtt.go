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
	"github.com/resident-x/go-grott/internal/homeassistant"
	"github.com/rs/zerolog"
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
	config            *config.Config
	client            mqtt.Client
	connected         bool
	clientFactory     func(*config.Config) mqtt.Client
	haDiscovery       *homeassistant.AutoDiscovery
	discoveredSensors map[string]bool // Track which sensors have been discovered
	logger            zerolog.Logger
}

// NewMQTTPublisher creates a new MQTT publisher.
func NewMQTTPublisher(cfg *config.Config) *MQTTPublisher {
	logger := log.With().Str("component", "mqtt").Logger()
	return &MQTTPublisher{
		config:            cfg,
		clientFactory:     createMQTTClient,
		discoveredSensors: make(map[string]bool),
		logger:            logger,
	}
}

// NewMQTTPublisherWithClient creates a new MQTT publisher with a custom client (for testing).
func NewMQTTPublisherWithClient(cfg *config.Config, client mqtt.Client) *MQTTPublisher {
	logger := log.With().Str("component", "mqtt").Logger()
	return &MQTTPublisher{
		config:            cfg,
		client:            client,
		connected:         false,
		discoveredSensors: make(map[string]bool),
		logger:            logger,
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

	// Check if this is InverterData and handle Home Assistant auto-discovery
	if inverterData, ok := data.(*domain.InverterData); ok {
		return p.publishInverterDataWithDiscovery(ctx, topic, inverterData)
	}

	// For non-InverterData, use simple JSON publish
	return p.publishGeneric(ctx, topic, data)
}

// publishGeneric handles simple JSON publishing for non-InverterData.
func (p *MQTTPublisher) publishGeneric(ctx context.Context, topic string, data interface{}) error {
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

// publishInverterDataWithDiscovery handles InverterData with Home Assistant auto-discovery.
func (p *MQTTPublisher) publishInverterDataWithDiscovery(ctx context.Context, topic string, data *domain.InverterData) error {
	// Generate device ID consistently for both raw and auto-discovery
	deviceID := data.PVSerial
	if deviceID == "" {
		deviceID = "unknown_inverter"
	}

	// Set the device field in the data for raw MQTT output
	data.Device = deviceID

	// Debug: Log what we're about to publish
	p.logger.Debug().
		Str("device_id", deviceID).
		Str("data_device", data.Device).
		Str("pv_serial", data.PVSerial).
		Msg("Publishing inverter data with Home Assistant support")

	if p.haDiscovery == nil && p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		if err := p.setupHomeAssistantDiscovery(deviceID); err != nil {
			return fmt.Errorf("failed to setup Home Assistant discovery: %w", err)
		}
	}

	// Determine topic based on configuration (override the provided topic for InverterData)
	baseTopic := p.config.MQTT.Topic

	// Add inverter ID to topic if configured
	if p.config.MQTT.IncludeInverterID && data.PVSerial != "" {
		baseTopic = fmt.Sprintf("%s/%s", baseTopic, data.PVSerial)
	}

	// Convert data to map for Home Assistant processing
	dataMap := make(map[string]interface{})
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data for processing: %w", err)
	}
	if err := json.Unmarshal(dataJSON, &dataMap); err != nil {
		return fmt.Errorf("failed to unmarshal data for processing: %w", err)
	}

	// Publish Home Assistant auto-discovery messages if enabled
	if p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		if err := p.publishHomeAssistantDiscovery(ctx, dataMap); err != nil {
			return fmt.Errorf("failed to publish Home Assistant discovery: %w", err)
		}
	}

	// Publish raw data if enabled
	if p.config.MQTT.PublishRaw {
		// Debug: Check the data right before publishing
		if debugJSON, err := json.Marshal(data); err == nil {
			p.logger.Debug().
				Str("topic", baseTopic).
				RawJSON("raw_data", debugJSON).
				Msg("Publishing raw MQTT data")
		}

		if err := p.publishGeneric(ctx, baseTopic, data); err != nil {
			return fmt.Errorf("failed to publish raw data: %w", err)
		}
	}

	return nil
}

// setupHomeAssistantDiscovery initializes Home Assistant auto-discovery if enabled.
func (p *MQTTPublisher) setupHomeAssistantDiscovery(deviceID string) error {
	if !p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		return nil
	}

	haConfig := homeassistant.Config{
		Enabled:             p.config.MQTT.HomeAssistantAutoDiscovery.Enabled,
		DiscoveryPrefix:     p.config.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix,
		DeviceName:          p.config.MQTT.HomeAssistantAutoDiscovery.DeviceName,
		DeviceManufacturer:  p.config.MQTT.HomeAssistantAutoDiscovery.DeviceManufacturer,
		DeviceModel:         p.config.MQTT.HomeAssistantAutoDiscovery.DeviceModel,
		RetainDiscovery:     p.config.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery,
		IncludeDiagnostic:   p.config.MQTT.HomeAssistantAutoDiscovery.IncludeDiagnostic,
		IncludeBattery:      p.config.MQTT.HomeAssistantAutoDiscovery.IncludeBattery,
		IncludeGrid:         p.config.MQTT.HomeAssistantAutoDiscovery.IncludeGrid,
		IncludePV:           p.config.MQTT.HomeAssistantAutoDiscovery.IncludePV,
		ValueTemplateSuffix: p.config.MQTT.HomeAssistantAutoDiscovery.ValueTemplateSuffix,
	}

	baseTopic := p.config.MQTT.Topic
	if p.config.MQTT.IncludeInverterID && deviceID != "" {
		baseTopic = fmt.Sprintf("%s/%s", baseTopic, deviceID)
	}

	var err error
	p.haDiscovery, err = homeassistant.New(haConfig, baseTopic, deviceID)
	return err
}

// publishHomeAssistantDiscovery publishes Home Assistant auto-discovery messages.
func (p *MQTTPublisher) publishHomeAssistantDiscovery(ctx context.Context, data map[string]interface{}) error {
	if p.haDiscovery == nil || !p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		return nil
	}

	// Generate discovery messages for all sensors
	discoveryMessages := p.haDiscovery.GenerateDiscoveryMessages(data)

	// Publish each discovery message
	for topic, message := range discoveryMessages {
		// Only publish if we haven't already discovered this sensor
		if !p.discoveredSensors[topic] {
			messageJSON, err := json.Marshal(message)
			if err != nil {
				return fmt.Errorf("failed to marshal discovery message: %w", err)
			}

			token := p.client.Publish(topic, 0, p.config.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery, messageJSON)
			if token.Wait() && token.Error() != nil {
				return fmt.Errorf("failed to publish discovery message to %s: %w", topic, token.Error())
			}

			p.discoveredSensors[topic] = true
		}
	}

	// Publish availability message
	availTopic := p.haDiscovery.GetAvailabilityTopic()
	availMessage := p.haDiscovery.CreateAvailabilityMessage(true)
	token := p.client.Publish(availTopic, 0, p.config.MQTT.Retain, availMessage)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish availability message: %w", token.Error())
	}

	return nil
}

// Close terminates the connection to the MQTT broker.
func (p *MQTTPublisher) Close() error {
	if p.client != nil && p.connected {
		p.client.Disconnect(250) // Disconnect with 250ms timeout
		p.connected = false
	}
	return nil
}
