// Package homeassistant provides MQTT auto-discovery support for Home Assistant integration.
package homeassistant

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/resident-x/go-grott/internal/domain"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

//go:embed layouts/homeassistant_sensors.yaml
var homeAssistantSensorsYAML []byte

// Config holds the Home Assistant auto-discovery configuration.
type Config struct {
	Enabled             bool
	DiscoveryPrefix     string
	DeviceName          string
	DeviceManufacturer  string
	DeviceModel         string
	RetainDiscovery     bool
	ValueTemplateSuffix string
}

// SensorConfig represents a sensor configuration from the layouts YAML.
type SensorConfig struct {
	Name              string `yaml:"name"`
	DeviceClass       string `yaml:"device_class,omitempty"`
	UnitOfMeasurement string `yaml:"unit_of_measurement,omitempty"`
	StateClass        string `yaml:"state_class,omitempty"`
	Category          string `yaml:"category"`
	Icon              string `yaml:"icon,omitempty"`
	StatusMapping     string `yaml:"status_mapping,omitempty"`
}

// LayoutConfig represents the full layout configuration for Home Assistant sensors.
type LayoutConfig struct {
	Version        string                            `yaml:"version"`
	Description    string                            `yaml:"description"`
	StatusMappings map[string]map[interface{}]string `yaml:"status_mappings"`
	Sensors        map[string]SensorConfig           `yaml:"sensors"`
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

// loadLayoutConfig loads the Home Assistant sensor configuration from embedded YAML.
func (ad *AutoDiscovery) loadLayoutConfig() error {
	var config LayoutConfig
	if err := yaml.Unmarshal(homeAssistantSensorsYAML, &config); err != nil {
		return fmt.Errorf("failed to unmarshal Home Assistant sensors config: %w", err)
	}

	ad.layoutConfig = &config
	log.Info().
		Str("version", config.Version).
		Int("sensor_count", len(config.Sensors)).
		Msg("Home Assistant layout configuration loaded from YAML")

	return nil
}

// ApplyCalculations applies status mapping to the provided data.
func (ad *AutoDiscovery) ApplyCalculations(data map[string]interface{}) map[string]interface{} {
	if ad.layoutConfig == nil {
		return data
	}

	processedData := make(map[string]interface{})
	for key, value := range data {
		processedData[key] = value
	}

	log.Debug().
		Int("input_field_count", len(data)).
		Int("sensor_count", len(ad.layoutConfig.Sensors)).
		Msg("Processing data")

	// Apply status mapping for configured sensors
	for fieldName, sensorConfig := range ad.layoutConfig.Sensors {
		if value, exists := data[fieldName]; exists && sensorConfig.StatusMapping != "" {
			if statusMappings, exists := ad.layoutConfig.StatusMappings[sensorConfig.StatusMapping]; exists {
				if mappedValue, found := statusMappings[value]; found {
					processedData[fieldName] = mappedValue
					log.Debug().
						Str("field", fieldName).
						Interface("original_value", value).
						Str("mapped_value", mappedValue).
						Msg("Status mapping successful")
				}
			}
		}
	}

	return processedData
}

// applyStatusMapping converts numeric status codes to human-readable strings.
func (ad *AutoDiscovery) applyStatusMapping(mappingKey string, rawValue interface{}) interface{} {
	mapping, exists := ad.layoutConfig.StatusMappings[mappingKey]
	if !exists {
		log.Warn().Str("mapping_key", mappingKey).Msg("Status mapping not found")
		return rawValue
	}

	// Convert raw value to string key for lookup
	var key interface{}
	if numVal, ok := ad.convertToFloat(rawValue); ok {
		key = int(numVal) // Try as integer first
		if result, found := mapping[key]; found {
			return result
		}
		key = numVal // Try as float
		if result, found := mapping[key]; found {
			return result
		}
	}

	// Try string key
	key = fmt.Sprintf("%v", rawValue)
	if result, found := mapping[key]; found {
		return result
	}

	// Return default value
	if defaultVal, found := mapping["default"]; found {
		return defaultVal
	}

	return rawValue
}

// convertToFloat converts various numeric types to float64.
func (ad *AutoDiscovery) convertToFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// evaluateFormula evaluates a calculation formula with the given numeric value.
func (ad *AutoDiscovery) evaluateFormula(formula string, value float64, fieldName string) interface{} {
	// Replace "raw_value" with the actual value in the formula
	expression := strings.ReplaceAll(formula, "raw_value", fmt.Sprintf("%f", value))

	// For now, handle common patterns. Could be extended with a math parser.
	if strings.Contains(expression, "/") {
		parts := strings.Split(expression, "/")
		if len(parts) == 2 {
			if divisor, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
				return value / divisor
			}
		}
	}

	log.Warn().
		Str("field", fieldName).
		Str("formula", formula).
		Msg("Could not evaluate calculation formula")
	return value
}

// GenerateDiscoveryMessages generates all discovery messages for the given data.
func (ad *AutoDiscovery) GenerateDiscoveryMessages(data map[string]interface{}) map[string]DiscoveryMessage {
	messages := make(map[string]DiscoveryMessage)

	// Extract PV serial for device model detection
	var pvSerial string
	if serial, ok := data["pvserial"].(string); ok {
		pvSerial = serial
	}

	// Extract device type for proper naming
	var deviceType string
	if dtype, ok := data["device_type"].(string); ok {
		deviceType = dtype
	}

	for fieldName, value := range data {
		sensorConfig, exists := ad.layoutConfig.Sensors[fieldName]
		if !exists {
			continue
		}

		// Generate the discovery message
		message := ad.createDiscoveryMessage(fieldName, sensorConfig, value, pvSerial, deviceType)
		if message != nil {
			topic := ad.getDiscoveryTopic(fieldName, deviceType)
			messages[topic] = *message
		}
	}

	return messages
}

// createDiscoveryMessage creates a discovery message for a specific sensor.
func (ad *AutoDiscovery) createDiscoveryMessage(fieldName string, sensorConfig SensorConfig, value interface{}, pvSerial, deviceType string) *DiscoveryMessage {
	// Extract base device ID (remove device type suffix if present) to avoid double device type
	baseDeviceID := ad.deviceID
	if strings.Contains(ad.deviceID, "_") {
		// If deviceID is like "CUK4CBQ05E_smart_meter", extract just "CUK4CBQ05E"
		parts := strings.Split(ad.deviceID, "_")
		if len(parts) >= 2 {
			baseDeviceID = parts[0]
		}
	}

	// Create unique ID with device type to avoid conflicts between smart meter and inverter sensors
	uniqueID := fmt.Sprintf("%s_%s_%s", baseDeviceID, deviceType, fieldName)

	// Create state topic
	stateTopic := ad.baseTopic

	// Create value template using integration settings
	valueTemplate := ad.getValueTemplate(fieldName)

	// Determine entity category based on sensor category
	var entityCategory string
	if sensorConfig.Category == "diagnostic" {
		entityCategory = "diagnostic"
	}

	// Create device name with device type for differentiation
	deviceName := ad.config.DeviceName
	if deviceType != "" {
		deviceName = fmt.Sprintf("%s (%s)", ad.config.DeviceName, ad.capitalizeDeviceType(deviceType))
	}

	// Create device identifier with device type for proper separation
	deviceIdentifier := fmt.Sprintf("%s_%s", baseDeviceID, deviceType)

	// Create device info with model detection from PV serial and device type differentiation
	deviceInfo := DeviceInfo{
		Identifiers:  []string{deviceIdentifier},
		Name:         deviceName,
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

	// Add availability topic if enabled in integration settings
	if ad.isAvailabilityEnabled() {
		message.AvailabilityTopic = ad.GetAvailabilityTopic()
		message.PayloadAvailable = ad.getPayloadAvailable()
		message.PayloadNotAvailable = ad.getPayloadNotAvailable()
	}

	return message
}

// getValueTemplate creates the appropriate value template based on integration settings.
func (ad *AutoDiscovery) getValueTemplate(fieldName string) string {
	// Use default template format
	templateFormat := "{{ value_json.%s }}"
	return fmt.Sprintf(templateFormat, fieldName) + ad.config.ValueTemplateSuffix
}

// isAvailabilityEnabled checks if availability topics should be used.
func (ad *AutoDiscovery) isAvailabilityEnabled() bool {
	return false
}

// getPayloadAvailable returns the payload for available status.
func (ad *AutoDiscovery) getPayloadAvailable() string {
	return "online"
}

// getPayloadNotAvailable returns the payload for not available status.
func (ad *AutoDiscovery) getPayloadNotAvailable() string {
	return "offline"
}

// getDiscoveryTopic generates the MQTT discovery topic for a sensor.
func (ad *AutoDiscovery) getDiscoveryTopic(fieldName, deviceType string) string {
	// Home Assistant discovery topic format:
	// <discovery_prefix>/sensor/<node_id>/<object_id>/config

	// Extract base device ID (remove device type suffix if present) to avoid double device type
	baseDeviceID := ad.deviceID
	if strings.Contains(ad.deviceID, "_") {
		// If deviceID is like "CUK4CBQ05E_smart_meter", extract just "CUK4CBQ05E"
		parts := strings.Split(ad.deviceID, "_")
		if len(parts) >= 2 {
			baseDeviceID = parts[0]
		}
	}

	// Create node ID with device type for proper separation
	nodeID := fmt.Sprintf("%s_%s", baseDeviceID, deviceType)
	nodeID = strings.ReplaceAll(nodeID, " ", "_")
	nodeID = strings.ToLower(nodeID)
	objectID := fmt.Sprintf("%s_%s", nodeID, fieldName)

	return fmt.Sprintf("%s/sensor/%s/%s/config", ad.config.DiscoveryPrefix, nodeID, objectID)
}

// capitalizeDeviceType converts device type to proper case for display.
func (ad *AutoDiscovery) capitalizeDeviceType(deviceType string) string {
	switch deviceType {
	case domain.DeviceTypeSmartMeter:
		return "Smart Meter"
	case domain.DeviceTypeInverter:
		return "Inverter"
	default:
		// Convert to proper case manually
		words := strings.Split(strings.ReplaceAll(deviceType, "_", " "), " ")
		for i, word := range words {
			if len(word) > 0 {
				words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
			}
		}
		return strings.Join(words, " ")
	}
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
	suffix := "/availability"
	return fmt.Sprintf("%s%s", ad.baseTopic, suffix)
}

// CreateAvailabilityMessage creates availability messages based on configuration.
func (ad *AutoDiscovery) CreateAvailabilityMessage(online bool) string {
	if online {
		return ad.getPayloadAvailable()
	}
	return ad.getPayloadNotAvailable()
}

// CleanupDiscoveryMessages generates cleanup (empty) messages to remove sensors from Home Assistant.
func (ad *AutoDiscovery) CleanupDiscoveryMessages(fieldNames []string, deviceType string) map[string]string {
	messages := make(map[string]string)

	for _, fieldName := range fieldNames {
		topic := ad.getDiscoveryTopic(fieldName, deviceType)
		messages[topic] = "" // Empty payload removes the entity
	}

	return messages
}
