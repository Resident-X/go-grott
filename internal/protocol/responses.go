// Package protocol provides response generation and validation for Growatt inverter communication.
package protocol

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sigurn/crc16"
)

// Response types for inverter communication.
const (
	ResponseTypeAck      = 0x01
	ResponseTypeNak      = 0x02
	ResponseTypeData     = 0x04
	ResponseTypeTimeSync = 0x18
	ResponseTypePing     = 0x16
	ResponseTypeIdentify = 0x05
)

// ResponseBuilder provides functionality to create responses to inverter commands.
type ResponseBuilder struct {
	commandBuilder *CommandBuilder
}

// NewResponseBuilder creates a new response builder instance.
func NewResponseBuilder() *ResponseBuilder {
	return &ResponseBuilder{
		commandBuilder: NewCommandBuilder(),
	}
}

// Response represents a response to be sent to an inverter.
type Response struct {
	Type      uint8
	Protocol  string
	Data      []byte
	Timestamp time.Time
}

// CreateAckResponse creates an acknowledgment response.
func (rb *ResponseBuilder) CreateAckResponse(protocol string, originalCommand []byte) (*Response, error) {
	if protocol == "" {
		return nil, fmt.Errorf("protocol cannot be empty")
	}

	response := &Response{
		Type:      ResponseTypeAck,
		Protocol:  protocol,
		Timestamp: time.Now(),
	}

	data, err := rb.buildAckResponse(protocol, originalCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to build ack response: %w", err)
	}

	response.Data = data
	return response, nil
}

// CreateTimeSyncResponse creates a response to a time sync request.
func (rb *ResponseBuilder) CreateTimeSyncResponse(protocol, loggerID string) (*Response, error) {
	if protocol == "" || loggerID == "" {
		return nil, fmt.Errorf("protocol and loggerID cannot be empty")
	}

	response := &Response{
		Type:      ResponseTypeTimeSync,
		Protocol:  protocol,
		Timestamp: time.Now(),
	}

	// Create time sync command as response
	_, data, err := rb.commandBuilder.CreateTimeSyncCommand(protocol, loggerID, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create time sync response: %w", err)
	}

	response.Data = data
	return response, nil
}

// CreatePingResponse creates a response to a ping request.
func (rb *ResponseBuilder) CreatePingResponse(protocol, loggerID string) (*Response, error) {
	if protocol == "" || loggerID == "" {
		return nil, fmt.Errorf("protocol and loggerID cannot be empty")
	}

	response := &Response{
		Type:      ResponseTypePing,
		Protocol:  protocol,
		Timestamp: time.Now(),
	}

	// Create ping command as response
	_, data, err := rb.commandBuilder.CreatePingCommand(protocol, loggerID, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create ping response: %w", err)
	}

	response.Data = data
	return response, nil
}

// buildAckResponse constructs a simple acknowledgment response.
func (rb *ResponseBuilder) buildAckResponse(protocol string, originalCommand []byte) ([]byte, error) {
	// Build minimal ACK header
	header := make([]byte, 8)
	header[0] = 0x00 // Header byte 0
	header[1] = 0x01 // Header byte 1
	header[2] = 0x00 // Header byte 2

	// Protocol
	if protocol == ProtocolV5 {
		header[3] = 0x05
	} else if protocol == ProtocolV6 {
		header[3] = 0x06
	} else {
		header[3] = 0x02
	}

	// Body length (0 for simple ACK)
	header[4] = 0x00
	header[5] = 0x00

	// Response type
	header[6] = 0x01
	header[7] = ResponseTypeAck

	return header, nil
}

// ResponseHandler manages response logic and decision making.
type ResponseHandler struct {
	responseBuilder *ResponseBuilder
	commandBuilder  *CommandBuilder
}

// NewResponseHandler creates a new response handler instance.
func NewResponseHandler() *ResponseHandler {
	return &ResponseHandler{
		responseBuilder: NewResponseBuilder(),
		commandBuilder:  NewCommandBuilder(),
	}
}

// ProcessIncomingData determines if and how to respond to incoming data.
func (rh *ResponseHandler) ProcessIncomingData(data []byte) (*Response, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data received")
	}

	// Parse command info
	cmdInfo := rh.commandBuilder.ParseCommandInfo(data)
	if !cmdInfo.IsValid {
		return nil, fmt.Errorf("invalid command format")
	}

	// Extract logger ID if possible
	loggerID, err := rh.extractLoggerID(data, cmdInfo)
	if err != nil {
		// If we can't extract logger ID, don't send a response
		return nil, fmt.Errorf("failed to extract logger ID: %w", err)
	}

	// Determine response based on command type
	switch cmdInfo.Command {
	case CommandTypeTimeSync:
		return rh.responseBuilder.CreateTimeSyncResponse(cmdInfo.Protocol, loggerID)
	case CommandTypePing:
		return rh.responseBuilder.CreatePingResponse(cmdInfo.Protocol, loggerID)
	case CommandTypeIdentify:
		// For identify commands, we typically just acknowledge
		return rh.responseBuilder.CreateAckResponse(cmdInfo.Protocol, data)
	default:
		// For unknown commands, return an error instead of nil, nil
		return nil, fmt.Errorf("unknown command type: %d", cmdInfo.Command)
	}
}

// extractLoggerID attempts to extract the logger ID from command data.
func (rh *ResponseHandler) extractLoggerID(data []byte, cmdInfo *CommandInfo) (string, error) {
	if len(data) < 10 {
		return "", fmt.Errorf("data too short to contain logger ID")
	}

	// For most commands, logger ID starts at byte 8
	startPos := 8

	// Try to find a reasonable logger ID length (typically 10 characters)
	maxLen := min(cmdInfo.BodyLen, 20) // Reasonable maximum
	if startPos+maxLen > len(data) {
		maxLen = len(data) - startPos
	}

	if maxLen <= 0 {
		return "", fmt.Errorf("no space for logger ID")
	}

	// Extract and clean the logger ID
	loggerBytes := data[startPos : startPos+maxLen]
	loggerID := cleanLoggerID(string(loggerBytes))

	if len(loggerID) == 0 {
		return "", fmt.Errorf("empty logger ID")
	}

	return loggerID, nil
}

// cleanLoggerID removes non-printable characters from logger ID.
func cleanLoggerID(id string) string {
	var result []byte
	for _, b := range []byte(id) {
		if b >= 32 && b <= 126 { // Printable ASCII
			result = append(result, b)
		} else if b == 0 { // Stop at null terminator
			break
		}
	}
	return string(result)
}

// ResponseMetrics holds metrics about response generation.
type ResponseMetrics struct {
	TotalResponses    int64
	TimeSyncResponses int64
	PingResponses     int64
	AckResponses      int64
	ErrorResponses    int64
	LastResponseTime  time.Time
}

// ResponseManager manages response generation with metrics and logging.
type ResponseManager struct {
	handler *ResponseHandler
	metrics *ResponseMetrics
}

// NewResponseManager creates a new response manager instance.
func NewResponseManager() *ResponseManager {
	return &ResponseManager{
		handler: NewResponseHandler(),
		metrics: &ResponseMetrics{},
	}
}

// HandleIncomingData processes incoming data and generates appropriate responses following Python logic.
func (rm *ResponseManager) HandleIncomingData(data []byte) (*Response, error) {
	if len(data) < 8 {
		rm.metrics.ErrorResponses++
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	// Extract header information
	header := data[:8]
	protocol := fmt.Sprintf("%02x", header[3])
	recType := fmt.Sprintf("%02x", header[7])

	var response *Response
	var err error

	// Update metrics
	rm.metrics.LastResponseTime = time.Now()
	rm.metrics.TotalResponses++

	// Process based on record type following Python logic
	switch recType {
	case "16": // Ping - echo back the original data
		response = &Response{
			Type:      ResponseTypePing,
			Protocol:  protocol,
			Data:      make([]byte, len(data)), // Copy the data
			Timestamp: time.Now(),
		}
		copy(response.Data, data) // Echo back original data
		rm.metrics.PingResponses++

	case "03", "04", "50", "1b", "20": // Data records - send ACK
		response, err = rm.createAckResponse(header, protocol)
		if err != nil {
			rm.metrics.ErrorResponses++
			return nil, fmt.Errorf("failed to create ACK response: %w", err)
		}
		rm.metrics.AckResponses++

		// Special handling for rectype "03" - schedule time sync after ACK
		if recType == "03" {
			// Note: The time sync scheduling will be handled by the caller
			// after sending this ACK response, as it requires session context
		}

	case "19", "05", "06", "18": // Command responses - no response needed
		return nil, fmt.Errorf("command response records do not require acknowledgment")

	case "10", "29": // Other records - no response needed  
		return nil, fmt.Errorf("records of type %s do not require acknowledgment", recType)

	default:
		// Unknown record type - return error instead of nil, nil
		return nil, fmt.Errorf("unknown record type: %s", recType)
	}

	return response, err
}

// GetMetrics returns current response metrics.
func (rm *ResponseManager) GetMetrics() ResponseMetrics {
	return *rm.metrics
}

// ResetMetrics resets all response metrics.
func (rm *ResponseManager) ResetMetrics() {
	rm.metrics = &ResponseMetrics{}
}

// ShouldRespond determines if we should respond to incoming data based on record type.
func (rm *ResponseManager) ShouldRespond(data []byte, clientAddr string) bool {
	if len(data) < 8 {
		return false
	}

	// Extract record type from bytes 7-8 (0-indexed) as hex string
	recType := fmt.Sprintf("%02x", data[7])

	// Determine if response is needed based on Python logic
	switch recType {
	case "16": // Ping - echo back data
		return true
	case "03", "04", "50", "1b", "20": // Data records - send ACK
		return true
	case "19", "05", "06", "18": // Command responses - no response
		return false
	case "10", "29": // Other records - no response
		return false
	default:
		return false
	}
}

// createAckResponse creates an ACK response following Python logic.
func (rm *ResponseManager) createAckResponse(header []byte, protocol string) (*Response, error) {
	var responseData []byte

	if protocol == "02" {
		// Protocol 02, unencrypted ACK
		// Python: header[0:8] + '0003' + header[12:16] + '00'
		// But header[12:16] doesn't exist in 8-byte header, so we use sequence number from 0:4
		responseData = make([]byte, 7)
		copy(responseData[0:4], header[0:4]) // Copy sequence number
		responseData[4] = 0x00              // Length high byte
		responseData[5] = 0x03              // Length low byte  
		responseData[6] = 0x00              // Terminator
	} else {
		// Protocol 05/06, encrypted ACK with CRC
		// Python: headerackx = header[0:8] + '0003' + header[12:16] + '47'
		headerAck := make([]byte, 8)
		copy(headerAck[0:4], header[0:4]) // Copy sequence number  
		copy(headerAck[4:8], header[4:8]) // Copy rest of header
		headerAck[4] = 0x00               // Length high byte
		headerAck[5] = 0x03               // Length low byte
		headerAck[6] = 0x47               // Response marker

		// Calculate CRC16 Modbus
		crc := rm.calculateModbusCRC(headerAck)
		
		// Combine header + CRC
		responseData = make([]byte, len(headerAck)+2)
		copy(responseData, headerAck)
		responseData[len(headerAck)] = byte(crc & 0xFF)
		responseData[len(headerAck)+1] = byte(crc >> 8)
	}

	return &Response{
		Type:      ResponseTypeAck,
		Protocol:  protocol,
		Data:      responseData,
		Timestamp: time.Now(),
	}, nil
}

// calculateModbusCRC calculates CRC16 Modbus checksum.
func (rm *ResponseManager) calculateModbusCRC(data []byte) uint16 {
	// Use the existing CRC table from CommandBuilder
	table := rm.handler.commandBuilder.crcTable
	return crc16.Checksum(data, table)
}

// FormatResponse returns a hex representation of response data for logging.
func FormatResponse(response *Response) string {
	if response == nil || len(response.Data) == 0 {
		return ""
	}
	return hex.EncodeToString(response.Data)
}

// Helper function for minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
