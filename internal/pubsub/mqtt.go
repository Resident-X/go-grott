// Package pubsub provides implementations of message publishers.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	config        *config.Config
	client        mqtt.Client
	logger        zerolog.Logger
	clientFactory func(*config.Config) mqtt.Client // Factory function for creating MQTT clients (testable)
	haDiscovery   *homeassistant.AutoDiscovery

	// Fields below are protected by mu for concurrent access
	mu                sync.RWMutex
	connected         bool
	discoveredSensors map[string]bool // Track which sensors have been discovered
	lastDiscoveryTime time.Time       // Track when we last sent discovery messages
	birthSubscribed   bool            // Track if we've subscribed to birth messages

	// Startup data filtering fields
	startupTime      time.Time // When the publisher was created
	lastDataTime     time.Time // When we last received data
	inGracePeriod    bool      // Whether we're currently in a grace period
	gracePeriodStart time.Time // When the current grace period started

	// Background reconnection fields
	stopReconnect chan struct{} // Channel to signal background reconnect to stop
	reconnecting  bool          // Whether background reconnection is active
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
		// Initialize background reconnection
		stopReconnect: make(chan struct{}),
		reconnecting:  false,
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
		// Initialize background reconnection
		stopReconnect: make(chan struct{}),
		reconnecting:  false,
	}
}

// createMQTTClient is the default factory function for creating MQTT clients.
func createMQTTClient(cfg *config.Config) mqtt.Client {
	// Note: Connection handlers are NOT set here - they're set in Connect()
	// to ensure the publisher instance is fully initialized before handlers are called
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
// If the initial connection fails, it starts a background retry process
// and returns nil to allow the application to start without MQTT.
func (p *MQTTPublisher) Connect(ctx context.Context) error {
	// If MQTT is disabled, do nothing
	if !p.config.MQTT.Enabled {
		return nil
	}

	// Create client with connection handlers if not already set (for testing)
	if p.client == nil {
		p.client = p.createClientWithHandlers()
	}

	// Try initial connection with timeout
	p.logger.Info().
		Str("host", p.config.MQTT.Host).
		Int("port", p.config.MQTT.Port).
		Msg("Attempting initial MQTT connection")

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	connToken := p.client.Connect()

	// Wait for connection or context timeout
	select {
	case <-connectCtx.Done():
		p.logger.Warn().
			Str("host", p.config.MQTT.Host).
			Int("port", p.config.MQTT.Port).
			Msg("Initial MQTT connection failed (timeout), will retry in background")
		go p.startBackgroundReconnect()
		return nil // Non-blocking: return success to allow app to start
	case <-connToken.Done():
		if connToken.Error() != nil {
			p.logger.Warn().
				Err(connToken.Error()).
				Str("host", p.config.MQTT.Host).
				Int("port", p.config.MQTT.Port).
				Msg("Initial MQTT connection failed, will retry in background")
			go p.startBackgroundReconnect()
			return nil // Non-blocking: return success to allow app to start
		}
	}

	// Connection successful
	p.mu.Lock()
	p.connected = true
	p.mu.Unlock()

	p.logger.Info().Msg("Initial MQTT connection successful")

	// Subscribe to birth message if enabled
	if p.config.MQTT.HomeAssistantAutoDiscovery.Enabled && p.config.MQTT.HomeAssistantAutoDiscovery.ListenToBirthMessage {
		p.subscribeToBirthMessage()
	}

	return nil
}

// startBackgroundReconnect attempts to reconnect to MQTT broker in the background
// using exponential backoff. This allows the application to continue running
// even if MQTT is unavailable at startup.
func (p *MQTTPublisher) startBackgroundReconnect() {
	p.mu.Lock()
	if p.reconnecting {
		p.mu.Unlock()
		return // Already reconnecting, don't start another goroutine
	}
	p.reconnecting = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.reconnecting = false
		p.mu.Unlock()
	}()

	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second
	attempt := 0

	for {
		// Check if we should stop (connection established or Close() called)
		select {
		case <-p.stopReconnect:
			p.logger.Debug().Msg("Background MQTT reconnection stopped")
			return
		default:
		}

		// Check if already connected (via auto-reconnect)
		p.mu.RLock()
		isConnected := p.connected
		p.mu.RUnlock()

		if isConnected {
			p.logger.Info().Msg("MQTT connection established via background reconnect")
			return
		}

		// Wait before retry
		attempt++
		p.logger.Info().
			Int("attempt", attempt).
			Dur("backoff", backoff).
			Str("host", p.config.MQTT.Host).
			Int("port", p.config.MQTT.Port).
			Msg("Retrying MQTT connection")

		select {
		case <-time.After(backoff):
			// Attempt connection
			if p.client == nil {
				p.client = p.createClientWithHandlers()
			}

			connToken := p.client.Connect()
			if connToken.Wait() && connToken.Error() == nil {
				// Connection successful - onConnect handler will handle the rest
				p.logger.Info().
					Int("attempt", attempt).
					Msg("MQTT connection established after retry")
				return
			}

			// Connection failed, increase backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		case <-p.stopReconnect:
			p.logger.Debug().Msg("Background MQTT reconnection stopped during backoff")
			return
		}
	}
}

// createClientWithHandlers creates an MQTT client with connection event handlers configured.
func (p *MQTTPublisher) createClientWithHandlers() mqtt.Client {
	// Connection established handler - called when connection is made/remade
	onConnect := func(client mqtt.Client) {
		p.logger.Info().Msg("MQTT connection established")

		p.mu.Lock()
		p.connected = true
		// Clear discovered sensors on reconnect to trigger re-discovery
		p.discoveredSensors = make(map[string]bool)
		p.lastDiscoveryTime = time.Time{}
		// Reset subscription flag so we can re-subscribe
		p.birthSubscribed = false
		p.mu.Unlock()

		p.logger.Debug().Msg("Cleared discovery cache on reconnection")

		// Re-subscribe to birth messages if enabled
		if p.config.MQTT.HomeAssistantAutoDiscovery.Enabled && p.config.MQTT.HomeAssistantAutoDiscovery.ListenToBirthMessage {
			p.subscribeToBirthMessage()
		}
	}

	// Connection lost handler - called when connection is lost
	onConnectionLost := func(client mqtt.Client, err error) {
		p.mu.Lock()
		p.connected = false
		p.birthSubscribed = false
		p.mu.Unlock()

		p.logger.Warn().Err(err).Msg("MQTT connection lost, will auto-reconnect")
	}

	// Create client options with handlers configured
	opts := mqtt.NewClientOptions().
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
		opts.SetUsername(p.config.MQTT.Username)
		opts.SetPassword(p.config.MQTT.Password)
	}

	return mqtt.NewClient(opts)
}

// subscribeToBirthMessage subscribes to Home Assistant birth messages.
func (p *MQTTPublisher) subscribeToBirthMessage() {
	p.mu.RLock()
	alreadySubscribed := p.birthSubscribed
	isConnected := p.connected
	p.mu.RUnlock()

	if alreadySubscribed || !isConnected {
		return
	}

	birthTopic := fmt.Sprintf("%s/status", p.config.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix)

	token := p.client.Subscribe(birthTopic, 0, p.handleBirthMessage)
	if token.Wait() && token.Error() != nil {
		p.logger.Warn().Err(token.Error()).Str("topic", birthTopic).Msg("Failed to subscribe to birth message")
		return
	}

	p.mu.Lock()
	p.birthSubscribed = true
	p.mu.Unlock()

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
		p.mu.Lock()
		p.discoveredSensors = make(map[string]bool)
		p.lastDiscoveryTime = time.Time{}
		p.mu.Unlock()
	}
}

// shouldRediscover checks if we should perform periodic rediscovery.
func (p *MQTTPublisher) shouldRediscover() bool {
	if p.config.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval <= 0 {
		return false
	}

	p.mu.RLock()
	lastDiscovery := p.lastDiscoveryTime
	p.mu.RUnlock()

	if lastDiscovery.IsZero() {
		return true
	}

	interval := time.Duration(p.config.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval) * time.Hour
	return time.Since(lastDiscovery) >= interval
}

// Publish sends data to the specified topic.
func (p *MQTTPublisher) Publish(ctx context.Context, topic string, data interface{}) error {
	p.mu.RLock()
	isConnected := p.connected
	p.mu.RUnlock()

	if !p.config.MQTT.Enabled || !isConnected {
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

	p.mu.Lock()
	defer p.mu.Unlock()

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
		// Check if this sensor needs discovery
		p.mu.RLock()
		alreadyDiscovered := p.discoveredSensors[topic]
		p.mu.RUnlock()

		// Publish if we haven't discovered this sensor or if rediscovery is needed
		if !alreadyDiscovered || shouldRediscover {
			messageJSON, err := json.Marshal(message)
			if err != nil {
				return fmt.Errorf("failed to marshal discovery message: %w", err)
			}

			token := p.client.Publish(topic, 0, p.config.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery, messageJSON)
			if token.Wait() && token.Error() != nil {
				return fmt.Errorf("failed to publish discovery message to %s: %w", topic, token.Error())
			}

			p.mu.Lock()
			p.discoveredSensors[topic] = true
			p.mu.Unlock()
		}
	}

	// Update last discovery time if we performed rediscovery
	if shouldRediscover {
		p.mu.Lock()
		p.lastDiscoveryTime = time.Now()
		p.mu.Unlock()
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

// Close terminates the connection to the MQTT broker and stops background reconnection.
func (p *MQTTPublisher) Close() error {
	// Stop background reconnection if running
	select {
	case <-p.stopReconnect:
		// Channel already closed
	default:
		close(p.stopReconnect)
	}

	p.mu.Lock()
	isConnected := p.connected
	p.mu.Unlock()

	if p.client != nil && isConnected {
		p.client.Disconnect(250) // Disconnect with 250ms timeout

		p.mu.Lock()
		p.connected = false
		p.mu.Unlock()
	}
	return nil
}
