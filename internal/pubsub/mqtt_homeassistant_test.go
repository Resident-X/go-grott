package pubsub

import (
	"context"
	"testing"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMQTTPublisher_HomeAssistantAutoDiscovery(t *testing.T) {
	// Configuration with Home Assistant auto-discovery enabled
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true
	cfg.MQTT.PublishRaw = true
	cfg.MQTT.HomeAssistantAutoDiscovery.Enabled = true
	cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceName = "Test Inverter"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceManufacturer = "Growatt"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceModel = "MIC 600TL-X"
	cfg.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery = true
	cfg.MQTT.Retain = false

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// We expect multiple publish calls:
	// 1. Several discovery messages for sensors found in the data
	// 2. One availability message
	// 3. One raw data message
	// Since we can't predict exactly how many discovery messages, we'll use mock.Anything
	mockClient.EXPECT().Publish(mock.AnythingOfType("string"), mock.Anything, mock.Anything, mock.Anything).
		Return(mockToken).Times(1) // At least one publish call for raw data

	// We might have additional publish calls for discovery messages and availability
	mockClient.EXPECT().Publish(mock.AnythingOfType("string"), mock.Anything, mock.Anything, mock.Anything).
		Return(mockToken).Maybe()

	doneChan := make(chan struct{})
	close(doneChan)
	mockToken.EXPECT().Done().Return(doneChan).Maybe()
	mockToken.EXPECT().Error().Return(nil).Maybe()
	mockToken.EXPECT().Wait().Return(true).Maybe()

	publisher := NewMQTTPublisherWithClient(cfg, mockClient)
	publisher.connected = true

	ctx := context.Background()
	testData := &domain.InverterData{
		DataloggerSerial: "TEST123",
		PVSerial:         "INV001",
		PVPowerIn:        1000.0,
		PVPowerOut:       950.0,
		// Add some fields that should trigger discovery messages
		EACToday: 5.5,
		EACTotal: 1250.8,
	}

	err := publisher.Publish(ctx, "", testData)
	assert.NoError(t, err)

	// Verify that the Home Assistant discovery was set up
	assert.NotNil(t, publisher.haDiscovery)
}

func TestMQTTPublisher_HomeAssistantAutoDiscovery_Disabled(t *testing.T) {
	// Configuration with Home Assistant auto-discovery disabled (default)
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true
	cfg.MQTT.PublishRaw = true
	// HomeAssistantAutoDiscovery.Enabled defaults to false

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// We expect only one publish call for raw data (no discovery messages)
	expectedTopic := "energy/growatt/INV001"
	mockClient.EXPECT().Publish(expectedTopic, mock.Anything, mock.Anything, mock.Anything).Return(mockToken).Once()

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

	// Verify that Home Assistant discovery was not set up
	assert.Nil(t, publisher.haDiscovery)
}

func TestMQTTPublisher_RawDataDisabled_DiscoveryEnabled(t *testing.T) {
	// Configuration with raw data disabled but Home Assistant auto-discovery enabled
	cfg := &config.Config{}
	cfg.MQTT.Enabled = true
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true
	cfg.MQTT.PublishRaw = false // Disable raw data publishing
	cfg.MQTT.HomeAssistantAutoDiscovery.Enabled = true
	cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceName = "Test Inverter"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceManufacturer = "Growatt"
	cfg.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery = true

	mockClient := mocks.NewMockClient(t)
	mockToken := mocks.NewMockToken(t)

	// We expect discovery and availability messages but no raw data message
	mockClient.EXPECT().Publish(mock.AnythingOfType("string"), mock.Anything, mock.Anything, mock.Anything).
		Return(mockToken).Maybe()

	mockToken.EXPECT().Wait().Return(true).Maybe()
	mockToken.EXPECT().Error().Return(nil).Maybe()

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

	// Verify that Home Assistant discovery was set up
	assert.NotNil(t, publisher.haDiscovery)
}
