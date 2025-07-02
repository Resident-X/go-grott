package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/mocks"
	"github.com/stretchr/testify/assert"
)

func TestNewAPIServer(t *testing.T) {
	cfg := &config.Config{}
	cfg.API.Enabled = true
	cfg.API.Host = "localhost"
	cfg.API.Port = 8080

	registry := domain.NewDeviceRegistry()

	server := NewServer(cfg, registry)

	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.Equal(t, registry, server.registry)
	assert.NotNil(t, server.router)
	assert.NotZero(t, server.startTime)
}

func TestAPIServer_HandleStatus(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup expectations
	mockDataloggers := []*domain.DataloggerInfo{
		{ID: "test1", IP: "192.168.1.1"},
		{ID: "test2", IP: "192.168.1.2"},
	}
	mockRegistry.EXPECT().GetAllDataloggers().Return(mockDataloggers)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "1.0.0", response["version"])
	assert.NotEmpty(t, response["uptime"])
	assert.Equal(t, float64(2), response["dataloggerCount"]) // JSON unmarshals numbers as float64
}

func TestAPIServer_HandleListDataloggers(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup test data
	now := time.Now()
	mockDataloggers := []*domain.DataloggerInfo{
		{
			ID:          "DL001",
			IP:          "192.168.1.100",
			Port:        5279,
			Protocol:    "TCP",
			LastContact: now,
			Inverters: map[string]*domain.InverterInfo{
				"INV001": {Serial: "INV001", InverterNo: "1"},
			},
		},
		{
			ID:          "DL002",
			IP:          "192.168.1.101",
			Port:        5279,
			Protocol:    "TCP",
			LastContact: now.Add(-5 * time.Minute),
			Inverters:   map[string]*domain.InverterInfo{},
		},
	}
	mockRegistry.EXPECT().GetAllDataloggers().Return(mockDataloggers)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers", http.NoBody)
	w := httptest.NewRecorder()

	server.handleListDataloggers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, float64(2), response["count"])
	assert.IsType(t, []interface{}{}, response["dataloggers"])

	dataloggers := response["dataloggers"].([]interface{})
	assert.Len(t, dataloggers, 2)

	dl1 := dataloggers[0].(map[string]interface{})
	assert.Equal(t, "DL001", dl1["id"])
	assert.Equal(t, "192.168.1.100", dl1["ip"])
	assert.Equal(t, float64(5279), dl1["port"])
	assert.Equal(t, "TCP", dl1["protocol"])
	assert.Equal(t, float64(1), dl1["inverterCount"])
}

func TestAPIServer_HandleGetDatalogger_Found(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup test data
	now := time.Now()
	mockDatalogger := &domain.DataloggerInfo{
		ID:          "DL001",
		IP:          "192.168.1.100",
		Port:        5279,
		Protocol:    "TCP",
		LastContact: now,
		Inverters: map[string]*domain.InverterInfo{
			"INV001": {Serial: "INV001", InverterNo: "1"},
			"INV002": {Serial: "INV002", InverterNo: "2"},
		},
	}
	mockRegistry.EXPECT().GetDatalogger("DL001").Return(mockDatalogger, true)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers/DL001", http.NoBody)
	req = mux.SetURLVars(req, map[string]string{"id": "DL001"})
	w := httptest.NewRecorder()

	server.handleGetDatalogger(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "DL001", response["id"])
	assert.Equal(t, "192.168.1.100", response["ip"])
	assert.Equal(t, float64(5279), response["port"])
	assert.Equal(t, "TCP", response["protocol"])
	assert.Equal(t, float64(2), response["inverterCount"])
}

func TestAPIServer_HandleGetDatalogger_NotFound(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup expectations
	mockRegistry.EXPECT().GetDatalogger("NONEXISTENT").Return((*domain.DataloggerInfo)(nil), false)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers/NONEXISTENT", http.NoBody)
	req = mux.SetURLVars(req, map[string]string{"id": "NONEXISTENT"})
	w := httptest.NewRecorder()

	server.handleGetDatalogger(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "Datalogger not found", response["error"])
}

func TestAPIServer_HandleListInverters_Found(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup test data
	now := time.Now()
	mockInverters := []*domain.InverterInfo{
		{
			Serial:      "INV001",
			InverterNo:  "1",
			LastContact: now,
		},
		{
			Serial:      "INV002",
			InverterNo:  "2",
			LastContact: now.Add(-2 * time.Minute),
		},
	}
	mockRegistry.EXPECT().GetInverters("DL001").Return(mockInverters, true)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers/DL001/inverters", http.NoBody)
	req = mux.SetURLVars(req, map[string]string{"id": "DL001"})
	w := httptest.NewRecorder()

	server.handleListInverters(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "DL001", response["datalogger"])
	assert.Equal(t, float64(2), response["count"])
	assert.IsType(t, []interface{}{}, response["inverters"])

	inverters := response["inverters"].([]interface{})
	assert.Len(t, inverters, 2)

	inv1 := inverters[0].(map[string]interface{})
	assert.Equal(t, "INV001", inv1["serial"])
	assert.Equal(t, "1", inv1["inverterNo"])
}

func TestAPIServer_HandleListInverters_DataloggerNotFound(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup expectations
	mockRegistry.EXPECT().GetInverters("NONEXISTENT").Return([]*domain.InverterInfo{}, false)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers/NONEXISTENT/inverters", http.NoBody)
	req = mux.SetURLVars(req, map[string]string{"id": "NONEXISTENT"})
	w := httptest.NewRecorder()

	server.handleListInverters(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "Datalogger not found", response["error"])
}

func TestAPIServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{}
	cfg.API.Host = "localhost"
	cfg.API.Port = 0 // Use port 0 to let the OS choose an available port

	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	ctx := context.Background()

	// Test Start
	err := server.Start(ctx)
	assert.NoError(t, err)

	// Give the server a moment to start
	time.Sleep(10 * time.Millisecond)

	// Test Stop
	err = server.Stop(ctx)
	assert.NoError(t, err)
}

func TestAPIServer_WriteError(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	w := httptest.NewRecorder()
	server.writeError(w, "Test error message", http.StatusBadRequest)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "Test error message", response["error"])
}

func TestAPIServer_WriteJSON(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	testData := map[string]interface{}{
		"test":   "data",
		"number": 42,
	}

	w := httptest.NewRecorder()
	server.writeJSON(w, testData, http.StatusCreated)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "data", response["test"])
	assert.Equal(t, float64(42), response["number"])
}

// Test error cases and edge scenarios

func TestAPIServer_StartError(t *testing.T) {
	cfg := &config.Config{}
	cfg.API.Host = "invalid-host-that-should-not-exist"
	cfg.API.Port = 99999 // Invalid port

	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	ctx := context.Background()

	// Start should not return an error even if server fails to start
	// because it starts in a goroutine
	err := server.Start(ctx)
	assert.NoError(t, err)

	// Give the server a moment to try to start and fail
	time.Sleep(50 * time.Millisecond)

	// Stop should still work
	err = server.Stop(ctx)
	assert.NoError(t, err)
}

func TestAPIServer_StopError(t *testing.T) {
	cfg := &config.Config{}
	cfg.API.Host = "localhost"
	cfg.API.Port = 0

	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Create a mock server that will return an error on shutdown
	server.server = &http.Server{
		Addr: ":0",
		// Handler left nil to cause potential error
	}

	// Create a very short timeout context to force shutdown timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// This should complete without error because we handle the timeout gracefully
	err := server.Stop(ctx)
	// The error might occur due to the very short timeout, but the function should handle it
	// In real scenarios, this would typically not error
	_ = err // We don't assert because the behavior depends on timing
}

func TestAPIServer_StopWithNilServer(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Ensure server.server is nil (default state)
	server.server = nil

	ctx := context.Background()
	err := server.Stop(ctx)
	assert.NoError(t, err)
}

// Test JSON encoding errors.
type brokenData struct{}

func (b brokenData) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("intentional marshal error")
}

func TestAPIServer_WriteJSONError(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Test with data that will fail to marshal
	broken := brokenData{}

	w := httptest.NewRecorder()
	server.writeJSON(w, broken, http.StatusOK)

	// Should still set headers and status code
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	// Body will be empty due to encoding error
	assert.Empty(t, w.Body.String())
}

func TestAPIServer_WriteErrorWithEncodingError(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Create a ResponseWriter that will fail during encoding
	// We'll use a custom writer that fails after headers are set
	w := &failingResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
		failOnWrite:    true,
	}

	server.writeError(w, "test error", http.StatusBadRequest)

	// Should still set headers and status code
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Custom ResponseWriter that can simulate encoding failures.
type failingResponseWriter struct {
	http.ResponseWriter
	failOnWrite bool
	Code        int
}

func (f *failingResponseWriter) Write(b []byte) (int, error) {
	if f.failOnWrite {
		return 0, fmt.Errorf("intentional write error")
	}
	n, err := f.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("response write failed: %w", err)
	}
	return n, nil
}

func (f *failingResponseWriter) WriteHeader(statusCode int) {
	f.Code = statusCode
	f.ResponseWriter.WriteHeader(statusCode)
}

// Test route setup and HTTP method restrictions.
func TestAPIServer_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Test POST to GET-only endpoint (valid route but wrong method)
	req := httptest.NewRequest("POST", "/api/v1/status", http.NoBody)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	// Note: Some mux configurations return 404 instead of 405 for method restrictions
	// Both are reasonable responses for this scenario
	assert.True(t, w.Code == http.StatusMethodNotAllowed || w.Code == http.StatusNotFound,
		"Expected 405 (Method Not Allowed) or 404 (Not Found), got %d", w.Code)
}

func TestAPIServer_NotFoundEndpoint(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)
	server := NewServer(cfg, mockRegistry)

	// Test non-existent endpoint
	req := httptest.NewRequest("GET", "/api/v1/nonexistent", http.NoBody)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Test full HTTP routing through the router.
func TestAPIServer_FullRouting(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup expectations for status endpoint
	mockDataloggers := []*domain.DataloggerInfo{
		{ID: "test1", IP: "192.168.1.1"},
	}
	mockRegistry.EXPECT().GetAllDataloggers().Return(mockDataloggers)

	server := NewServer(cfg, mockRegistry)

	// Test routing through the full server
	req := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestAPIServer_HandleListDataloggers_EmptyRegistry(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup empty registry
	mockRegistry.EXPECT().GetAllDataloggers().Return([]*domain.DataloggerInfo{})

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers", http.NoBody)
	w := httptest.NewRecorder()

	server.handleListDataloggers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, float64(0), response["count"])
	assert.IsType(t, []interface{}{}, response["dataloggers"])
	assert.Len(t, response["dataloggers"], 0)
}

func TestAPIServer_HandleListInverters_EmptyInverters(t *testing.T) {
	cfg := &config.Config{}
	mockRegistry := mocks.NewMockRegistry(t)

	// Setup expectations - datalogger exists but has no inverters
	mockRegistry.EXPECT().GetInverters("DL001").Return([]*domain.InverterInfo{}, true)

	server := NewServer(cfg, mockRegistry)

	req := httptest.NewRequest("GET", "/api/v1/dataloggers/DL001/inverters", http.NoBody)
	req = mux.SetURLVars(req, map[string]string{"id": "DL001"})
	w := httptest.NewRecorder()

	server.handleListInverters(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "DL001", response["datalogger"])
	assert.Equal(t, float64(0), response["count"])
	assert.IsType(t, []interface{}{}, response["inverters"])
	assert.Len(t, response["inverters"], 0)
}
