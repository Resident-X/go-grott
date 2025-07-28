// Package homeassistant provides MQTT auto-discovery support for Home Assistant integration.
package homeassistant

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

//go:embed layouts/homeassistant_sensors.json
var homeAssistantSensorsJSON []byte

// Config holds the Home Assistant auto-discovery configuration.
type Config struct {
	Enabled             bool
	DiscoveryPrefix     string
	DeviceName          string
	DeviceManufacturer  string
	DeviceModel         string
	RetainDiscovery     bool
	IncludeDiagnostic   bool
	IncludeBattery      bool
	IncludeGrid         bool
	IncludePV           bool
	ValueTemplateSuffix string
}

// SensorConfig represents a sensor configuration from the layouts JSON.
type SensorConfig struct {
	Name              string `json:"name"`
	DeviceClass       string `json:"device_class,omitempty"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	StateClass        string `json:"state_class,omitempty"`
	Category          string `json:"category"`
	Icon              string `json:"icon,omitempty"`
}

// LayoutConfig represents the full layout configuration for Home Assistant sensors.
type LayoutConfig struct {
	Version     string                  `json:"version"`
	Description string                  `json:"description"`
	Sensors     map[string]SensorConfig `json:"sensors"`
}

// DiscoveryMessage represents a Home Assistant MQTT discovery message.
type DiscoveryMessage struct {
	Name                string     `json:"name"`
	UniqueID            string     `json:"unique_id"`
	StateTopic          string     `json:"state_topic"`
	ValueTemplate       string     `json:"value_template"`
	DeviceClass         string     `json:"device_class,omitempty"`
	UnitOfMeasurement   string     `json:"unit_of_measurement,omitempty"`
	StateClass          string     `json:"state_class,omitempty"`
	Icon                string     `json:"icon,omitempty"`
	EntityCategory      string     `json:"entity_category,omitempty"`
	Device              DeviceInfo `json:"device"`
	AvailabilityTopic   string     `json:"availability_topic,omitempty"`
	PayloadAvailable    string     `json:"payload_available,omitempty"`
	PayloadNotAvailable string     `json:"payload_not_available,omitempty"`
}

// DeviceInfo represents device information for Home Assistant.
type DeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model,omitempty"`
	SwVersion    string   `json:"sw_version,omitempty"`
}

// AutoDiscovery handles Home Assistant MQTT auto-discovery.
type AutoDiscovery struct {
	config       Config
	layoutConfig *LayoutConfig
	baseTopic    string
	deviceID     string
}

// New creates a new Home Assistant auto-discovery instance.
func New(config Config, baseTopic, deviceID string) (*AutoDiscovery, error) {
	ad := &AutoDiscovery{
		config:    config,
		baseTopic: baseTopic,
		deviceID:  deviceID,
	}

	// Load the layout configuration
	if err := ad.loadLayoutConfig(); err != nil {
		return nil, fmt.Errorf("failed to load layout config: %w", err)
	}

	return ad, nil
}

// loadLayoutConfig loads the Home Assistant sensor configuration from embedded JSON.
func (ad *AutoDiscovery) loadLayoutConfig() error {
	var config LayoutConfig
	if err := json.Unmarshal(homeAssistantSensorsJSON, &config); err != nil {
		return fmt.Errorf("failed to unmarshal Home Assistant sensors config: %w", err)
	}

	ad.layoutConfig = &config
	log.Info().
		Str("version", config.Version).
		Int("sensor_count", len(config.Sensors)).
		Msg("Home Assistant layout configuration loaded from JSON")

	return nil
}

// GenerateDiscoveryMessages generates all discovery messages for the given data.
func (ad *AutoDiscovery) GenerateDiscoveryMessages(data map[string]interface{}) map[string]DiscoveryMessage {
	messages := make(map[string]DiscoveryMessage)

	// Extract PV serial for device model detection
	var pvSerial string
	if serial, ok := data["pvserial"].(string); ok {
		pvSerial = serial
	}

	for fieldName, value := range data {
		sensorConfig, exists := ad.layoutConfig.Sensors[fieldName]
		if !exists {
			continue
		}

		// Check category filters
		if !ad.shouldIncludeCategory(sensorConfig.Category) {
			continue
		}

		// Generate the discovery message
		message := ad.createDiscoveryMessage(fieldName, sensorConfig, value, pvSerial)
		if message != nil {
			topic := ad.getDiscoveryTopic(fieldName)
			messages[topic] = *message
		}
	}

	return messages
}

// shouldIncludeCategory checks if a category should be included based on configuration.
func (ad *AutoDiscovery) shouldIncludeCategory(category string) bool {
	switch category {
	case "diagnostic":
		return ad.config.IncludeDiagnostic
	case "battery":
		return ad.config.IncludeBattery
	case "grid":
		return ad.config.IncludeGrid
	case "pv":
		return ad.config.IncludePV
	default:
		return true // Include unknown categories by default
	}
}

// createDiscoveryMessage creates a discovery message for a specific sensor.
func (ad *AutoDiscovery) createDiscoveryMessage(fieldName string, sensorConfig SensorConfig, value interface{}, pvSerial string) *DiscoveryMessage {
	// Create unique ID
	uniqueID := fmt.Sprintf("%s_%s", ad.deviceID, fieldName)

	// Create state topic
	stateTopic := ad.baseTopic

	// Create value template
	valueTemplate := fmt.Sprintf("{{ value_json.%s%s }}", fieldName, ad.config.ValueTemplateSuffix)

	// Determine entity category
	var entityCategory string
	if sensorConfig.Category == "diagnostic" {
		entityCategory = "diagnostic"
	}

	// Create device info with model detection from PV serial
	deviceInfo := DeviceInfo{
		Identifiers:  []string{ad.deviceID},
		Name:         ad.config.DeviceName,
		Manufacturer: ad.config.DeviceManufacturer,
		Model:        ad.getDeviceModelFromSerial(pvSerial),
		SwVersion:    "go-grott",
	}

	message := &DiscoveryMessage{
		Name:              sensorConfig.Name,
		UniqueID:          uniqueID,
		StateTopic:        stateTopic,
		ValueTemplate:     valueTemplate,
		DeviceClass:       sensorConfig.DeviceClass,
		UnitOfMeasurement: sensorConfig.UnitOfMeasurement,
		StateClass:        sensorConfig.StateClass,
		Icon:              sensorConfig.Icon,
		EntityCategory:    entityCategory,
		Device:            deviceInfo,
	}

	return message
}

// getDiscoveryTopic generates the MQTT discovery topic for a sensor.
func (ad *AutoDiscovery) getDiscoveryTopic(fieldName string) string {
	// Home Assistant discovery topic format:
	// <discovery_prefix>/sensor/<node_id>/<object_id>/config
	nodeID := strings.ReplaceAll(ad.deviceID, " ", "_")
	nodeID = strings.ToLower(nodeID)
	objectID := fmt.Sprintf("%s_%s", nodeID, fieldName)

	return fmt.Sprintf("%s/sensor/%s/%s/config", ad.config.DiscoveryPrefix, nodeID, objectID)
}

// getDeviceModel returns the device model, using auto-detection if not configured.
func (ad *AutoDiscovery) getDeviceModel() string {
	if ad.config.DeviceModel != "" {
		return ad.config.DeviceModel
	}

	return "Growatt Inverter" // Default fallback
}

// getDeviceModelFromSerial extracts the device model from the PV serial number.
func (ad *AutoDiscovery) getDeviceModelFromSerial(pvSerial string) string {
	if ad.config.DeviceModel != "" {
		return ad.config.DeviceModel
	}

	if pvSerial == "" {
		return ad.getDeviceModel()
	}

	serial := strings.ToUpper(pvSerial)

	// Growatt inverter model detection from serial number patterns
	// Serial format is typically: [MODEL][POWER][VERSION][YEAR][SERIAL]
	// Examples: MIC600TLX230001, SPH5000TL3BH230001, MIN3600TLXE230001

	if strings.HasPrefix(serial, "MIC") {
		if strings.Contains(serial, "1000TL") {
			return "MIC 1000TL-X"
		} else if strings.Contains(serial, "1500TL") {
			return "MIC 1500TL-X"
		} else if strings.Contains(serial, "600TL") {
			return "MIC 600TL-X"
		}
		return "MIC Series"
	} else if strings.HasPrefix(serial, "SPH") {
		if strings.Contains(serial, "10000TL3") {
			return "SPH 10000TL3"
		} else if strings.Contains(serial, "8000TL3") {
			return "SPH 8000TL3"
		} else if strings.Contains(serial, "6000TL3") {
			return "SPH 6000TL3"
		} else if strings.Contains(serial, "5000TL3") {
			return "SPH 5000TL3"
		} else if strings.Contains(serial, "4000TL3") {
			return "SPH 4000TL3"
		} else if strings.Contains(serial, "3000TL3") {
			return "SPH 3000TL3"
		}
		return "SPH Series"
	} else if strings.HasPrefix(serial, "MIN") {
		if strings.Contains(serial, "6000TL") {
			return "MIN 6000TL-X"
		} else if strings.Contains(serial, "5000TL") {
			return "MIN 5000TL-X"
		} else if strings.Contains(serial, "4200TL") {
			return "MIN 4200TL-XE"
		} else if strings.Contains(serial, "3600TL") {
			return "MIN 3600TL-XE"
		} else if strings.Contains(serial, "3000TL") {
			return "MIN 3000TL-XE"
		} else if strings.Contains(serial, "2500TL") {
			return "MIN 2500TL-XE"
		}
		return "MIN Series"
	} else if strings.HasPrefix(serial, "MAX") {
		if strings.Contains(serial, "100KTL3") {
			return "MAX 100KTL3 LV"
		} else if strings.Contains(serial, "80KTL3") {
			return "MAX 80KTL3 LV"
		} else if strings.Contains(serial, "60KTL3") {
			return "MAX 60KTL3 LV"
		} else if strings.Contains(serial, "50KTL3") {
			return "MAX 50KTL3 LV"
		}
		return "MAX Series"
	} else if strings.Contains(serial, "TL3") {
		if strings.Contains(serial, "25000") {
			return "TL3-25000"
		} else if strings.Contains(serial, "20000") {
			return "TL3-20000"
		} else if strings.Contains(serial, "15000") {
			return "TL3-15000"
		}
		return "TL3 Series"
	} else if strings.HasPrefix(serial, "MOD") {
		return "MOD Series"
	}

	// If no specific pattern matches, return a generic model
	return "Growatt Inverter"
}

// GetAvailabilityTopic returns the availability topic for the device.
func (ad *AutoDiscovery) GetAvailabilityTopic() string {
	return fmt.Sprintf("%s/availability", ad.baseTopic)
}

// CreateAvailabilityMessage creates availability messages.
func (ad *AutoDiscovery) CreateAvailabilityMessage(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

// CleanupDiscoveryMessages generates cleanup (empty) messages to remove sensors from Home Assistant.
func (ad *AutoDiscovery) CleanupDiscoveryMessages(fieldNames []string) map[string]string {
	messages := make(map[string]string)

	for _, fieldName := range fieldNames {
		topic := ad.getDiscoveryTopic(fieldName)
		messages[topic] = "" // Empty payload removes the entity
	}

	return messages
}
