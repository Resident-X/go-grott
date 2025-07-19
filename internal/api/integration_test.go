package api_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestHTTPAPIIntegration tests the complete HTTP API integration
func TestHTTPAPIIntegration(t *testing.T) {
	// Create test configuration
	cfg := createTestConfig()

	// Create registry with test data
	registry := domain.NewDeviceRegistry()
	setupTestRegistry(registry)

	// Create HTTP API server
	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(t, apiServer)

	// Use httptest.NewServer for testing instead of starting the actual server
	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	t.Run("Server Status", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/api/v1/status")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var status map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&status)
		require.NoError(t, err)

		assert.Equal(t, "ok", status["status"])
		assert.Contains(t, status, "uptime")
		assert.Contains(t, status, "dataloggerCount")
		assert.Equal(t, float64(2), status["dataloggerCount"])
	})

	t.Run("List Dataloggers", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/api/v1/dataloggers")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var response map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)

		assert.Contains(t, response, "dataloggers")
		assert.Contains(t, response, "count")
		assert.Equal(t, float64(2), response["count"])

		dataloggers, ok := response["dataloggers"].([]interface{})
		require.True(t, ok)
		assert.Len(t, dataloggers, 2)

		// Check that both dataloggers are present in the list
		dataloggerIDs := make([]string, 0, 2)
		for _, dl := range dataloggers {
			datalogger, ok := dl.(map[string]interface{})
			require.True(t, ok)
			id, ok := datalogger["id"].(string)
			require.True(t, ok)
			dataloggerIDs = append(dataloggerIDs, id)
		}
		assert.Contains(t, dataloggerIDs, "TEST_DL_001")
		assert.Contains(t, dataloggerIDs, "TEST_DL_002")
	})

	t.Run("Get Specific Datalogger", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/api/v1/dataloggers/TEST_DL_001")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var datalogger domain.DataloggerInfo
		err = json.NewDecoder(resp.Body).Decode(&datalogger)
		require.NoError(t, err)

		assert.Equal(t, "TEST_DL_001", datalogger.ID)
		assert.Equal(t, "192.168.1.100", datalogger.IP)
		assert.Equal(t, 5279, datalogger.Port)
	})

	t.Run("List Inverters for Datalogger", func(t *testing.T) {
		resp, err := http.Get(testServer.URL + "/api/v1/dataloggers/TEST_DL_001/inverters")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var response map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)

		assert.Contains(t, response, "inverters")
		assert.Contains(t, response, "count")
		assert.Contains(t, response, "datalogger")
		assert.Equal(t, "TEST_DL_001", response["datalogger"])
		assert.Equal(t, float64(2), response["count"])

		inverters, ok := response["inverters"].([]interface{})
		require.True(t, ok)
		assert.Len(t, inverters, 2)

		firstInverter, ok := inverters[0].(map[string]interface{})
		require.True(t, ok)
		// Check that it's one of our test inverters (order might vary)
		serial := firstInverter["serial"].(string)
		assert.True(t, serial == "TEST_INV_001" || serial == "TEST_INV_002")
	})

	t.Run("Format Converter Validation", func(t *testing.T) {
		// Test format conversion through API calls
		testCases := []struct {
			name     string
			format   string
			expected int
		}{
			{"Decimal Format", "dec", http.StatusBadRequest}, // No device connected, expect timeout/error
			{"Hex Format", "hex", http.StatusBadRequest},
			{"Text Format", "text", http.StatusBadRequest},
			{"Invalid Format", "invalid", http.StatusBadRequest},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				url := fmt.Sprintf("%s/datalogger?serial=TEST_DL_001&command=19&register=0001&format=%s", testServer.URL, tc.format)
				resp, err := http.Get(url)
				require.NoError(t, err)
				defer resp.Body.Close()

				// Since no device is actually connected, we expect either timeout or bad request
				// The important thing is that the format validation works
				if tc.format == "invalid" {
					assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
				} else {
					// For valid formats, we might get timeout or not found
					assert.True(t, resp.StatusCode >= 400)
				}
			})
		}
	})
}

// TestAPIServerLifecycle tests the API server lifecycle integration
func TestAPIServerLifecycle(t *testing.T) {
	cfg := createTestConfig()
	registry := domain.NewDeviceRegistry()

	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(t, apiServer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	apiServer.Start(ctx)

	// Since we're using port 0 (random port), we can't easily test network connectivity
	// Just verify the server doesn't panic and accepts the start call
	time.Sleep(10 * time.Millisecond) // Give server time to start

	// Test graceful stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	apiServer.Stop(shutdownCtx)
}

// TestCommandQueueIntegration tests command queue functionality
func TestCommandQueueIntegration(t *testing.T) {
	cfg := createTestConfig()
	registry := domain.NewDeviceRegistry()
	setupTestRegistry(registry)

	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(t, apiServer)

	// Test command queue operations
	commandMgr := apiServer.GetCommandQueueManager()
	queue := commandMgr.GetOrCreateQueue("192.168.1.100", 5279)
	assert.NotNil(t, queue)

	// Test queuing a command
	testCommand := []byte{0x01, 0x02, 0x03, 0x04}
	commandMgr.QueueCommand("192.168.1.100", 5279, testCommand)

	// Verify command was queued
	select {
	case cmd := <-queue:
		assert.Equal(t, testCommand, cmd)
	case <-time.After(time.Second):
		t.Fatal("Command not received from queue")
	}

	// Test queue cleanup
	commandMgr.RemoveQueue("192.168.1.100", 5279)
}

// TestResponseTrackerIntegration tests response tracking functionality
func TestResponseTrackerIntegration(t *testing.T) {
	cfg := createTestConfig()
	registry := domain.NewDeviceRegistry()

	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(t, apiServer)

	tracker := apiServer.GetResponseTracker()

	// Test storing and retrieving responses
	response := &api.CommandResponse{
		Value:  "1234",
		Result: "00",
	}

	tracker.SetResponse(protocol.ProtocolInverterRead, "0001", response)

	// Retrieve response
	retrieved, exists := tracker.GetResponse(protocol.ProtocolInverterRead, "0001")
	assert.True(t, exists)
	assert.Equal(t, response.Value, retrieved.Value)
	assert.Equal(t, response.Result, retrieved.Result)

	// Test delete response
	tracker.DeleteResponse(protocol.ProtocolInverterRead, "0001")
	_, exists = tracker.GetResponse(protocol.ProtocolInverterRead, "0001")
	assert.False(t, exists)
}

// TestFormatConverterIntegration tests format conversion functionality
func TestFormatConverterIntegration(t *testing.T) {
	converter := api.NewFormatConverter()

	t.Run("Decimal Conversions", func(t *testing.T) {
		// Test dec to hex
		hexVal, err := converter.ConvertToHex("1234", api.FormatDec)
		assert.NoError(t, err)
		assert.Equal(t, "04d2", hexVal)

		// Test hex to dec
		decVal, err := converter.ConvertFromHex("04d2", api.FormatDec)
		assert.NoError(t, err)
		assert.Equal(t, 1234, decVal)
	})

	t.Run("Hexadecimal Conversions", func(t *testing.T) {
		// Test hex normalization
		hexVal, err := converter.ConvertToHex("A1B2", api.FormatHex)
		assert.NoError(t, err)
		assert.Equal(t, "a1b2", hexVal)

		// Test hex passthrough
		result, err := converter.ConvertFromHex("a1b2", api.FormatHex)
		assert.NoError(t, err)
		assert.Equal(t, "a1b2", result)
	})

	t.Run("Text Conversions", func(t *testing.T) {
		// Test text to hex (little-endian)
		hexVal, err := converter.ConvertToHex("AB", api.FormatText)
		assert.NoError(t, err)
		assert.Equal(t, "4241", hexVal) // Little-endian B(0x42) A(0x41)

		// Test hex to text
		textVal, err := converter.ConvertFromHex("4241", api.FormatText)
		assert.NoError(t, err)
		assert.Equal(t, "AB", textVal)
	})

	t.Run("Format Validation", func(t *testing.T) {
		// Test valid formats
		assert.True(t, converter.IsValidFormat("dec"))
		assert.True(t, converter.IsValidFormat("hex"))
		assert.True(t, converter.IsValidFormat("text"))
		assert.True(t, converter.IsValidFormat("DEC")) // Case insensitive

		// Test invalid format
		assert.False(t, converter.IsValidFormat("invalid"))
	})
}

// TestEndToEndAPIFlow simulates a complete API request flow
func TestEndToEndAPIFlow(t *testing.T) {
	// Create API server
	cfg := createTestConfig()
	registry := domain.NewDeviceRegistry()
	setupTestRegistry(registry)

	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(t, apiServer)

	// Create test server for API requests
	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	t.Run("Datalogger Register Read", func(t *testing.T) {
		// This will timeout since no real device is connected, but tests the flow
		resp, err := http.Get(testServer.URL + "/datalogger?serial=TEST_DL_001&command=19&register=0001&format=dec")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get timeout or not found since no device connected
		assert.True(t, resp.StatusCode >= 400)
	})

	t.Run("Inverter Register Operations", func(t *testing.T) {
		// Test GET request
		resp, err := http.Get(testServer.URL + "/inverter?serial=TEST_DL_001&invserial=TEST_INV_001&command=05&register=0001&format=hex")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get timeout or not found since no device connected
		assert.True(t, resp.StatusCode >= 400)

		// Test PUT request
		req, err := http.NewRequest("PUT", testServer.URL+"/inverter?serial=TEST_DL_001&invserial=TEST_INV_001&command=06&register=0001&value=1234&format=dec", nil)
		require.NoError(t, err)

		client := &http.Client{}
		resp, err = client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get timeout or not found since no device connected
		assert.True(t, resp.StatusCode >= 400)
	})

	t.Run("Multi-register Operations", func(t *testing.T) {
		// Test multi-register GET
		resp, err := http.Get(testServer.URL + "/multiregister?serial=TEST_DL_001&invserial=TEST_INV_001&command=10&registers=0001,0002,0003&format=dec")
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get timeout or not found since no device connected
		assert.True(t, resp.StatusCode >= 400)

		// Test multi-register PUT
		req, err := http.NewRequest("PUT", testServer.URL+"/multiregister?serial=TEST_DL_001&invserial=TEST_INV_001&command=10&registers=0001,0002&values=1234,5678&format=dec", nil)
		require.NoError(t, err)

		client := &http.Client{}
		resp, err = client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get timeout or not found since no device connected
		assert.True(t, resp.StatusCode >= 400)
	})
}

// Helper functions

func createTestConfig() *config.Config {
	return &config.Config{
		Server: struct {
			Host string `mapstructure:"host"`
			Port int    `mapstructure:"port"`
		}{
			Host: "localhost",
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

func setupTestRegistry(registry *domain.DeviceRegistry) {
	// Add test dataloggers
	registry.RegisterDatalogger("TEST_DL_001", "192.168.1.100", 5279, protocol.ProtocolInverterWrite)
	registry.RegisterDatalogger("TEST_DL_002", "192.168.1.101", 5279, protocol.ProtocolInverterWrite)

	// Add test inverters
	registry.RegisterInverter("TEST_DL_001", "TEST_INV_001", "01")
	registry.RegisterInverter("TEST_DL_001", "TEST_INV_002", "02")
	registry.RegisterInverter("TEST_DL_002", "TEST_INV_003", "01")
}

// BenchmarkHTTPAPIPerformance benchmarks API performance
func BenchmarkHTTPAPIPerformance(b *testing.B) {
	cfg := createTestConfig()
	registry := domain.NewDeviceRegistry()
	setupTestRegistry(registry)

	apiServer := createTestAPIServer(cfg, registry)
	require.NotNil(b, apiServer)

	testServer := httptest.NewServer(apiServer.GetRouter())
	defer testServer.Close()

	b.Run("StatusEndpoint", func(b *testing.B) {
		client := &http.Client{Timeout: 5 * time.Second}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := client.Get(testServer.URL + "/api/v1/status")
				if err == nil {
					resp.Body.Close()
				}
			}
		})
	})

	b.Run("ListDataloggers", func(b *testing.B) {
		client := &http.Client{Timeout: 5 * time.Second}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := client.Get(testServer.URL + "/api/v1/dataloggers")
				if err == nil {
					resp.Body.Close()
				}
			}
		})
	})

	b.Run("FormatConversion", func(b *testing.B) {
		converter := api.NewFormatConverter()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				converter.ConvertToHex("1234", api.FormatDec)
				converter.ConvertFromHex("04d2", api.FormatDec)
			}
		})
	})
}

// createTestAPIServer creates an API server with optimized timeout for testing
func createTestAPIServer(cfg *config.Config, registry domain.Registry) *api.Server {
	apiServer := api.NewServer(cfg, registry)
	// Set very short timeout for tests to avoid stalling
	apiServer.SetRequestTimeout(50 * time.Millisecond)
	return apiServer
}
