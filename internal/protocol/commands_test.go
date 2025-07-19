package protocol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandBuilder(t *testing.T) {
	builder := NewCommandBuilder()
	require.NotNil(t, builder)
	require.NotNil(t, builder.crcTable)
}

func TestCreateTimeSyncCommand(t *testing.T) {
	builder := NewCommandBuilder()

	tests := []struct {
		name        string
		protocol    string
		loggerID    string
		sequenceNo  uint16
		expectError bool
	}{
		{
			name:        "valid protocol 06",
			protocol:    ProtocolInverterWrite,
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: false,
		},
		{
			name:        "valid protocol 05",
			protocol:    ProtocolInverterRead,
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: false,
		},
		{
			name:        "valid protocol 02",
			protocol:    ProtocolTCP,
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: false,
		},
		{
			name:        "empty protocol",
			protocol:    "",
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: true,
		},
		{
			name:        "empty logger ID",
			protocol:    ProtocolInverterWrite,
			loggerID:    "",
			sequenceNo:  1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, data, err := builder.CreateTimeSyncCommand(tt.protocol, tt.loggerID, tt.sequenceNo)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, cmd)
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, cmd)
				require.NotNil(t, data)

				assert.Equal(t, tt.protocol, cmd.Protocol)
				assert.Equal(t, tt.loggerID, cmd.LoggerID)
				assert.Equal(t, tt.sequenceNo, cmd.SequenceNo)
				assert.WithinDuration(t, time.Now(), cmd.Timestamp, time.Second)

				// Verify data is not empty
				assert.Greater(t, len(data), 0)

				// For protocol 06, verify it's longer due to padding
				if tt.protocol == ProtocolInverterWrite {
					assert.Greater(t, len(data), 30) // Should have padding
				}

				// For protocols other than 02, should have CRC
				if tt.protocol != ProtocolTCP {
					assert.Greater(t, len(data), 20) // Should have CRC
				}
			}
		})
	}
}

func TestCreatePingCommand(t *testing.T) {
	builder := NewCommandBuilder()

	tests := []struct {
		name        string
		protocol    string
		loggerID    string
		sequenceNo  uint16
		expectError bool
	}{
		{
			name:        "valid ping command",
			protocol:    ProtocolInverterWrite,
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: false,
		},
		{
			name:        "empty protocol",
			protocol:    "",
			loggerID:    "TEST123456",
			sequenceNo:  1,
			expectError: true,
		},
		{
			name:        "empty logger ID",
			protocol:    ProtocolInverterWrite,
			loggerID:    "",
			sequenceNo:  1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, data, err := builder.CreatePingCommand(tt.protocol, tt.loggerID, tt.sequenceNo)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, cmd)
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, cmd)
				require.NotNil(t, data)

				assert.Equal(t, tt.protocol, cmd.Protocol)
				assert.Equal(t, tt.loggerID, cmd.LoggerID)
				assert.Equal(t, tt.sequenceNo, cmd.SequenceNo)
				assert.Greater(t, len(data), 0)
			}
		})
	}
}

func TestValidateCommand(t *testing.T) {
	builder := NewCommandBuilder()

	tests := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{
			name:        "too short command",
			data:        []byte{0x00, 0x01},
			expectError: true,
		},
		{
			name:        "minimum valid command",
			data:        make([]byte, 10),
			expectError: false,
		},
		{
			name:        "command with CRC validation",
			data:        []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54, 0x00, 0x00},
			expectError: true, // CRC will likely fail on this test data
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := builder.ValidateCommand(tt.data)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseCommandInfo(t *testing.T) {
	builder := NewCommandBuilder()

	tests := []struct {
		name     string
		data     []byte
		expected *CommandInfo
	}{
		{
			name: "too short data",
			data: []byte{0x00, 0x01},
			expected: &CommandInfo{
				IsValid: false,
			},
		},
		{
			name: "valid command info",
			data: []byte{0x00, 0x01, 0x00, 0x10, 0x00, 0x0A, 0x01, 0x18},
			expected: &CommandInfo{
				Protocol: ProtocolMultiRegister,
				Command:  0x18,
				BodyLen:  10,
				IsValid:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := builder.ParseCommandInfo(tt.data)
			assert.Equal(t, tt.expected, info)
		})
	}
}

func TestEncryptData(t *testing.T) {
	builder := NewCommandBuilder()

	data := []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x54, 0x45, 0x53, 0x54}
	encrypted := builder.encryptData(data)

	// Verify header is unchanged
	assert.Equal(t, data[:8], encrypted[:8])

	// Verify data after header is changed
	if len(data) > 8 {
		assert.NotEqual(t, data[8:], encrypted[8:])
	}

	// Verify length is the same
	assert.Equal(t, len(data), len(encrypted))
}

func TestFormatCommandHex(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "",
		},
		{
			name:     "simple data",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			expected: "00010203",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCommandHex(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildHeader(t *testing.T) {
	builder := NewCommandBuilder()

	tests := []struct {
		name     string
		protocol string
		bodyLen  int
		cmdType  uint8
		expected []byte
	}{
		{
			name:     "protocol 06",
			protocol: ProtocolInverterWrite,
			bodyLen:  10,
			cmdType:  CommandTypeTimeSync,
			expected: []byte{0x00, 0x01, 0x00, 0x06, 0x00, 0x0A, 0x01, 0x18, 0x00, 0x00},
		},
		{
			name:     "protocol 05",
			protocol: ProtocolInverterRead,
			bodyLen:  5,
			cmdType:  CommandTypePing,
			expected: []byte{0x00, 0x01, 0x00, 0x05, 0x00, 0x05, 0x01, 0x16, 0x00, 0x00},
		},
		{
			name:     "protocol 02",
			protocol: ProtocolTCP,
			bodyLen:  8,
			cmdType:  CommandTypeIdentify,
			expected: []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x08, 0x01, 0x05, 0x00, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := builder.buildHeader(tt.protocol, tt.bodyLen, tt.cmdType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests
func BenchmarkCreateTimeSyncCommand(b *testing.B) {
	builder := NewCommandBuilder()
	for i := 0; i < b.N; i++ {
		_, _, err := builder.CreateTimeSyncCommand(ProtocolInverterWrite, "TEST123456", uint16(i))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreatePingCommand(b *testing.B) {
	builder := NewCommandBuilder()
	for i := 0; i < b.N; i++ {
		_, _, err := builder.CreatePingCommand(ProtocolInverterWrite, "TEST123456", uint16(i))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncryptData(b *testing.B) {
	builder := NewCommandBuilder()
	data := make([]byte, 100)
	for i := 0; i < b.N; i++ {
		_ = builder.encryptData(data)
	}
}
