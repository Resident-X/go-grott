package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 100, cfg.MinRecordLen)
	assert.Equal(t, true, cfg.Decrypt)
	assert.Equal(t, "default", cfg.InverterType)
	assert.Equal(t, "UTC", cfg.TimeZone)

	// Server defaults
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 5279, cfg.Server.Port)

	// API defaults
	assert.Equal(t, true, cfg.API.Enabled)
	assert.Equal(t, "0.0.0.0", cfg.API.Host)
	assert.Equal(t, 8080, cfg.API.Port)

	// MQTT defaults
	assert.Equal(t, true, cfg.MQTT.Enabled)
	assert.Equal(t, "localhost", cfg.MQTT.Host)
	assert.Equal(t, 1883, cfg.MQTT.Port)
	assert.Equal(t, "energy/growatt", cfg.MQTT.Topic)
	assert.Equal(t, "energy/meter", cfg.MQTT.SmartMeterTopic)
	assert.Equal(t, true, cfg.MQTT.UseSmartMeterTopic)
	assert.Equal(t, false, cfg.MQTT.IncludeInverterID)
	assert.Equal(t, false, cfg.MQTT.Retain)
	assert.Equal(t, 5, cfg.MQTT.ConnectionRetryAttempts)
	assert.Equal(t, 2, cfg.MQTT.ConnectionRetryBaseDelay)
	assert.Equal(t, 10, cfg.MQTT.ConnectionTimeout)

	// PVOutput defaults
	assert.Equal(t, false, cfg.PVOutput.Enabled)
	assert.Equal(t, 5, cfg.PVOutput.UpdateLimitMinutes)

	// Record whitelist
	assert.NotEmpty(t, cfg.RecordWhitelist)
	assert.Contains(t, cfg.RecordWhitelist, "0103")
	assert.Contains(t, cfg.RecordWhitelist, "0104")
}

func TestLoadConfigWithNonExistentFile(t *testing.T) {
	_, err := Load("nonexistent_config.yaml")

	// Should error when file doesn't exist
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error reading config")
}

func TestLoadConfigWithValidYAML(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test_config.yaml")

	configContent := `
log_level: debug
min_record_length: 200
decrypt: false
inverter_type: test
timezone: EST
server:
  host: 127.0.0.1
  port: 9999
api:
  enabled: false
  host: 192.168.1.1
  port: 9000
mqtt:
  enabled: false
  host: mqtt.example.com
  port: 8883
  username: testuser
  password: testpass
  topic: test/topic
  smart_meter_topic: test/meter
  use_smart_meter_topic: false
  include_inverter_id: true
  retain: true
  connection_retry_attempts: 3
  connection_retry_base_delay_seconds: 5
  connection_timeout_seconds: 15
pvoutput:
  enabled: true
  api_key: test_api_key
  system_id: test_system_id
  use_inverter_temp: true
  disable_energy_today: true
  update_limit_minutes: 10
  multiple_inverters: true
  inverter_mappings:
    - inverter_serial: "INV001"
      system_id: "SYS001"
    - inverter_serial: "INV002"
      system_id: "SYS002"
record_whitelist:
  - "test1"
  - "test2"
`

	err := os.WriteFile(configFile, []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 200, cfg.MinRecordLen)
	assert.Equal(t, false, cfg.Decrypt)
	assert.Equal(t, "test", cfg.InverterType)
	assert.Equal(t, "EST", cfg.TimeZone)

	// Server config
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9999, cfg.Server.Port)

	// API config
	assert.Equal(t, false, cfg.API.Enabled)
	assert.Equal(t, "192.168.1.1", cfg.API.Host)
	assert.Equal(t, 9000, cfg.API.Port)

	// MQTT config
	assert.Equal(t, false, cfg.MQTT.Enabled)
	assert.Equal(t, "mqtt.example.com", cfg.MQTT.Host)
	assert.Equal(t, 8883, cfg.MQTT.Port)
	assert.Equal(t, "testuser", cfg.MQTT.Username)
	assert.Equal(t, "testpass", cfg.MQTT.Password)
	assert.Equal(t, "test/topic", cfg.MQTT.Topic)
	assert.Equal(t, "test/meter", cfg.MQTT.SmartMeterTopic)
	assert.Equal(t, false, cfg.MQTT.UseSmartMeterTopic)
	assert.Equal(t, true, cfg.MQTT.IncludeInverterID)
	assert.Equal(t, true, cfg.MQTT.Retain)
	assert.Equal(t, 3, cfg.MQTT.ConnectionRetryAttempts)
	assert.Equal(t, 5, cfg.MQTT.ConnectionRetryBaseDelay)
	assert.Equal(t, 15, cfg.MQTT.ConnectionTimeout)

	// PVOutput config
	assert.Equal(t, true, cfg.PVOutput.Enabled)
	assert.Equal(t, "test_api_key", cfg.PVOutput.APIKey)
	assert.Equal(t, "test_system_id", cfg.PVOutput.SystemID)
	assert.Equal(t, true, cfg.PVOutput.UseInverterTemp)
	assert.Equal(t, true, cfg.PVOutput.DisableEnergyToday)
	assert.Equal(t, 10, cfg.PVOutput.UpdateLimitMinutes)
	assert.Equal(t, true, cfg.PVOutput.MultipleInverters)

	// Inverter mappings
	require.Len(t, cfg.PVOutput.InverterMappings, 2)
	assert.Equal(t, "INV001", cfg.PVOutput.InverterMappings[0].InverterSerial)
	assert.Equal(t, "SYS001", cfg.PVOutput.InverterMappings[0].SystemID)
	assert.Equal(t, "INV002", cfg.PVOutput.InverterMappings[1].InverterSerial)
	assert.Equal(t, "SYS002", cfg.PVOutput.InverterMappings[1].SystemID)

	// Other config - Note: RecordWhitelist may be modified by layout loading, so just check it contains the originals
	assert.Contains(t, cfg.RecordWhitelist, "test1")
	assert.Contains(t, cfg.RecordWhitelist, "test2")
}

func TestLoadConfigWithInvalidYAML(t *testing.T) {
	// Create a temporary invalid config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "invalid_config.yaml")

	invalidContent := `
invalid: yaml: content: [
`

	err := os.WriteFile(configFile, []byte(invalidContent), 0o644)
	require.NoError(t, err)

	_, err = Load(configFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error reading config")
}

func TestPrint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LogLevel = "debug"
	cfg.Server.Host = "test.example.com"
	cfg.Server.Port = 1234

	// This test mainly ensures Print() doesn't panic
	// In a real test environment, you might want to capture the output
	assert.NotPanics(t, func() {
		cfg.Print()
	})
}
