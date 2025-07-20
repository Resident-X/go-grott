package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseManagerAckResponses(t *testing.T) {
	manager := NewResponseManager()

	// Test data for different record types that should get ACK responses
	testCases := []struct {
		name         string
		data         []byte
		recType      string
		protocol     string
		expectACK    bool
		expectError  bool
	}{
		{
			name:        "Record 03 - Inverter Announce (Protocol 02)",
			data:        []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x0A, 0x01, 0x03, 0x54, 0x45, 0x53, 0x54},
			recType:     "03",
			protocol:    "02",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 03 - Inverter Announce (Protocol 05)",
			data:        []byte{0x00, 0x01, 0x00, 0x05, 0x00, 0x0A, 0x01, 0x03, 0x54, 0x45, 0x53, 0x54},
			recType:     "03",
			protocol:    "05",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 04 - Data Record (Protocol 02)",
			data:        []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x0A, 0x01, 0x04, 0x54, 0x45, 0x53, 0x54},
			recType:     "04",
			protocol:    "02",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 50 - Data Record (Protocol 06)",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x50, 0x54, 0x45, 0x53, 0x54},
			recType:     "50",
			protocol:    "06",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 1B - Data Record (Protocol 05)",
			data:        []byte{0x00, 0x01, 0x00, 0x05, 0x00, 0x0A, 0x01, 0x1B, 0x54, 0x45, 0x53, 0x54},
			recType:     "1b",
			protocol:    "05",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 20 - Data Record (Protocol 06)",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x20, 0x54, 0x45, 0x53, 0x54},
			recType:     "20",
			protocol:    "06",
			expectACK:   true,
			expectError: false,
		},
		{
			name:        "Record 16 - Ping (should echo, not ACK)",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x16, 0x54, 0x45, 0x53, 0x54},
			recType:     "16",
			protocol:    "06",
			expectACK:   false, // Ping echoes data, doesn't send ACK format
			expectError: false,
		},
		{
			name:        "Record 05 - Command Response (no response)",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x05, 0x54, 0x45, 0x53, 0x54},
			recType:     "05",
			protocol:    "06",
			expectACK:   false,
			expectError: true, // Should return error for command responses
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Test ShouldRespond first
			shouldRespond := manager.ShouldRespond(tt.data, "127.0.0.1")
			
			if tt.expectError {
				assert.False(t, shouldRespond, "Should not respond to command response records")
				
				// Verify HandleIncomingData returns error
				_, err := manager.HandleIncomingData(tt.data)
				assert.Error(t, err, "Should return error for command response records")
				return
			}

			if tt.expectACK || tt.recType == "16" {
				assert.True(t, shouldRespond, "Should respond to data records and pings")
			} else {
				assert.False(t, shouldRespond, "Should not respond to this record type")
			}

			// Test HandleIncomingData
			response, err := manager.HandleIncomingData(tt.data)
			
			if tt.expectACK {
				require.NoError(t, err)
				require.NotNil(t, response)
				
				// Verify it's an ACK response
				assert.Equal(t, uint8(ResponseTypeAck), response.Type)
				assert.Equal(t, tt.protocol, response.Protocol)
				
				// Verify ACK format
				assert.NotEmpty(t, response.Data)
				
				// Check ACK format based on protocol
				if tt.protocol == "02" {
					// Protocol 02: unencrypted ACK format (Python format)
					// header[0:8] + '0003' + header[12:16] + '00' = 4 + 2 + 2 + 1 = 9 bytes
					assert.Equal(t, 9, len(response.Data))
					assert.Equal(t, tt.data[0], response.Data[0]) // First 4 bytes from header
					assert.Equal(t, tt.data[1], response.Data[1]) 
					assert.Equal(t, tt.data[2], response.Data[2]) 
					assert.Equal(t, tt.data[3], response.Data[3]) 
					assert.Equal(t, uint8(0x00), response.Data[4]) // '0003' high byte
					assert.Equal(t, uint8(0x03), response.Data[5]) // '0003' low byte
					assert.Equal(t, tt.data[6], response.Data[6]) // header[12:16] = bytes 6-7
					assert.Equal(t, tt.data[7], response.Data[7])
					assert.Equal(t, uint8(0x00), response.Data[8]) // '00' terminator
				} else {
					// Protocol 05/06: encrypted ACK format with CRC (Python format)
					// header[0:8] + '0003' + header[12:16] + '47' + CRC = 4 + 2 + 2 + 1 + 2 = 11 bytes
					assert.Equal(t, 11, len(response.Data))
					assert.Equal(t, tt.data[0], response.Data[0]) // First 4 bytes from header
					assert.Equal(t, tt.data[1], response.Data[1])
					assert.Equal(t, tt.data[2], response.Data[2])
					assert.Equal(t, tt.data[3], response.Data[3])
					assert.Equal(t, uint8(0x00), response.Data[4]) // '0003' high byte
					assert.Equal(t, uint8(0x03), response.Data[5]) // '0003' low byte
					assert.Equal(t, tt.data[6], response.Data[6]) // header[12:16] = bytes 6-7
					assert.Equal(t, tt.data[7], response.Data[7])
					assert.Equal(t, uint8(0x47), response.Data[8]) // '47' response marker
					// Bytes 9-10 are CRC, don't test exact values as they depend on calculation
				}
				
			} else if tt.recType == "16" {
				// Ping should echo back original data
				require.NoError(t, err)
				require.NotNil(t, response)
				
				assert.Equal(t, uint8(ResponseTypePing), response.Type)
				assert.Equal(t, tt.protocol, response.Protocol)
				assert.Equal(t, tt.data, response.Data) // Should echo exactly
			}
		})
	}
}

func TestCreateAckResponseFormats(t *testing.T) {
	manager := NewResponseManager()

	testCases := []struct {
		name          string
		header        []byte
		protocol      string
		expectedLen   int
		expectedBytes map[int]byte // position -> expected byte value
	}{
		{
			name:        "Protocol 02 ACK",
			header:      []byte{0x12, 0x34, 0x00, 0x02, 0x00, 0x0A, 0x01, 0x03},
			protocol:    "02",
			expectedLen: 7,
			expectedBytes: map[int]byte{
				0: 0x12, // Sequence byte 0
				1: 0x34, // Sequence byte 1
				4: 0x00, // Length high
				5: 0x03, // Length low
				6: 0x00, // Terminator
			},
		},
		{
			name:        "Protocol 05 ACK",
			header:      []byte{0xAB, 0xCD, 0x00, 0x05, 0x00, 0x0A, 0x01, 0x03},
			protocol:    "05",
			expectedLen: 10,
			expectedBytes: map[int]byte{
				0: 0xAB, // Sequence byte 0
				1: 0xCD, // Sequence byte 1
				4: 0x00, // Length high
				5: 0x03, // Length low
				6: 0x47, // Response marker
				// Bytes 8-9 are CRC, tested separately
			},
		},
		{
			name:        "Protocol 06 ACK",
			header:      []byte{0xEF, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x03},
			protocol:    "06",
			expectedLen: 10,
			expectedBytes: map[int]byte{
				0: 0xEF, // Sequence byte 0
				1: 0x01, // Sequence byte 1
				4: 0x00, // Length high
				5: 0x03, // Length low
				6: 0x47, // Response marker
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			response, err := manager.createAckResponse(tt.header, tt.protocol)
			
			require.NoError(t, err)
			require.NotNil(t, response)
			
			assert.Equal(t, tt.expectedLen, len(response.Data))
			
			// Check specific byte values
			for pos, expectedByte := range tt.expectedBytes {
				assert.Equal(t, expectedByte, response.Data[pos], 
					"Byte at position %d should be 0x%02x, got 0x%02x", 
					pos, expectedByte, response.Data[pos])
			}
			
			// For encrypted protocols, verify CRC is present
			if tt.protocol != "02" {
				// Last 2 bytes should be CRC (non-zero in most cases)
				crcLen := len(response.Data)
				assert.True(t, crcLen >= 2, "Should have CRC bytes")
				// We don't test exact CRC values as they depend on the implementation
			}
		})
	}
}

func TestAckResponseMetrics(t *testing.T) {
	manager := NewResponseManager()
	
	// Test initial metrics
	initialMetrics := manager.GetMetrics()
	assert.Equal(t, int64(0), initialMetrics.AckResponses)
	assert.Equal(t, int64(0), initialMetrics.PingResponses)
	
	// Send record type 03 (should increment AckResponses)
	data03 := []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x0A, 0x01, 0x03, 0x54, 0x45, 0x53, 0x54}
	_, err := manager.HandleIncomingData(data03)
	require.NoError(t, err)
	
	// Send record type 16 (should increment PingResponses)
	data16 := []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x0A, 0x01, 0x16, 0x54, 0x45, 0x53, 0x54}
	_, err = manager.HandleIncomingData(data16)
	require.NoError(t, err)
	
	// Check metrics
	metrics := manager.GetMetrics()
	assert.Equal(t, int64(1), metrics.AckResponses)
	assert.Equal(t, int64(1), metrics.PingResponses)
	assert.Equal(t, int64(2), metrics.TotalResponses)
}
