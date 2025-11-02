// Package pubsub provides implementations of message publishers.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	logger            zerolog.Logger
	clientFactory     func(*config.Config) mqtt.Client // Factory function for creating MQTT clients (testable)
	haDiscovery       *homeassistant.AutoDiscovery
	discoveredSensors map[string]bool // Track which sensors have been discovered
	lastDiscoveryTime time.Time       // Track when we last sent discovery messages
	birthSubscribed   bool            // Track if we've subscribed to birth messages

	// Startup data filtering fields
	startupTime      time.Time // When the publisher was created
	lastDataTime     time.Time // When we last received data
	inGracePeriod    bool      // Whether we're currently in a grace period
	gracePeriodStart time.Time // When the current grace period started
}

// NewMQTTPublisher creates a new MQTT publisher.
func NewMQTTPublisher(cfg *config.Config) *MQTTPublisher {
	logger := log.With().Str("component", "mqtt").Logger()
	now := time.Now()
	return &MQTTPublisher{
		config:            cfg,
		clientFactory:     createMQTTClient,
		discoveredSensors: make(map[string]bool),
		lastDiscoveryTime: time.Time{},
		birthSubscribed:   false,
		connected:         false,
		logger:            logger,
		// Initialize startup data filtering
		startupTime:      now,
		lastDataTime:     now,
		inGracePeriod:    true,
		gracePeriodStart: now,
	}
}

// NewMQTTPublisherWithClient creates a new MQTT publisher with a custom client (for testing).
func NewMQTTPublisherWithClient(cfg *config.Config, client mqtt.Client) *MQTTPublisher {
	logger := log.With().Str("component", "mqtt").Logger()
	now := time.Now()
	return &MQTTPublisher{
		config:            cfg,
		client:            client,
		connected:         false,
		discoveredSensors: make(map[string]bool),
		lastDiscoveryTime: time.Time{},
		birthSubscribed:   false,
		logger:            logger,
		// Initialize startup data filtering
		startupTime:      now,
		lastDataTime:     now,
		inGracePeriod:    true,
		gracePeriodStart: now,
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

	// Set up connection handlers
	p.setupConnectionHandlers()

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

	// Subscribe to birth message if enabled
	if p.config.MQTT.HomeAssistantAutoDiscovery.Enabled && p.config.MQTT.HomeAssistantAutoDiscovery.ListenToBirthMessage {
		p.subscribeToBirthMessage()
	}

	return nil
}

// setupConnectionHandlers sets up MQTT connection event handlers.
func (p *MQTTPublisher) setupConnectionHandlers() {
	// Skip setup if we already have a client (e.g., in tests with mocks)
	if p.client != nil {
		return
	}

	// Connection established handler - called when connection is made/remade
	onConnect := func(client mqtt.Client) {
		p.logger.Info().Msg("MQTT connection established")
		p.connected = true

		// Clear discovered sensors on reconnect to trigger re-discovery
		p.discoveredSensors = make(map[string]bool)
		p.lastDiscoveryTime = time.Time{}

		p.logger.Debug().Msg("Cleared discovery cache on reconnection")
	}

	// Connection lost handler - called when connection is lost
	onConnectionLost := func(client mqtt.Client, err error) {
		p.connected = false
		p.birthSubscribed = false
		p.logger.Warn().Err(err).Msg("MQTT connection lost")
	}

	// Set the handlers on the client options
	// We need to recreate the client with handlers since we can't modify existing options
	newOpts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", p.config.MQTT.Host, p.config.MQTT.Port)).
		SetClientID(fmt.Sprintf("go-grott-%d", time.Now().Unix())).
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetWriteTimeout(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetCleanSession(false).
		SetOnConnectHandler(onConnect).
		SetConnectionLostHandler(onConnectionLost)

	// Set credentials if provided
	if p.config.MQTT.Username != "" {
		newOpts.SetUsername(p.config.MQTT.Username)
		newOpts.SetPassword(p.config.MQTT.Password)
	}

	p.client = mqtt.NewClient(newOpts)
}

// subscribeToBirthMessage subscribes to Home Assistant birth messages.
func (p *MQTTPublisher) subscribeToBirthMessage() {
	if p.birthSubscribed || !p.connected {
		return
	}

	birthTopic := fmt.Sprintf("%s/status", p.config.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix)

	token := p.client.Subscribe(birthTopic, 0, p.handleBirthMessage)
	if token.Wait() && token.Error() != nil {
		p.logger.Warn().Err(token.Error()).Str("topic", birthTopic).Msg("Failed to subscribe to birth message")
		return
	}

	p.birthSubscribed = true
	p.logger.Info().Str("topic", birthTopic).Msg("Subscribed to Home Assistant birth messages")
}

// handleBirthMessage handles Home Assistant birth messages.
func (p *MQTTPublisher) handleBirthMessage(client mqtt.Client, msg mqtt.Message) {
	payload := string(msg.Payload())

	p.logger.Debug().
		Str("topic", msg.Topic()).
		Str("payload", payload).
		Msg("Received Home Assistant birth message")

	// If Home Assistant comes online, clear discovery cache to trigger re-discovery
	if payload == "online" {
		p.logger.Info().Msg("Home Assistant came online, triggering auto-discovery refresh")
		p.discoveredSensors = make(map[string]bool)
		p.lastDiscoveryTime = time.Time{}
	}
}

// shouldRediscover checks if we should perform periodic rediscovery.
func (p *MQTTPublisher) shouldRediscover() bool {
	if p.config.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval <= 0 {
		return false
	}

	if p.lastDiscoveryTime.IsZero() {
		return true
	}

	interval := time.Duration(p.config.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval) * time.Hour
	return time.Since(p.lastDiscoveryTime) >= interval
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

// shouldFilterStartupData checks if data should be filtered due to startup data filtering rules.
func (p *MQTTPublisher) shouldFilterStartupData() bool {
	if !p.config.MQTT.StartupDataFilter.Enabled {
		return false
	}

	now := time.Now()

	// Update last data time
	timeSinceLastData := now.Sub(p.lastDataTime)
	p.lastDataTime = now

	// Check if we should start a new grace period due to data gap
	dataGapThreshold := time.Duration(p.config.MQTT.StartupDataFilter.DataGapThresholdHours) * time.Hour
	if timeSinceLastData > dataGapThreshold {
		p.logger.Info().
			Dur("gap_duration", timeSinceLastData).
			Dur("threshold", dataGapThreshold).
			Msg("Data gap detected, starting grace period")
		p.inGracePeriod = true
		p.gracePeriodStart = now
	}

	// Check if we're still in grace period
	if p.inGracePeriod {
		gracePeriodDuration := time.Duration(p.config.MQTT.StartupDataFilter.GracePeriodSeconds) * time.Second
		if now.Sub(p.gracePeriodStart) >= gracePeriodDuration {
			p.logger.Info().
				Dur("grace_period", gracePeriodDuration).
				Msg("Grace period ended, resuming data publishing")
			p.inGracePeriod = false
		}
	}

	// Return true if we should filter (still in grace period)
	if p.inGracePeriod {
		p.logger.Debug().
			Dur("remaining", time.Duration(p.config.MQTT.StartupDataFilter.GracePeriodSeconds)*time.Second-now.Sub(p.gracePeriodStart)).
			Msg("Filtering startup data - still in grace period")
		return true
	}

	return false
}

// publishInverterDataWithDiscovery handles InverterData with Home Assistant auto-discovery.
func (p *MQTTPublisher) publishInverterDataWithDiscovery(ctx context.Context, topic string, data *domain.InverterData) error {
	// Require PVSerial to be present; skip message if missing
	if data.PVSerial == "" {
		p.logger.Debug().Msg("Skipping publish: PVSerial is empty")
		return nil
	}

	// Check startup data filtering
	if p.shouldFilterStartupData() {
		return nil // Skip publishing during grace period
	}

	// Ensure DeviceType is set with a sensible default
	if data.DeviceType == "" {
		data.DeviceType = domain.DeviceTypeInverter
		p.logger.Debug().Msg("DeviceType was empty, defaulting to inverter")
	}

	// Generate device ID from PVSerial and device type to avoid conflicts
	deviceID := fmt.Sprintf("%s_%s", data.PVSerial, data.DeviceType)

	// Set the device field in the data for raw MQTT output
	data.Device = deviceID

	// Debug: Log what we're about to publish
	p.logger.Debug().
		Str("device_id", deviceID).
		Str("data_device", data.Device).
		Str("pv_serial", data.PVSerial).
		Str("device_type", data.DeviceType).
		Msg("Publishing inverter data with Home Assistant support")

	if p.haDiscovery == nil && p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		if err := p.setupHomeAssistantDiscovery(deviceID); err != nil {
			return fmt.Errorf("failed to setup Home Assistant discovery: %w", err)
		}
	}

	// Determine topic based on configuration (override the provided topic for InverterData)
	baseTopic := p.config.MQTT.Topic

	// Add inverter ID to topic if configured (keep original format for backward compatibility)
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

	// Flatten ExtendedData to root level (Python grott compatibility)
	if data.ExtendedData != nil {
		for key, value := range data.ExtendedData {
			// Skip validation metadata to keep MQTT messages clean
			if key != "validation_confidence" && key != "validation_errors" && key != "validation_warnings" {
				dataMap[key] = value
			}
		}
	}

	// Apply calculations to convert raw values to proper units for Home Assistant
	if p.haDiscovery != nil {
		dataMap = p.haDiscovery.ApplyCalculations(dataMap)
	}

	// Publish Home Assistant auto-discovery messages if enabled
	if p.config.MQTT.HomeAssistantAutoDiscovery.Enabled {
		if err := p.publishHomeAssistantDiscovery(ctx, dataMap); err != nil {
			return fmt.Errorf("failed to publish Home Assistant discovery: %w", err)
		}
	}

	// Publish converted data if enabled
	if p.config.MQTT.PublishRaw {
		// Debug: Check the converted data before publishing
		if debugJSON, err := json.Marshal(dataMap); err == nil {
			p.logger.Debug().
				Str("topic", baseTopic).
				RawJSON("converted_data", debugJSON).
				Msg("Publishing calculated MQTT data")
		}

		if err := p.publishGeneric(ctx, baseTopic, dataMap); err != nil {
			return fmt.Errorf("failed to publish converted data: %w", err)
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
		ValueTemplateSuffix: p.config.MQTT.HomeAssistantAutoDiscovery.ValueTemplateSuffix,
	}

	// Use the same baseTopic format as the actual MQTT messages for consistency
	baseTopic := p.config.MQTT.Topic
	// For auto-discovery baseTopic, we need to extract the PVSerial from the deviceID
	// deviceID format is {pvSerial}_{deviceType}, so extract just the PVSerial part
	pvSerial := deviceID
	if idx := strings.LastIndex(deviceID, "_"); idx != -1 {
		pvSerial = deviceID[:idx]
	}

	if p.config.MQTT.IncludeInverterID && pvSerial != "" {
		baseTopic = fmt.Sprintf("%s/%s", baseTopic, pvSerial)
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

	// Check if we should rediscover sensors (periodic or after reconnection)
	shouldRediscover := p.shouldRediscover()

	// Generate discovery messages for all sensors
	discoveryMessages := p.haDiscovery.GenerateDiscoveryMessages(data)

	// Publish each discovery message
	for topic, message := range discoveryMessages {
		// Publish if we haven't discovered this sensor or if rediscovery is needed
		if !p.discoveredSensors[topic] || shouldRediscover {
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

	// Update last discovery time if we performed rediscovery
	if shouldRediscover {
		p.lastDiscoveryTime = time.Now()
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
