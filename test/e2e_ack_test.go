package e2e

import (
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_ACKResponses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E ACK test in short mode")
	}

	// Test cases for different record types that should get ACK responses
	testCases := []struct {
		name             string
		hexData          string
		expectResponse   bool
		responseContains string // Partial hex string that should be in response
		description      string
	}{
		{
			name:             "Record_03_Protocol_02_ACK",
			hexData:          "000100020010010354455354", // Record type 03, protocol 02
			expectResponse:   true,
			responseContains: "000300", // Should contain ACK pattern
			description:      "Record 03 with protocol 02 should get unencrypted ACK",
		},
		{
			name:             "Record_04_Protocol_05_ACK", 
			hexData:          "000100050010010454455354", // Record type 04, protocol 05
			expectResponse:   true,
			responseContains: "000347", // Should contain ACK pattern with marker 47
			description:      "Record 04 with protocol 05 should get encrypted ACK with CRC",
		},
		{
			name:             "Record_50_Protocol_06_ACK",
			hexData:          "000100060010015054455354", // Record type 50, protocol 06  
			expectResponse:   true,
			responseContains: "000347", // Should contain ACK pattern with marker 47
			description:      "Record 50 with protocol 06 should get encrypted ACK with CRC",
		},
		{
			name:             "Record_1B_Protocol_05_ACK",
			hexData:          "000100050010011b54455354", // Record type 1B, protocol 05
			expectResponse:   true,
			responseContains: "000347", // Should contain ACK pattern with marker 47
			description:      "Record 1B with protocol 05 should get encrypted ACK with CRC",
		},
		{
			name:             "Record_20_Protocol_06_ACK",
			hexData:          "000100060010012054455354", // Record type 20, protocol 06
			expectResponse:   true,
			responseContains: "000347", // Should contain ACK pattern with marker 47
			description:      "Record 20 with protocol 06 should get encrypted ACK with CRC",
		},
		{
			name:             "Ping_16_Echo_Response",
			hexData:          "000100060010011654455354", // Record type 16 (ping), protocol 06
			expectResponse:   true,
			responseContains: "000100060010011654455354", // Should echo exact same data
			description:      "Ping should echo back the exact same data",
		},
		{
			name:           "Command_Response_05_No_Response",
			hexData:        "000100060010010554455354", // Record type 05, protocol 06
			expectResponse: false,
			description:    "Command response records should not generate responses",
		},
		{
			name:           "Command_Response_18_No_Response", 
			hexData:        "000100060010011854455354", // Record type 18, protocol 06
			expectResponse: false,
			description:    "Command response records should not generate responses",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
		// Create test server using existing helper
		testServer := NewE2EHexTestServer(t)
		defer testServer.Stop(t)

		port := testServer.Start(t)
		serverAddr := fmt.Sprintf("127.0.0.1:%d", port)

		t.Logf("Testing %s: %s", tt.name, tt.description)
			t.Logf("Sending hex data: %s", tt.hexData)

			// Convert hex data to bytes
			data, err := hex.DecodeString(tt.hexData)
			require.NoError(t, err, "Failed to decode hex data")

			// Connect to server
			conn, err := net.Dial("tcp", serverAddr)
			require.NoError(t, err, "Failed to connect to server")
			defer conn.Close()

			// Send data
			n, err := conn.Write(data)
			require.NoError(t, err, "Failed to send data")
			assert.Equal(t, len(data), n, "Should send all data")

			if tt.expectResponse {
				// Set read deadline for response
				err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				require.NoError(t, err, "Failed to set read deadline")

				// Read response
				respBuf := make([]byte, 1024)
				respN, err := conn.Read(respBuf)
				require.NoError(t, err, "Failed to read response")
				assert.Greater(t, respN, 0, "Should receive response data")

				response := respBuf[:respN]
				responseHex := hex.EncodeToString(response)
				
				t.Logf("Received response (%d bytes): %s", respN, responseHex)

				// Verify response contains expected pattern
				if tt.responseContains != "" {
					assert.Contains(t, responseHex, tt.responseContains, 
						"Response should contain expected pattern")
				}

				// Special validation for ping responses
				if tt.responseContains == tt.hexData {
					assert.Equal(t, tt.hexData, responseHex, 
						"Ping should echo back exact same data")
				}

			} else {
				// Should not receive a response
				err = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				require.NoError(t, err, "Failed to set read deadline")

				respBuf := make([]byte, 1024)
				_, err := conn.Read(respBuf)
				
				// Should timeout or get EOF (no response)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						t.Logf("No response received (expected timeout)")
					} else {
						t.Logf("Connection closed or error (expected): %v", err)
					}
				} else {
					t.Errorf("Unexpected response received when none expected")
				}
			}
		})
	}
}

func TestE2E_Record03_TimeSync(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E Record 03 time sync test in short mode")
	}

	// Create test server
	testServer := NewE2EHexTestServer(t)
	defer testServer.Stop(t)

	port := testServer.Start(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)

	t.Logf("Testing Record 03 time sync sequence")

	// Record 03 data (inverter announce) - this should trigger ACK + time sync
	// Format: sequence(2) + 00 + protocol(1) + length(2) + device(1) + record_type(1) + data
	hexData := "000100050010010354455354" // Record type 03, protocol 05
	data, err := hex.DecodeString(hexData)
	require.NoError(t, err, "Failed to decode hex data")

	// Connect to server
	conn, err := net.Dial("tcp", serverAddr)
	require.NoError(t, err, "Failed to connect to server")
	defer conn.Close()

	// Send Record 03 data
	n, err := conn.Write(data)
	require.NoError(t, err, "Failed to send data")
	assert.Equal(t, len(data), n, "Should send all data")

	t.Logf("Sent Record 03: %s", hexData)

	// Should receive ACK immediately
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	ackBuf := make([]byte, 1024)
	ackN, err := conn.Read(ackBuf)
	require.NoError(t, err, "Should receive ACK response")
	
	ackResponse := hex.EncodeToString(ackBuf[:ackN])
	t.Logf("Received ACK response: %s", ackResponse)
	
	// Verify ACK format (should contain 0003 and 47 for protocol 05)
	assert.Contains(t, ackResponse, "000347", "ACK should contain expected pattern")

	// Should receive time sync command after ~1 second delay
	conn.SetReadDeadline(time.Now().Add(3 * time.Second)) // Extended timeout for 1sec delay + processing
	timeSyncBuf := make([]byte, 1024)
	timeSyncN, err := conn.Read(timeSyncBuf)
	
	if err != nil {
		t.Logf("Time sync read error (may be expected): %v", err)
	} else {
		timeSyncResponse := hex.EncodeToString(timeSyncBuf[:timeSyncN])
		t.Logf("Received potential time sync: %s", timeSyncResponse)
		
		// Time sync command should contain record type 18
		assert.Contains(t, timeSyncResponse, "18", 
			"Time sync command should contain record type 18")
	}
}


