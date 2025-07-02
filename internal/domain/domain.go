// Package domain provides core domain models and interfaces for the go-grott application
package domain

import (
	"context"
	"time"
)

// InverterData represents parsed data from an inverter.
type InverterData struct {
	// Standard inverter data fields
	DataloggerSerial string    `json:"datalogserial,omitempty"`
	PVSerial         string    `json:"pvserial,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
	PVStatus         int       `json:"pvstatus,omitempty"`
	PVPowerIn        float64   `json:"pvpowerin,omitempty"`
	PVPowerOut       float64   `json:"pvpowerout,omitempty"`
	PVEnergyToday    float64   `json:"pvenergytoday,omitempty"`
	PVEnergyTotal    float64   `json:"pvenergytotal,omitempty"`
	PV1Voltage       float64   `json:"pv1voltage,omitempty"`
	PV1Current       float64   `json:"pv1current,omitempty"`
	PV1Watt          float64   `json:"pv1watt,omitempty"`
	PV2Voltage       float64   `json:"pv2voltage,omitempty"`
	PV2Current       float64   `json:"pv2current,omitempty"`
	PV2Watt          float64   `json:"pv2watt,omitempty"`
	PVFrequency      float64   `json:"pvfrequency,omitempty"`
	PVGridVoltage    float64   `json:"pvgridvoltage,omitempty"`
	PVGridCurrent    float64   `json:"pvgridcurrent,omitempty"`
	PVGridPower      float64   `json:"pvgridpower,omitempty"`
	PVTemperature    float64   `json:"pvtemperature,omitempty"`

	// Extended data fields
	ExtendedData map[string]interface{} `json:"extended,omitempty"`

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
