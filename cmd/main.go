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

	// Initialize MQTT publisher with retry logic and continuous reconnection
	var publisher domain.MessagePublisher
	if cfg.MQTT.Enabled {
		mqttPublisher := pubsub.NewMQTTPublisher(cfg)

		// Retry connection with exponential backoff
		maxRetries := cfg.MQTT.ConnectionRetryAttempts
		baseDelay := time.Duration(cfg.MQTT.ConnectionRetryBaseDelay) * time.Second
		var connectionErr error

		for attempt := 0; attempt < maxRetries; attempt++ {
			// Apply exponential backoff delay for retry attempts (not first attempt)
			if attempt > 0 {
				// Exponential backoff: baseDelay * 2^(attempt-1)
				// e.g., with baseDelay=2s: 2s, 4s, 8s, 16s
				delay := baseDelay * time.Duration(1<<uint(attempt-1))
				log.Info().
					Int("attempt", attempt+1).
					Int("max_attempts", maxRetries).
					Dur("delay", delay).
					Msg("Retrying MQTT connection after delay")
				time.Sleep(delay)
			}

			log.Info().
				Int("attempt", attempt+1).
				Int("max_attempts", maxRetries).
				Str("broker", fmt.Sprintf("%s:%d", cfg.MQTT.Host, cfg.MQTT.Port)).
				Msg("Attempting MQTT connection")

			connectionErr = mqttPublisher.Connect(ctx)
			if connectionErr == nil {
				// Connection successful
				log.Info().
					Int("attempt", attempt+1).
					Msg("MQTT publisher connected successfully")
				break
			}

			// Log the failure
			log.Warn().
				Err(connectionErr).
				Int("attempt", attempt+1).
				Int("max_attempts", maxRetries).
				Msg("Failed to connect to MQTT broker")
		}

		// Even if initial connection failed, use MQTT publisher and start reconnection loop
		// The background loop will keep trying to connect indefinitely
		publisher = mqttPublisher

		if connectionErr != nil {
			log.Warn().
				Err(connectionErr).
				Int("total_attempts", maxRetries).
				Msg("Initial MQTT connection failed, but reconnection loop will continue trying")
		}

		// Start background reconnection loop - this runs forever and will heal from any outage
		mqttPublisher.StartReconnectionLoop(ctx)
		log.Info().Msg("MQTT reconnection loop started (will automatically reconnect if connection lost)")
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
