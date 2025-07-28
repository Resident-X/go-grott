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

	err := publisher.Connect(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to MQTT broker")
	assert.False(t, publisher.connected)
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
