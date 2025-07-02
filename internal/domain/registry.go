// Package domain provides core domain implementations.
package domain

import (
	"fmt"
	"sync"
	"time"
)

// DeviceRegistry implements the Registry interface.
type DeviceRegistry struct {
	dataloggers map[string]*DataloggerInfo
	mutex       sync.RWMutex
}

// NewDeviceRegistry creates a new device registry.
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		dataloggers: make(map[string]*DataloggerInfo),
	}
}

// RegisterDatalogger adds or updates a datalogger in the registry.
func (r *DeviceRegistry) RegisterDatalogger(id, ip string, port int, protocol string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Check if datalogger already exists
	datalogger, exists := r.dataloggers[id]
	if !exists {
		// Create new datalogger entry
		datalogger = &DataloggerInfo{
			ID:          id,
			IP:          ip,
			Port:        port,
			Protocol:    protocol,
			LastContact: time.Now(),
			Inverters:   make(map[string]*InverterInfo),
		}
		r.dataloggers[id] = datalogger
	} else {
		// Update existing datalogger
		datalogger.IP = ip
		datalogger.Port = port
		datalogger.Protocol = protocol
		datalogger.LastContact = time.Now()
	}

	return nil
}

// RegisterInverter adds or updates an inverter in the registry.
func (r *DeviceRegistry) RegisterInverter(dataloggerID, inverterSerial, inverterNo string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Check if datalogger exists
	datalogger, exists := r.dataloggers[dataloggerID]
	if !exists {
		return fmt.Errorf("datalogger %s not found", dataloggerID)
	}

	// Check if inverter already exists for this datalogger
	inverter, exists := datalogger.Inverters[inverterSerial]
	if !exists {
		// Create new inverter entry
		inverter = &InverterInfo{
			Serial:      inverterSerial,
			InverterNo:  inverterNo,
			LastContact: time.Now(),
		}
		datalogger.Inverters[inverterSerial] = inverter
	} else {
		// Update existing inverter
		inverter.InverterNo = inverterNo
		inverter.LastContact = time.Now()
	}

	return nil
}

// GetDatalogger retrieves information about a datalogger.
func (r *DeviceRegistry) GetDatalogger(id string) (*DataloggerInfo, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	datalogger, exists := r.dataloggers[id]
	if !exists {
		return nil, false
	}

	return datalogger, true
}

// GetAllDataloggers returns information about all dataloggers.
func (r *DeviceRegistry) GetAllDataloggers() []*DataloggerInfo {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	dataloggers := make([]*DataloggerInfo, 0, len(r.dataloggers))
	for _, datalogger := range r.dataloggers {
		dataloggers = append(dataloggers, datalogger)
	}

	return dataloggers
}

// GetInverters returns all inverters for a datalogger.
func (r *DeviceRegistry) GetInverters(dataloggerID string) ([]*InverterInfo, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	datalogger, exists := r.dataloggers[dataloggerID]
	if !exists {
		return nil, false
	}

	inverters := make([]*InverterInfo, 0, len(datalogger.Inverters))
	for _, inverter := range datalogger.Inverters {
		inverters = append(inverters, inverter)
	}

	return inverters, true
}
