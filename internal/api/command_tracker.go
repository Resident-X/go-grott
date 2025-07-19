// Package api provides HTTP API functionality for the go-grott server.
package api

import (
	"fmt"
	"sync"

	"github.com/resident-x/go-grott/internal/domain"
	"github.com/rs/zerolog"
)

// CommandQueueManager manages command queues for different device connections.
type CommandQueueManager struct {
	queues map[string]chan []byte
	mutex  sync.RWMutex
	logger zerolog.Logger
}

// NewCommandQueueManager creates a new command queue manager.
func NewCommandQueueManager(logger zerolog.Logger) *CommandQueueManager {
	return &CommandQueueManager{
		queues: make(map[string]chan []byte),
		logger: logger.With().Str("component", "command_queue").Logger(),
	}
}

// GetOrCreateQueue gets or creates a command queue for a device connection.
func (cqm *CommandQueueManager) GetOrCreateQueue(ip string, port int) chan []byte {
	key := fmt.Sprintf("%s_%d", ip, port)
	
	cqm.mutex.Lock()
	defer cqm.mutex.Unlock()
	
	if queue, exists := cqm.queues[key]; exists {
		return queue
	}
	
	// Create new queue with reasonable buffer size
	queue := make(chan []byte, 100)
	cqm.queues[key] = queue
	
	cqm.logger.Debug().
		Str("key", key).
		Msg("Created new command queue")
	
	return queue
}

// QueueCommand queues a command for a specific device connection.
func (cqm *CommandQueueManager) QueueCommand(ip string, port int, command []byte) error {
	queue := cqm.GetOrCreateQueue(ip, port)
	
	select {
	case queue <- command:
		cqm.logger.Debug().
			Str("ip", ip).
			Int("port", port).
			Int("command_size", len(command)).
			Msg("Command queued successfully")
		return nil
	default:
		return fmt.Errorf("command queue is full for %s:%d", ip, port)
	}
}

// RemoveQueue removes a command queue when a connection is closed.
func (cqm *CommandQueueManager) RemoveQueue(ip string, port int) {
	key := fmt.Sprintf("%s_%d", ip, port)
	
	cqm.mutex.Lock()
	defer cqm.mutex.Unlock()
	
	if queue, exists := cqm.queues[key]; exists {
		close(queue)
		delete(cqm.queues, key)
		
		cqm.logger.Debug().
			Str("key", key).
			Msg("Removed command queue")
	}
}

// GetQueueCount returns the number of active command queues.
func (cqm *CommandQueueManager) GetQueueCount() int {
	cqm.mutex.RLock()
	defer cqm.mutex.RUnlock()
	return len(cqm.queues)
}

// ResponseProcessor handles responses received from devices and updates the response tracker.
type ResponseProcessor struct {
	tracker *ResponseTracker
	logger  zerolog.Logger
}

// NewResponseProcessor creates a new response processor.
func NewResponseProcessor(tracker *ResponseTracker, logger zerolog.Logger) *ResponseProcessor {
	return &ResponseProcessor{
		tracker: tracker,
		logger:  logger.With().Str("component", "response_processor").Logger(),
	}
}

// ProcessResponse processes a response from a device and updates the tracker.
func (rp *ResponseProcessor) ProcessResponse(data []byte) {
	if len(data) < 8 {
		rp.logger.Debug().
			Int("data_length", len(data)).
			Msg("Response too short to process")
		return
	}

	// Extract basic response information from the header
	recType := fmt.Sprintf("%02x", data[7])
	
	// Process different response types similar to Python grottserver
	switch recType {
	case "05": // Register read response from inverter
		rp.processRegisterReadResponse("05", data)
	case "06": // Register write response from inverter  
		rp.processRegisterWriteResponse("06", data)
	case "18": // Datalogger write response
		rp.processDataloggerWriteResponse("18", data)
	case "19": // Datalogger read response
		rp.processDataloggerReadResponse("19", data)
	case "10": // Multi-register response
		rp.processMultiRegisterResponse("10", data)
	default:
		rp.logger.Debug().
			Str("record_type", recType).
			Msg("Unhandled response type")
	}
}

// processRegisterReadResponse processes register read responses.
func (rp *ResponseProcessor) processRegisterReadResponse(cmdType string, data []byte) {
	if len(data) < 48 { // Minimum expected length
		rp.logger.Debug().Msg("Register read response too short")
		return
	}

	// Decrypt data if needed (simplified - would need proper decryption)
	plainData := rp.decryptIfNeeded(data)
	
	// Extract register and value from the response
	// This is a simplified version - real implementation would need proper parsing
	offset := 0
	if len(plainData) >= 76 && plainData[3] == 0x06 {
		offset = 40 // Protocol 06 has offset
	}
	
	if len(plainData) < 36+offset+8 {
		return
	}
	
	// Extract register (2 bytes at offset 36+offset)
	register := (uint16(plainData[36+offset]) << 8) | uint16(plainData[37+offset])
	
	// Check if response is empty (CRC only)
	if len(plainData) == 48+offset {
		rp.logger.Debug().Msg("Empty register read response received, ignoring")
		return
	}
	
	// Extract value (2 bytes at offset 44+offset)  
	if len(plainData) >= 44+offset+4 {
		valueBytes := plainData[44+offset : 48+offset]
		value := fmt.Sprintf("%02x%02x", valueBytes[0], valueBytes[1])
		
		registerKey := fmt.Sprintf("%04x", register)
		response := &CommandResponse{
			Value: value,
		}
		
		rp.tracker.SetResponse(cmdType, registerKey, response)
		
		rp.logger.Debug().
			Str("cmd_type", cmdType).
			Str("register_key", registerKey).
			Str("value", value).
			Msg("Processed register read response")
	}
}

// processRegisterWriteResponse processes register write responses.
func (rp *ResponseProcessor) processRegisterWriteResponse(cmdType string, data []byte) {
	// Similar to processRegisterReadResponse but for write operations
	plainData := rp.decryptIfNeeded(data)
	
	offset := 0
	if len(plainData) >= 76 && plainData[3] == 0x06 {
		offset = 40
	}
	
	if len(plainData) < 36+offset+8 {
		return
	}
	
	// Extract register
	register := (uint16(plainData[36+offset]) << 8) | uint16(plainData[37+offset])
	
	// Extract result and value from write response
	if len(plainData) >= 42+offset+4 {
		result := fmt.Sprintf("%02x", plainData[40+offset])
		valueBytes := plainData[42+offset : 46+offset]
		value := fmt.Sprintf("%02x%02x", valueBytes[0], valueBytes[1])
		
		registerKey := fmt.Sprintf("%04x", register)
		
		// Store both 06 (write) and 05 (read) format responses like Python
		writeResponse := &CommandResponse{
			Value:  value,
			Result: result,
		}
		readResponse := &CommandResponse{
			Value: value,
		}
		
		rp.tracker.SetResponse(cmdType, registerKey, writeResponse)
		rp.tracker.SetResponse("05", registerKey, readResponse)
		
		rp.logger.Debug().
			Str("cmd_type", cmdType).
			Str("register_key", registerKey).
			Str("value", value).
			Str("result", result).
			Msg("Processed register write response")
	}
}

// processDataloggerWriteResponse processes datalogger write responses.
func (rp *ResponseProcessor) processDataloggerWriteResponse(cmdType string, data []byte) {
	plainData := rp.decryptIfNeeded(data)
	
	offset := 0
	if len(plainData) >= 76 && plainData[3] == 0x06 {
		offset = 40
	}
	
	if len(plainData) < 36+offset+8 {
		return
	}
	
	// Extract register
	register := (uint16(plainData[36+offset]) << 8) | uint16(plainData[37+offset])
	
	// Extract result
	if len(plainData) >= 42+offset {
		result := fmt.Sprintf("%02x", plainData[40+offset])
		
		registerKey := fmt.Sprintf("%04x", register)
		response := &CommandResponse{
			Result: result,
		}
		
		rp.tracker.SetResponse(cmdType, registerKey, response)
		
		rp.logger.Debug().
			Str("cmd_type", cmdType).
			Str("register_key", registerKey).
			Str("result", result).
			Msg("Processed datalogger write response")
	}
}

// processDataloggerReadResponse processes datalogger read responses.
func (rp *ResponseProcessor) processDataloggerReadResponse(cmdType string, data []byte) {
	plainData := rp.decryptIfNeeded(data)
	
	offset := 0
	if len(plainData) >= 76 && plainData[3] == 0x06 {
		offset = 40
	}
	
	if len(plainData) < 36+offset+8 {
		return
	}
	
	// Extract register
	register := (uint16(plainData[36+offset]) << 8) | uint16(plainData[37+offset])
	
	// Extract value length and value
	if len(plainData) >= 44+offset {
		valueLen := (uint16(plainData[40+offset]) << 8) | uint16(plainData[41+offset])
		
		if len(plainData) >= 44+offset+int(valueLen) {
			valueBytes := plainData[44+offset : 44+offset+int(valueLen)]
			value := string(valueBytes) // Convert to string for datalogger responses
			
			registerKey := fmt.Sprintf("%04x", register)
			response := &CommandResponse{
				Value: value,
			}
			
			rp.tracker.SetResponse(cmdType, registerKey, response)
			
			rp.logger.Debug().
				Str("cmd_type", cmdType).
				Str("register_key", registerKey).
				Str("value", value).
				Msg("Processed datalogger read response")
		}
	}
}

// processMultiRegisterResponse processes multi-register operation responses.
func (rp *ResponseProcessor) processMultiRegisterResponse(cmdType string, data []byte) {
	plainData := rp.decryptIfNeeded(data)
	
	if len(plainData) < 84 {
		return
	}
	
	// Extract start and end registers
	startRegister := (uint16(plainData[76]) << 8) | uint16(plainData[77])
	endRegister := (uint16(plainData[80]) << 8) | uint16(plainData[81])
	
	// Extract value
	if len(plainData) >= 86 {
		value := fmt.Sprintf("%02x", plainData[84])
		
		registerKey := fmt.Sprintf("%04x%04x", startRegister, endRegister)
		response := &CommandResponse{
			Value: value,
		}
		
		rp.tracker.SetResponse(cmdType, registerKey, response)
		
		rp.logger.Debug().
			Str("cmd_type", cmdType).
			Str("register_key", registerKey).
			Str("value", value).
			Msg("Processed multi-register response")
	}
}

// decryptIfNeeded decrypts data if it's encrypted (simplified version).
func (rp *ResponseProcessor) decryptIfNeeded(data []byte) []byte {
	if len(data) < 4 {
		return data
	}
	
	// Check if this is protocol 02 (unencrypted)
	if data[3] == 0x02 {
		return data
	}
	
	// For protocols 05/06, decrypt using Growatt XOR mask
	mask := []byte("Growatt")
	result := make([]byte, len(data))
	
	// Copy header unchanged (first 8 bytes)
	copy(result[:8], data[:8])
	
	// XOR the rest with the mask (excluding CRC at end)
	dataEnd := len(data)
	if dataEnd >= 2 {
		dataEnd -= 2 // Exclude CRC
	}
	
	for i := 8; i < dataEnd; i++ {
		maskIdx := (i - 8) % len(mask)
		result[i] = data[i] ^ mask[maskIdx]
	}
	
	// Copy CRC unchanged
	if len(data) >= 2 {
		copy(result[dataEnd:], data[dataEnd:])
	}
	
	return result
}

// DeviceConnectionTracker tracks active device connections for API operations.
type DeviceConnectionTracker struct {
	connections map[string]*domain.DataloggerInfo
	mutex       sync.RWMutex
	logger      zerolog.Logger
}

// NewDeviceConnectionTracker creates a new device connection tracker.
func NewDeviceConnectionTracker(logger zerolog.Logger) *DeviceConnectionTracker {
	return &DeviceConnectionTracker{
		connections: make(map[string]*domain.DataloggerInfo),
		logger:      logger.With().Str("component", "connection_tracker").Logger(),
	}
}

// UpdateConnection updates connection information for a device.
func (dct *DeviceConnectionTracker) UpdateConnection(datalogger *domain.DataloggerInfo) {
	dct.mutex.Lock()
	defer dct.mutex.Unlock()
	
	dct.connections[datalogger.ID] = datalogger
	dct.logger.Debug().
		Str("datalogger_id", datalogger.ID).
		Str("ip", datalogger.IP).
		Int("port", datalogger.Port).
		Msg("Updated device connection")
}

// GetConnection retrieves connection information for a device.
func (dct *DeviceConnectionTracker) GetConnection(dataloggerID string) (*domain.DataloggerInfo, bool) {
	dct.mutex.RLock()
	defer dct.mutex.RUnlock()
	
	connection, exists := dct.connections[dataloggerID]
	return connection, exists
}

// RemoveConnection removes connection information for a device.
func (dct *DeviceConnectionTracker) RemoveConnection(dataloggerID string) {
	dct.mutex.Lock()
	defer dct.mutex.Unlock()
	
	delete(dct.connections, dataloggerID)
	dct.logger.Debug().
		Str("datalogger_id", dataloggerID).
		Msg("Removed device connection")
}
