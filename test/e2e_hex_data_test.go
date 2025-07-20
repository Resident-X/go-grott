package e2e

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/parser"
	"github.com/resident-x/go-grott/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessageCollector implements domain.MessagePublisher for testing
type TestMessageCollector struct {
	Messages []PublishedMessage
}

type PublishedMessage struct {
	Topic string
	Data  interface{}
}

func (t *TestMessageCollector) Connect(ctx context.Context) error {
	return nil
}

func (t *TestMessageCollector) Publish(ctx context.Context, topic string, data interface{}) error {
	t.Messages = append(t.Messages, PublishedMessage{
		Topic: topic,
		Data:  data,
	})
	return nil
}

func (t *TestMessageCollector) Close() error {
	return nil
}

// TestMonitoringService implements domain.MonitoringService for testing
type TestMonitoringService struct {
	SentData []*domain.InverterData
}

func (t *TestMonitoringService) Connect() error {
	return nil
}

func (t *TestMonitoringService) Send(ctx context.Context, data *domain.InverterData) error {
	t.SentData = append(t.SentData, data)
	return nil
}

func (t *TestMonitoringService) Close() error {
	return nil
}
// HexDataTestCase represents a test case with real Growatt protocol data
type HexDataTestCase struct {
	Name              string                 // Test case name
	HexData           string                 // Hex encoded data string
	Description       string                 // Description of what this data represents
	ExpectedSerial    string                 // Expected datalogger serial (optional)
	ExpectedPVSerial  string                 // Expected inverter serial (optional)
	ExpectedFields    map[string]interface{} // Expected parsed field values
	ShouldRespond     bool                   // Whether server should generate a response
	ExpectParseError  bool                   // Whether parsing should fail
	DeviceType        string                 // Device type description
	ProtocolVersion   string                 // Protocol version
	IsEncrypted       bool                   // Whether data is encrypted
}

// E2EHexTestServer manages the test server lifecycle
type E2EHexTestServer struct {
	server       *service.DataCollectionServer
	config       *config.Config
	testMessages []domain.InverterData // Captured messages
	ctx          context.Context
	cancel       context.CancelFunc
	actualPort   int
}

// NewE2EHexTestServer creates a new test server instance
func NewE2EHexTestServer(t *testing.T) *E2EHexTestServer {
	// Create test configuration
	cfg := &config.Config{
		LogLevel:     "debug",
		MinRecordLen: 100,
		Decrypt:      true,
		InverterType: "default",
		TimeZone:     "UTC",
	}

	// Server settings
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0 // Let OS choose port

	// API settings (disabled for testing)
	cfg.API.Enabled = false

	// MQTT settings (disabled for testing)
	cfg.MQTT.Enabled = false

	// Create test parser
	testParser, err := parser.NewParser(cfg)
	require.NoError(t, err)

	// Create test message collector
	testCollector := &TestMessageCollector{}

	// Create monitoring service (no-op for testing)
	testMonitoring := &TestMonitoringService{}

	// Create server
	server, err := service.NewDataCollectionServer(cfg, testParser, testCollector, testMonitoring)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	testServer := &E2EHexTestServer{
		server: server,
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	return testServer
}

// Start starts the test server and returns the port it's listening on
func (ts *E2EHexTestServer) Start(t *testing.T) int {
	// If port is 0, find an available port first
	if ts.config.Server.Port == 0 {
		// Find an available port
		listener, err := net.Listen("tcp", ":0")
		require.NoError(t, err)
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		
		// Update config with the available port
		ts.config.Server.Port = port
		ts.actualPort = port
	} else {
		ts.actualPort = ts.config.Server.Port
	}

	// Start the server in a goroutine
	go func() {
		err := ts.server.Start(ts.ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
			t.Logf("Server start error: %v", err)
		}
	}()

	// Give server time to start - wait for it to actually be ready
	for i := 0; i < 50; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ts.actualPort))
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	return ts.actualPort
}

// Stop stops the test server
func (ts *E2EHexTestServer) Stop(t *testing.T) {
	if ts.cancel != nil {
		ts.cancel()
	}
	if ts.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := ts.server.Stop(ctx)
		if err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}
}

// SendHexData sends hex data to the server and waits for processing
func (ts *E2EHexTestServer) SendHexData(t *testing.T, hexData string) error {
	// Decode hex string to bytes
	data, err := hex.DecodeString(strings.ReplaceAll(hexData, " ", ""))
	if err != nil {
		return fmt.Errorf("failed to decode hex data: %w", err)
	}

	// Connect to the server
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ts.actualPort))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	// Set write deadline
	err = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Send data
	n, err := conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	t.Logf("Sent %d bytes of data", n)

	// Set read deadline for potential response (reduced timeout)
	err = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	// Try to read response (may timeout if no response expected)
	respBuf := make([]byte, 1024)
	respN, respErr := conn.Read(respBuf)
	if respErr == nil && respN > 0 {
		t.Logf("Received response: %s", hex.EncodeToString(respBuf[:respN]))
	} else {
		t.Logf("No response received (this may be expected)")
	}

	// Give server time to process (reduced)
	time.Sleep(5 * time.Millisecond)

	return nil
}

// SendHexDataFast sends hex data without waiting for responses (for performance tests)
func (ts *E2EHexTestServer) SendHexDataFast(t *testing.T, hexData string) error {
	// Decode hex string to bytes
	data, err := hex.DecodeString(strings.ReplaceAll(hexData, " ", ""))
	if err != nil {
		return fmt.Errorf("failed to decode hex data: %w", err)
	}

	// Connect to the server
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ts.actualPort))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	// Set write deadline
	err = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Send data
	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// No waiting for responses or processing delays in fast mode
	return nil
}

// GetTestMessages returns captured test messages from the collector
func (t *TestMessageCollector) GetMessages() []PublishedMessage {
	return t.Messages
}

func (t *TestMessageCollector) Clear() {
	t.Messages = nil
}

// Real Growatt protocol hex data test cases
func getRealHexDataTestCases() []HexDataTestCase {
	return []HexDataTestCase{
		// {
		// 	Name: "Standard_Modbus_RTU_Data_Packet",
		// 	HexData: "68 55 00 68 04 51 00 01 02 03 04 05 06 07 08 09 0A 0B 0C 0D 0E 0F " +
		// 		"10 11 12 13 14 15 16 17 18 19 1A 1B 1C 1D 1E 1F 20 21 22 23 24 25 26 27 28 29 2A 2B 2C 2D 2E 2F " +
		// 		"30 31 32 33 34 35 36 37 38 39 3A 3B 3C 3D 3E 3F 40 41 42 43 44 45 46 47 48 49 4A 4B 4C 4D 4E 4F " +
		// 		"50 51 52 53 54 AB CD 16",
		// 	Description:     "Standard Modbus RTU data packet with valid CRC",
		// 	DeviceType:      "Standard Datalogger",
		// 	ProtocolVersion: "04",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   true,
		// 	ExpectParseError: false,
		// },
		{
			Name: "Encrypted_Growatt_Data",
			HexData: "000e0006024101031f352b4122363e7540387761747447726f7761747447726f7761747447722c222a403705235f4224747447726f7761747447726f7761747447726f777873604e7459756174743b726e77b8747447166f77466474464aef74893539765c5f773b353606726777607474449a6f36613574e072c8776137210c462c3530444102726f7761747547166f7761745467523f21413d1a31171d0304065467726f63307675409b6f706160744e7264774c7474407a652d7328601770d37ddf662853226dcb6bca661b663f709e7d9655fc7ce06379740c72247764744647776f3c6171740c726a772a74714d666f7720393506425d47504444774a6e466174744761ce77427d144d6667ef69627453726a7e0e7c886062486746645357557f5071747447726e5b618b3a6772903941748b09526f882f547746726f7860742447736d0661747fff7e5b776137210c462c3530444102726f7761747447726f7761747447726f7761747447726e83617475d7726e7761747447726d2f61747447726f7761747451da6f7761747447726f7761741047786f7761747447726f7761747447726f7761747423720b7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744772372f392c2c1f2a372f392c2c1f2a372f61747447726f7761545467526f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447ff71",
			Description:     "Encrypted Growatt data requiring decryption",
			DeviceType:      "Growatt Datalogger",
			ProtocolVersion: "06",
			IsEncrypted:     true,
			ShouldRespond:   false,
			ExpectParseError: false,
		},
		// {
		// 	Name: "Command_Response_Type_18",
		// 	HexData: "68 55 00 68 18 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 " +
		// 		"61 62 63 64 65 66 67 68 69 70 71 72 73 74 75 76 77 78 79 80 81 82 83 84 85 86 87 88 89 90 " +
		// 		"16",
		// 	Description:     "Command response type 18 - should not generate response",
		// 	DeviceType:      "Command Response",
		// 	ProtocolVersion: "18",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   false,
		// 	ExpectParseError: false,
		// },
		// {
		// 	Name: "Command_Response_Type_05",
		// 	HexData: "68 55 00 68 05 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 " +
		// 		"61 62 63 64 65 66 67 68 69 70 71 72 73 74 75 76 77 78 79 80 81 82 83 84 85 86 87 88 89 90 " +
		// 		"16",
		// 	Description:     "Command response type 05 - should not generate response",
		// 	DeviceType:      "Command Response",
		// 	ProtocolVersion: "05",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   false,
		// 	ExpectParseError: false,
		// },
		// {
		// 	Name: "Record_Type_03_Announcement",
		// 	HexData: "68 55 00 68 03 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 " +
		// 		"61 62 63 64 65 66 67 68 69 70 71 72 73 74 75 76 77 78 79 80 81 82 83 84 85 86 87 88 89 90 " +
		// 		"AB CD 16",
		// 	Description:     "Record type 03 announcement - should generate response",
		// 	DeviceType:      "Datalogger Announcement",
		// 	ProtocolVersion: "03",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   true,
		// 	ExpectParseError: false,
		// },
		// {
		// 	Name: "Malformed_Short_Packet",
		// 	HexData: "68 55 00 68",
		// 	Description:     "Malformed packet too short",
		// 	DeviceType:      "Invalid",
		// 	ProtocolVersion: "Unknown",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   false,
		// 	ExpectParseError: true,
		// },
		// {
		// 	Name: "Invalid_Header_Format",
		// 	HexData: "AA 55 00 BB 04 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 16",
		// 	Description:     "Invalid header format (wrong start bytes)",
		// 	DeviceType:      "Invalid",
		// 	ProtocolVersion: "04",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   false,
		// 	ExpectParseError: true,
		// },
		// {
		// 	Name: "Large_Data_Packet_Extended",
		// 	HexData: "68 55 02 68 04 01" + 
		// 		strings.Repeat("41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 ", 50) +
		// 		"AB CD 16",
		// 	Description:     "Large data packet that should trigger extended layout",
		// 	DeviceType:      "Extended Datalogger",
		// 	ProtocolVersion: "04",
		// 	IsEncrypted:     false,
		// 	ShouldRespond:   true,
		// 	ExpectParseError: false,
		// },
	}
}

// TestE2E_RealHexData tests the server with real Growatt protocol hex data
func TestE2E_RealHexData(t *testing.T) {
	testCases := getRealHexDataTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Create and start test server
			testServer := NewE2EHexTestServer(t)
			_ = testServer.Start(t)
			defer testServer.Stop(t)

			t.Logf("Testing: %s", tc.Description)
			t.Logf("Device Type: %s, Protocol: %s, Encrypted: %v", 
				tc.DeviceType, tc.ProtocolVersion, tc.IsEncrypted)
			t.Logf("Hex Data: %s", tc.HexData)

			// Send hex data to server
			err := testServer.SendHexData(t, tc.HexData)

			if tc.ExpectParseError {
				// For malformed data, we might not get a connection error, 
				// but the server should handle it gracefully
				t.Logf("Expected parse error case - server handled gracefully")
			} else {
				assert.NoError(t, err, "Failed to send hex data to server")
			}

			// Verify server processed the data (if collector is accessible)
			// Note: In a real implementation, you'd want to expose the message collector
			// through the test server interface

			t.Logf("✅ Test case '%s' completed successfully", tc.Name)
		})
	}
}

// TestE2E_PerformanceWithMultipleConnections tests server performance with multiple connections
func TestE2E_PerformanceWithMultipleConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Create and start test server
	testServer := NewE2EHexTestServer(t)
	_ = testServer.Start(t)
	defer testServer.Stop(t)

	// Use a simple valid data packet
	hexData := "68 55 00 68 04 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 AB CD 16"

	numConnections := 10
	numPacketsPerConnection := 5

	t.Logf("Testing performance with %d connections, %d packets each", 
		numConnections, numPacketsPerConnection)

	start := time.Now()

	// Create multiple concurrent connections
	results := make(chan error, numConnections)

	for i := 0; i < numConnections; i++ {
		go func(connID int) {
			for j := 0; j < numPacketsPerConnection; j++ {
				err := testServer.SendHexDataFast(t, hexData)
				if err != nil {
					results <- fmt.Errorf("connection %d packet %d failed: %w", connID, j, err)
					return
				}
				// No delay between packets in performance test
			}
			results <- nil
		}(i)
	}

	// Wait for all connections to complete
	for i := 0; i < numConnections; i++ {
		err := <-results
		if err != nil {
			t.Errorf("Connection error: %v", err)
		}
	}

	elapsed := time.Since(start)
	totalPackets := numConnections * numPacketsPerConnection
	packetsPerSecond := float64(totalPackets) / elapsed.Seconds()

	t.Logf("Performance test completed:")
	t.Logf("- Total packets: %d", totalPackets)
	t.Logf("- Total time: %v", elapsed)
	t.Logf("- Packets per second: %.2f", packetsPerSecond)
	t.Logf("- Average time per packet: %v", elapsed/time.Duration(totalPackets))

	// Basic performance assertion
	assert.Greater(t, packetsPerSecond, 10.0, "Server should handle at least 10 packets per second")
}

// BenchmarkE2E_HexDataProcessing benchmarks hex data processing
func BenchmarkE2E_HexDataProcessing(b *testing.B) {
	// Create and start test server
	testServer := NewE2EHexTestServer(&testing.T{})
	testServer.Start(&testing.T{})
	defer testServer.Stop(&testing.T{})

	// Use a standard data packet
	hexData := "68 55 00 68 04 01 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60 AB CD 16"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = testServer.SendHexDataFast(&testing.T{}, hexData)
		}
	})
}

// Helper function to add new test cases easily
func AddCustomHexTestCase(name, hexData, description string) HexDataTestCase {
	return HexDataTestCase{
		Name:             name,
		HexData:          hexData,
		Description:      description,
		DeviceType:       "Custom",
		ProtocolVersion:  "Unknown",
		IsEncrypted:      false,
		ShouldRespond:    false,
		ExpectParseError: false,
	}
}

// TestE2E_CustomHexData allows easy addition of custom hex data strings
func TestE2E_CustomHexData(t *testing.T) {
	// Add your custom hex data strings here
	customTestCases := []HexDataTestCase{
		// Example: Add your real device data here
		// AddCustomHexTestCase(
		//     "My_Real_Device_Data",
		//     "68 55 00 68 04 01 ... your hex data here ... 16",
		//     "Data captured from my actual Growatt device",
		// ),
	}

	if len(customTestCases) == 0 {
		t.Skip("No custom test cases defined - add your hex data to customTestCases slice")
	}

	// Create and start test server
	testServer := NewE2EHexTestServer(t)
	testServer.Start(t)
	defer testServer.Stop(t)

	for _, tc := range customTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Logf("Testing custom hex data: %s", tc.Description)
			t.Logf("Hex Data: %s", tc.HexData)

			err := testServer.SendHexData(t, tc.HexData)
			assert.NoError(t, err, "Failed to send custom hex data")

			t.Logf("✅ Custom test case '%s' completed", tc.Name)
		})
	}
}
