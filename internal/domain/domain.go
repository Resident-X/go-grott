// Package domain provides core domain models and interfaces for the go-grott application
package domain

import (
	"context"
	"time"
)

// InverterData represents parsed data from an inverter.
type InverterData struct {
	// Device identifier (used for MQTT device field and Home Assistant auto-discovery)
	Device string `json:"device,omitempty"`

	// Buffered indicates if this is buffered data (for Home Assistant compatibility)
	Buffered string `json:"buffered,omitempty"`

	// Core fields that are always serialized directly
	DataloggerSerial string    `json:"datalogserial,omitempty"`
	PVSerial         string    `json:"pvserial,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
	
	// Typed fields for Go code - values come from ExtendedData when flattened
	PVStatus         int     `json:"-"` // Available in ExtendedData as "pvstatus"
	PVPowerIn        float64 `json:"-"` // Available in ExtendedData as "pvpowerin"
	PVPowerOut       float64 `json:"-"` // Available in ExtendedData as "pvpowerout"
	PVEnergyToday    float64 `json:"-"` // Available in ExtendedData as "pvenergytoday"
	PVEnergyTotal    float64 `json:"-"` // Available in ExtendedData as "pvenergytotal"
	PV1Voltage       float64 `json:"-"` // Available in ExtendedData as "pv1voltage"
	PV1Current       float64 `json:"-"` // Available in ExtendedData as "pv1current"
	PV1Watt          float64 `json:"-"` // Available in ExtendedData as "pv1watt"
	PV2Voltage       float64 `json:"-"` // Available in ExtendedData as "pv2voltage"
	PV2Current       float64 `json:"-"` // Available in ExtendedData as "pv2current"
	PV2Watt          float64 `json:"-"` // Available in ExtendedData as "pv2watt"
	PV3Voltage       float64 `json:"-"` // Available in ExtendedData as "pv3voltage"
	PV3Current       float64 `json:"-"` // Available in ExtendedData as "pv3current"
	PV3Watt          float64 `json:"-"` // Available in ExtendedData as "pv3watt"
	PVFrequency      float64 `json:"-"` // Available in ExtendedData as "pvfrequency"
	PVGridVoltage    float64 `json:"-"` // Available in ExtendedData as "pvgridvoltage"
	PVGridCurrent    float64 `json:"-"` // Available in ExtendedData as "pvgridcurrent"
	PVGridPower      float64 `json:"-"` // Available in ExtendedData as "pvgridpower"
	PVTemperature    float64 `json:"-"` // Available in ExtendedData as "pvtemperature"

	// Extended data fields - flattened to root level like Python grott
	ExtendedData map[string]interface{} `json:"-"` // Don't serialize as nested object

	// Raw data for debugging
	RawHex string `json:"-"`
}

// DataParser defines the interface for parsing incoming data.
type DataParser interface {
	// Parse converts a byte array into a structured InverterData object
	Parse(ctx context.Context, data []byte) (*InverterData, error)

	// Validate checks if the data is valid according to protocol rules
	Validate(data []byte) error
}

// MessagePublisher defines the interface for publishing parsed data.
type MessagePublisher interface {
	// Connect establishes a connection to the messaging system
	Connect(ctx context.Context) error

	// Publish sends data to the specified topic
	Publish(ctx context.Context, topic string, data interface{}) error

	// Close terminates the connection to the messaging system
	Close() error
}

// MonitoringService defines the interface for external monitoring services.
type MonitoringService interface {
	// Send publishes inverter data to the monitoring service
	Send(ctx context.Context, data *InverterData) error

	// Connect establishes a connection to the service
	Connect() error

	// Close terminates the connection to the service
	Close() error
}

// Registry keeps track of connected devices.
type Registry interface {
	// RegisterDatalogger adds or updates a datalogger in the registry
	RegisterDatalogger(id string, ip string, port int, protocol string) error

	// RegisterInverter adds or updates an inverter in the registry
	RegisterInverter(dataloggerID string, inverterSerial string, inverterNo string) error

	// GetDatalogger retrieves information about a datalogger
	GetDatalogger(id string) (*DataloggerInfo, bool)

	// GetAllDataloggers returns information about all dataloggers
	GetAllDataloggers() []*DataloggerInfo

	// GetInverters returns all inverters for a datalogger
	GetInverters(dataloggerID string) ([]*InverterInfo, bool)

	// GetDataloggerBySerial retrieves a datalogger by its serial number
	GetDataloggerBySerial(serial string) *DataloggerInfo

	// GetInverterBySerial retrieves an inverter by its serial number from a specific datalogger
	GetInverterBySerial(dataloggerID, inverterSerial string) *InverterInfo
}

// DataloggerInfo contains information about a connected datalogger.
type DataloggerInfo struct {
	ID          string
	IP          string
	Port        int
	Protocol    string
	LastContact time.Time
	Inverters   map[string]*InverterInfo
}

// InverterInfo contains information about an inverter.
type InverterInfo struct {
	Serial      string
	InverterNo  string
	LastContact time.Time
}
