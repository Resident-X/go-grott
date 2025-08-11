package homeassistant

import (
	"testing"
)

func TestNew(t *testing.T) {
	config := Config{
		Enabled:             true,
		DiscoveryPrefix:     "homeassistant",
		DeviceName:          "Test Inverter",
		DeviceManufacturer:  "Growatt",
		DeviceModel:         "MIC 600TL-X",
		RetainDiscovery:     true,
		ValueTemplateSuffix: "",
	}

	baseTopic := "energy/growatt"
	deviceID := "ABC123456"

	ad, err := New(config, baseTopic, deviceID)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if ad == nil {
		t.Fatal("Expected AutoDiscovery instance, got nil")
	}

	if ad.config.DeviceName != config.DeviceName {
		t.Errorf("Expected device name %s, got %s", config.DeviceName, ad.config.DeviceName)
	}

	if ad.baseTopic != baseTopic {
		t.Errorf("Expected base topic %s, got %s", baseTopic, ad.baseTopic)
	}

	if ad.deviceID != deviceID {
		t.Errorf("Expected device ID %s, got %s", deviceID, ad.deviceID)
	}
}

func TestGenerateDiscoveryMessages(t *testing.T) {
	config := Config{
		Enabled:             true,
		DiscoveryPrefix:     "homeassistant",
		DeviceName:          "Test Inverter",
		DeviceManufacturer:  "Growatt",
		DeviceModel:         "MIC 600TL-X",
		RetainDiscovery:     true,
		ValueTemplateSuffix: "",
	}

	ad, err := New(config, "energy/growatt", "ABC123456")
	if err != nil {
		t.Fatalf("Failed to create AutoDiscovery: %v", err)
	}

	data := map[string]interface{}{
		"pvpowerin":     1500.0,
		"pvpowerout":    1450.0,
		"pvtemperature": 35.5,
		"pv1voltage":    240.5,
		"device_type":   "inverter",
		"unknown_field": 123.0, // This should be ignored
	}

	messages := ad.GenerateDiscoveryMessages(data)

	// Check that we got the expected number of messages (4 known fields)
	expectedCount := 4
	if len(messages) != expectedCount {
		t.Errorf("Expected %d discovery messages, got %d", expectedCount, len(messages))
	}

	// Check that unknown fields are ignored
	for topic := range messages {
		if topic == "unknown_field" {
			t.Error("Unknown field should not generate discovery message")
		}
	}

	// Check that discovery topics are properly formatted
	for topic := range messages {
		if !containsString(topic, "homeassistant/sensor/abc123456_inverter/") {
			t.Errorf("Discovery topic should contain expected prefix, got: %s", topic)
		}
	}
}

func TestGetDiscoveryTopic(t *testing.T) {
	config := Config{
		DiscoveryPrefix: "homeassistant",
	}

	ad := &AutoDiscovery{
		config:   config,
		deviceID: "ABC123456",
	}

	fieldName := "pvpower"
	deviceType := "inverter"
	topic := ad.getDiscoveryTopic(fieldName, deviceType)
	expected := "homeassistant/sensor/abc123456_inverter/abc123456_inverter_pvpower/config"

	if topic != expected {
		t.Errorf("Expected topic %s, got %s", expected, topic)
	}
}

func TestCreateDiscoveryMessage(t *testing.T) {
	config := Config{
		DiscoveryPrefix:    "homeassistant",
		DeviceName:         "Test Inverter",
		DeviceManufacturer: "Growatt",
		DeviceModel:        "MIC 600TL-X",
	}

	ad := &AutoDiscovery{
		config:    config,
		deviceID:  "ABC123456",
		baseTopic: "energy/growatt",
	}

	// Initialize layout config
	err := ad.loadLayoutConfig()
	if err != nil {
		t.Fatalf("Failed to load layout config: %v", err)
	}

	sensorConfig := SensorConfig{
		Name:              "PV Power",
		DeviceClass:       "power",
		UnitOfMeasurement: "W",
		StateClass:        "measurement",
		Category:          "pv",
		Icon:              "mdi:solar-power",
	}

	message := ad.createDiscoveryMessage("pvpower", sensorConfig, 1500.0, "MIC123456", "inverter")

	if message == nil {
		t.Fatal("Expected discovery message, got nil")
	}

	if message.Name != "PV Power" {
		t.Errorf("Expected name 'PV Power', got %s", message.Name)
	}

	if message.UniqueID != "ABC123456_inverter_pvpower" {
		t.Errorf("Expected unique ID 'ABC123456_inverter_pvpower', got %s", message.UniqueID)
	}

	if message.StateTopic != "energy/growatt" {
		t.Errorf("Expected state topic 'energy/growatt', got %s", message.StateTopic)
	}

	if message.ValueTemplate != "{{ value_json.pvpower }}" {
		t.Errorf("Expected value template '{{ value_json.pvpower }}', got %s", message.ValueTemplate)
	}

	if message.DeviceClass != "power" {
		t.Errorf("Expected device class 'power', got %s", message.DeviceClass)
	}

	if message.UnitOfMeasurement != "W" {
		t.Errorf("Expected unit 'W', got %s", message.UnitOfMeasurement)
	}

	if message.Device.Name != "Test Inverter (Inverter)" {
		t.Errorf("Expected device name 'Test Inverter (Inverter)', got %s", message.Device.Name)
	}

	if len(message.Device.Identifiers) != 1 || message.Device.Identifiers[0] != "ABC123456_inverter" {
		t.Errorf("Expected device identifier ['ABC123456_inverter'], got %v", message.Device.Identifiers)
	}
}

func TestGetDeviceModel(t *testing.T) {
	tests := []struct {
		configModel string
		pvSerial    string
		expected    string
	}{
		{"Custom Model", "MIC600TLX230001", "Custom Model"}, // Explicit model overrides detection
		{"", "MIC600TLX230001", "MIC 600TL-X"},              // Auto-detect MIC 600TL-X
		{"", "SPH5000TL3BH230001", "SPH 5000TL3"},           // Auto-detect SPH 5000TL3
		{"", "MAX50KTL3LV230001", "MAX 50KTL3 LV"},          // Auto-detect MAX 50KTL3
		{"", "ABC123456", "Growatt Inverter"},               // Unknown serial format
	}

	for _, test := range tests {
		config := Config{DeviceModel: test.configModel}
		ad := &AutoDiscovery{
			config: config,
		}

		result := ad.getDeviceModelFromSerial(test.pvSerial)
		if result != test.expected {
			t.Errorf("For pvSerial %s and configModel %s, expected %s, got %s",
				test.pvSerial, test.configModel, test.expected, result)
		}
	}
}

func TestGetAvailabilityTopic(t *testing.T) {
	ad := &AutoDiscovery{
		baseTopic: "energy/growatt/ABC123456",
	}

	topic := ad.GetAvailabilityTopic()
	expected := "energy/growatt/ABC123456/availability"

	if topic != expected {
		t.Errorf("Expected availability topic %s, got %s", expected, topic)
	}
}

func TestCreateAvailabilityMessage(t *testing.T) {
	ad := &AutoDiscovery{}

	onlineMsg := ad.CreateAvailabilityMessage(true)
	if onlineMsg != "online" {
		t.Errorf("Expected 'online', got %s", onlineMsg)
	}

	offlineMsg := ad.CreateAvailabilityMessage(false)
	if offlineMsg != "offline" {
		t.Errorf("Expected 'offline', got %s", offlineMsg)
	}
}

func TestCleanupDiscoveryMessages(t *testing.T) {
	config := Config{DiscoveryPrefix: "homeassistant"}
	ad := &AutoDiscovery{
		config:   config,
		deviceID: "ABC123456",
	}

	fieldNames := []string{"pvpower", "outputpower"}
	deviceType := "inverter"
	messages := ad.CleanupDiscoveryMessages(fieldNames, deviceType)

	if len(messages) != 2 {
		t.Errorf("Expected 2 cleanup messages, got %d", len(messages))
	}

	for topic, payload := range messages {
		if payload != "" {
			t.Errorf("Expected empty payload for cleanup, got %s", payload)
		}
		if !containsString(topic, "homeassistant/sensor/abc123456_inverter/") {
			t.Errorf("Cleanup topic should contain expected prefix with device type, got: %s", topic)
		}
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findString(s, substr) >= 0)
}

// Helper function to find substring
func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestGetDeviceModelFromSerial(t *testing.T) {
	config := Config{
		DeviceName:         "Test Inverter",
		DeviceManufacturer: "Growatt",
	}

	ad := &AutoDiscovery{
		config: config,
	}

	tests := []struct {
		name     string
		pvSerial string
		expected string
	}{
		{
			name:     "MIC 600TL-X series",
			pvSerial: "MIC600TL230001",
			expected: "MIC 600TL-X",
		},
		{
			name:     "MIC 1000TL-X series",
			pvSerial: "MIC1000TL230001",
			expected: "MIC 1000TL-X",
		},
		{
			name:     "MIC series generic",
			pvSerial: "MIC750TL230001",
			expected: "MIC Series",
		},
		{
			name:     "SPH 5000TL3 series",
			pvSerial: "SPH5000TL3BH230001",
			expected: "SPH 5000TL3",
		},
		{
			name:     "SPH 3000TL3 series",
			pvSerial: "SPH3000TL3230001",
			expected: "SPH 3000TL3",
		},
		{
			name:     "SPH series generic",
			pvSerial: "SPH7000TL3230001",
			expected: "SPH Series",
		},
		{
			name:     "TL3-15000 series",
			pvSerial: "TL315000230001",
			expected: "TL3-15000",
		},
		{
			name:     "TL3-20000 series",
			pvSerial: "TL320000230001",
			expected: "TL3-20000",
		},
		{
			name:     "MIN 5000TL-X series",
			pvSerial: "MIN5000TLX230001",
			expected: "MIN 5000TL-X",
		},
		{
			name:     "MIN 3600TL-XE series",
			pvSerial: "MIN3600TLXE230001",
			expected: "MIN 3600TL-XE",
		},
		{
			name:     "MIN series generic",
			pvSerial: "MIN4000TLX230001",
			expected: "MIN Series",
		},
		{
			name:     "MAX 50KTL3 series",
			pvSerial: "MAX50KTL3LV230001",
			expected: "MAX 50KTL3 LV",
		},
		{
			name:     "MAX series generic",
			pvSerial: "MAX75KTL3LV230001",
			expected: "MAX Series",
		},
		{
			name:     "Unknown pattern",
			pvSerial: "XYZ123456",
			expected: "Growatt Inverter",
		},
		{
			name:     "Empty serial",
			pvSerial: "",
			expected: "Growatt Inverter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ad.getDeviceModelFromSerial(tt.pvSerial)
			if result != tt.expected {
				t.Errorf("getDeviceModelFromSerial(%q) = %q, want %q", tt.pvSerial, result, tt.expected)
			}
		})
	}
}

func TestGetDeviceModelFromSerialWithConfiguredModel(t *testing.T) {
	config := Config{
		DeviceName:         "Test Inverter",
		DeviceManufacturer: "Growatt",
		DeviceModel:        "Custom Model",
	}

	ad := &AutoDiscovery{
		config: config,
	}

	// When device model is configured, it should always return the configured model
	result := ad.getDeviceModelFromSerial("MIC600TL230001")
	expected := "Custom Model"

	if result != expected {
		t.Errorf("getDeviceModelFromSerial with configured model = %q, want %q", result, expected)
	}
}

func TestGenerateDiscoveryMessagesWithDifferentDeviceTypes(t *testing.T) {
	config := Config{
		Enabled:             true,
		DiscoveryPrefix:     "homeassistant",
		DeviceName:          "Test Device",
		DeviceManufacturer:  "Growatt",
		DeviceModel:         "MIC 600TL-X",
		RetainDiscovery:     true,
		ValueTemplateSuffix: "",
	}

	// Test with inverter device type
	adInverter, err := New(config, "energy/growatt", "CUK4CBQ05E_inverter")
	if err != nil {
		t.Fatalf("Failed to create AutoDiscovery for inverter: %v", err)
	}

	inverterData := map[string]interface{}{
		"pvpowerin":   1500.0,
		"pvpowerout":  1450.0,
		"device_type": "inverter",
		"pvserial":    "CUK4CBQ05E",
	}

	inverterMessages := adInverter.GenerateDiscoveryMessages(inverterData)

	// Test with smart meter device type
	adSmartMeter, err := New(config, "energy/growatt", "CUK4CBQ05E_smart_meter")
	if err != nil {
		t.Fatalf("Failed to create AutoDiscovery for smart meter: %v", err)
	}

	smartMeterData := map[string]interface{}{
		"pvpowerin":   1200.0,
		"pvpowerout":  1180.0,
		"device_type": "smart_meter",
		"pvserial":    "CUK4CBQ05E",
	}

	smartMeterMessages := adSmartMeter.GenerateDiscoveryMessages(smartMeterData)

	// Verify that topics are different between inverter and smart meter
	inverterTopics := make([]string, 0, len(inverterMessages))
	smartMeterTopics := make([]string, 0, len(smartMeterMessages))

	for topic := range inverterMessages {
		inverterTopics = append(inverterTopics, topic)
		// Should contain inverter device type in topic
		if !containsString(topic, "cuk4cbq05e_inverter") {
			t.Errorf("Inverter discovery topic should contain device type, got: %s", topic)
		}
	}

	for topic := range smartMeterMessages {
		smartMeterTopics = append(smartMeterTopics, topic)
		// Should contain smart_meter device type in topic
		if !containsString(topic, "cuk4cbq05e_smart_meter") {
			t.Errorf("Smart meter discovery topic should contain device type, got: %s", topic)
		}
	}

	// Verify no topic overlap (they should be completely separate)
	for _, inverterTopic := range inverterTopics {
		for _, smartMeterTopic := range smartMeterTopics {
			if inverterTopic == smartMeterTopic {
				t.Errorf("Discovery topics should not overlap between device types. Found duplicate: %s", inverterTopic)
			}
		}
	}

	// Verify device identifiers are different
	for _, inverterMsg := range inverterMessages {
		for _, smartMeterMsg := range smartMeterMessages {
			if len(inverterMsg.Device.Identifiers) > 0 && len(smartMeterMsg.Device.Identifiers) > 0 {
				if inverterMsg.Device.Identifiers[0] == smartMeterMsg.Device.Identifiers[0] {
					t.Errorf("Device identifiers should be different between device types. Both use: %s", inverterMsg.Device.Identifiers[0])
				}
			}
		}
	}

	// Verify unique IDs are different
	for _, inverterMsg := range inverterMessages {
		for _, smartMeterMsg := range smartMeterMessages {
			if inverterMsg.UniqueID == smartMeterMsg.UniqueID {
				t.Errorf("Unique IDs should be different between device types. Both use: %s", inverterMsg.UniqueID)
			}
		}
	}

	t.Logf("Generated %d inverter discovery topics and %d smart meter discovery topics",
		len(inverterMessages), len(smartMeterMessages))
}
