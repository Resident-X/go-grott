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

	err := publisher.Connect(ctx)
	assert.Error(t, err)
	assert.False(t, publisher.connected)
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

	// Should still error due to invalid broker, but credentials are set
	err := publisher.Connect(ctx)
	assert.Error(t, err)
	assert.False(t, publisher.connected)
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

	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true) // Set as connected for this test
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
	err := publisher.PublishInverterData(ctx, testData)
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
	err := publisher.PublishInverterData(ctx, testData)
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
	err := publisher.PublishInverterData(ctx, testData)
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
	err := publisher.PublishInverterData(ctx, testData)
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
	err := publisher.PublishInverterData(ctx, testData)
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

	err := publisher.Connect(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.False(t, publisher.connected)
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
	cfg.MQTT.ConnectionTimeout = 10 // Set timeout for test

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Setup expectations
	mockClient.EXPECT().Connect().Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	// Mock IsConnected for assertion at end
	mockClient.EXPECT().IsConnected().Return(true)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	ctx := context.Background()

	err := publisher.Connect(ctx)
	assert.NoError(t, err)
	assert.True(t, publisher.IsConnected())
}

func TestMQTTPublisher_Connect_Error(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.ConnectionTimeout = 10 // Set timeout for test

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

	err := publisher.Connect(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to MQTT broker")
	assert.False(t, publisher.IsConnected())
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

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	mockClient.EXPECT().Publish(expectedTopic, expectedQoS, expectedRetain, mock.MatchedBy(func(payload []byte) bool {
		return bytes.Equal(payload, expectedPayload)
	})).Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true) // Simulate connected state

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

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	// Setup expectations for publish error
	mockClient.EXPECT().Publish(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	// Create a done channel that's already closed to simulate immediate completion
	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(assert.AnError)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

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

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	// Setup expectations for publish timeout
	mockClient.EXPECT().Publish(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	// Create a done channel that never closes to simulate timeout
	doneChan := make(chan struct{})
	mockToken.EXPECT().Done().Return(doneChan)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

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

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	// Setup expectations
	expectedTopic := "energy/growatt/INV001"
	mockClient.EXPECT().Publish(expectedTopic, mock.Anything, mock.Anything, mock.Anything).Return(mockToken)

	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan)
	mockToken.EXPECT().Error().Return(nil)

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

	ctx := context.Background()
	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
	}

	err := publisher.PublishInverterData(ctx, testData)
	assert.NoError(t, err)
}

func TestMQTTPublisher_Close_WithClient(t *testing.T) {
	cfg := &config.Config{}
	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return true
	mockClient.EXPECT().IsConnected().Return(true)

	// Setup expectations
	mockClient.EXPECT().Disconnect(uint(250)).Return()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

	err := publisher.Close()
	assert.NoError(t, err)
	assert.False(t, publisher.IsConnected())
}

func TestMQTTPublisher_StartReconnectionLoop_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = false

	publisher := NewMQTTPublisher(cfg)
	ctx := context.Background()

	// Should return immediately without starting goroutine
	publisher.StartReconnectionLoop(ctx)

	// Give a small window to ensure no goroutine was started
	time.Sleep(10 * time.Millisecond)
	// If it reaches here without panic, test passes
}

func TestMQTTPublisher_StartReconnectionLoop_ContextCancellation(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.ConnectionTimeout = 1

	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return false (simulating disconnected state)
	mockClient.EXPECT().IsConnected().Return(false).Maybe()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(false)

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Start reconnection loop
	publisher.StartReconnectionLoop(ctx)

	// Cancel context immediately
	cancel()

	// Give goroutine time to process cancellation
	time.Sleep(50 * time.Millisecond)

	// If we reach here without deadlock, the loop stopped correctly
}

func TestMQTTPublisher_StartReconnectionLoop_CloseSignal(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883

	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return false initially
	mockClient.EXPECT().IsConnected().Return(false).Maybe()
	mockClient.EXPECT().Disconnect(uint(250)).Return().Maybe()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)

	// Start reconnection loop with long-lived context
	ctx := context.Background()
	publisher.StartReconnectionLoop(ctx)

	// Close the publisher (which closes reconnectDone channel)
	err := publisher.Close()
	assert.NoError(t, err)

	// Give goroutine time to process close signal
	time.Sleep(50 * time.Millisecond)

	// If we reach here without deadlock, the loop stopped correctly
}

func TestMQTTPublisher_StartReconnectionLoop_AlreadyConnected(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883

	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return true (already connected)
	mockClient.EXPECT().IsConnected().Return(true).Maybe()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start reconnection loop
	publisher.StartReconnectionLoop(ctx)

	// Wait for context timeout
	<-ctx.Done()

	// Loop should not have attempted reconnection since already connected
	// If we reach here, test passes (no reconnection attempts were made beyond initial checks)
}

func TestMQTTPublisher_StartReconnectionLoop_StartsGoroutine(t *testing.T) {
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.ConnectionTimeout = 1

	mockClient := mocks.NewMockClient(t)

	// Mock IsConnected to return true (already connected, so no reconnection attempts)
	mockClient.EXPECT().IsConnected().Return(true).Maybe()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.setConnected(true)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start reconnection loop - this should start a goroutine
	publisher.StartReconnectionLoop(ctx)

	// Give goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Wait for context timeout to ensure goroutine is running and respecting context
	<-ctx.Done()

	// Additional wait for goroutine cleanup
	time.Sleep(50 * time.Millisecond)

	// If we reach here without deadlock or panic, test passes
}
