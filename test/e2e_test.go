package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/api"
	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullSystemIntegration tests the complete system integration
func TestFullSystemIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create configuration
	cfg := createE2ETestConfig()

	// Create registry
	registry := domain.NewDeviceRegistry()
	setupE2ETestRegistry(registry)

	// Create HTTP API server
	apiServer := api.NewServer(cfg, registry)
	require.NotNil(t, apiServer)

	// Base URLs - using httptest for testing
	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	// Test scenarios
	t.Run("System Status Check", func(t *testing.T) {
		// Check API server status
		resp, err := http.Get(testServer.URL + "/api/v1/status")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var status map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&status)
		require.NoError(t, err)

		assert.Equal(t, "ok", status["status"])
	})

	t.Run("Device Discovery", func(t *testing.T) {
		// Test datalogger listing
		resp, err := http.Get(testServer.URL + "/api/v1/dataloggers")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var response map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)

		assert.Contains(t, response, "dataloggers")
		assert.Contains(t, response, "count")
		assert.Equal(t, float64(1), response["count"])

		dataloggers, ok := response["dataloggers"].([]interface{})
		require.True(t, ok)
		assert.Len(t, dataloggers, 1)

		firstDatalogger, ok := dataloggers[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "TEST_E2E_DL", firstDatalogger["id"])
		assert.Equal(t, "127.0.0.1", firstDatalogger["ip"])

		// Test inverter listing for datalogger
		resp, err = http.Get(testServer.URL + "/api/v1/dataloggers/TEST_E2E_DL/inverters")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var invResponse map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&invResponse)
		require.NoError(t, err)

		assert.Contains(t, invResponse, "inverters")
		assert.Contains(t, invResponse, "count")
		assert.Equal(t, "TEST_E2E_DL", invResponse["datalogger"])
		assert.Equal(t, float64(1), invResponse["count"])

		inverters, ok := invResponse["inverters"].([]interface{})
		require.True(t, ok)
		assert.Len(t, inverters, 1)

		firstInverter, ok := inverters[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "TEST_E2E_INV", firstInverter["serial"])
	})

	t.Run("Command Flow Simulation", func(t *testing.T) {
		// Test register read command via API
		// This should timeout since no real device is connected, but tests the flow
		resp, err := http.Get(testServer.URL + "/datalogger?serial=TEST_E2E_DL&command=19&register=0001&format=dec")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Expect timeout or error since mock device doesn't respond properly
		assert.True(t, resp.StatusCode >= 400)
	})

	t.Run("Concurrent API Requests", func(t *testing.T) {
		// Test concurrent API requests to ensure thread safety
		numRequests := 10
		done := make(chan bool, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				defer func() { done <- true }()

				// Make status request
				resp, err := http.Get(testServer.URL + "/api/v1/status")
				if err != nil {
					t.Errorf("Request failed: %v", err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected status 200, got %d", resp.StatusCode)
				}
			}()
		}

		// Wait for all requests to complete
		for i := 0; i < numRequests; i++ {
			<-done
		}
	})

	t.Run("Memory and Resource Management", func(t *testing.T) {
		// Test that resources are properly managed
		// Make multiple requests and check for resource leaks

		client := &http.Client{Timeout: 5 * time.Second}

		for i := 0; i < 100; i++ {
			resp, err := client.Get(testServer.URL + "/api/v1/dataloggers")
			if err != nil {
				continue
			}
			resp.Body.Close()
		}

		// Check that we can still make requests after many iterations
		resp, err := http.Get(testServer.URL + "/api/v1/status")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestSystemPerformance tests system performance under load
func TestSystemPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	cfg := createE2ETestConfig()
	registry := domain.NewDeviceRegistry()
	setupE2ETestRegistry(registry)

	apiServer := api.NewServer(cfg, registry)
	require.NotNil(t, apiServer)

	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	t.Run("High Concurrent Load", func(t *testing.T) {
		numConcurrent := 50
		numRequestsPerClient := 20
		done := make(chan bool, numConcurrent)

		startTime := time.Now()

		for i := 0; i < numConcurrent; i++ {
			go func(clientID int) {
				defer func() { done <- true }()

				client := &http.Client{Timeout: 10 * time.Second}

				for j := 0; j < numRequestsPerClient; j++ {
					endpoints := []string{
						"/api/v1/status",
						"/api/v1/dataloggers",
						"/api/v1/dataloggers/TEST_E2E_DL",
						"/api/v1/dataloggers/TEST_E2E_DL/inverters",
					}

					for _, endpoint := range endpoints {
						resp, err := client.Get(testServer.URL + endpoint)
						if err != nil {
							t.Errorf("Client %d request %d to %s failed: %v", clientID, j, endpoint, err)
							continue
						}
						resp.Body.Close()

						if resp.StatusCode != http.StatusOK {
							t.Errorf("Client %d request %d to %s got status %d", clientID, j, endpoint, resp.StatusCode)
						}
					}
				}
			}(i)
		}

		// Wait for all clients to complete
		for i := 0; i < numConcurrent; i++ {
			<-done
		}

		duration := time.Since(startTime)
		totalRequests := numConcurrent * numRequestsPerClient * 4 // 4 endpoints per client per iteration

		t.Logf("Completed %d requests in %v (%.2f req/sec)",
			totalRequests, duration, float64(totalRequests)/duration.Seconds())

		// Performance assertion: should handle at least 100 req/sec
		reqPerSec := float64(totalRequests) / duration.Seconds()
		assert.Greater(t, reqPerSec, 100.0, "Expected at least 100 req/sec, got %.2f", reqPerSec)
	})
}

// TestErrorRecovery tests error recovery scenarios
func TestErrorRecovery(t *testing.T) {
	cfg := createE2ETestConfig()
	registry := domain.NewDeviceRegistry()

	// Don't add any devices to registry to test error handling
	apiServer := api.NewServer(cfg, registry)
	require.NotNil(t, apiServer)

	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	t.Run("Nonexistent Datalogger", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/api/v1/dataloggers/NONEXISTENT")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Invalid Parameters", func(t *testing.T) {
		// Test invalid register format
		resp, err := http.Get(testServer.URL + "/datalogger?serial=TEST&command=invalid&register=invalid&format=invalid")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Recovery After Errors", func(t *testing.T) {
		// Make several error requests
		for i := 0; i < 10; i++ {
			resp, _ := http.Get(testServer.URL + "/api/v1/dataloggers/NONEXISTENT")
			if resp != nil {
				resp.Body.Close()
			}
		}

		// Verify system still works for valid requests
		resp, err := http.Get(testServer.URL + "/api/v1/status")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// Helper types and functions

func createE2ETestConfig() *config.Config {
	return &config.Config{
		Server: struct {
			Host string `mapstructure:"host"`
			Port int    `mapstructure:"port"`
		}{
			Host: "127.0.0.1",
			Port: 5279,
		},
		API: struct {
			Enabled bool   `mapstructure:"enabled"`
			Host    string `mapstructure:"host"`
			Port    int    `mapstructure:"port"`
		}{
			Enabled: true,
			Host:    "localhost",
			Port:    0,
		},
		LogLevel: "debug",
	}
}

func setupE2ETestRegistry(registry *domain.DeviceRegistry) {
	// Add single test datalogger and inverter
	registry.RegisterDatalogger("TEST_E2E_DL", "127.0.0.1", 5279, protocol.ProtocolInverterWrite)
	registry.RegisterInverter("TEST_E2E_DL", "TEST_E2E_INV", "01")
}

// BenchmarkFullSystem benchmarks the complete system
func BenchmarkFullSystem(b *testing.B) {
	cfg := createE2ETestConfig()
	registry := domain.NewDeviceRegistry()
	setupE2ETestRegistry(registry)

	apiServer := api.NewServer(cfg, registry)
	require.NotNil(b, apiServer)

	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	b.Run("StatusRequests", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := client.Get(testServer.URL + "/api/v1/status")
				if err == nil {
					resp.Body.Close()
				}
			}
		})
	})

	b.Run("DataloggerListing", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := client.Get(testServer.URL + "/api/v1/dataloggers")
				if err == nil {
					resp.Body.Close()
				}
			}
		})
	})
}
