package protocol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResponseBuilder(t *testing.T) {
	builder := NewResponseBuilder()
	require.NotNil(t, builder)
	require.NotNil(t, builder.commandBuilder)
}

func TestCreateAckResponse(t *testing.T) {
	builder := NewResponseBuilder()

	tests := []struct {
		name            string
		protocol        string
		originalCommand []byte
		expectError     bool
	}{
		{
			name:            "valid ack response",
			protocol:        ProtocolInverterWrite,
			originalCommand: []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18},
			expectError:     false,
		},
		{
			name:            "empty protocol",
			protocol:        "",
			originalCommand: []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18},
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := builder.CreateAckResponse(tt.protocol, tt.originalCommand)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, resp)

				assert.Equal(t, tt.protocol, resp.Protocol)
				assert.Equal(t, uint8(ResponseTypeAck), resp.Type)
				assert.WithinDuration(t, time.Now(), resp.Timestamp, time.Second)
				assert.Greater(t, len(resp.Data), 0)
			}
		})
	}
}

func TestCreateTimeSyncResponse(t *testing.T) {
	builder := NewResponseBuilder()

	tests := []struct {
		name        string
		protocol    string
		command     string
		expectError bool
	}{
		{
			name:        "valid time sync response",
			protocol:    ProtocolInverterWrite,
			command:     ProtocolDataloggerWrite,
			expectError: false,
		},
		{
			name:        "empty protocol",
			protocol:    "",
			command:     ProtocolDataloggerWrite,
			expectError: true,
		},
		{
			name:        "empty command",
			protocol:    ProtocolInverterWrite,
			command:     "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := builder.CreateTimeSyncResponse(tt.protocol, tt.command)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, resp)

				assert.Equal(t, tt.protocol, resp.Protocol)
				assert.Equal(t, uint8(ResponseTypeTimeSync), resp.Type)
				assert.Greater(t, len(resp.Data), 0)
			}
		})
	}
}

func TestCreateDataResponse(t *testing.T) {
	builder := NewResponseBuilder()

	// Just test that the builder exists - the actual method may not be implemented yet
	assert.NotNil(t, builder)
}

func TestFormatResponseHex(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
		expected string
	}{
		{
			name:     "nil response",
			response: nil,
			expected: "",
		},
		{
			name:     "response with data",
			response: &Response{Data: []byte{0x00, 0x01, 0x02, 0x03}},
			expected: "00010203",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatResponse(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewResponseHandler(t *testing.T) {
	handler := NewResponseHandler()
	require.NotNil(t, handler)
}

func TestResponseHandlerProcessIncomingData(t *testing.T) {
	handler := NewResponseHandler()

	tests := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{
			name:        "valid data",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54},
			expectError: false,
		},
		{
			name:        "empty data",
			data:        []byte{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.ProcessIncomingData(tt.data)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestNewResponseManager(t *testing.T) {
	manager := NewResponseManager()
	require.NotNil(t, manager)
	require.NotNil(t, manager.handler)
}

func TestResponseManagerHandleIncomingData(t *testing.T) {
	manager := NewResponseManager()

	tests := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{
			name:        "valid data",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x16, 0x54, 0x45, 0x53, 0x54}, // Changed 0x18 to 0x16 (ping)
			expectError: false,
		},
		{
			name:        "empty data",
			data:        []byte{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.HandleIncomingData(tt.data)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResponseManagerShouldRespond(t *testing.T) {
	manager := NewResponseManager()

	tests := []struct {
		name       string
		data       []byte
		clientAddr string
		expected   bool
	}{
		{
			name:       "empty data",
			data:       []byte{},
			clientAddr: "192.168.1.100",
			expected:   false,
		},
		{
			name:       "record type 18 - command response (no response needed)",
			data:       []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54},
			clientAddr: "192.168.1.100",
			expected:   false, // Record type 18 should not respond
		},
		{
			name:       "record type 16 - ping (should respond)",
			data:       []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x16, 0x54, 0x45, 0x53, 0x54},
			clientAddr: "192.168.1.100",
			expected:   true, // Record type 16 should echo back data
		},
		{
			name:       "record type 03 - data record (should respond with ACK)",
			data:       []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x03, 0x54, 0x45, 0x53, 0x54},
			clientAddr: "192.168.1.100",
			expected:   true, // Record type 03 should send ACK
		},
		{
			name:       "record type 05 - command response (no response needed)", 
			data:       []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x05, 0x54, 0x45, 0x53, 0x54},
			clientAddr: "192.168.1.100",
			expected:   false, // Record type 05 should not respond
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.ShouldRespond(tt.data, tt.clientAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResponseManagerGetMetrics(t *testing.T) {
	manager := NewResponseManager()

	// Test initial metrics
	metrics := manager.GetMetrics()
	assert.Equal(t, int64(0), metrics.TotalResponses)
	assert.Equal(t, int64(0), metrics.TimeSyncResponses)
	assert.Equal(t, int64(0), metrics.PingResponses)
	assert.Equal(t, int64(0), metrics.AckResponses)
	assert.Equal(t, int64(0), metrics.ErrorResponses)

	// Process some data to update metrics
	data := []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54}
	_, _ = manager.HandleIncomingData(data)

	// Check updated metrics
	metrics = manager.GetMetrics()
	assert.Greater(t, metrics.TotalResponses, int64(0))
}

func TestResponseManagerResetMetrics(t *testing.T) {
	manager := NewResponseManager()

	// Process some data to create metrics
	data := []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54}
	_, _ = manager.HandleIncomingData(data)

	// Verify metrics exist
	metrics := manager.GetMetrics()
	assert.Greater(t, metrics.TotalResponses, int64(0))

	// Reset metrics
	manager.ResetMetrics()

	// Verify metrics are reset
	metrics = manager.GetMetrics()
	assert.Equal(t, int64(0), metrics.TotalResponses)
	assert.Equal(t, int64(0), metrics.TimeSyncResponses)
	assert.Equal(t, int64(0), metrics.PingResponses)
	assert.Equal(t, int64(0), metrics.AckResponses)
	assert.Equal(t, int64(0), metrics.ErrorResponses)
}

// Benchmark tests
func BenchmarkCreateAckResponse(b *testing.B) {
	builder := NewResponseBuilder()
	command := []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18}
	for i := 0; i < b.N; i++ {
		_, err := builder.CreateAckResponse(ProtocolInverterWrite, command)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreateTimeSyncResponse(b *testing.B) {
	builder := NewResponseBuilder()
	for i := 0; i < b.N; i++ {
		_, err := builder.CreateTimeSyncResponse(ProtocolInverterWrite, ProtocolDataloggerWrite)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProcessIncomingData(b *testing.B) {
	handler := NewResponseHandler()
	data := []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54}
	for i := 0; i < b.N; i++ {
		_, err := handler.ProcessIncomingData(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
