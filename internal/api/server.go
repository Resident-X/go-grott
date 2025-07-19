// Package api provides HTTP API functionality for the go-grott server.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Server represents the HTTP API server that provides monitoring and management functionality.
type Server struct {
	config    *config.Config
	server    *http.Server
	router    *mux.Router
	registry  domain.Registry
	logger    zerolog.Logger
	startTime time.Time
}

// NewServer creates a new HTTP API server.
func NewServer(cfg *config.Config, registry domain.Registry) *Server {
	router := mux.NewRouter()

	// Create logger with API component context
	logger := log.With().Str("component", "api").Logger()

	// Create API server instance
	apiServer := &Server{
		config:    cfg,
		router:    router,
		registry:  registry,
		logger:    logger,
		startTime: time.Now(),
	}

	// Set up API routes
	apiServer.setupRoutes()

	return apiServer
}

// setupRoutes configures all API endpoint handlers.
func (s *Server) setupRoutes() {
	// API versioning
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Server status endpoint
	api.HandleFunc("/status", s.handleStatus).Methods("GET")

	// Dataloggers endpoints
	api.HandleFunc("/dataloggers", s.handleListDataloggers).Methods("GET")
	api.HandleFunc("/dataloggers/{id}", s.handleGetDatalogger).Methods("GET")
	api.HandleFunc("/dataloggers/{id}/inverters", s.handleListInverters).Methods("GET")
}

// Start begins listening for HTTP requests.
func (s *Server) Start(_ context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.API.Host, s.config.API.Port)

	// Create HTTP server
	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		s.logger.Info().
			Str("host", s.config.API.Host).
			Int("port", s.config.API.Port).
			Msg("Starting HTTP API server")

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info().Msg("Stopping HTTP API server")

	// Create a timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if s.server != nil {
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP server shutdown error: %w", err)
		}
	}

	return nil
}

// handleStatus returns server status information.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := map[string]interface{}{
		"status":          "ok",
		"version":         "dev",
		"uptime":          time.Since(s.startTime).String(),
		"dataloggerCount": len(s.registry.GetAllDataloggers()),
	}

	s.writeJSON(w, status, http.StatusOK)
}

// handleListDataloggers returns a list of all dataloggers.
func (s *Server) handleListDataloggers(w http.ResponseWriter, _ *http.Request) {
	dataloggers := s.registry.GetAllDataloggers()

	result := make([]map[string]interface{}, 0, len(dataloggers))
	for _, dl := range dataloggers {
		result = append(result, map[string]interface{}{
			"id":            dl.ID,
			"ip":            dl.IP,
			"port":          dl.Port,
			"protocol":      dl.Protocol,
			"lastContact":   dl.LastContact,
			"inverterCount": len(dl.Inverters),
		})
	}

	s.writeJSON(w, map[string]interface{}{
		"dataloggers": result,
		"count":       len(result),
	}, http.StatusOK)
}

// handleGetDatalogger returns information about a specific datalogger.
func (s *Server) handleGetDatalogger(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	datalogger, found := s.registry.GetDatalogger(id)
	if !found {
		s.writeError(w, "Datalogger not found", http.StatusNotFound)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"id":            datalogger.ID,
		"ip":            datalogger.IP,
		"port":          datalogger.Port,
		"protocol":      datalogger.Protocol,
		"lastContact":   datalogger.LastContact,
		"inverterCount": len(datalogger.Inverters),
	}, http.StatusOK)
}

// handleListInverters returns a list of inverters for a specific datalogger.
func (s *Server) handleListInverters(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	inverters, found := s.registry.GetInverters(id)
	if !found {
		s.writeError(w, "Datalogger not found", http.StatusNotFound)
		return
	}

	result := make([]map[string]interface{}, 0, len(inverters))
	for _, inv := range inverters {
		result = append(result, map[string]interface{}{
			"serial":      inv.Serial,
			"inverterNo":  inv.InverterNo,
			"lastContact": inv.LastContact,
		})
	}

	s.writeJSON(w, map[string]interface{}{
		"datalogger": id,
		"inverters":  result,
		"count":      len(result),
	}, http.StatusOK)
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResponse := map[string]string{"error": message}
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode error response")
	}
}
