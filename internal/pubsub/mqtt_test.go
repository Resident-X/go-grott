package pubsub

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewNoopPublisher(t *testing.T) {
	publisher := NewNoopPublisher()
	assert.NotNil(t, publisher)
}

func TestNoopPublisher_Connect(t *testing.T) {
	publisher := NewNoopPublisher()
	ctx := context.Background()
	err := publisher.Connect(ctx)
	assert.NoError(t, err)
}

func TestNoopPublisher_Publish(t *testing.T) {
	publisher := NewNoopPublisher()
	ctx := context.Background()

	testData := map[string]interface{}{
		"test": "data",
	}

	err := publisher.Publish(ctx, "test/topic", testData)
	assert.NoError(t, err)
}

func TestNoopPublisher_Close(t *testing.T) {
	publisher := NewNoopPublisher()
	err := publisher.Close()
	assert.NoError(t, err)
}

func TestNewMQTTPublisher(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	assert.NotNil(t, publisher)
	assert.Equal(t, cfg, publisher.config)
	assert.False(t, publisher.connected)
	assert.Nil(t, publisher.client)
}

func TestMQTTPublisher_Connect_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	err := publisher.Connect(ctx)
	assert.NoError(t, err)
	assert.False(t, publisher.connected)
}

func TestMQTTPublisher_Connect_InvalidBroker(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "invalid-broker-that-does-not-exist"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	defer publisher.Close() // Clean up background retry goroutine

	// Connect should not return error even with invalid broker (non-blocking)
	err := publisher.Connect(ctx)
	assert.NoError(t, err)

	// Connection should not be established yet
	publisher.mu.RLock()
	connected := publisher.connected
	publisher.mu.RUnlock()

	assert.False(t, connected, "should not be connected")
}

func TestMQTTPublisher_Connect_WithCredentials(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "invalid-broker-that-does-not-exist"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Username = "testuser"
	cfg.MQTT.Password = "testpass"
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	defer publisher.Close() // Clean up background retry goroutine

	// Connect should not return error even with invalid broker (non-blocking)
	err := publisher.Connect(ctx)
	assert.NoError(t, err)

	// Connection should not be established yet
	publisher.mu.RLock()
	connected := publisher.connected
	publisher.mu.RUnlock()

	assert.False(t, connected, "should not be connected")
}

func TestMQTTPublisher_Publish_NotConnected(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		Timestamp:        time.Now(),
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error when not connected but MQTT enabled (just returns nil)
	err := publisher.Publish(ctx, "test/topic", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Publish_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := map[string]string{"test": "data"}

	// Should not error when MQTT disabled
	err := publisher.Publish(ctx, "test/topic", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Publish_InvalidData(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	publisher.connected = true // Set as connected for this test
	ctx := context.Background()

	// Test with data that cannot be JSON marshaled
	invalidData := make(chan int)

	err := publisher.Publish(ctx, "test/topic", invalidData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

func TestMQTTPublisher_PublishInverterData_NotConnected(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		Timestamp:        time.Now(),
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error when not connected but MQTT enabled (just returns nil)
	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_PublishInverterData_TopicGeneration(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false // Disable to avoid actual connection
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		Timestamp:        time.Now(),
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error when MQTT disabled
	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_PublishInverterData_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error when MQTT disabled
	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_PublishInverterData_WithoutInverterID(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false // Disable to avoid connection
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = false

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error when MQTT disabled, topic should not include inverter ID
	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_PublishInverterData_EmptySerial(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false // Disable to avoid connection
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "", // Empty serial
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	// Should not error, topic should use base topic (no serial to append)
	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Connect_Timeout(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "192.0.2.1" // Non-routable IP to cause timeout
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	publisher := NewMQTTPublisher(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	defer publisher.Close() // Clean up background retry goroutine

	// Connect should not return error even with timeout (non-blocking)
	err := publisher.Connect(ctx)
	assert.NoError(t, err)

	// Connection should not be established yet
	publisher.mu.RLock()
	connected := publisher.connected
	publisher.mu.RUnlock()

	assert.False(t, connected, "should not be connected after timeout")
}

func TestMQTTPublisher_Close_NotConnected(t *testing.T) {
	cfg := &config.Config{}
	publisher := NewMQTTPublisher(cfg)

	err := publisher.Close()
	assert.NoError(t, err)
}

func TestMQTTPublisher_Close_Connected(t *testing.T) {
	cfg := &config.Config{}
	publisher := NewMQTTPublisher(cfg)

	// Simulate connected state
	publisher.connected = true

	err := publisher.Close()
	assert.NoError(t, err)
	// Don't assert connected state as it might vary based on implementation
}

func TestMQTTPublisher_Connect_Successful(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "test/topic"

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations
	mockClient.EXPECT().Connect().Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	ctx := context.Background()

	err := publisher.Connect(ctx)
	assert.NoError(t, err)
	assert.True(t, publisher.connected)
}

func TestMQTTPublisher_Connect_Error(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations for connection error
	mockClient.EXPECT().Connect().Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(assert.AnError)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	ctx := context.Background()
	defer publisher.Close() // Clean up background retry goroutine

	// Connect should not return error even with connection failure (non-blocking)
	err := publisher.Connect(ctx)
	assert.NoError(t, err)

	// Connection should not be established
	publisher.mu.RLock()
	connected := publisher.connected
	publisher.mu.RUnlock()

	assert.False(t, connected, "should not be connected after error")
}

func TestMQTTPublisher_Publish_Successful(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Retain = true

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations for successful publish
	expectedTopic := "test/topic"
	expectedQoS := byte(0)
	expectedRetain := true
	expectedPayload := []byte(`{"test":"data"}`)

	mockClient.EXPECT().Publish(expectedTopic, expectedQoS, expectedRetain, mock.MatchedBy(func(payload []byte) bool {
		return bytes.Equal(payload, expectedPayload)
	})).Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true // Simulate connected state

	ctx := context.Background()
	testData := map[string]string{"test": "data"}

	err := publisher.Publish(ctx, expectedTopic, testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Publish_Error(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations for publish error
	mockClient.EXPECT().Publish(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(assert.AnError)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	ctx := context.Background()
	testData := map[string]string{"test": "data"}

	err := publisher.Publish(ctx, "test/topic", testData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish message")
}

func TestMQTTPublisher_Publish_Timeout(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations for publish timeout
	mockClient.EXPECT().Publish(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	// Create a done channel that never closes to simulate timeout
	doneChan := make(chan struct{})
	mockToken.EXPECT().Done().Return(doneChan)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	// Use a very short timeout to trigger timeout quickly
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	testData := map[string]string{"test": "data"}

	err := publisher.Publish(ctx, "test/topic", testData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestMQTTPublisher_PublishInverterData_Successful(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true
	cfg.MQTT.PublishRaw = true // Explicitly enable raw data publishing

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations
	expectedTopic := "energy/growatt/INV001"
	mockClient.EXPECT().Publish(expectedTopic, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	ctx := context.Background()
	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Close_WithClient(t *testing.T) {
	cfg := &config.Config{}
	mockClient := mocks.NewMockClient(t)

	// Setup expectations
	mockClient.EXPECT().Disconnect(uint(250)).Return()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	err := publisher.Close()
	assert.NoError(t, err)
	assert.False(t, publisher.connected)
}

// TestMQTTPublisher_StartupDataFiltering tests the startup data filtering functionality
func TestMQTTPublisher_StartupDataFiltering(t *testing.T) {
	tests := []struct {
		name                  string
		filterEnabled         bool
		gracePeriodSeconds    int
		dataGapThresholdHours int
		timeSinceStartup      time.Duration
		timeSinceLastData     time.Duration
		expectedFiltered      bool
		description           string
	}{
		{
			name:                  "filtering disabled",
			filterEnabled:         false,
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			timeSinceStartup:      5 * time.Second,
			timeSinceLastData:     0,
			expectedFiltered:      false,
			description:           "should not filter when disabled",
		},
		{
			name:                  "within grace period after startup",
			filterEnabled:         true,
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			timeSinceStartup:      5 * time.Second,
			timeSinceLastData:     0,
			expectedFiltered:      true,
			description:           "should filter data within grace period",
		},
		{
			name:                  "after grace period",
			filterEnabled:         true,
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			timeSinceStartup:      15 * time.Second,
			timeSinceLastData:     0,
			expectedFiltered:      false,
			description:           "should not filter data after grace period ends",
		},
		{
			name:                  "data gap triggers new grace period",
			filterEnabled:         true,
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			timeSinceStartup:      30 * time.Second,
			timeSinceLastData:     2 * time.Hour,
			expectedFiltered:      true,
			description:           "should start new grace period after data gap",
		},
		{
			name:                  "small data gap no filtering",
			filterEnabled:         true,
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			timeSinceStartup:      30 * time.Second,
			timeSinceLastData:     30 * time.Minute,
			expectedFiltered:      false,
			description:           "should not filter for small data gaps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create config with test parameters
			cfg := &config.Config{}
			cfg.MQTT.StartupDataFilter.Enabled = tt.filterEnabled
			cfg.MQTT.StartupDataFilter.GracePeriodSeconds = tt.gracePeriodSeconds
			cfg.MQTT.StartupDataFilter.DataGapThresholdHours = tt.dataGapThresholdHours

			// Create mock MQTT client
			mockClient := &mocks.MockClient{}
			mockClient.On("IsConnected").Return(true)

			// Create publisher
			publisher := NewMQTTPublisherWithClient(cfg, mockClient)

			// Simulate time passing since startup
			now := time.Now()
			publisher.startupTime = now.Add(-tt.timeSinceStartup)
			publisher.gracePeriodStart = publisher.startupTime
			publisher.lastDataTime = now.Add(-tt.timeSinceLastData)
			publisher.inGracePeriod = tt.timeSinceStartup < time.Duration(tt.gracePeriodSeconds)*time.Second

			// Test the filtering logic
			shouldFilter := publisher.shouldFilterStartupData()

			assert.Equal(t, tt.expectedFiltered, shouldFilter, tt.description)
		})
	}
}

// TestMQTTPublisher_StartupDataFilteringIntegration tests the integration with publishInverterDataWithDiscovery
func TestMQTTPublisher_StartupDataFilteringIntegration(t *testing.T) {
	// Create config with filtering enabled
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.PublishRaw = true // Enable raw data publishing
	cfg.MQTT.StartupDataFilter.Enabled = true
	cfg.MQTT.StartupDataFilter.GracePeriodSeconds = 10
	cfg.MQTT.StartupDataFilter.DataGapThresholdHours = 1
	cfg.MQTT.HomeAssistantAutoDiscovery.Enabled = false // Simplify test

	// Create mock MQTT client
	mockClient := &mocks.MockClient{}
	mockClient.On("IsConnected").Return(true)

	// Create publisher
	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	// Create test inverter data
	testData := &domain.InverterData{
		PVSerial:   "TEST123456",
		DeviceType: "inverter",
		ExtendedData: map[string]interface{}{
			"pvpowerout":   1500.0,
			"etogridtoday": 32775.0, // This is the problematic rollover value
		},
	}

	ctx := context.Background()

	// Test 1: During grace period - should not publish
	t.Run("blocks publishing during grace period", func(t *testing.T) {
		// Reset mock expectations
		mockClient.ExpectedCalls = nil

		// Publisher is in grace period (just started)
		now := time.Now()
		publisher.startupTime = now
		publisher.gracePeriodStart = now
		publisher.lastDataTime = now
		publisher.inGracePeriod = true

		// Should not call Publish on the MQTT client
		err := publisher.publishInverterDataWithDiscovery(ctx, "test/topic", testData)
		assert.NoError(t, err)

		// Verify no MQTT publish calls were made
		mockClient.AssertNotCalled(t, "Publish")
	})

	// Test 2: After grace period - should publish
	t.Run("allows publishing after grace period", func(t *testing.T) {
		// Reset mock expectations
		mockClient.ExpectedCalls = nil
		mockClient.Calls = nil

		// Publisher is past grace period
		now := time.Now()
		publisher.startupTime = now.Add(-15 * time.Second) // 15 seconds ago
		publisher.gracePeriodStart = publisher.startupTime
		publisher.lastDataTime = now.Add(-1 * time.Second)
		publisher.inGracePeriod = false

		// Mock successful publish
		mockToken := &mocks.MockToken{}
		doneChan := make(chan struct{})
		close(doneChan) // Close immediately to simulate completed publish
		mockToken.On("Done").Return((<-chan struct{})(doneChan))
		mockToken.On("Error").Return(nil)

		mockClient.On("Publish", mock.AnythingOfType("string"), byte(0), false, mock.Anything).Return(mockToken)

		// Should publish normally
		err := publisher.publishInverterDataWithDiscovery(ctx, "test/topic", testData)
		assert.NoError(t, err)

		// Verify MQTT publish was called
		mockClient.AssertCalled(t, "Publish", mock.AnythingOfType("string"), byte(0), false, mock.Anything)
	})
}

// TestMQTTPublisher_StartupDataFilteringGracePeriodTransition tests the transition from grace period to normal operation
func TestMQTTPublisher_StartupDataFilteringGracePeriodTransition(t *testing.T) {
	// Create config
	cfg := &config.Config{}
	cfg.MQTT.StartupDataFilter.Enabled = true
	cfg.MQTT.StartupDataFilter.GracePeriodSeconds = 2 // Short for testing
	cfg.MQTT.StartupDataFilter.DataGapThresholdHours = 1

	// Create mock MQTT client
	mockClient := &mocks.MockClient{}
	mockClient.On("IsConnected").Return(true)

	// Create publisher
	publisher := NewMQTTPublisherWithClient(cfg, mockClient)

	// Test the grace period transition
	t.Run("grace period ends after configured time", func(t *testing.T) {
		// Start in grace period
		assert.True(t, publisher.shouldFilterStartupData(), "should filter initially")

		// Wait for grace period to end (add small buffer for test timing)
		time.Sleep(2100 * time.Millisecond)

		// Should no longer filter
		assert.False(t, publisher.shouldFilterStartupData(), "should not filter after grace period")
	})
}

// TestMQTTPublisher_StartupDataFilteringDataGap tests data gap detection
func TestMQTTPublisher_StartupDataFilteringDataGap(t *testing.T) {
	// Create config
	cfg := &config.Config{}
	cfg.MQTT.StartupDataFilter.Enabled = true
	cfg.MQTT.StartupDataFilter.GracePeriodSeconds = 1 // Short for testing
	cfg.MQTT.StartupDataFilter.DataGapThresholdHours = 1

	// Create mock MQTT client
	mockClient := &mocks.MockClient{}
	mockClient.On("IsConnected").Return(true)

	// Create publisher
	publisher := NewMQTTPublisherWithClient(cfg, mockClient)

	// Wait for initial grace period to end
	time.Sleep(1100 * time.Millisecond)

	// Verify we're not filtering
	assert.False(t, publisher.shouldFilterStartupData(), "should not filter after initial grace period")

	// Simulate data gap by setting lastDataTime to 2 hours ago
	publisher.lastDataTime = time.Now().Add(-2 * time.Hour)

	// Next call should detect gap and start filtering again
	assert.True(t, publisher.shouldFilterStartupData(), "should start filtering after data gap")
}

// TestMQTTPublisher_StartupDataFilteringConfiguration tests configuration validation
func TestMQTTPublisher_StartupDataFilteringConfiguration(t *testing.T) {
	tests := []struct {
		name                  string
		gracePeriodSeconds    int
		dataGapThresholdHours int
		expectFiltering       bool
	}{
		{
			name:                  "zero grace period",
			gracePeriodSeconds:    0,
			dataGapThresholdHours: 1,
			expectFiltering:       false,
		},
		{
			name:                  "normal configuration",
			gracePeriodSeconds:    10,
			dataGapThresholdHours: 1,
			expectFiltering:       true,
		},
		{
			name:                  "large grace period",
			gracePeriodSeconds:    60,
			dataGapThresholdHours: 2,
			expectFiltering:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create config
			cfg := &config.Config{}
			cfg.MQTT.StartupDataFilter.Enabled = true
			cfg.MQTT.StartupDataFilter.GracePeriodSeconds = tt.gracePeriodSeconds
			cfg.MQTT.StartupDataFilter.DataGapThresholdHours = tt.dataGapThresholdHours

			// Create mock MQTT client
			mockClient := &mocks.MockClient{}
			mockClient.On("IsConnected").Return(true)

			// Create publisher (this sets inGracePeriod = true initially)
			publisher := NewMQTTPublisherWithClient(cfg, mockClient)

			// Test initial state
			shouldFilter := publisher.shouldFilterStartupData()
			assert.Equal(t, tt.expectFiltering, shouldFilter, "initial filtering should match expected")
		})
	}
}

// TestMQTTPublisher_ShouldRediscover tests the periodic rediscovery logic
func TestMQTTPublisher_ShouldRediscover(t *testing.T) {
	t.Run("RediscoveryDisabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval = 0 // Disabled

		publisher := NewMQTTPublisher(cfg)

		// Should return false when rediscovery is disabled
		assert.False(t, publisher.shouldRediscover())
	})

	t.Run("FirstDiscovery", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval = 24 // 24 hours

		publisher := NewMQTTPublisher(cfg)

		// Should return true on first discovery (lastDiscoveryTime is zero)
		assert.True(t, publisher.shouldRediscover())
	})

	t.Run("RecentDiscovery", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval = 24 // 24 hours

		publisher := NewMQTTPublisher(cfg)

		// Set recent discovery time
		publisher.mu.Lock()
		publisher.lastDiscoveryTime = time.Now().Add(-1 * time.Hour) // 1 hour ago
		publisher.mu.Unlock()

		// Should return false (not enough time has passed)
		assert.False(t, publisher.shouldRediscover())
	})

	t.Run("PastRediscoveryInterval", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval = 1 // 1 hour

		publisher := NewMQTTPublisher(cfg)

		// Set old discovery time
		publisher.mu.Lock()
		publisher.lastDiscoveryTime = time.Now().Add(-2 * time.Hour) // 2 hours ago
		publisher.mu.Unlock()

		// Should return true (interval has passed)
		assert.True(t, publisher.shouldRediscover())
	})
}

// mockMessage is a simple test implementation of MQTT Message interface
type mockMessage struct {
	topic   string
	payload []byte
}

func (m *mockMessage) Duplicate() bool   { return false }
func (m *mockMessage) Qos() byte         { return 0 }
func (m *mockMessage) Retained() bool    { return false }
func (m *mockMessage) Topic() string     { return m.topic }
func (m *mockMessage) MessageID() uint16 { return 0 }
func (m *mockMessage) Payload() []byte   { return m.payload }
func (m *mockMessage) Ack()              {}

// TestMQTTPublisher_HandleBirthMessage tests Home Assistant birth message handling
func TestMQTTPublisher_HandleBirthMessage(t *testing.T) {
	t.Run("OnlineMessage", func(t *testing.T) {
		cfg := &config.Config{}
		publisher := NewMQTTPublisher(cfg)

		// Set some discovered sensors
		publisher.mu.Lock()
		publisher.discoveredSensors["test/sensor1"] = true
		publisher.discoveredSensors["test/sensor2"] = true
		publisher.lastDiscoveryTime = time.Now().Add(-1 * time.Hour)
		publisher.mu.Unlock()

		// Create a mock MQTT message
		mockClient := mocks.NewMockClient(t)
		mockMsg := &mockMessage{
			topic:   "homeassistant/status",
			payload: []byte("online"),
		}

		// Handle the birth message
		publisher.handleBirthMessage(mockClient, mockMsg)

		// Verify discovery cache was cleared
		publisher.mu.RLock()
		assert.Empty(t, publisher.discoveredSensors, "discovered sensors should be cleared")
		assert.True(t, publisher.lastDiscoveryTime.IsZero(), "last discovery time should be reset")
		publisher.mu.RUnlock()
	})

	t.Run("OfflineMessage", func(t *testing.T) {
		cfg := &config.Config{}
		publisher := NewMQTTPublisher(cfg)

		// Set some discovered sensors
		publisher.mu.Lock()
		publisher.discoveredSensors["test/sensor1"] = true
		discoveryTime := time.Now().Add(-1 * time.Hour)
		publisher.lastDiscoveryTime = discoveryTime
		publisher.mu.Unlock()

		// Create a mock MQTT message
		mockClient := mocks.NewMockClient(t)
		mockMsg := &mockMessage{
			topic:   "homeassistant/status",
			payload: []byte("offline"),
		}

		// Handle the birth message
		publisher.handleBirthMessage(mockClient, mockMsg)

		// Verify discovery cache was NOT cleared for offline message
		publisher.mu.RLock()
		assert.Len(t, publisher.discoveredSensors, 1, "discovered sensors should not be cleared")
		assert.Equal(t, discoveryTime.Unix(), publisher.lastDiscoveryTime.Unix(), "last discovery time should not change")
		publisher.mu.RUnlock()
	})
}

// TestMQTTPublisher_SubscribeToBirthMessage tests birth message subscription
func TestMQTTPublisher_SubscribeToBirthMessage(t *testing.T) {
	t.Run("SuccessfulSubscription", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"

		mockClient := mocks.NewMockClient(t)
		mockToken := mocks.NewMockToken(t)

		// Setup mock expectations
		mockToken.On("Wait").Return(true)
		mockToken.On("Error").Return(nil)
		mockClient.On("Subscribe", "homeassistant/status", byte(0), mock.Anything).Return(mockToken)

		publisher := NewMQTTPublisherWithClient(cfg, mockClient)
		publisher.mu.Lock()
		publisher.connected = true
		publisher.mu.Unlock()

		// Call subscribe
		publisher.subscribeToBirthMessage()

		// Verify subscription was set
		publisher.mu.RLock()
		assert.True(t, publisher.birthSubscribed)
		publisher.mu.RUnlock()

		mockClient.AssertExpectations(t)
		mockToken.AssertExpectations(t)
	})

	t.Run("AlreadySubscribed", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"

		mockClient := mocks.NewMockClient(t)

		publisher := NewMQTTPublisherWithClient(cfg, mockClient)
		publisher.mu.Lock()
		publisher.connected = true
		publisher.birthSubscribed = true
		publisher.mu.Unlock()

		// Call subscribe - should do nothing
		publisher.subscribeToBirthMessage()

		// Should not call Subscribe on client
		mockClient.AssertNotCalled(t, "Subscribe")
	})

	t.Run("NotConnected", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"

		mockClient := mocks.NewMockClient(t)

		publisher := NewMQTTPublisherWithClient(cfg, mockClient)
		publisher.mu.Lock()
		publisher.connected = false
		publisher.mu.Unlock()

		// Call subscribe - should do nothing when not connected
		publisher.subscribeToBirthMessage()

		// Should not call Subscribe on client
		mockClient.AssertNotCalled(t, "Subscribe")
	})

	t.Run("SubscriptionError", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"

		mockClient := mocks.NewMockClient(t)
		mockToken := mocks.NewMockToken(t)

		// Setup mock expectations - subscription fails
		mockToken.On("Wait").Return(true)
		mockToken.On("Error").Return(assert.AnError)
		mockClient.On("Subscribe", "homeassistant/status", byte(0), mock.Anything).Return(mockToken)

		publisher := NewMQTTPublisherWithClient(cfg, mockClient)
		publisher.mu.Lock()
		publisher.connected = true
		publisher.mu.Unlock()

		// Call subscribe
		publisher.subscribeToBirthMessage()

		// Verify subscription was NOT set due to error
		publisher.mu.RLock()
		assert.False(t, publisher.birthSubscribed)
		publisher.mu.RUnlock()

		mockClient.AssertExpectations(t)
		mockToken.AssertExpectations(t)
	})
}
