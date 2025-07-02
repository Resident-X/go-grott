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
	version = "1.0.0"
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
		fmt.Printf("go-grott server %s\n", version)
		return 1
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

	log.Info().Str("version", version).Msg("Starting go-grott server")

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
