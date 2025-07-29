// Package main provides the entry point for the refactored go-grott server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/parser"
	"github.com/resident-x/go-grott/internal/pubsub"
	"github.com/resident-x/go-grott/internal/service"
	pvoutput "github.com/resident-x/go-grott/internal/service/pvoutput"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	Version = "unknown" // Default version, can be overridden by build flags
)

func main() {
	code := run() // run() returns an int
	os.Exit(code) // os.Exit is called after deferred functions in run() execute
}

func run() int {
	// Parse command line flags
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	// Show version if requested
	if *showVersion {
		fmt.Printf("go-grott server %s\n", Version)
		return 0
	}

	// Initialize context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		return 1
	}

	// Initialize logger with the configured log level
	initLogger(cfg.LogLevel)

	log.Info().Str("version", Version).Msg("Starting go-grott server")

	// Log service configuration for debugging
	logServiceConfiguration(cfg)

	// Initialize parser
	dataParser, err := parser.NewParser(cfg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize parser")
		return 1
	}

	// Initialize MQTT publisher
	var publisher domain.MessagePublisher
	if cfg.MQTT.Enabled {
		mqttPublisher := pubsub.NewMQTTPublisher(cfg)
		if err := mqttPublisher.Connect(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to connect to MQTT broker, using noop publisher")
			publisher = pubsub.NewNoopPublisher()
		} else {
			publisher = mqttPublisher
			log.Info().Msg("MQTT publisher connected successfully")
		}
	} else {
		log.Info().Msg("MQTT disabled, using noop publisher")
		publisher = pubsub.NewNoopPublisher()
	}

	// Initialize PVOutput service
	var monitoringService domain.MonitoringService
	if cfg.PVOutput.Enabled {
		pvoutClient := pvoutput.NewClient(cfg)
		if err := pvoutClient.Connect(); err != nil {
			log.Warn().Err(err).Msg("Failed to initialize PVOutput client")
			monitoringService = pvoutput.NewNoopClient()
		} else {
			monitoringService = pvoutClient
		}
	} else {
		// Use NoopClient when PVOutput is disabled
		monitoringService = pvoutput.NewNoopClient()
	}

	// Create and start data collection server
	srv, err := service.NewDataCollectionServer(cfg, dataParser, publisher, monitoringService)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create data collection server")
		return 1
	}

	// Start the data collection server
	if err := srv.Start(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to start data collection server")
		return 1
	}

	log.Info().
		Str("host", cfg.Server.Host).
		Int("port", cfg.Server.Port).
		Msg("Data collection server started successfully")

	// Handle graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-signalChan
	log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	// Create context with timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Stop the server
	if err := srv.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error stopping server")
		return 1
	}

	log.Info().Msg("Server stopped")
	return 0
}

// initLogger configures the global zerolog logger.
func initLogger(level string) {
	// Set up pretty console logging for development
	output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}

	// Parse the log level
	logLevel, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		fmt.Printf("Invalid log level '%s', defaulting to 'info'\n", level)
		logLevel = zerolog.InfoLevel
	}

	// Configure global logger
	zerolog.SetGlobalLevel(logLevel)
	log.Logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()
}

// logServiceConfiguration logs the current service configuration for debugging.
func logServiceConfiguration(cfg *config.Config) {
	log.Debug().Msg("=== Service Configuration ===")
	
	// General settings
	log.Debug().
		Str("log_level", cfg.LogLevel).
		Str("inverter_type", cfg.InverterType).
		Str("timezone", cfg.TimeZone).
		Bool("decrypt", cfg.Decrypt).
		Int("min_record_length", cfg.MinRecordLen).
		Msg("General settings")

	// Server settings
	log.Debug().
		Str("host", cfg.Server.Host).
		Int("port", cfg.Server.Port).
		Msg("TCP server configuration")

	// API settings
	log.Debug().
		Bool("enabled", cfg.API.Enabled).
		Str("host", cfg.API.Host).
		Int("port", cfg.API.Port).
		Msg("HTTP API configuration")

	// MQTT settings
	if cfg.MQTT.Enabled {
		log.Debug().
			Bool("enabled", cfg.MQTT.Enabled).
			Str("host", cfg.MQTT.Host).
			Int("port", cfg.MQTT.Port).
			Str("username", cfg.MQTT.Username).
			Str("topic", cfg.MQTT.Topic).
			Bool("include_inverter_id", cfg.MQTT.IncludeInverterID).
			Bool("retain", cfg.MQTT.Retain).
			Bool("publish_raw", cfg.MQTT.PublishRaw).
			Msg("MQTT configuration")

		// Home Assistant Auto-Discovery
		ha := cfg.MQTT.HomeAssistantAutoDiscovery
		if ha.Enabled {
			log.Debug().
				Bool("enabled", ha.Enabled).
				Str("discovery_prefix", ha.DiscoveryPrefix).
				Str("device_name", ha.DeviceName).
				Str("device_manufacturer", ha.DeviceManufacturer).
				Str("device_model", ha.DeviceModel).
				Bool("retain_discovery", ha.RetainDiscovery).
				Msg("Home Assistant auto-discovery configuration")
		} else {
			log.Debug().Bool("enabled", false).Msg("Home Assistant auto-discovery disabled")
		}
	} else {
		log.Debug().Bool("enabled", false).Msg("MQTT disabled")
	}

	// PVOutput settings
	if cfg.PVOutput.Enabled {
		log.Debug().
			Bool("enabled", cfg.PVOutput.Enabled).
			Str("system_id", cfg.PVOutput.SystemID).
			Bool("use_inverter_temp", cfg.PVOutput.UseInverterTemp).
			Bool("disable_energy_today", cfg.PVOutput.DisableEnergyToday).
			Int("update_limit_minutes", cfg.PVOutput.UpdateLimitMinutes).
			Bool("multiple_inverters", cfg.PVOutput.MultipleInverters).
			Int("inverter_mappings_count", len(cfg.PVOutput.InverterMappings)).
			Msg("PVOutput configuration")
	} else {
		log.Debug().Bool("enabled", false).Msg("PVOutput disabled")
	}

	// Record whitelist
	if len(cfg.RecordWhitelist) > 0 {
		log.Debug().
			Strs("whitelist", cfg.RecordWhitelist).
			Msg("Record whitelist configured")
	} else {
		log.Debug().Msg("No record whitelist configured (all records allowed)")
	}

	log.Debug().Msg("=== End Configuration ===")
}
