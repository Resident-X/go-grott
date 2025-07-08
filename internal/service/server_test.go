package service

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewDataCollectionServer(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)

	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.Equal(t, mockParser, server.parser)
	assert.Equal(t, mockPublisher, server.publisher)
	assert.Equal(t, mockMonitoring, server.monitoring)
	assert.NotNil(t, server.registry)
}

func TestNewDataCollectionServer_WithAPIEnabled(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279
	cfg.API.Enabled = true
	cfg.API.Host = "localhost"
	cfg.API.Port = 8080

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)

	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.apiServer, "API server should be created when enabled")
}

func TestDataCollectionServer_Start(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 0 // Use port 0 to let OS choose an available port

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mock expectations for Stop calls
	mockPublisher.EXPECT().Close().Return(nil)
	mockMonitoring.EXPECT().Close().Return(nil)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Start
	err = server.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, server.listener, "Listener should be created")

	// Test Stop
	err = server.Stop(ctx)
	assert.NoError(t, err)
}

func TestDataCollectionServer_Start_WithAPI(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 0
	cfg.API.Enabled = true
	cfg.API.Host = "localhost"
	cfg.API.Port = 0

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mock expectations for Stop calls
	mockPublisher.EXPECT().Close().Return(nil)
	mockMonitoring.EXPECT().Close().Return(nil)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Start with API
	err = server.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, server.listener)
	assert.NotNil(t, server.apiServer)

	// Test Stop
	err = server.Stop(ctx)
	assert.NoError(t, err)
}

func TestDataCollectionServer_Start_ListenerError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "invalid-host"
	cfg.Server.Port = 999999 // Invalid port

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()

	// Test Start with invalid configuration
	err = server.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start listener")
}

func TestDataCollectionServer_Stop_ErrorHandling(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 0

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mocks to return errors on close
	mockPublisher.EXPECT().Close().Return(assert.AnError)
	mockMonitoring.EXPECT().Close().Return(assert.AnError)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()

	// Start server
	err = server.Start(ctx)
	require.NoError(t, err)

	// Add a mock client connection using the mockery-generated mock
	mockClientConn := mocks.NewMockConn(t)
	mockClientConn.EXPECT().Close().Return(nil)
	server.clientMutex.Lock()
	server.clients["test-client"] = mockClientConn
	server.clientMutex.Unlock()

	// Test Stop with errors (should not fail, just log errors)
	err = server.Stop(ctx)
	assert.NoError(t, err) // Stop should succeed even with errors
}

func TestDataCollectionServer_ProcessData(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.MQTT.Topic = "test/topic"
	cfg.MQTT.IncludeInverterID = true

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mock expectations
	testData := []byte("test data")
	inverterData := &domain.InverterData{
		DataloggerSerial: "DL001",
		PVSerial:         "INV001",
		PVPowerIn:        100.0,
	}

	mockParser.EXPECT().Parse(mock.Anything, testData).Return(inverterData, nil)
	mockPublisher.EXPECT().Publish(mock.Anything, "test/topic/INV001", inverterData).Return(nil)
	mockMonitoring.EXPECT().Send(mock.Anything, inverterData).Return(nil)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()
	clientAddr := "192.168.1.100:12345"

	// Test processData
	err = server.processData(ctx, clientAddr, testData)
	assert.NoError(t, err)

	// Verify devices were registered
	datalogger, found := server.registry.GetDatalogger("DL001")
	assert.True(t, found)
	assert.Equal(t, "DL001", datalogger.ID)
	assert.Equal(t, "192.168.1.100", datalogger.IP)

	inverters, found := server.registry.GetInverters("DL001")
	assert.True(t, found)
	assert.Len(t, inverters, 1)
	assert.Equal(t, "INV001", inverters[0].Serial)
}

func TestDataCollectionServer_ProcessData_ParserError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mock to return error
	testData := []byte("invalid data")
	mockParser.EXPECT().Parse(mock.Anything, testData).Return(nil, assert.AnError)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()
	clientAddr := "192.168.1.100:12345"

	// Test processData with parser error
	err = server.processData(ctx, clientAddr, testData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse error")
}

func TestDataCollectionServer_ProcessData_InvalidClientAddr(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mock to return valid data
	testData := []byte("test data")
	inverterData := &domain.InverterData{
		DataloggerSerial: "DL001",
		PVSerial:         "INV001",
	}
	mockParser.EXPECT().Parse(mock.Anything, testData).Return(inverterData, nil)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()
	invalidClientAddr := "invalid-address"

	// Test processData with invalid client address
	err = server.processData(ctx, invalidClientAddr, testData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse client address")
}

func TestDataCollectionServer_ProcessData_PublishError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.MQTT.Topic = "test/topic"

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mocks
	testData := []byte("test data")
	inverterData := &domain.InverterData{
		DataloggerSerial: "DL001",
		PVSerial:         "INV001",
	}

	mockParser.EXPECT().Parse(mock.Anything, testData).Return(inverterData, nil)
	mockPublisher.EXPECT().Publish(mock.Anything, "test/topic", inverterData).Return(assert.AnError)
	mockMonitoring.EXPECT().Send(mock.Anything, inverterData).Return(nil)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()
	clientAddr := "192.168.1.100:12345"

	// Test processData with publish error (should not fail, just log)
	err = server.processData(ctx, clientAddr, testData)
	assert.NoError(t, err) // Should succeed even with publish error
}

func TestDataCollectionServer_ProcessData_MonitoringError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.MQTT.Topic = "test/topic"

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	// Setup mocks
	testData := []byte("test data")
	inverterData := &domain.InverterData{
		DataloggerSerial: "DL001",
		PVSerial:         "INV001",
	}

	mockParser.EXPECT().Parse(mock.Anything, testData).Return(inverterData, nil)
	mockPublisher.EXPECT().Publish(mock.Anything, "test/topic", inverterData).Return(nil)
	mockMonitoring.EXPECT().Send(mock.Anything, inverterData).Return(assert.AnError)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	ctx := context.Background()
	clientAddr := "192.168.1.100:12345"

	// Test processData with monitoring error (should not fail, just log)
	err = server.processData(ctx, clientAddr, testData)
	assert.NoError(t, err) // Should succeed even with monitoring error
}

// Tests for acceptConnections function.
func TestDataCollectionServer_AcceptConnections(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock listener and connection
	mockListener := mocks.NewMockListener(t)
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	server.listener = mockListener

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr)
	mockAddr.EXPECT().String().Return("192.168.1.100:12345")
	mockConn.EXPECT().LocalAddr().Return(mockAddr)
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil)
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, net.ErrClosed) // Simulate disconnection
	mockConn.EXPECT().Close().Return(nil)

	// Accept should return the mock connection once, then an error to stop the loop
	mockListener.EXPECT().Accept().Return(mockConn, nil).Once()
	mockListener.EXPECT().Accept().Return(nil, net.ErrClosed) // Stop the accept loop

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start acceptConnections in a goroutine
	go server.acceptConnections(ctx)

	// Wait for context timeout
	<-ctx.Done()

	// Verify the connection was handled
	time.Sleep(10 * time.Millisecond) // Give some time for goroutines to finish
}

func TestDataCollectionServer_AcceptConnections_ContextCancelled(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock listener
	mockListener := mocks.NewMockListener(t)
	server.listener = mockListener

	// Create a context that is immediately cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Start acceptConnections in a goroutine
	done := make(chan bool)
	go func() {
		server.acceptConnections(ctx)
		done <- true
	}()

	// Should return quickly due to cancelled context
	select {
	case <-done:
		// Expected - function should exit due to cancelled context
	case <-time.After(100 * time.Millisecond):
		t.Fatal("acceptConnections should have exited due to cancelled context")
	}
}

func TestDataCollectionServer_AcceptConnections_ServerDone(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock listener
	mockListener := mocks.NewMockListener(t)
	server.listener = mockListener

	ctx := context.Background()

	// Close the done channel to simulate server shutdown
	close(server.done)

	// Start acceptConnections in a goroutine
	done := make(chan bool)
	go func() {
		server.acceptConnections(ctx)
		done <- true
	}()

	// Should return quickly due to done channel being closed
	select {
	case <-done:
		// Expected - function should exit due to done channel
	case <-time.After(100 * time.Millisecond):
		t.Fatal("acceptConnections should have exited due to done channel")
	}
}

func TestDataCollectionServer_AcceptConnections_AcceptError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 5279

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock listener
	mockListener := mocks.NewMockListener(t)
	server.listener = mockListener

	// Setup mock to return error first, then a good connection, then close
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	// First call returns error
	mockListener.EXPECT().Accept().Return(nil, assert.AnError).Once()

	// Second call returns good connection
	mockListener.EXPECT().Accept().Return(mockConn, nil).Once()
	mockConn.EXPECT().RemoteAddr().Return(mockAddr)
	mockAddr.EXPECT().String().Return("192.168.1.100:12345")
	mockConn.EXPECT().LocalAddr().Return(mockAddr)
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil)
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, net.ErrClosed)
	mockConn.EXPECT().Close().Return(nil)

	// Third call returns error to stop the loop
	mockListener.EXPECT().Accept().Return(nil, net.ErrClosed)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start acceptConnections in a goroutine
	go server.acceptConnections(ctx)

	// Wait for context timeout
	<-ctx.Done()

	// Give some time for goroutines to finish
	time.Sleep(10 * time.Millisecond)
}

// Tests for handleConnection function.
func TestDataCollectionServer_HandleConnection_Success(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}
	cfg.MQTT.Topic = "test/topic"
	cfg.MQTT.IncludeInverterID = true

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	testData := []byte("test data packet")
	inverterData := &domain.InverterData{
		DataloggerSerial: "DL001",
		PVSerial:         "INV001",
		PVPowerIn:        100.0,
	}

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation

	// First read returns data
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Run(func(b []byte) {
		copy(b, testData)
	}).Return(len(testData), nil).Once()

	// Second read returns error to exit loop
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, net.ErrClosed).Once()

	mockConn.EXPECT().Close().Return(nil)

	// Setup parser and publisher expectations
	mockParser.EXPECT().Parse(mock.Anything, testData).Return(inverterData, nil)
	mockPublisher.EXPECT().Publish(mock.Anything, "test/topic/INV001", inverterData).Return(nil)
	mockMonitoring.EXPECT().Send(mock.Anything, inverterData).Return(nil)

	ctx := context.Background()

	// Call handleConnection
	server.handleConnection(ctx, mockConn)

	// Verify client was registered and then removed
	server.clientMutex.Lock()
	_, exists := server.clients["192.168.1.100:12345"]
	server.clientMutex.Unlock()
	assert.False(t, exists, "Client should be removed after disconnection")
}

func TestDataCollectionServer_HandleConnection_ReadTimeout(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	// Create a timeout error
	timeoutErr := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: &mockTimeoutError{},
	}

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation  
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation

	// First read times out (should continue)
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, timeoutErr).Once()

	// Second read returns error to exit loop
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, net.ErrClosed).Once()

	mockConn.EXPECT().Close().Return(nil)

	ctx := context.Background()

	// Call handleConnection
	server.handleConnection(ctx, mockConn)
}

func TestDataCollectionServer_HandleConnection_SetReadDeadlineError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation  
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation

	// SetReadDeadline returns error (should exit)
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(assert.AnError)
	mockConn.EXPECT().Close().Return(nil)

	ctx := context.Background()

	// Call handleConnection
	server.handleConnection(ctx, mockConn)
}

func TestDataCollectionServer_HandleConnection_ContextCancelled(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation  
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation
	mockConn.EXPECT().Close().Return(nil)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Call handleConnection
	server.handleConnection(ctx, mockConn)
}

func TestDataCollectionServer_HandleConnection_ServerDone(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation  
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation
	mockConn.EXPECT().Close().Return(nil)

	// Close the done channel to simulate server shutdown
	close(server.done)

	ctx := context.Background()

	// Call handleConnection
	server.handleConnection(ctx, mockConn)
}

func TestDataCollectionServer_HandleConnection_ProcessDataError(t *testing.T) {
	cfg := &config.Config{
		LogLevel: "info",
	}

	mockParser := mocks.NewMockDataParser(t)
	mockPublisher := mocks.NewMockMessagePublisher(t)
	mockMonitoring := mocks.NewMockMonitoringService(t)

	server, err := NewDataCollectionServer(cfg, mockParser, mockPublisher, mockMonitoring)
	require.NoError(t, err)

	// Mock connection and address
	mockConn := mocks.NewMockConn(t)
	mockAddr := mocks.NewMockAddr(t)

	testData := []byte("invalid data")

	// Setup mock expectations
	mockConn.EXPECT().RemoteAddr().Return(mockAddr).Times(3) // Called for client addr and session creation
	mockAddr.EXPECT().String().Return("192.168.1.100:12345").Times(4)
	mockConn.EXPECT().LocalAddr().Return(mockAddr).Once() // Called during session creation

	// First read returns data
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Run(func(b []byte) {
		copy(b, testData)
	}).Return(len(testData), nil).Once()

	// Second read returns error to exit loop
	mockConn.EXPECT().SetReadDeadline(mock.AnythingOfType("time.Time")).Return(nil).Once()
	mockConn.EXPECT().Read(mock.AnythingOfType("[]uint8")).Return(0, net.ErrClosed).Once()

	mockConn.EXPECT().Close().Return(nil)

	// Setup parser to return error
	mockParser.EXPECT().Parse(mock.Anything, testData).Return(nil, assert.AnError)

	ctx := context.Background()

	// Call handleConnection (should not fail even with process data error)
	server.handleConnection(ctx, mockConn)
}

// Mock timeout error for testing.
type mockTimeoutError struct{}

func (e *mockTimeoutError) Error() string {
	return "timeout"
}

func (e *mockTimeoutError) Timeout() bool {
	return true
}

func (e *mockTimeoutError) Temporary() bool {
	return true
}
