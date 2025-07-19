// Package api provides HTTP API functionality for the go-grott server.
package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// API Server constants
const (
	MaxRegisterValue      = 4096
	DefaultTimeout        = 5 * time.Second
	LongTimeout          = 10 * time.Second
	ShutdownTimeout      = 5 * time.Second
	ResponseCheckInterval = 500 * time.Millisecond
	MinProtocolDataLen    = 8
	TimeSyncRegister     = 31
	
	// Command constants
	CommandRegister      = "register"
	CommandRegAll        = "regall"
	CommandMultiRegister = "multiregister"
	CommandDateTime      = "datetime"
	
	// Additional constants not defined in protocol package yet
	// TODO: Move these to protocol package eventually
)

// Custom error types for better error handling
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error on field %s: %s", e.Field, e.Message)
}

type APIError struct {
	Code    int
	Message string
	Cause   error
}

func (e APIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("API error (%d): %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("API error (%d): %s", e.Code, e.Message)
}

func (e APIError) Unwrap() error {
	return e.Cause
}

// CommandResponse represents a command response in the tracking system.
type CommandResponse struct {
	Value  interface{} `json:"value,omitempty"`
	Result string      `json:"result,omitempty"`
}

// RegisterValue represents a register-value pair for multi-register operations.
type RegisterValue struct {
	Register uint16 `json:"register"`
	Value    int    `json:"value"`
}

// ResponseTracker tracks command responses similar to Python's commandresponse dictionary.
type ResponseTracker struct {
	responses map[string]map[string]*CommandResponse
	mutex     sync.RWMutex
}

// NewResponseTracker creates a new response tracker.
func NewResponseTracker() *ResponseTracker {
	return &ResponseTracker{
		responses: make(map[string]map[string]*CommandResponse),
	}
}

// SetResponse stores a response for a given command type and register key.
func (rt *ResponseTracker) SetResponse(cmdType, registerKey string, response *CommandResponse) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	if rt.responses[cmdType] == nil {
		rt.responses[cmdType] = make(map[string]*CommandResponse)
	}
	rt.responses[cmdType][registerKey] = response
}

// GetResponse retrieves a response for a given command type and register key.
func (rt *ResponseTracker) GetResponse(cmdType, registerKey string) (*CommandResponse, bool) {
	rt.mutex.RLock()
	defer rt.mutex.RUnlock()

	if cmdResponses, exists := rt.responses[cmdType]; exists {
		if response, exists := cmdResponses[registerKey]; exists {
			return response, true
		}
	}
	return nil, false
}

// DeleteResponse removes a response for a given command type and register key.
func (rt *ResponseTracker) DeleteResponse(cmdType, registerKey string) {
	rt.mutex.Lock()
	defer rt.mutex.Unlock()

	if cmdResponses, exists := rt.responses[cmdType]; exists {
		delete(cmdResponses, registerKey)
	}
}

// GetAllResponses returns all responses for a given command type.
func (rt *ResponseTracker) GetAllResponses(cmdType string) map[string]*CommandResponse {
	rt.mutex.RLock()
	defer rt.mutex.RUnlock()

	if cmdResponses, exists := rt.responses[cmdType]; exists {
		// Return a copy to avoid race conditions
		result := make(map[string]*CommandResponse)
		for k, v := range cmdResponses {
			result[k] = v
		}
		return result
	}
	return make(map[string]*CommandResponse)
}

// Server represents the HTTP API server that provides monitoring and management functionality.
type Server struct {
	config             *config.Config
	server             *http.Server
	router             *mux.Router
	registry           domain.Registry
	logger             zerolog.Logger
	startTime          time.Time
	responseTracker    *ResponseTracker
	commandBuilder     *protocol.CommandBuilder
	commandQueueMgr    *CommandQueueManager
	responseProcessor  *ResponseProcessor
	connectionTracker  *DeviceConnectionTracker
	formatConverter    *FormatConverter
	requestTimeout     time.Duration // Configurable timeout for testing
	shutdown           bool
	shutdownMu         sync.RWMutex
}

// NewServer creates a new HTTP API server.
func NewServer(cfg *config.Config, registry domain.Registry) *Server {
	router := mux.NewRouter()

	// Create logger with API component context
	logger := log.With().Str("component", "api").Logger()

	// Create response tracker and processor
	responseTracker := NewResponseTracker()
	responseProcessor := NewResponseProcessor(responseTracker, logger)

	// Create API server instance
	apiServer := &Server{
		config:            cfg,
		router:            router,
		registry:          registry,
		logger:            logger,
		startTime:         time.Now(),
		responseTracker:   responseTracker,
		commandBuilder:    protocol.NewCommandBuilder(),
		commandQueueMgr:   NewCommandQueueManager(logger),
		responseProcessor: responseProcessor,
		connectionTracker: NewDeviceConnectionTracker(logger),
		formatConverter:   NewFormatConverter(),
		requestTimeout:    10 * time.Second, // Default timeout
	}

	// Set up API routes
	apiServer.setupRoutes()

	return apiServer
}

// GetRouter returns the HTTP router for testing purposes
func (s *Server) GetRouter() *mux.Router {
	return s.router
}

// GetCommandQueueManager returns the command queue manager for testing purposes
func (s *Server) GetCommandQueueManager() *CommandQueueManager {
	return s.commandQueueMgr
}

// SetRequestTimeout sets the timeout for device requests (primarily for testing)
func (s *Server) SetRequestTimeout(timeout time.Duration) {
	s.requestTimeout = timeout
}

// GetResponseTracker returns the response tracker for testing purposes
func (s *Server) GetResponseTracker() *ResponseTracker {
	return s.responseTracker
}

// IsShutdown returns whether the server is shutting down
func (s *Server) IsShutdown() bool {
	s.shutdownMu.RLock()
	defer s.shutdownMu.RUnlock()
	return s.shutdown
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

	// Python grottserver compatibility endpoints
	s.router.HandleFunc("/", s.handleHome).Methods("GET")
	s.router.HandleFunc("/info", s.handleInfo).Methods("GET")
	
	// Datalogger register operations
	s.router.HandleFunc("/datalogger", s.handleDataloggerGet).Methods("GET")
	s.router.HandleFunc("/datalogger", s.handleDataloggerPut).Methods("PUT")
	
	// Inverter register operations  
	s.router.HandleFunc("/inverter", s.handleInverterGet).Methods("GET")
	s.router.HandleFunc("/inverter", s.handleInverterPut).Methods("PUT")
	
	// Multi-register operations
	s.router.HandleFunc("/multiregister", s.handleMultiregisterGet).Methods("GET")
	s.router.HandleFunc("/multiregister", s.handleMultiregisterPut).Methods("PUT")
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

	// Set shutdown flag to prevent new requests
	s.shutdownMu.Lock()
	s.shutdown = true
	s.shutdownMu.Unlock()

	// Create a timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, ShutdownTimeout)
	defer cancel()

	if s.server != nil {
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP server shutdown error: %w", err)
		}
	}

	return nil
}

// Validation helpers
func validateRegister(registerStr string) (uint16, error) {
	if registerStr == "" {
		return 0, ValidationError{Field: "register", Message: "no register specified"}
	}

	regVal, err := strconv.Atoi(registerStr)
	if err != nil {
		return 0, ValidationError{Field: "register", Message: "invalid register format"}
	}

	if regVal < 0 || regVal >= MaxRegisterValue {
		return 0, ValidationError{Field: "register", Message: fmt.Sprintf("register value must be between 0 and %d", MaxRegisterValue-1)}
	}

	return uint16(regVal), nil
}

func validateDataloggerID(dataloggerID string) error {
	if dataloggerID == "" {
		return ValidationError{Field: "datalogger", Message: "no datalogger id specified"}
	}
	return nil
}

func validateCommand(command string, validCommands []string) error {
	if command == "" {
		return ValidationError{Field: "command", Message: "no command entered"}
	}

	for _, valid := range validCommands {
		if command == valid {
			return nil
		}
	}

	return ValidationError{Field: "command", Message: fmt.Sprintf("invalid command '%s', must be one of: %v", command, validCommands)}
}

// handleStatus returns server status information.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := map[string]interface{}{
		"status":          "ok",
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

// handleHome serves a simple welcome page.
func (s *Server) handleHome(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	welcomeHTML := `<h2>Welcome to go-grott the Growatt inverter monitor</h2><br><h3>Go implementation based on Grott by Ledidobe, Johan Meijer</h3>`
	if _, err := w.Write([]byte(welcomeHTML)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to write welcome response")
		return
	}
}

// handleInfo returns server information and statistics.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	s.logger.Info().Msg("Server info requested")

	// Get server metrics
	uptime := time.Since(s.startTime)
	dataloggers := s.registry.GetAllDataloggers()
	
	// Build info response
	info := map[string]interface{}{
		"server_name":      "go-grott",
		"server_type":      "Go implementation",
		"uptime_seconds":   uptime.Seconds(),
		"uptime_string":    uptime.String(),
		"start_time":       s.startTime.Format(time.RFC3339),
		"datalogger_count": len(dataloggers),
		"active_connections": s.commandQueueMgr.GetQueueCount(),
		"dataloggers": func() []map[string]interface{} {
			result := make([]map[string]interface{}, 0, len(dataloggers))
			for _, dl := range dataloggers {
				result = append(result, map[string]interface{}{
					"id":       dl.ID,
					"ip":       dl.IP,
					"protocol": dl.Protocol,
					"inverter_count": len(dl.Inverters),
				})
			}
			return result
		}(),
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	infoHTML := "<h2>go-grott Server Info</h2><br><h3>See server logs for detailed information</h3>"
	if _, err := w.Write([]byte(infoHTML)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to write info response")
		return
	}
	
	// Log detailed info to server logs
	s.logger.Info().Interface("server_info", info).Msg("Server information displayed")
}

// handleDataloggerGet handles GET requests for datalogger register operations.
func (s *Server) handleDataloggerGet(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msg("Datalogger GET request received")

	query := r.URL.Query()
	
	// If no query parameters, return datalogger registry info
	if len(query) == 0 {
		dataloggers := s.registry.GetAllDataloggers()
		registryInfo := make(map[string]interface{})
		
		for _, dl := range dataloggers {
			registryInfo[dl.ID] = map[string]interface{}{
				"ip":       dl.IP,
				"port":     dl.Port,
				"protocol": dl.Protocol,
				"inverters": func() map[string]interface{} {
					invs := make(map[string]interface{})
					for _, inv := range dl.Inverters {
						invs[inv.Serial] = map[string]interface{}{
							"inverterno": inv.InverterNo,
						}
					}
					return invs
				}(),
			}
		}
		
		s.writeJSON(w, registryInfo, http.StatusOK)
		return
	}

	// Validate command parameter
	command := query.Get("command")
	if err := validateCommand(command, []string{"register", "regall"}); err != nil {
		var valErr ValidationError
		if errors.As(err, &valErr) {
			s.writeError(w, "no valid command entered", http.StatusBadRequest)
		} else {
			s.writeError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Validate datalogger ID
	dataloggerID := query.Get("datalogger")
	if err := validateDataloggerID(dataloggerID); err != nil {
		s.writeError(w, "no datalogger id specified", http.StatusBadRequest)
		return
	}

	datalogger, found := s.registry.GetDatalogger(dataloggerID)
	if !found {
		s.writeError(w, "invalid datalogger id", http.StatusBadRequest)
		return
	}

	// Handle regall command
	if command == CommandRegAll {
		allResponses := s.responseTracker.GetAllResponses(protocol.ProtocolDataloggerRead)
		s.writeJSON(w, allResponses, http.StatusOK)
		return
	}

	// Handle register command
	if command == CommandRegister {
		registerStr := query.Get("register")
		register, err := validateRegister(registerStr)
		if err != nil {
			var valErr ValidationError
			if errors.As(err, &valErr) {
				s.writeError(w, valErr.Message, http.StatusBadRequest)
			} else {
				s.writeError(w, err.Error(), http.StatusBadRequest)
			}
			return
		}

		// Generate and queue register read command
		if err := s.queueRegisterReadCommand(r.Context(), datalogger, protocol.ProtocolDataloggerRead, register); err != nil {
			s.writeError(w, "failed to queue command", http.StatusInternalServerError)
			s.logger.Error().Err(err).Msg("Failed to queue register read command")
			return
		}

		// Wait for response
		registerKey := fmt.Sprintf("%04x", register)
		response, err := s.waitForResponse(protocol.ProtocolDataloggerRead, registerKey, DefaultTimeout)
		if err != nil {
			s.writeError(w, "no or invalid response received", http.StatusBadRequest)
			s.logger.Warn().Err(err).Msg("No response received for register read")
			return
		}

		s.writeJSON(w, response, http.StatusOK)
		return
	}

	s.writeError(w, "command not defined or not available yet", http.StatusBadRequest)
}

// handleDataloggerPut handles PUT requests for datalogger register operations.
func (s *Server) handleDataloggerPut(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msg("Datalogger PUT request received")

	query := r.URL.Query()
	
	if len(query) == 0 {
		s.writeError(w, "empty put received", http.StatusBadRequest)
		return
	}

	// Validate command parameter
	command := query.Get("command")
	if command == "" {
		s.writeError(w, "no command entered", http.StatusBadRequest)
		return
	}

	if command != "register" && command != "datetime" {
		s.writeError(w, "no valid command entered", http.StatusBadRequest)
		return
	}

	// Validate datalogger ID
	dataloggerID := query.Get("datalogger")
	if dataloggerID == "" {
		s.writeError(w, "no datalogger or inverterid specified", http.StatusBadRequest)
		return
	}

	datalogger, found := s.registry.GetDatalogger(dataloggerID)
	if !found {
		s.writeError(w, "invalid datalogger id", http.StatusBadRequest)
		return
	}

	var register uint16
	var value string

	if command == "register" {
		// Validate register
		registerStr := query.Get("register")
		if registerStr == "" {
			s.writeError(w, "no register specified", http.StatusBadRequest)
			return
		}

		regVal, err := strconv.Atoi(registerStr)
		if err != nil || regVal < 0 || regVal >= MaxRegisterValue {
			s.writeError(w, "invalid reg value specified", http.StatusBadRequest)
			return
		}
		register = uint16(regVal)

		// Validate value
		value = query.Get("value")
		if value == "" {
			s.writeError(w, "no value specified", http.StatusBadRequest)
			return
		}
	} else if command == "datetime" {
		// Set datetime command
		register = TimeSyncRegister // Time sync register
		value = time.Now().Format("2006-01-02 15:04:05")
	}

	// Generate and queue register write command
	if err := s.queueRegisterWriteCommand(r.Context(), datalogger, protocol.ProtocolDataloggerWrite, register, value); err != nil {
		s.writeError(w, "failed to queue command", http.StatusInternalServerError)
		s.logger.Error().Err(err).Msg("Failed to queue register write command")
		return
	}

	// Wait for response
	registerKey := fmt.Sprintf("%04x", register)
	_, err := s.waitForResponse(protocol.ProtocolDataloggerWrite, registerKey, DefaultTimeout)
	if err != nil {
		s.writeError(w, "no or invalid response received", http.StatusBadRequest)
		s.logger.Warn().Err(err).Msg("No response received for register write")
		return
	}

	s.writeJSON(w, map[string]string{"status": "OK"}, http.StatusOK)
}

// handleInverterGet handles GET requests for inverter register operations.
func (s *Server) handleInverterGet(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msg("Inverter GET request received")

	query := r.URL.Query()
	
	// If no query parameters, return inverter registry info
	if len(query) == 0 {
		s.handleInverterRegistryInfo(w)
		return
	}

	// Validate and extract parameters
	params, err := s.validateInverterGetParams(query)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Execute the command
	if err := s.executeInverterCommand(w, params); err != nil {
		// Error response is already sent within executeInverterCommand
		return
	}
}

// InverterGetParams holds validated parameters for inverter GET requests.
type InverterGetParams struct {
	Command      string
	InverterID   string
	Register     int
	Format       FormatType
	Datalogger   *domain.DataloggerInfo
	InverterInfo *domain.InverterInfo
}

// handleInverterRegistryInfo returns information about all inverters in the registry.
func (s *Server) handleInverterRegistryInfo(w http.ResponseWriter) {
	dataloggers := s.registry.GetAllDataloggers()
	inverterInfo := make(map[string]interface{})
	
	for _, dl := range dataloggers {
		for _, inv := range dl.Inverters {
			inverterInfo[inv.Serial] = map[string]interface{}{
				"datalogger": dl.ID,
				"inverterno": inv.InverterNo,
				"ip":         dl.IP,
				"port":       dl.Port,
				"protocol":   dl.Protocol,
			}
		}
	}
	
	s.writeJSON(w, inverterInfo, http.StatusOK)
}

// validateInverterGetParams validates and extracts parameters from the query.
func (s *Server) validateInverterGetParams(query url.Values) (*InverterGetParams, error) {
	// Validate command parameter
	command := query.Get("command")
	if command == "" {
		return nil, fmt.Errorf("no command entered")
	}
	
	if command != "register" && command != "regall" {
		return nil, fmt.Errorf("no valid command entered")
	}

	// Validate inverter ID
	inverterID := query.Get("inverter")
	if inverterID == "" {
		return nil, fmt.Errorf("no or no valid invertid specified")
	}

	// Find datalogger and inverter
	datalogger, inverterInfo, err := s.findInverterInRegistry(inverterID)
	if err != nil {
		return nil, err
	}

	// Validate format
	formatStr := query.Get("format")
	if formatStr == "" {
		formatStr = "dec"
	}

	if err := s.formatConverter.ValidateFormat(formatStr); err != nil {
		return nil, fmt.Errorf("invalid format specified")
	}

	params := &InverterGetParams{
		Command:      command,
		InverterID:   inverterID,
		Format:       FormatType(formatStr),
		Datalogger:   datalogger,
		InverterInfo: inverterInfo,
	}

	// Validate register parameter for register command
	if command == "register" {
		registerStr := query.Get("register")
		if registerStr == "" {
			return nil, fmt.Errorf("no register specified")
		}

		register, err := strconv.Atoi(registerStr)
		if err != nil || register < 0 || register >= 4096 {
			return nil, fmt.Errorf("invalid reg value specified")
		}
		params.Register = register
	}

	return params, nil
}

// findInverterInRegistry finds the datalogger and inverter info for the given inverter ID.
func (s *Server) findInverterInRegistry(inverterID string) (*domain.DataloggerInfo, *domain.InverterInfo, error) {
	dataloggers := s.registry.GetAllDataloggers()
	for _, dl := range dataloggers {
		for _, inv := range dl.Inverters {
			if inv.Serial == inverterID {
				return dl, inv, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no or no valid invertid specified")
}

// executeInverterCommand executes the specified inverter command.
func (s *Server) executeInverterCommand(w http.ResponseWriter, params *InverterGetParams) error {
	switch params.Command {
	case "regall":
		return s.handleInverterRegallCommand(w, params)
	case "register":
		return s.handleInverterRegisterCommand(w, params)
	default:
		s.writeError(w, "command not defined or not available yet", http.StatusBadRequest)
		return fmt.Errorf("unknown command: %s", params.Command)
	}
}

// handleInverterRegallCommand handles the "regall" command for inverters.
func (s *Server) handleInverterRegallCommand(w http.ResponseWriter, params *InverterGetParams) error {
	allResponses := s.responseTracker.GetAllResponses(protocol.ProtocolInverterRead)
	
	// Convert response values to requested format
	convertedResponses := s.formatAllResponses(allResponses, params.Format)
	
	s.writeJSON(w, convertedResponses, http.StatusOK)
	return nil
}

// handleInverterRegisterCommand handles the "register" command for inverters.
func (s *Server) handleInverterRegisterCommand(w http.ResponseWriter, params *InverterGetParams) error {
	// Generate and queue inverter register read command
	if err := s.queueInverterRegisterReadCommand(params.Datalogger, params.InverterInfo, protocol.ProtocolInverterRead, uint16(params.Register)); err != nil {
		s.writeError(w, "failed to queue command", http.StatusInternalServerError)
		s.logger.Error().Err(err).Msg("Failed to queue inverter register read command")
		return err
	}

	// Wait for response
	registerKey := fmt.Sprintf("%04x", params.Register)
	response, err := s.waitForResponse(protocol.ProtocolInverterRead, registerKey, 10*time.Second) // Longer timeout for inverters
	if err != nil {
		s.writeError(w, "no or invalid response received", http.StatusBadRequest)
		s.logger.Warn().Err(err).Msg("No response received for inverter register read")
		return err
	}

	// Convert response value to requested format
	s.formatSingleResponse(response, params.Format)

	s.writeJSON(w, response, http.StatusOK)
	return nil
}

// formatAllResponses formats all response values according to the specified format.
func (s *Server) formatAllResponses(allResponses map[string]*CommandResponse, format FormatType) map[string]interface{} {
	convertedResponses := make(map[string]interface{})
	for regKey, response := range allResponses {
		if response.Value != nil {
			//nolint:errcheck // Error is checked in if condition
			if convertedValue, err := s.formatConverter.FormatResponseValue(response.Value.(string), format); err == nil {
				convertedResponses[regKey] = map[string]interface{}{
					"value": convertedValue,
				}
			} else {
				// Log error but continue with original value
				s.logger.Warn().Err(err).Str("register", regKey).Msg("Failed to convert response value format")
				convertedResponses[regKey] = map[string]interface{}{
					"value": response.Value,
				}
			}
		} else {
			convertedResponses[regKey] = response
		}
	}
	return convertedResponses
}

// formatSingleResponse formats a single response value according to the specified format.
func (s *Server) formatSingleResponse(response *CommandResponse, format FormatType) {
	if response.Value != nil {
		//nolint:errcheck // Error is checked in if condition
		if convertedValue, err := s.formatConverter.FormatResponseValue(response.Value.(string), format); err == nil {
			response.Value = convertedValue
		} else {
			// Log error but continue with original value
			s.logger.Warn().Err(err).Msg("Failed to convert response value format")
		}
	}
}

// handleInverterPut handles PUT requests for inverter register operations.
func (s *Server) handleInverterPut(w http.ResponseWriter, r *http.Request) {
	s.logger.Info().Msg("Inverter PUT request received")

	// Parse and validate parameters
	params, err := s.parseInverterPutParams(r)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find inverter and datalogger
	datalogger, inverterInfo, err := s.findInverterAndDatalogger(params.inverterID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Execute command based on type
	switch params.command {
	case "register":
		if err := s.handleInverterRegisterWrite(datalogger, inverterInfo, params); err != nil {
			s.handleInverterPutError(w, err)
			return
		}
		s.writeJSON(w, map[string]string{"status": "OK"}, http.StatusOK)
	case "multiregister":
		s.writeError(w, "multiregister command not yet implemented", http.StatusNotImplemented)
	default:
		s.writeError(w, "command not defined or not available yet", http.StatusBadRequest)
	}
}

// inverterPutParams holds parameters for inverter PUT operations
type inverterPutParams struct {
	command    string
	inverterID string
	format     FormatType
	register   uint16
	value      string
}

// parseInverterPutParams parses and validates parameters for inverter PUT requests
func (s *Server) parseInverterPutParams(r *http.Request) (*inverterPutParams, error) {
	query := r.URL.Query()
	
	if len(query) == 0 {
		return nil, fmt.Errorf("empty put received")
	}

	// Validate command parameter
	command := query.Get("command")
	if command == "" {
		return nil, fmt.Errorf("no command entered")
	}

	if command != "register" && command != "multiregister" {
		return nil, fmt.Errorf("no valid command entered")
	}

	// Validate inverter ID
	inverterID := query.Get("inverter")
	if inverterID == "" {
		return nil, fmt.Errorf("no or invalid invertid specified")
	}

	// Get format parameter (default to dec)
	formatStr := query.Get("format")
	if formatStr == "" {
		formatStr = "dec"
	}

	if err := s.formatConverter.ValidateFormat(formatStr); err != nil {
		return nil, fmt.Errorf("invalid format specified")
	}

	params := &inverterPutParams{
		command:    command,
		inverterID: inverterID,
		format:     FormatType(formatStr),
	}

	// For register command, parse register and value
	if command == "register" {
		registerStr := query.Get("register")
		if registerStr == "" {
			return nil, fmt.Errorf("no register specified")
		}

		regVal, err := strconv.Atoi(registerStr)
		if err != nil || regVal < 0 || regVal >= 4096 {
			return nil, fmt.Errorf("invalid reg value specified")
		}
		params.register = uint16(regVal)

		// Validate value
		value := query.Get("value")
		if value == "" {
			return nil, fmt.Errorf("no value specified")
		}
		params.value = value
	}

	return params, nil
}

// findInverterAndDatalogger finds datalogger and inverter by inverter ID
func (s *Server) findInverterAndDatalogger(inverterID string) (*domain.DataloggerInfo, *domain.InverterInfo, error) {
	dataloggers := s.registry.GetAllDataloggers()
	for _, dl := range dataloggers {
		for _, inv := range dl.Inverters {
			if inv.Serial == inverterID {
				return dl, inv, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("no or invalid invertid specified")
}

// handleInverterRegisterWrite processes inverter register write requests
func (s *Server) handleInverterRegisterWrite(datalogger *domain.DataloggerInfo, inverterInfo *domain.InverterInfo, params *inverterPutParams) error {
	// Validate and convert value to appropriate format
	if err := s.formatConverter.ValidateValue(params.value, params.format); err != nil {
		return fmt.Errorf("invalid value specified")
	}

	// Convert value to hex for command
	hexValue, err := s.formatConverter.CreateValueForCommand(params.value, params.format)
	if err != nil {
		return fmt.Errorf("invalid value format")
	}

	// Convert hex back to integer value for the command
	intValue, err := s.formatConverter.ConvertFromHex(hexValue, FormatDec)
	if err != nil {
		return fmt.Errorf("value conversion error")
	}

	// Generate and queue inverter register write command
	//nolint:errcheck // Error is properly checked and handled
	if err := s.queueInverterRegisterWriteCommand(datalogger, inverterInfo, protocol.ProtocolInverterWrite, params.register, intValue.(int)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to queue inverter register write command")
		return fmt.Errorf("failed to queue command")
	}

	// Wait for response
	registerKey := fmt.Sprintf("%04x", params.register)
	_, err = s.waitForResponse(protocol.ProtocolInverterWrite, registerKey, 10*time.Second) // Longer timeout for inverters
	if err != nil {
		s.logger.Warn().Err(err).Msg("No response received for inverter register write")
		return fmt.Errorf("no or invalid response received")
	}

	return nil
}

// handleInverterPutError handles errors from inverter PUT operations
func (s *Server) handleInverterPutError(w http.ResponseWriter, err error) {
	// Map error messages to appropriate HTTP status codes
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "failed to queue command"):
		s.writeError(w, errMsg, http.StatusInternalServerError)
	case strings.Contains(errMsg, "value conversion error"):
		s.writeError(w, errMsg, http.StatusInternalServerError)
	default:
		s.writeError(w, errMsg, http.StatusBadRequest)
	}
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

	errorResponse := map[string]interface{}{
		"error":     message,
		"status":    statusCode,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode error response")
	}
}

// handleAPIError handles different types of errors appropriately
func (s *Server) handleAPIError(w http.ResponseWriter, err error) {
	var valErr ValidationError
	var apiErr APIError
	
	switch {
	case errors.As(err, &valErr):
		s.writeError(w, valErr.Message, http.StatusBadRequest)
	case errors.As(err, &apiErr):
		s.writeError(w, apiErr.Message, apiErr.Code)
	default:
		s.logger.Error().Err(err).Msg("Unexpected error")
		s.writeError(w, "internal server error", http.StatusInternalServerError)
	}
}

// queueRegisterReadCommand creates and queues a register read command.
func (s *Server) queueRegisterReadCommand(ctx context.Context, datalogger *domain.DataloggerInfo, commandType string, register uint16) error {
	// Check if context is cancelled
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}
	
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register address and end register (same for single register read)
	regBytes := []byte{byte(register >> 8), byte(register & 0xFF)}
	body = append(body, regBytes...)
	body = append(body, regBytes...) // End register same as start
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	deviceID := protocol.DeviceDatalogger // Datalogger device ID
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for this register
	registerKey := fmt.Sprintf("%04x", register)
	s.responseTracker.DeleteResponse(commandType, registerKey)
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("command_type", commandType).
		Uint16("register", register).
		Msg("Queued register read command")
	
	return nil
}

// queueRegisterWriteCommand creates and queues a register write command.
func (s *Server) queueRegisterWriteCommand(ctx context.Context, datalogger *domain.DataloggerInfo, commandType string, register uint16, value string) error {
	// Check if context is cancelled
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}
	
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register address
	regBytes := []byte{byte(register >> 8), byte(register & 0xFF)}
	body = append(body, regBytes...)
	
	// Add value
	valueBytes := []byte(value)
	valueLenBytes := []byte{byte(len(valueBytes) >> 8), byte(len(valueBytes) & 0xFF)}
	body = append(body, valueLenBytes...)
	body = append(body, valueBytes...)
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	deviceID := protocol.DeviceDatalogger // Datalogger device ID
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for this register
	registerKey := fmt.Sprintf("%04x", register)
	s.responseTracker.DeleteResponse(commandType, registerKey)
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("command_type", commandType).
		Uint16("register", register).
		Str("value", value).
		Msg("Queued register write command")
	
	return nil
}

// waitForResponse waits for a response to be received for a specific command and register.
func (s *Server) waitForResponse(commandType, registerKey string, timeout time.Duration) (*CommandResponse, error) {
	ticker := time.NewTicker(ResponseCheckInterval) // Check every 500ms
	defer ticker.Stop()

	timeoutChan := time.After(timeout)
	
	for {
		select {
		case <-ticker.C:
			if response, found := s.responseTracker.GetResponse(commandType, registerKey); found {
				return response, nil
			}
		case <-timeoutChan:
			return nil, fmt.Errorf("timeout waiting for response")
		}
	}
}

// encryptCommand encrypts command data using the Growatt XOR mask.
func (s *Server) encryptCommand(data []byte) []byte {
	mask := []byte("Growatt")
	result := make([]byte, len(data))
		// Copy header unchanged (first 8 bytes)
	copy(result[:MinProtocolDataLen], data[:MinProtocolDataLen])

	// XOR the rest with the mask
	for i := MinProtocolDataLen; i < len(data); i++ {
		maskIdx := (i - MinProtocolDataLen) % len(mask)
		result[i] = data[i] ^ mask[maskIdx]
	}
	
	return result
}

// calculateCRC calculates CRC16 for the command.
func (s *Server) calculateCRC(data []byte) []byte {
	// Simple CRC16 calculation (should use proper Modbus CRC16)
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return []byte{byte(crc & 0xFF), byte(crc >> 8)}
}

// ProcessDeviceResponse processes a response received from a device.
func (s *Server) ProcessDeviceResponse(data []byte) {
	s.responseProcessor.ProcessResponse(data)
}

// GetCommandQueue returns the command queue for a specific device connection.
func (s *Server) GetCommandQueue(ip string, port int) chan []byte {
	return s.commandQueueMgr.GetOrCreateQueue(ip, port)
}

// UpdateDeviceConnection updates the connection tracking for a device.
func (s *Server) UpdateDeviceConnection(datalogger *domain.DataloggerInfo) {
	s.connectionTracker.UpdateConnection(datalogger)
}

// RemoveDeviceConnection removes connection tracking for a device.
func (s *Server) RemoveDeviceConnection(dataloggerID string, ip string, port int) {
	s.connectionTracker.RemoveConnection(dataloggerID)
	s.commandQueueMgr.RemoveQueue(ip, port)
}

// queueInverterRegisterReadCommand creates and queues an inverter register read command.
func (s *Server) queueInverterRegisterReadCommand(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, commandType string, register uint16) error {
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register address and end register (same for single register read)
	regBytes := []byte{byte(register >> 8), byte(register & 0xFF)}
	body = append(body, regBytes...)
	body = append(body, regBytes...) // End register same as start
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	
	// For inverter commands, use the inverter number as device ID
	deviceID := inverter.InverterNo
	if len(deviceID) < 2 {
		deviceID = "0" + deviceID // Pad to 2 characters
	}
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for this register
	registerKey := fmt.Sprintf("%04x", register)
	s.responseTracker.DeleteResponse(commandType, registerKey)
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("inverter", inverter.Serial).
		Str("command_type", commandType).
		Uint16("register", register).
		Msg("Queued inverter register read command")
	
	return nil
}

// queueInverterRegisterWriteCommand creates and queues an inverter register write command.
func (s *Server) queueInverterRegisterWriteCommand(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, commandType string, register uint16, value int) error {
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register address
	regBytes := []byte{byte(register >> 8), byte(register & 0xFF)}
	body = append(body, regBytes...)
	
	// Add value (2 bytes, big endian for inverter commands)
	valueBytes := []byte{byte(value >> 8), byte(value & 0xFF)}
	body = append(body, valueBytes...)
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	
	// For inverter commands, use the inverter number as device ID
	deviceID := inverter.InverterNo
	if len(deviceID) < 2 {
		deviceID = "0" + deviceID // Pad to 2 characters
	}
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for this register
	registerKey := fmt.Sprintf("%04x", register)
	s.responseTracker.DeleteResponse(commandType, registerKey)
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("inverter", inverter.Serial).
		Str("command_type", commandType).
		Uint16("register", register).
		Int("value", value).
		Msg("Queued inverter register write command")
	
	return nil
}

// queueInverterRegallCommand creates and queues an inverter register all command.
func (s *Server) queueInverterRegallCommand(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, commandType string) error {
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// For regall commands, typically read from register 0 to end of meaningful registers
	// Based on Python grott implementation, this is usually register 0x0000 to 0x00FF
	startReg := uint16(0x0000)
	endReg := uint16(0x00FF)
	
	// Add start register
	startBytes := []byte{byte(startReg >> 8), byte(startReg & 0xFF)}
	body = append(body, startBytes...)
	
	// Add end register
	endBytes := []byte{byte(endReg >> 8), byte(endReg & 0xFF)}
	body = append(body, endBytes...)
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	
	// For inverter commands, use the inverter number as device ID
	deviceID := inverter.InverterNo
	if len(deviceID) < 2 {
		deviceID = "0" + deviceID // Pad to 2 characters
	}
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for regall
	s.responseTracker.DeleteResponse(commandType, "regall")
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("inverter", inverter.Serial).
		Str("command_type", commandType).
		Uint16("start_reg", startReg).
		Uint16("end_reg", endReg).
		Msg("Queued inverter regall command")
	
	return nil
}

// handleMultiregisterGet handles multi-register read requests.
func (s *Server) handleMultiregisterGet(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug().
		Str("method", r.Method).
		Str("url", r.URL.String()).
		Msg("Received multiregister GET request")

	// Parse and validate parameters  
	params, err := s.parseMultiregisterGetParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse register list
	registers, err := s.parseRegisterList(params.registersParam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find devices
	datalogger, inverter, err := s.findDevicesForMultiregister(params.dataloggerSerial, params.inverterSerial)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Execute read command and get response
	if err := s.executeMultiregisterRead(datalogger, inverter, params.command, registers); err != nil {
		s.logger.Error().Err(err).Msg("Failed to queue multi-register read command")
		http.Error(w, "Failed to queue command: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Send response
	s.sendMultiregisterReadResponse(w, params.dataloggerSerial, params.inverterSerial, params.command, params.format, len(registers))
}

// multiregisterGetParams holds parameters for multi-register GET operations
type multiregisterGetParams struct {
	dataloggerSerial string
	inverterSerial   string
	command          string
	registersParam   string
	format           string
}

// parseMultiregisterGetParams parses and validates parameters for multi-register GET requests
func (s *Server) parseMultiregisterGetParams(r *http.Request) (*multiregisterGetParams, error) {
	query := r.URL.Query()
	params := &multiregisterGetParams{
		dataloggerSerial: query.Get("serial"),
		inverterSerial:   query.Get("invserial"),
		command:          query.Get("command"),
		registersParam:   query.Get("registers"),
		format:           query.Get("format"),
	}

	// Validate required parameters
	if params.dataloggerSerial == "" || params.inverterSerial == "" || params.command == "" || params.registersParam == "" {
		return nil, fmt.Errorf("Missing required parameters: serial, invserial, command, registers")
	}

	// Validate command type - multi-register commands use ProtocolMultiRegister
	if params.command != protocol.ProtocolInverterWrite {
		return nil, fmt.Errorf("Invalid command type for multiregister operation: %s", params.command)
	}

	// Default format is dec if not specified
	if params.format == "" {
		params.format = "dec"
	}

	// Validate format
	if !s.formatConverter.IsValidFormat(params.format) {
		return nil, fmt.Errorf("Invalid format: %s. Supported: dec, hex, text", params.format)
	}

	return params, nil
}

// parseRegisterList parses a comma-separated list of registers
func (s *Server) parseRegisterList(registersParam string) ([]uint16, error) {
	registerStrings := strings.Split(registersParam, ",")
	if len(registerStrings) == 0 {
		return nil, fmt.Errorf("No registers specified")
	}

	var registers []uint16
	for _, regStr := range registerStrings {
		regStr = strings.TrimSpace(regStr)
		if regStr == "" {
			continue
		}

		reg, err := s.parseRegisterString(regStr)
		if err != nil {
			return nil, fmt.Errorf("Invalid register format: %s", regStr)
		}

		registers = append(registers, uint16(reg))
	}

	if len(registers) == 0 {
		return nil, fmt.Errorf("No valid registers provided")
	}

	return registers, nil
}

// executeMultiregisterRead queues the multi-register read command and waits for response
func (s *Server) executeMultiregisterRead(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, command string, registers []uint16) error {
	// Queue multi-register read command
	if err := s.queueMultiRegisterReadCommand(datalogger, inverter, command, registers); err != nil {
		return err
	}

	// Wait for response
	timeout := s.requestTimeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	responseKey := "multiregister"

	for {
		select {
		case <-time.After(timeout):
			return fmt.Errorf("Command timeout")

		case <-ticker.C:
			if _, exists := s.responseTracker.GetResponse(command, responseKey); exists {
				return nil // Success
			}
		}
	}
}

// sendMultiregisterReadResponse sends the success response for multi-register read
func (s *Server) sendMultiregisterReadResponse(w http.ResponseWriter, dataloggerSerial, inverterSerial, command, format string, registerCount int) {
	// Get the actual response data
	responseKey := "multiregister"
	responseData, exists := s.responseTracker.GetResponse(command, responseKey)
	
	var registers interface{} = "Multi-register read completed"
	if exists && responseData != nil {
		registers = responseData.Value
	}

	response := map[string]interface{}{
		"status":    "success",
		"format":    format,
		"message":   "Multi-register read completed",
		"registers": registers,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug().
		Str("datalogger", dataloggerSerial).
		Str("inverter", inverterSerial).
		Int("register_count", registerCount).
		Str("format", format).
		Msg("Multi-register GET response sent")
}

// handleMultiregisterPut handles multi-register write requests.
func (s *Server) handleMultiregisterPut(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug().
		Str("method", r.Method).
		Str("url", r.URL.String()).
		Msg("Received multiregister PUT request")

	// Parse and validate parameters
	params, err := s.parseMultiregisterPutParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse register values
	regValues, err := s.parseRegisterValues(params.registersParam, params.valuesParam, params.format)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find devices
	datalogger, inverter, err := s.findDevicesForMultiregister(params.dataloggerSerial, params.inverterSerial)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Queue command and wait for response
	if err := s.executeMultiregisterWrite(datalogger, inverter, params.command, regValues); err != nil {
		s.logger.Error().Err(err).Msg("Failed to queue multi-register write command")
		http.Error(w, "Failed to queue command: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Send response
	s.sendMultiregisterWriteResponse(w, params.dataloggerSerial, params.inverterSerial, params.command, params.format, len(regValues))
}

// multiregisterPutParams holds parameters for multi-register PUT operations
type multiregisterPutParams struct {
	dataloggerSerial string
	inverterSerial   string
	command          string
	registersParam   string
	valuesParam      string
	format           string
}

// parseMultiregisterPutParams parses and validates parameters for multi-register PUT requests
func (s *Server) parseMultiregisterPutParams(r *http.Request) (*multiregisterPutParams, error) {
	query := r.URL.Query()
	params := &multiregisterPutParams{
		dataloggerSerial: query.Get("serial"),
		inverterSerial:   query.Get("invserial"),
		command:          query.Get("command"),
		registersParam:   query.Get("registers"),
		valuesParam:      query.Get("values"),
		format:           query.Get("format"),
	}

	// Validate required parameters
	if params.dataloggerSerial == "" || params.inverterSerial == "" || params.command == "" || 
	   params.registersParam == "" || params.valuesParam == "" {
		return nil, fmt.Errorf("Missing required parameters: serial, invserial, command, registers, values")
	}

	// Validate command type - multi-register commands use ProtocolMultiRegister
	if params.command != protocol.ProtocolInverterWrite {
		return nil, fmt.Errorf("Invalid command type for multiregister operation: %s", params.command)
	}

	// Default format is dec if not specified
	if params.format == "" {
		params.format = "dec"
	}

	// Validate format
	if !s.formatConverter.IsValidFormat(params.format) {
		return nil, fmt.Errorf("Invalid format: %s. Supported: dec, hex, text", params.format)
	}

	return params, nil
}

// parseRegisterValues parses register and value strings into RegisterValue structs
func (s *Server) parseRegisterValues(registersParam, valuesParam, format string) ([]RegisterValue, error) {
	registerStrings := strings.Split(registersParam, ",")
	valueStrings := strings.Split(valuesParam, ",")

	if len(registerStrings) != len(valueStrings) {
		return nil, fmt.Errorf("Mismatch between number of registers and values")
	}

	if len(registerStrings) == 0 {
		return nil, fmt.Errorf("no registers specified")
	}

	var regValues []RegisterValue
	for i, regStr := range registerStrings {
		regStr = strings.TrimSpace(regStr)
		valueStr := strings.TrimSpace(valueStrings[i])

		if regStr == "" || valueStr == "" {
			continue
		}

		// Parse register
		reg, err := s.parseRegisterString(regStr)
		if err != nil {
			return nil, fmt.Errorf("invalid register format: %s", regStr)
		}

		// Parse value
		intValue, err := s.parseValueString(valueStr, format)
		if err != nil {
			return nil, fmt.Errorf("invalid value format: %s", valueStr)
		}

		regValues = append(regValues, RegisterValue{
			Register: uint16(reg),
			Value:    intValue,
		})
	}

	if len(regValues) == 0 {
		return nil, fmt.Errorf("no valid register-value pairs provided")
	}

	return regValues, nil
}

// parseRegisterString parses a register string (hex or decimal)
func (s *Server) parseRegisterString(regStr string) (uint64, error) {
	if strings.HasPrefix(regStr, "0x") || strings.HasPrefix(regStr, "0X") {
		return strconv.ParseUint(regStr[2:], 16, 16)
	} else if len(regStr) <= 4 && strings.ContainsAny(regStr, "abcdefABCDEF") {
		return strconv.ParseUint(regStr, 16, 16)
	} else {
		return strconv.ParseUint(regStr, 10, 16)
	}
}

// parseValueString parses a value string using the format converter
func (s *Server) parseValueString(valueStr, format string) (int, error) {
	// Parse value using format converter
	value, err := s.formatConverter.ConvertFromHex(valueStr, FormatType(format))
	if err != nil {
		return 0, err
	}

	// ConvertFromHex returns interface{}, need to convert to int
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid value type returned from converter")
	}
}

// findDevicesForMultiregister finds datalogger and inverter for multiregister operations
func (s *Server) findDevicesForMultiregister(dataloggerSerial, inverterSerial string) (*domain.DataloggerInfo, *domain.InverterInfo, error) {
	// Find datalogger
	datalogger := s.registry.GetDataloggerBySerial(dataloggerSerial)
	if datalogger == nil {
		return nil, nil, fmt.Errorf("datalogger not found: %s", dataloggerSerial)
	}

	// Find inverter
	inverter := s.registry.GetInverterBySerial(datalogger.ID, inverterSerial)
	if inverter == nil {
		return nil, nil, fmt.Errorf("inverter not found: %s", inverterSerial)
	}

	return datalogger, inverter, nil
}

// executeMultiregisterWrite queues the multi-register write command and waits for response
func (s *Server) executeMultiregisterWrite(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, command string, regValues []RegisterValue) error {
	// Queue multi-register write command
	if err := s.queueMultiRegisterWriteCommand(datalogger, inverter, command, regValues); err != nil {
		return err
	}

	// Wait for response
	timeout := s.requestTimeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	responseKey := "multiregister"

	for {
		select {
		case <-time.After(timeout):
			return fmt.Errorf("Command timeout")

		case <-ticker.C:
			if _, exists := s.responseTracker.GetResponse(command, responseKey); exists {
				return nil // Success
			}
		}
	}
}

// sendMultiregisterWriteResponse sends the success response for multi-register write
func (s *Server) sendMultiregisterWriteResponse(w http.ResponseWriter, dataloggerSerial, inverterSerial, command, format string, registerCount int) {
	response := map[string]interface{}{
		"status":            "success",
		"datalogger":        dataloggerSerial,
		"inverter":          inverterSerial,
		"command":           command,
		"registers_written": registerCount,
		"message":           "Multi-register write completed successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	s.logger.Debug().
		Str("datalogger", dataloggerSerial).
		Str("inverter", inverterSerial).
		Int("register_count", registerCount).
		Str("format", format).
		Msg("Multi-register PUT response sent")
}

// queueMultiRegisterReadCommand creates and queues a multi-register read command.
func (s *Server) queueMultiRegisterReadCommand(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, commandType string, registers []uint16) error {
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register count (2 bytes, big endian)
	if len(registers) > 65535 {
		return fmt.Errorf("too many registers: %d, maximum is 65535", len(registers))
	}
	//nolint:gosec // Bounds checked above
	regCount := uint16(len(registers))
	countBytes := []byte{byte(regCount >> 8), byte(regCount & 0xFF)}
	body = append(body, countBytes...)
	
	// Add all registers (2 bytes each, big endian)
	for _, register := range registers {
		regBytes := []byte{byte(register >> 8), byte(register & 0xFF)}
		body = append(body, regBytes...)
	}
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	
	// For multi-register commands, use the inverter number as device ID
	deviceID := inverter.InverterNo
	if len(deviceID) < 2 {
		deviceID = "0" + deviceID // Pad to 2 characters
	}
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for multiregister
	s.responseTracker.DeleteResponse(commandType, "multiregister")
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("inverter", inverter.Serial).
		Str("command_type", commandType).
		Int("register_count", len(registers)).
		Msg("Queued multi-register read command")
	
	return nil
}

// queueMultiRegisterWriteCommand creates and queues a multi-register write command.
func (s *Server) queueMultiRegisterWriteCommand(datalogger *domain.DataloggerInfo, inverter *domain.InverterInfo, commandType string, regValues []RegisterValue) error {
	// Build command body
	body := []byte(datalogger.ID)
	
	// Add padding for protocol ProtocolInverterWrite
	if datalogger.Protocol == protocol.ProtocolInverterWrite {
		padding := make([]byte, 20)
		body = append(body, padding...)
	}
	
	// Add register count (2 bytes, big endian)
	if len(regValues) > 65535 {
		return fmt.Errorf("too many register values: %d, maximum is 65535", len(regValues))
	}
	//nolint:gosec // Bounds checked above
	regCount := uint16(len(regValues))
	countBytes := []byte{byte(regCount >> 8), byte(regCount & 0xFF)}
	body = append(body, countBytes...)
	
	// Add all register-value pairs (4 bytes each: 2 for register, 2 for value)
	for _, rv := range regValues {
		// Add register address (2 bytes, big endian)
		regBytes := []byte{byte(rv.Register >> 8), byte(rv.Register & 0xFF)}
		body = append(body, regBytes...)
		
		// Add value (2 bytes, big endian)
		valueBytes := []byte{byte(rv.Value >> 8), byte(rv.Value & 0xFF)}
		body = append(body, valueBytes...)
	}
	
	// Create header
	sequenceNo := uint16(1) // TODO: Implement proper sequence number tracking
	bodyLen := len(body) + 2
	
	// For multi-register commands, use the inverter number as device ID
	deviceID := inverter.InverterNo
	if len(deviceID) < 2 {
		deviceID = "0" + deviceID // Pad to 2 characters
	}
	
	header := fmt.Sprintf("%04x00%s%04x%s%s", sequenceNo, datalogger.Protocol, bodyLen, deviceID, commandType)
	
	// Combine header and body
	commandHex := header + fmt.Sprintf("%x", body)
	commandBytes, err := hex.DecodeString(commandHex)
	if err != nil {
		return fmt.Errorf("failed to decode command hex: %w", err)
	}
	
	// Encrypt and add CRC if needed
	if datalogger.Protocol != protocol.ProtocolTCP {
		encrypted := s.encryptCommand(commandBytes)
		crc := s.calculateCRC(encrypted)
		commandBytes = append(encrypted, crc...)
	}
	
	// Queue the command
	if err := s.commandQueueMgr.QueueCommand(datalogger.IP, datalogger.Port, commandBytes); err != nil {
		return fmt.Errorf("failed to queue command: %w", err)
	}
	
	// Clear any existing response for multiregister
	s.responseTracker.DeleteResponse(commandType, "multiregister")
	
	s.logger.Debug().
		Str("datalogger", datalogger.ID).
		Str("inverter", inverter.Serial).
		Str("command_type", commandType).
		Int("register_count", len(regValues)).
		Msg("Queued multi-register write command")
	
	return nil
}

// parseMultiRegisterResponse parses a multi-register response and formats the values.
func (s *Server) parseMultiRegisterResponse(responseData []byte, registers []uint16, format string) map[string]interface{} {
	response := map[string]interface{}{
		"status":   "success",
		"format":   format,
		"registers": make(map[string]interface{}),
	}
	
	// Basic response validation
	if len(responseData) < 10 {
		response["status"] = "error"
		response["message"] = "Response too short"
		return response
	}
	
	// Skip header and protocol overhead (this varies by protocol)
	// For now, assume the register values start at offset 20
	dataOffset := 20
	if len(responseData) < dataOffset {
		response["status"] = "error"
		response["message"] = "Insufficient response data"
		return response
	}
	
	registersMap := make(map[string]interface{})
	
	// Parse each register value (assuming 2 bytes per register)
	for i, register := range registers {
		valueOffset := dataOffset + (i * 2)
		if valueOffset+1 >= len(responseData) {
			registersMap[fmt.Sprintf("%04x", register)] = map[string]interface{}{
				"error": "No data available",
			}
			continue
		}
		
		// Extract 2-byte value (big endian)
		rawValue := int(responseData[valueOffset])<<8 | int(responseData[valueOffset+1])
		
		// Convert to requested format
		valueStr := fmt.Sprintf("%d", rawValue) // Convert int to string
		formattedValue, err := s.formatConverter.ConvertToHex(valueStr, FormatType(format))
		if err != nil {
			registersMap[fmt.Sprintf("%04x", register)] = map[string]interface{}{
				"error": "Format conversion failed",
				"raw":   rawValue,
			}
			continue
		}
		
		registersMap[fmt.Sprintf("%04x", register)] = map[string]interface{}{
			"value":  formattedValue,
			"format": format,
			"raw":    rawValue,
		}
	}
	
	response["registers"] = registersMap
	return response
}
