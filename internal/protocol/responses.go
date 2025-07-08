// Package protocol provides response generation and validation for Growatt inverter communication.
package protocol

import (
	"encoding/hex"
	"fmt"
	"time"
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
		return nil, nil
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
		// For unknown commands, don't respond
		return nil, nil
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

// HandleIncomingData processes incoming data and generates appropriate responses.
func (rm *ResponseManager) HandleIncomingData(data []byte) (*Response, error) {
	response, err := rm.handler.ProcessIncomingData(data)

	// Update metrics
	rm.metrics.LastResponseTime = time.Now()
	rm.metrics.TotalResponses++

	if err != nil {
		rm.metrics.ErrorResponses++
		return nil, err
	}

	if response != nil {
		switch response.Type {
		case ResponseTypeTimeSync:
			rm.metrics.TimeSyncResponses++
		case ResponseTypePing:
			rm.metrics.PingResponses++
		case ResponseTypeAck:
			rm.metrics.AckResponses++
		}
	}

	return response, nil
}

// GetMetrics returns current response metrics.
func (rm *ResponseManager) GetMetrics() ResponseMetrics {
	return *rm.metrics
}

// ResetMetrics resets all response metrics.
func (rm *ResponseManager) ResetMetrics() {
	rm.metrics = &ResponseMetrics{}
}

// ShouldRespond determines if we should respond to incoming data based on various criteria.
func (rm *ResponseManager) ShouldRespond(data []byte, clientAddr string) bool {
	if len(data) == 0 {
		return false
	}

	// Parse command info
	cmdInfo := rm.handler.commandBuilder.ParseCommandInfo(data)
	if !cmdInfo.IsValid {
		return false
	}

	// Only respond to specific command types
	switch cmdInfo.Command {
	case CommandTypeTimeSync, CommandTypePing, CommandTypeIdentify:
		return true
	default:
		return false
	}
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
