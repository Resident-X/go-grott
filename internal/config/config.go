// Package config provides configuration management for the go-grott application.
package config

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	// General settings
	LogLevel     string `mapstructure:"log_level"`
	MinRecordLen int    `mapstructure:"min_record_length"`
	Decrypt      bool   `mapstructure:"decrypt"`
	InverterType string `mapstructure:"inverter_type"`
	TimeZone     string `mapstructure:"timezone"`

	// Server settings
	Server struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	} `mapstructure:"server"`

	// HTTP API settings
	API struct {
		Enabled bool   `mapstructure:"enabled"`
		Host    string `mapstructure:"host"`
		Port    int    `mapstructure:"port"`
	} `mapstructure:"api"`

	// MQTT settings
	MQTT struct {
		Enabled           bool   `mapstructure:"enabled"`
		Host              string `mapstructure:"host"`
		Port              int    `mapstructure:"port"`
		Username          string `mapstructure:"username"`
		Password          string `mapstructure:"password"`
		Topic             string `mapstructure:"topic"`
		IncludeInverterID bool   `mapstructure:"include_inverter_id"`
		Retain            bool   `mapstructure:"retain"`
		PublishRaw        bool   `mapstructure:"publish_raw"`

		// Home Assistant Auto-Discovery settings
		HomeAssistantAutoDiscovery struct {
			Enabled             bool   `mapstructure:"enabled"`
			DiscoveryPrefix     string `mapstructure:"discovery_prefix"`
			DeviceName          string `mapstructure:"device_name"`
			DeviceManufacturer  string `mapstructure:"device_manufacturer"`
			DeviceModel         string `mapstructure:"device_model"`
			RetainDiscovery     bool   `mapstructure:"retain_discovery"`
			IncludeDiagnostic   bool   `mapstructure:"include_diagnostic"`
			IncludeBattery      bool   `mapstructure:"include_battery"`
			IncludeGrid         bool   `mapstructure:"include_grid"`
			IncludePV           bool   `mapstructure:"include_pv"`
			ValueTemplateSuffix string `mapstructure:"value_template_suffix"`
			ListenToBirthMessage bool  `mapstructure:"listen_to_birth_message"`
			RediscoveryInterval int   `mapstructure:"rediscovery_interval_hours"`
		} `mapstructure:"homeassistant_autodiscovery"`
	} `mapstructure:"mqtt"`

	// PVOutput settings
	PVOutput struct {
		Enabled            bool                    `mapstructure:"enabled"`
		APIKey             string                  `mapstructure:"api_key"`
		SystemID           string                  `mapstructure:"system_id"`
		UseInverterTemp    bool                    `mapstructure:"use_inverter_temp"`
		DisableEnergyToday bool                    `mapstructure:"disable_energy_today"`
		UpdateLimitMinutes int                     `mapstructure:"update_limit_minutes"`
		MultipleInverters  bool                    `mapstructure:"multiple_inverters"`
		InverterMappings   []InverterSystemMapping `mapstructure:"inverter_mappings"`
	} `mapstructure:"pvoutput"`

	// Record whitelist for command filtering
	RecordWhitelist []string `mapstructure:"record_whitelist"`
}

// InverterSystemMapping maps inverter serials to PVOutput system IDs.
type InverterSystemMapping struct {
	InverterSerial string `mapstructure:"inverter_serial"`
	SystemID       string `mapstructure:"system_id"`
}

// DefaultConfig returns a configuration with default values.
func DefaultConfig() *Config {
	cfg := &Config{
		LogLevel:     "info",
		MinRecordLen: 100,
		Decrypt:      true,
		InverterType: "default",
		TimeZone:     "UTC",
	}

	// Default server settings
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 5279

	// Default API settings
	cfg.API.Enabled = true
	cfg.API.Host = "0.0.0.0"
	cfg.API.Port = 8080

	// Default MQTT settings
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = 1883
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = false
	cfg.MQTT.Retain = false
	cfg.MQTT.PublishRaw = true

	// Default Home Assistant Auto-Discovery settings
	cfg.MQTT.HomeAssistantAutoDiscovery.Enabled = false
	cfg.MQTT.HomeAssistantAutoDiscovery.DiscoveryPrefix = "homeassistant"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceName = "Growatt Inverter"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceManufacturer = "Growatt"
	cfg.MQTT.HomeAssistantAutoDiscovery.DeviceModel = ""
	cfg.MQTT.HomeAssistantAutoDiscovery.RetainDiscovery = true
	cfg.MQTT.HomeAssistantAutoDiscovery.IncludeDiagnostic = true
	cfg.MQTT.HomeAssistantAutoDiscovery.IncludeBattery = true
	cfg.MQTT.HomeAssistantAutoDiscovery.IncludeGrid = true
	cfg.MQTT.HomeAssistantAutoDiscovery.IncludePV = true
	cfg.MQTT.HomeAssistantAutoDiscovery.ValueTemplateSuffix = ""
	cfg.MQTT.HomeAssistantAutoDiscovery.ListenToBirthMessage = true
	cfg.MQTT.HomeAssistantAutoDiscovery.RediscoveryInterval = 24 // 24 hours

	// Default PVOutput settings
	cfg.PVOutput.Enabled = false
	cfg.PVOutput.UpdateLimitMinutes = 5 // 5 minutes between updates

	// Default record whitelist
	cfg.RecordWhitelist = []string{
		"0103", "0104", "0116", "0105", "0119", "0120", "0150",
		"5003", "5004", "5016", "5005", "5019", "501b", "5050",
		"5103", "5104", "5116", "5105", "5119", "5129", "5150",
		"5103", "5104", "5216", "5105", "5219", "5229", "5250",
	}

	return cfg
}

// Load reads the configuration from a file and environment variables.
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Set up Viper
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")

	// Override with specific config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// Config file not found, use defaults
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			fmt.Println("No configuration file found, using defaults")
		} else {
			// Other errors (like invalid YAML) should be returned
			return nil, fmt.Errorf("error reading config: %w", err)
		}
	}

	// Bind environment variables
	v.SetEnvPrefix("GROTT")
	v.AutomaticEnv()

	// Unmarshal config
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unable to decode config: %w", err)
	}

	return cfg, nil
}

// Print displays the current configuration.
func (c *Config) Print() {
	logger := log.With().Str("component", "config").Logger()
	logger.Info().Msg("go-grott Server Configuration:")
	logger.Info().Msg("-----------------------------")
	logger.Info().Str("log_level", c.LogLevel).Msg("Log Level")
	logger.Info().Int("min_record_length", c.MinRecordLen).Msg("Minimum Record Length")
	logger.Info().Bool("decrypt", c.Decrypt).Msg("Decrypt")
	logger.Info().Str("inverter_type", c.InverterType).Msg("Inverter Type")
	logger.Info().Str("timezone", c.TimeZone).Msg("Timezone")

	logger.Info().
		Str("host", c.Server.Host).
		Int("port", c.Server.Port).
		Msg("Server")

	logger.Info().Bool("enabled", c.API.Enabled).Msg("API Enabled")
	if c.API.Enabled {
		logger.Info().
			Str("host", c.API.Host).
			Int("port", c.API.Port).
			Msg("API Server")
	}

	logger.Info().Bool("enabled", c.MQTT.Enabled).Msg("MQTT Enabled")
	if c.MQTT.Enabled {
		logger.Info().
			Str("host", c.MQTT.Host).
			Int("port", c.MQTT.Port).
			Str("topic", c.MQTT.Topic).
			Bool("include_inverter_id", c.MQTT.IncludeInverterID).
			Bool("publish_raw", c.MQTT.PublishRaw).
			Bool("homeassistant_autodiscovery_enabled", c.MQTT.HomeAssistantAutoDiscovery.Enabled).
			Msg("MQTT Configuration")
	}

	logger.Info().Bool("enabled", c.PVOutput.Enabled).Msg("PVOutput Enabled")
	if c.PVOutput.Enabled {
		logger.Info().
			Str("system_id", c.PVOutput.SystemID).
			Int("update_limit_minutes", c.PVOutput.UpdateLimitMinutes).
			Msg("PVOutput Configuration")
	}

	logger.Info().Msg("-----------------------------")
}
