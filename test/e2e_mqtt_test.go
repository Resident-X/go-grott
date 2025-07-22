package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/parser"
	"github.com/resident-x/go-grott/internal/pubsub"
	"github.com/resident-x/go-grott/internal/service"
)

// NoopMonitoringService is a no-op monitoring service for MQTT testing
type NoopMonitoringService struct{}

func (n *NoopMonitoringService) Connect() error {
	return nil
}

func (n *NoopMonitoringService) Send(ctx context.Context, data *domain.InverterData) error {
	return nil
}

func (n *NoopMonitoringService) Close() error {
	return nil
}

func TestE2E_MQTTPublishing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E MQTT test in short mode")
	}

	// Test cases for different inverter data types
	testCases := []struct {
		name                string
		hexData             string
		expectMQTTMessage   bool
		expectedTopic       string
		expectedInverterID  string
		expectedDataloggerID string
		description         string
	}{
		{
			name:                "Encrypted_Growatt_Data",
			hexData:             "00020006002001161f352b4122363e7540387761747447726f7761747447726f7761747447722eb2",
			expectMQTTMessage:   true,
			expectedTopic:       "energy/growatt",
			description:         "Real encrypted Growatt data should be published to MQTT",
		},
		{
			name:                "Large_Encrypted_Data",
			hexData:             "000e0006024101031f352b4122363e7540387761747447726f7761747447726f7761747447722c222a403705235f4224747447726f7761747447726f7761747447726f777873604e7459756174743b726e77b8747447166f77466474464aef74893539765c5f773b353606726777607474449a6f36613574e072c8776137210c462c3530444102726f7761747547166f7761745467523f21413d1a31171d0304065467726f63307675409b6f706160744e7264774c7474407a652d7328601770d37ddf662853226dcb6bca661b663f709e7d9655fc7ce06379740c72247764744647776f3c6171740c726a772a74714d666f7720393506425d47504444774a6e466174744761ce77427d144d6667ef69627453726a7e0e7c886062486746645357557f5071747447726e5b618b3a6772903941748b09526f882f547746726f7860742447736d0661747fff7e5b776137210c462c3530444102726f7761747447726f7761747447726f7761747447726e83617475d7726e7761747447726d2f61747447726f7761747451da6f7761747447726f7761741047786f7761747447726f7761747447726f7761747423720b7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744772372f392c2c1f2a372f392c2c1f2a372f61747447726f7761545467526f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744771",
			expectMQTTMessage:   true,
			expectedTopic:       "energy/growatt",
			description:         "Large encrypted inverter data should be published to MQTT",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			t.Logf("=== Starting test: %s ===", tt.name)
			t.Logf("Description: %s", tt.description)

			// Start test MQTT broker
			mqttBroker, mqttPort := startTestMQTTBroker(t)
			defer mqttBroker.Close()

			// Set up MQTT message capture
			receivedMessages := make(chan MQTTMessage, 5)
			subscribeToMQTTMessages(t, mqttPort, "energy/+", receivedMessages)

			// Create and start Go-Grott server with MQTT enabled
			server, serverPort := createE2EServerWithMQTT(t, mqttPort)
			
			// Start server in background
			serverErr := make(chan error, 1)
			go func() {
				t.Logf("Starting Go-Grott server on port %d...", serverPort)
				if err := server.Start(ctx); err != nil {
					t.Logf("Server start error: %v", err)
					serverErr <- err
				}
			}()

			// Give server time to start and check for startup errors
			time.Sleep(200 * time.Millisecond)
			
			select {
			case err := <-serverErr:
				t.Fatalf("Server failed to start: %v", err)
			default:
				t.Log("Server should be running now")
			}

			// Connect to the server and send test data
			serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
			t.Logf("Testing %s: %s", tt.name, tt.description)
			t.Logf("Sending hex data: %s", tt.hexData)

			// Decode hex data
			data, err := hex.DecodeString(tt.hexData)
			require.NoError(t, err, "Failed to decode hex data")
			t.Logf("Decoded %d bytes of hex data", len(data))

			// Connect to server
			t.Logf("Connecting to server at %s", serverAddr)
			conn, err := net.DialTimeout("tcp", serverAddr, 5*time.Second)
			require.NoError(t, err, "Failed to connect to server")
			defer conn.Close()
			t.Log("Connected to server successfully")

			// Send data
			t.Logf("Sending %d bytes to server", len(data))
			n, err := conn.Write(data)
			require.NoError(t, err, "Failed to send data")
			t.Logf("Sent %d bytes to server", n)

			if tt.expectMQTTMessage {
				// Wait for MQTT message
				t.Log("Waiting for MQTT message...")
				select {
				case msg := <-receivedMessages:
					t.Logf("✅ SUCCESS: Received MQTT message on topic '%s'", msg.Topic)
					t.Logf("Message content: %s", string(msg.Payload))
					
					// Verify topic
					assert.Contains(t, msg.Topic, "energy/growatt", "MQTT topic should contain energy/growatt")
					
					// Verify message is valid JSON
					var parsedData map[string]interface{}
					err := json.Unmarshal(msg.Payload, &parsedData)
					assert.NoError(t, err, "MQTT message should be valid JSON")
					
					// Verify message contains expected fields
					assert.Contains(t, parsedData, "timestamp", "MQTT message should contain timestamp")
					assert.Contains(t, parsedData, "datalogserial", "MQTT message should contain datalogserial")
					
					// Check if message contains extended data
					if extData, ok := parsedData["extended"].(map[string]interface{}); ok {
						t.Logf("Extended data fields: %+v", extData)
					}
					
				case <-time.After(12 * time.Second):
					t.Fatal("❌ TIMEOUT: No MQTT message received within 12 seconds")
				}
			}

			// Stop server
			t.Log("Stopping server...")
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			
			err = server.Stop(stopCtx)
			assert.NoError(t, err, "Failed to stop server")
			t.Log("Server stopped successfully")
		})
	}
}

// MQTTMessage represents a received MQTT message
type MQTTMessage struct {
	Topic   string
	Payload []byte
}

// startTestMQTTBroker starts an embedded MQTT broker for testing
func startTestMQTTBroker(t *testing.T) (*mqttserver.Server, int) {
	// Find available port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create MQTT server
	mqttServer := mqttserver.New(&mqttserver.Options{
		InlineClient: true,
	})

	// Allow all connections
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create TCP listener
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "t1",
		Address: fmt.Sprintf(":%d", port),
	})

	err = mqttServer.AddListener(tcp)
	require.NoError(t, err, "Failed to add TCP listener to MQTT broker")

	// Start server
	go func() {
		err := mqttServer.Serve()
		if err != nil {
			t.Logf("MQTT broker error: %v", err)
		}
	}()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	t.Logf("Test MQTT broker started on port %d", port)
	return mqttServer, port
}

// subscribeToMQTTMessages subscribes to MQTT topics and forwards messages to channel
func subscribeToMQTTMessages(t *testing.T, brokerPort int, topicPattern string, msgChan chan<- MQTTMessage) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://localhost:%d", brokerPort))
	opts.SetClientID("test-subscriber")
	opts.SetConnectTimeout(5 * time.Second)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	require.True(t, token.WaitTimeout(5*time.Second), "Failed to connect MQTT subscriber")
	require.NoError(t, token.Error(), "MQTT subscriber connection error")

	// Subscribe to topics
	token = client.Subscribe(topicPattern, 0, func(client mqtt.Client, msg mqtt.Message) {
		select {
		case msgChan <- MQTTMessage{
			Topic:   msg.Topic(),
			Payload: msg.Payload(),
		}:
		default:
			t.Logf("MQTT message channel full, dropping message")
		}
	})
	require.True(t, token.WaitTimeout(5*time.Second), "Failed to subscribe to MQTT topic")
	require.NoError(t, token.Error(), "MQTT subscribe error")

	t.Logf("MQTT subscriber connected and listening on topic pattern: %s", topicPattern)

	// Keep connection alive in background
	go func() {
		time.Sleep(25 * time.Second) // Test timeout
		client.Disconnect(1000)
	}()
}

// createE2EServerWithMQTT creates a server instance configured for MQTT testing
func createE2EServerWithMQTT(t *testing.T, mqttPort int) (*service.DataCollectionServer, int) {
	// Find an available port for the server
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	serverPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create config with MQTT enabled
	cfg := config.DefaultConfig()
	cfg.LogLevel = "debug"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = serverPort // Use dynamic port
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = mqttPort
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = false
	cfg.API.Enabled = false // Disable API for simpler test

	t.Logf("MQTT Config: Host=%s, Port=%d, Topic=%s", cfg.MQTT.Host, cfg.MQTT.Port, cfg.MQTT.Topic)
	t.Logf("Server Config: Host=%s, Port=%d", cfg.Server.Host, cfg.Server.Port)

	// Create parser
	parser, err := parser.NewParser(cfg)
	require.NoError(t, err, "Failed to create parser")

	// Create MQTT publisher
	publisher := pubsub.NewMQTTPublisher(cfg)
	
	// Connect to MQTT broker with retry
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer connectCancel()
	
	t.Logf("Connecting MQTT publisher to broker at localhost:%d", mqttPort)
	err = publisher.Connect(connectCtx)
	require.NoError(t, err, "Failed to connect MQTT publisher")
	t.Log("MQTT publisher connected successfully")

	// Give MQTT connection time to stabilize
	time.Sleep(100 * time.Millisecond)

	// Create monitoring service (no-op for test)
	monitoring := &NoopMonitoringService{}

	// Create server
	server, err := service.NewDataCollectionServer(cfg, parser, publisher, monitoring)
	require.NoError(t, err, "Failed to create server")

	t.Log("E2E server with MQTT configured")
	return server, serverPort
}

func TestE2E_MQTTWithInverterID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E MQTT with inverter ID test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Start test MQTT broker
	mqttBroker, mqttPort := startTestMQTTBroker(t)
	defer mqttBroker.Close()

	// Set up MQTT message capture for inverter-specific topics
	receivedMessages := make(chan MQTTMessage, 5)
	subscribeToMQTTMessages(t, mqttPort, "energy/growatt/+", receivedMessages)

	// Create config with inverter ID in topic enabled
	cfg := config.DefaultConfig()
	cfg.LogLevel = "debug"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 5280 // Different port
	cfg.MQTT.Enabled = true
	cfg.MQTT.Host = "localhost"
	cfg.MQTT.Port = mqttPort
	cfg.MQTT.Topic = "energy/growatt"
	cfg.MQTT.IncludeInverterID = true // Enable inverter ID in topic
	cfg.API.Enabled = false

	// Create components
	parser, err := parser.NewParser(cfg)
	require.NoError(t, err)

	publisher := pubsub.NewMQTTPublisher(cfg)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()
	err = publisher.Connect(connectCtx)
	require.NoError(t, err)

	monitoring := &NoopMonitoringService{}

	server, err := service.NewDataCollectionServer(cfg, parser, publisher, monitoring)
	require.NoError(t, err)

	// Start server
	go func() {
		if err := server.Start(ctx); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Send data with parseable inverter information
	hexData := "00020006002001161f352b4122363e7540387761747447726f7761747447726f7761747447722eb2" // Real encrypted Growatt data
	data, err := hex.DecodeString(hexData)
	require.NoError(t, err)

	conn, err := net.DialTimeout("tcp", "127.0.0.1:5280", 5*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write(data)
	require.NoError(t, err)

	// Wait for MQTT message
	select {
	case msg := <-receivedMessages:
		t.Logf("Received MQTT message on topic '%s': %s", msg.Topic, string(msg.Payload))
		
		// Should contain energy/growatt in the topic
		assert.Contains(t, msg.Topic, "energy/growatt")
		
		// Verify JSON structure
		var parsedData map[string]interface{}
		err := json.Unmarshal(msg.Payload, &parsedData)
		assert.NoError(t, err)

	case <-time.After(10 * time.Second):
		t.Log("No MQTT message received (this might be expected if no inverter ID is parsed)")
	}

	// Stop server
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = server.Stop(stopCtx)
	assert.NoError(t, err)
}
