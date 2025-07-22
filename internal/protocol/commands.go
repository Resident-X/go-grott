// Package protocol provides command generation and response handling for Growatt inverter communication.
package protocol

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sigurn/crc16"
)

// Command types for inverter communication.
const (
	CommandTypeTimeSync    = 0x18
	CommandTypePing        = 0x16
	CommandTypeRegRead     = 0x03
	CommandTypeRegWrite    = 0x06
	CommandTypeIdentify    = 0x05
	CommandTypeDatalogConf = 0x19
)

// Protocol versions and command types.
const (
	ProtocolTCP             = "02" // TCP protocol (unencrypted)
	ProtocolInverterRead    = "05" // Inverter read command
	ProtocolInverterWrite   = "06" // Inverter write command
	ProtocolV8              = "08" // Protocol version 8
	ProtocolV9              = "09" // Protocol version 9
	ProtocolMultiRegister   = "10" // Multi-register command
	ProtocolDataloggerWrite = "18" // Datalogger write command
	ProtocolDataloggerRead  = "19" // Datalogger read command
)

// Protocol byte values for header[3] - these match the string constants above
const (
	ProtocolTCPByte             = 0x02 // TCP protocol (unencrypted)
	ProtocolInverterReadByte    = 0x05 // Inverter read command
	ProtocolInverterWriteByte   = 0x06 // Inverter write command
	ProtocolV8Byte              = 0x08 // Protocol version 8
	ProtocolV9Byte              = 0x09 // Protocol version 9
	ProtocolMultiRegisterByte   = 0x10 // Multi-register command
	ProtocolDataloggerWriteByte = 0x18 // Datalogger write command
	ProtocolDataloggerReadByte  = 0x19 // Datalogger read command
)

// Device ID constants
const (
	DeviceDatalogger     = "01" // Standard datalogger device ID
	DeviceDataloggerByte = 0x01 // Standard datalogger device ID byte value
)

// ProtocolStringToByte converts protocol string to byte value for headers.
func ProtocolStringToByte(protocol string) byte {
	switch protocol {
	case ProtocolTCP:
		return ProtocolTCPByte
	case ProtocolInverterRead:
		return ProtocolInverterReadByte
	case ProtocolInverterWrite:
		return ProtocolInverterWriteByte
	case ProtocolV8:
		return ProtocolV8Byte
	case ProtocolV9:
		return ProtocolV9Byte
	case ProtocolMultiRegister:
		return ProtocolMultiRegisterByte
	case ProtocolDataloggerWrite:
		return ProtocolDataloggerWriteByte
	case ProtocolDataloggerRead:
		return ProtocolDataloggerReadByte
	default:
		return ProtocolTCPByte // Default to TCP protocol
	}
}

// Register addresses for common operations.
const (
	RegisterTimeSync = 0x001F
	RegisterIPConfig = 0x0011
)

// CommandBuilder provides functionality to create inverter commands.
type CommandBuilder struct {
	crcTable *crc16.Table
}

// NewCommandBuilder creates a new command builder instance.
func NewCommandBuilder() *CommandBuilder {
	// Create CRC16 table for Modbus
	table := crc16.MakeTable(crc16.Params{
		Poly:   0xA001,
		Init:   0xFFFF,
		RefIn:  true,
		RefOut: true,
		XorOut: 0,
	})

	return &CommandBuilder{
		crcTable: table,
	}
}

// TimeSyncCommand represents a time synchronization command.
type TimeSyncCommand struct {
	Protocol   string
	LoggerID   string
	SequenceNo uint16
	Timestamp  time.Time
}

// CreateTimeSyncCommand generates a time synchronization command for an inverter.
func (cb *CommandBuilder) CreateTimeSyncCommand(protocol, loggerID string, sequenceNo uint16) (*TimeSyncCommand, []byte, error) {
	if protocol == "" || loggerID == "" {
		return nil, nil, fmt.Errorf("protocol and loggerID cannot be empty")
	}

	cmd := &TimeSyncCommand{
		Protocol:   protocol,
		LoggerID:   loggerID,
		SequenceNo: sequenceNo,
		Timestamp:  time.Now(),
	}

	data, err := cb.buildTimeSyncData(cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build time sync data: %w", err)
	}

	return cmd, data, nil
}

// buildTimeSyncData constructs the binary data for a time sync command.
func (cb *CommandBuilder) buildTimeSyncData(cmd *TimeSyncCommand) ([]byte, error) {
	// Build command body
	body, err := cb.buildTimeSyncBody(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to build command body: %w", err)
	}

	// Create header
	header := cb.buildHeader(cmd.Protocol, len(body)+2, CommandTypeTimeSync)

	// Combine header and body
	fullCommand := append(header, body...)

	// Add CRC if not protocol 02
	if cmd.Protocol != ProtocolTCP {
		// Encrypt the command first
		encrypted := cb.encryptData(fullCommand)

		// Calculate CRC
		crc := crc16.Checksum(encrypted, cb.crcTable)
		crcBytes := []byte{byte(crc & 0xFF), byte(crc >> 8)}

		return append(encrypted, crcBytes...), nil
	}

	return fullCommand, nil
}

// buildTimeSyncBody creates the body portion of the time sync command.
func (cb *CommandBuilder) buildTimeSyncBody(cmd *TimeSyncCommand) ([]byte, error) {
	var body []byte

	// Add logger ID (encoded as UTF-8)
	loggerBytes := []byte(cmd.LoggerID)
	body = append(body, loggerBytes...)

	// Add padding for protocol 06
	if cmd.Protocol == ProtocolInverterWrite {
		padding := make([]byte, 20) // 20 bytes of zeros
		body = append(body, padding...)
	}

	// Add register address (time sync register)
	registerBytes := []byte{
		byte(RegisterTimeSync >> 8),
		byte(RegisterTimeSync & 0xFF),
	}
	body = append(body, registerBytes...)

	// Add timestamp
	timeStr := cmd.Timestamp.Format("2006-01-02 15:04:05")
	timeBytes := []byte(timeStr)

	// Add time length (2 bytes, big endian)
	timeLen := len(timeBytes)
	timeLenBytes := []byte{
		byte(timeLen >> 8),
		byte(timeLen & 0xFF),
	}
	body = append(body, timeLenBytes...)
	body = append(body, timeBytes...)

	return body, nil
}

// buildHeader creates the command header.
func (cb *CommandBuilder) buildHeader(protocol string, bodyLen int, cmdType uint8) []byte {
	header := make([]byte, 10)

	// Fixed header parts
	header[0] = 0x00 // Header byte 0
	header[1] = 0x01 // Header byte 1
	header[2] = 0x00 // Header byte 2

	// Protocol - use helper function to convert string to byte
	header[3] = ProtocolStringToByte(protocol)

	// Body length (2 bytes, big endian)
	header[4] = byte(bodyLen >> 8)
	header[5] = byte(bodyLen & 0xFF)

	// Command type
	header[6] = 0x01 // Fixed
	header[7] = cmdType

	return header
}

// encryptData encrypts command data using the Growatt XOR mask.
func (cb *CommandBuilder) encryptData(data []byte) []byte {
	mask := []byte("Growatt")
	result := make([]byte, len(data))

	// Copy header unchanged (first 8 bytes)
	copy(result[:8], data[:8])

	// XOR the rest with the mask
	for i := 8; i < len(data); i++ {
		maskIdx := (i - 8) % len(mask)
		result[i] = data[i] ^ mask[maskIdx]
	}

	return result
}

// PingCommand represents a keep-alive ping command.
type PingCommand struct {
	Protocol   string
	LoggerID   string
	SequenceNo uint16
}

// CreatePingCommand generates a ping/keep-alive command.
func (cb *CommandBuilder) CreatePingCommand(protocol, loggerID string, sequenceNo uint16) (*PingCommand, []byte, error) {
	if protocol == "" || loggerID == "" {
		return nil, nil, fmt.Errorf("protocol and loggerID cannot be empty")
	}

	cmd := &PingCommand{
		Protocol:   protocol,
		LoggerID:   loggerID,
		SequenceNo: sequenceNo,
	}

	// Build simple ping command
	header := cb.buildHeader(protocol, len(loggerID), CommandTypePing)
	body := []byte(loggerID)
	fullCommand := append(header, body...)

	// Add CRC if not protocol 02
	if protocol != ProtocolTCP {
		encrypted := cb.encryptData(fullCommand)
		crc := crc16.Checksum(encrypted, cb.crcTable)
		crcBytes := []byte{byte(crc & 0xFF), byte(crc >> 8)}
		return cmd, append(encrypted, crcBytes...), nil
	}

	return cmd, fullCommand, nil
}

// ValidateCommand checks if a received command is valid.
func (cb *CommandBuilder) ValidateCommand(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("command too short: %d bytes", len(data))
	}

	// Check if this looks like an encrypted command (has CRC)
	if len(data) >= 12 {
		// Try to validate CRC
		dataPart := data[:len(data)-2]
		receivedCRC := uint16(data[len(data)-2]) | uint16(data[len(data)-1])<<8
		calculatedCRC := crc16.Checksum(dataPart, cb.crcTable)

		if receivedCRC != calculatedCRC {
			return fmt.Errorf("CRC validation failed: expected 0x%04X, got 0x%04X", receivedCRC, calculatedCRC)
		}
	}

	return nil
}

// CommandInfo extracts basic information from a command.
type CommandInfo struct {
	Protocol string
	Command  uint8
	BodyLen  int
	IsValid  bool
}

// ParseCommandInfo extracts command information without full parsing.
func (cb *CommandBuilder) ParseCommandInfo(data []byte) *CommandInfo {
	if len(data) < 8 {
		return &CommandInfo{IsValid: false}
	}

	info := &CommandInfo{
		Protocol: fmt.Sprintf("%02x", data[3]),
		Command:  data[7],
		BodyLen:  int(data[4])<<8 | int(data[5]),
		IsValid:  true,
	}

	return info
}

// FormatCommandHex returns a hex representation of command data for logging.
func FormatCommandHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return hex.EncodeToString(data)
}
