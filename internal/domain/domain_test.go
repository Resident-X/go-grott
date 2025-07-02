package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeviceRegistry(t *testing.T) {
	registry := NewDeviceRegistry()

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.dataloggers)
	assert.Empty(t, registry.dataloggers)
}

func TestRegisterDatalogger(t *testing.T) {
	registry := NewDeviceRegistry()

	// Register a new datalogger
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	// Verify it was registered
	datalogger, found := registry.GetDatalogger("DL001")
	require.True(t, found)
	assert.Equal(t, "DL001", datalogger.ID)
	assert.Equal(t, "192.168.1.100", datalogger.IP)
	assert.Equal(t, 5279, datalogger.Port)
	assert.Equal(t, "TCP", datalogger.Protocol)
	assert.NotNil(t, datalogger.Inverters)
	assert.Empty(t, datalogger.Inverters)

	// LastContact should be recent
	assert.WithinDuration(t, time.Now(), datalogger.LastContact, time.Second)
}

func TestRegisterDataloggerUpdate(t *testing.T) {
	registry := NewDeviceRegistry()

	// Register a datalogger
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	originalTime := time.Now()
	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamp

	// Update the same datalogger with new information
	err = registry.RegisterDatalogger("DL001", "192.168.1.101", 5280, "UDP")
	require.NoError(t, err)

	// Verify it was updated
	datalogger, found := registry.GetDatalogger("DL001")
	require.True(t, found)
	assert.Equal(t, "DL001", datalogger.ID)
	assert.Equal(t, "192.168.1.101", datalogger.IP)
	assert.Equal(t, 5280, datalogger.Port)
	assert.Equal(t, "UDP", datalogger.Protocol)

	// LastContact should be more recent
	assert.True(t, datalogger.LastContact.After(originalTime))
}

func TestRegisterInverter(t *testing.T) {
	registry := NewDeviceRegistry()

	// First register a datalogger
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	// Register an inverter
	err = registry.RegisterInverter("DL001", "INV001", "1")
	require.NoError(t, err)

	// Verify the inverter was registered
	inverters, found := registry.GetInverters("DL001")
	require.True(t, found)
	require.Len(t, inverters, 1)

	inverter := inverters[0]
	assert.Equal(t, "INV001", inverter.Serial)
	assert.Equal(t, "1", inverter.InverterNo)
	assert.WithinDuration(t, time.Now(), inverter.LastContact, time.Second)
}

func TestRegisterInverterForNonExistentDatalogger(t *testing.T) {
	registry := NewDeviceRegistry()

	// Try to register an inverter for a non-existent datalogger
	err := registry.RegisterInverter("NonExistent", "INV001", "1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "datalogger NonExistent not found")
}

func TestRegisterInverterUpdate(t *testing.T) {
	registry := NewDeviceRegistry()

	// Register datalogger and inverter
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	err = registry.RegisterInverter("DL001", "INV001", "1")
	require.NoError(t, err)

	originalTime := time.Now()
	time.Sleep(10 * time.Millisecond)

	// Update the same inverter
	err = registry.RegisterInverter("DL001", "INV001", "2")
	require.NoError(t, err)

	// Verify it was updated
	inverters, found := registry.GetInverters("DL001")
	require.True(t, found)
	require.Len(t, inverters, 1)

	inverter := inverters[0]
	assert.Equal(t, "INV001", inverter.Serial)
	assert.Equal(t, "2", inverter.InverterNo)
	assert.True(t, inverter.LastContact.After(originalTime))
}

func TestGetDataloggerNotFound(t *testing.T) {
	registry := NewDeviceRegistry()

	datalogger, found := registry.GetDatalogger("NonExistent")
	assert.False(t, found)
	assert.Nil(t, datalogger)
}

func TestGetAllDataloggers(t *testing.T) {
	registry := NewDeviceRegistry()

	// Initially should be empty
	dataloggers := registry.GetAllDataloggers()
	assert.Empty(t, dataloggers)

	// Register some dataloggers
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	err = registry.RegisterDatalogger("DL002", "192.168.1.101", 5279, "TCP")
	require.NoError(t, err)

	err = registry.RegisterDatalogger("DL003", "192.168.1.102", 5279, "TCP")
	require.NoError(t, err)

	// Get all dataloggers
	dataloggers = registry.GetAllDataloggers()
	assert.Len(t, dataloggers, 3)

	// Verify we have all the expected IDs
	ids := make([]string, len(dataloggers))
	for i, dl := range dataloggers {
		ids[i] = dl.ID
	}
	assert.ElementsMatch(t, []string{"DL001", "DL002", "DL003"}, ids)
}

func TestGetInvertersNotFound(t *testing.T) {
	registry := NewDeviceRegistry()

	inverters, found := registry.GetInverters("NonExistent")
	assert.False(t, found)
	assert.Nil(t, inverters)
}

func TestGetInvertersMultiple(t *testing.T) {
	registry := NewDeviceRegistry()

	// Register datalogger
	err := registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
	require.NoError(t, err)

	// Register multiple inverters
	err = registry.RegisterInverter("DL001", "INV001", "1")
	require.NoError(t, err)

	err = registry.RegisterInverter("DL001", "INV002", "2")
	require.NoError(t, err)

	err = registry.RegisterInverter("DL001", "INV003", "3")
	require.NoError(t, err)

	// Get all inverters for the datalogger
	inverters, found := registry.GetInverters("DL001")
	require.True(t, found)
	require.Len(t, inverters, 3)

	// Verify we have all the expected serials
	serials := make([]string, len(inverters))
	for i, inv := range inverters {
		serials[i] = inv.Serial
	}
	assert.ElementsMatch(t, []string{"INV001", "INV002", "INV003"}, serials)
}

func TestConcurrentAccess(t *testing.T) {
	registry := NewDeviceRegistry()

	// Test concurrent access to ensure thread safety
	done := make(chan bool, 4)

	// Goroutine 1: Register dataloggers
	go func() {
		for i := 0; i < 10; i++ {
			registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
		}
		done <- true
	}()

	// Goroutine 2: Register inverters
	go func() {
		registry.RegisterDatalogger("DL001", "192.168.1.100", 5279, "TCP")
		for i := 0; i < 10; i++ {
			registry.RegisterInverter("DL001", "INV001", "1")
		}
		done <- true
	}()

	// Goroutine 3: Read dataloggers
	go func() {
		for i := 0; i < 10; i++ {
			registry.GetDatalogger("DL001")
			registry.GetAllDataloggers()
		}
		done <- true
	}()

	// Goroutine 4: Read inverters
	go func() {
		for i := 0; i < 10; i++ {
			registry.GetInverters("DL001")
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify final state is consistent
	datalogger, found := registry.GetDatalogger("DL001")
	assert.True(t, found)
	assert.Equal(t, "DL001", datalogger.ID)

	inverters, found := registry.GetInverters("DL001")
	assert.True(t, found)
	assert.Len(t, inverters, 1)
	assert.Equal(t, "INV001", inverters[0].Serial)
}
