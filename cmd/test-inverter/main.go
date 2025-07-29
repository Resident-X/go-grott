package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// InverterSimulator simulates a Growatt inverter sending data to go-grott server
type InverterSimulator struct {
	serverAddr   string
	deviceSerial string
	interval     time.Duration
	verbose      bool
	conn         net.Conn // Persistent connection
}

// GrowattMessage represents different types of Growatt protocol messages
type GrowattMessage struct {
	Name               string
	HexData            string
	Description        string
	RecordType         string
	ShouldSendSequence bool // If true, send as part of normal sequence
}

// getGrowattMessages returns realistic Growatt protocol messages from your e2e tests
func getGrowattMessages() []GrowattMessage {
	return []GrowattMessage{
		{
			Name:               "Inverter_Announcement",
			HexData:            "00020006024101031f352b4122363e7540387761747447726f7761747447726f7761747447722c222a403705235f4224747447726f7761747447726f7761747447726f777873604e7459756174743b726e77b8747447166f77466474464aef74893539765c5f773b353606726777607474449a6f36613574e072c8776137210c462c3530444102726f7761747547166f7761745467523f21413d1a31171d0304065467726f63307675409b6f706160744e7264774c7474407a652d7328601770d37ddf662853226dcb6bca661b663f709e7d9655fc7ce06379740c72247764744647776f3c6171740c726a772a74714d666f7720393506425d47504444774a6e466174744761ce77427d144d6667ef69627453726a7e0e7c886062486746645357557f5071747447726e5b618b3a6772903941748b09526f882f547746726f7860742447736d0661747fff7e5b776137210c462c3530444102726f7761747447726f7761747447726f7761747447726e83617475d7726e7761747447726d2f61747447726f7761747451da6f7761747447726f7761741047786f7761747447726f7761747447726f7761747423720b7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744772372f392c2c1f2a372f392c2c1f2a372f61747447726f7761545467526f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744721f6",
			Description:        "Inverter announcement/registration with datalogger",
			RecordType:         "03",
			ShouldSendSequence: true,
		},
		{
			Name:               "Regular_Data_Packet",
			HexData:            "001d0006024101041f352b4122363e7540387761747447726f7761747447726f7761747447722c222a403705235f4224747447726f7761747447726f7761747447726f777873604c5f75756174743b726e77b8747447166f77466474464aef74893539765c5f773b353606726777607474449a6f36613574e072c8776137210c462c3530444102726f7761747547166f7761745467523f21413d1a31171d0304065467726f63307675409b6f706160744c7240774f7474407a652d7328601770d37ddf662853226dcb6bca661b663f709e7d9655fc7ce06379740c72247764744647776f3c6171740c726a772a74714d666f7720393506425d47504444774a6e466174744761ce77427d144d6667ef69627453726a7e0e7c886062486746645357557f5071747447726e5b618b3a6772903941748b09526f882f547746726f7860742447736d0661747fff7e5b776137210c462c3530444102726f7761747447726f7761747447726f7761747447726e83617475d7726e7761747447726d2f61747447726f7761747451da6f7761747447726f7761741047786f7761747447726f7761747447726f7761747423720b7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744772372f392c2c1f2a372f392c2c1f2a372f61747447726f7761545467526f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744721f6",
			Description:        "Regular inverter data transmission",
			RecordType:         "04",
			ShouldSendSequence: true,
		},
		{
			Name:               "Ping_Response",
			HexData:            "00060006002001161f352b4122363e7540387761747447726f7761747447726f776174744772eec4",
			Description:        "Ping response to server",
			RecordType:         "16",
			ShouldSendSequence: false, // Only send when specifically requested
		},
		{
			Name:               "Extended_Data_Packet",
			HexData:            "000e0006024101031f352b4122363e7540387761747447726f7761747447726f7761747447722c222a403705235f4224747447726f7761747447726f7761747447726f777873604e7459756174743b726e77b8747447166f77466474464aef74893539765c5f773b353606726777607474449a6f36613574e072c8776137210c462c3530444102726f7761747547166f7761745467523f21413d1a31171d0304065467726f63307675409b6f706160744e7264774c7474407a652d7328601770d37ddf662853226dcb6bca661b663f709e7d9655fc7ce06379740c72247764744647776f3c6171740c726a772a74714d666f7720393506425d47504444774a6e466174744761ce77427d144d6667ef69627453726a7e0e7c886062486746645357557f5071747447726e5b618b3a6772903941748b09526f882f547746726f7860742447736d0661747fff7e5b776137210c462c3530444102726f7761747447726f7761747447726f7761747447726e83617475d7726e7761747447726d2f61747447726f7761747451da6f7761747447726f7761741047786f7761747447726f7761747447726f7761747423720b7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f776174744772372f392c2c1f2a372f392c2c1f2a372f61747447726f7761545467526f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447726f7761747447ff71",
			Description:        "Extended data packet with additional sensor readings",
			RecordType:         "03",
			ShouldSendSequence: true,
		},
		{
			Name:               "Short_Status_Update",
			HexData:            "00020006002001161f352b4122363e7540387761747447726f7761747447726f776174744772eeb2",
			Description:        "Short status update message",
			RecordType:         "16",
			ShouldSendSequence: false,
		},
	}
}

// NewInverterSimulator creates a new inverter simulator
func NewInverterSimulator(serverAddr, deviceSerial string, interval time.Duration, verbose bool) *InverterSimulator {
	return &InverterSimulator{
		serverAddr:   serverAddr,
		deviceSerial: deviceSerial,
		interval:     interval,
		verbose:      verbose,
	}
}

// connect establishes connection to the go-grott server
func (sim *InverterSimulator) connect(ctx context.Context) error {
	if sim.verbose {
		log.Printf("🔗 Connecting to go-grott server at %s", sim.serverAddr)
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", sim.serverAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", sim.serverAddr, err)
	}

	sim.conn = conn

	if sim.verbose {
		log.Printf("✅ Connected to go-grott server")
	}

	return nil
}

// disconnect closes the connection to the server
func (sim *InverterSimulator) disconnect() {
	if sim.conn != nil {
		sim.conn.Close()
		sim.conn = nil
		if sim.verbose {
			log.Printf("🔌 Disconnected from server")
		}
	}
}

// isConnected checks if we have an active connection
func (sim *InverterSimulator) isConnected() bool {
	return sim.conn != nil
}

// sendMessage sends a single message using the persistent connection
func (sim *InverterSimulator) sendMessage(ctx context.Context, msg GrowattMessage) error {
	if !sim.isConnected() {
		return fmt.Errorf("not connected to server")
	}

	// Decode hex string to bytes
	data, err := hex.DecodeString(strings.ReplaceAll(msg.HexData, " ", ""))
	if err != nil {
		return fmt.Errorf("failed to decode hex data for %s: %w", msg.Name, err)
	}

	// Set write deadline
	if err := sim.conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Send data
	n, err := sim.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	if sim.verbose {
		log.Printf("📤 Sent %s (%d bytes): %s", msg.Name, n, msg.Description)
		log.Printf("   Record Type: %s, Hex: %s...", msg.RecordType, msg.HexData[:min(40, len(msg.HexData))])
	}

	// Try to read response with longer timeout for time sync
	if err := sim.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	respBuf := make([]byte, 1024)
	respN, respErr := sim.conn.Read(respBuf)
	if respErr == nil && respN > 0 {
		if sim.verbose {
			log.Printf("📥 Received response (%d bytes): %s", respN, hex.EncodeToString(respBuf[:respN]))
		}
	} else if sim.verbose && respErr != nil {
		// Only log if it's not a timeout (which is expected for some messages)
		if !strings.Contains(respErr.Error(), "timeout") && !strings.Contains(respErr.Error(), "deadline") {
			log.Printf("   Response error: %v", respErr)
		}
	}

	return nil
}

// getRandomDataMessage returns a random data message with some variation
func (sim *InverterSimulator) getRandomDataMessage() GrowattMessage {
	messages := getGrowattMessages()

	// Filter to only sequence messages (regular data)
	var dataMessages []GrowattMessage
	for _, msg := range messages {
		if msg.ShouldSendSequence {
			dataMessages = append(dataMessages, msg)
		}
	}

	if len(dataMessages) == 0 {
		// Fallback to first message
		return messages[0]
	}

	// Select random message
	return dataMessages[rand.Intn(len(dataMessages))]
}

// varyMessageData adds small variations to the hex data to simulate changing sensor values
func (sim *InverterSimulator) varyMessageData(hexData string) string {
	// Convert to bytes
	data, err := hex.DecodeString(strings.ReplaceAll(hexData, " ", ""))
	if err != nil {
		return hexData // Return original if decode fails
	}

	// Add small random variations to some bytes (simulate changing sensor readings)
	// Only modify data payload area (after header), avoid protocol bytes
	if len(data) > 20 {
		// Modify a few bytes in the middle section to simulate sensor value changes
		for i := 0; i < 3; i++ {
			pos := 20 + rand.Intn(min(len(data)-25, 50)) // Modify bytes 20-70
			if pos < len(data)-2 {                       // Avoid CRC area
				variation := byte(rand.Intn(30) - 15) // Small variation -15 to +15
				data[pos] = byte(int(data[pos]) + int(variation))
			}
		}
	}

	return hex.EncodeToString(data)
}

// simulateInverterSequence simulates a realistic inverter communication sequence
func (sim *InverterSimulator) simulateInverterSequence(ctx context.Context) error {
	messages := getGrowattMessages()

	// 1. Start with announcement (occasionally)
	if rand.Float32() < 0.1 { // 10% chance to send announcement
		for _, msg := range messages {
			if msg.RecordType == "03" && msg.Name == "Inverter_Announcement" {
				msg.HexData = sim.varyMessageData(msg.HexData)
				if err := sim.sendMessage(ctx, msg); err != nil {
					return err
				}
				time.Sleep(100 * time.Millisecond) // Small delay after announcement
				break
			}
		}
	}

	// 2. Send regular data
	dataMsg := sim.getRandomDataMessage()
	dataMsg.HexData = sim.varyMessageData(dataMsg.HexData)

	return sim.sendMessage(ctx, dataMsg)
}

// Run starts the inverter simulator
func (sim *InverterSimulator) Run(ctx context.Context) error {
	log.Printf("🔌 Starting Growatt Inverter Simulator")
	log.Printf("   Device Serial: %s", sim.deviceSerial)
	log.Printf("   Server Address: %s", sim.serverAddr)
	log.Printf("   Send Interval: %v", sim.interval)
	log.Printf("   Verbose: %v", sim.verbose)
	log.Printf("")

	// Establish persistent connection
	if err := sim.connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer sim.disconnect()

	// Seed random number generator (fix deprecation)
	source := rand.NewSource(time.Now().UnixNano())
	_ = rand.New(source) // Initialize but use global rand for simplicity

	ticker := time.NewTicker(sim.interval)
	defer ticker.Stop()

	packetCount := 0
	startTime := time.Now()

	// Send initial announcement
	messages := getGrowattMessages()
	for _, msg := range messages {
		if msg.Name == "Inverter_Announcement" {
			log.Printf("🚀 Sending initial inverter announcement...")
			if err := sim.sendMessage(ctx, msg); err != nil {
				log.Printf("❌ Failed to send initial announcement: %v", err)
				// Try to reconnect
				sim.disconnect()
				if err := sim.connect(ctx); err != nil {
					return fmt.Errorf("failed to reconnect after announcement error: %w", err)
				}
			}
			break
		}
	}

	log.Printf("📡 Starting regular data transmission every %v", sim.interval)
	log.Printf("Press Ctrl+C to stop...")
	log.Printf("")

	for {
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime)
			log.Printf("")
			log.Printf("🛑 Inverter simulator stopped")
			log.Printf("   Packets sent: %d", packetCount)
			log.Printf("   Runtime: %v", elapsed.Round(time.Second))
			if packetCount > 0 {
				log.Printf("   Average rate: %.2f packets/min", float64(packetCount)/elapsed.Minutes())
			}
			return ctx.Err()

		case <-ticker.C:
			// Check if connection is still alive
			if !sim.isConnected() {
				log.Printf("🔗 Connection lost, reconnecting...")
				if err := sim.connect(ctx); err != nil {
					log.Printf("❌ Failed to reconnect: %v", err)
					continue
				}
			}

			if err := sim.simulateInverterSequence(ctx); err != nil {
				log.Printf("❌ Error sending data: %v", err)
				// On error, try to reconnect for next attempt
				sim.disconnect()
				continue
			}

			packetCount++
			if !sim.verbose {
				// Show periodic status in non-verbose mode
				if packetCount%10 == 0 {
					elapsed := time.Since(startTime)
					rate := float64(packetCount) / elapsed.Minutes()
					log.Printf("📊 Sent %d packets in %v (%.1f packets/min)",
						packetCount, elapsed.Round(time.Second), rate)
				}
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	var (
		serverAddr   = flag.String("server", "localhost:5279", "go-grott server address (host:port)")
		deviceSerial = flag.String("serial", "MIC600TLX230001", "Device serial number")
		interval     = flag.Duration("interval", 10*time.Second, "Interval between data transmissions")
		verbose      = flag.Bool("verbose", false, "Enable verbose logging")
		help         = flag.Bool("help", false, "Show help message")
	)
	flag.Parse()

	if *help {
		fmt.Printf("Test Inverter Simulator for go-grott\n\n")
		fmt.Printf("This tool simulates a Growatt inverter sending realistic protocol data\n")
		fmt.Printf("to your go-grott server for testing Home Assistant auto-discovery\n")
		fmt.Printf("and general server functionality.\n\n")
		fmt.Printf("Usage:\n")
		flag.PrintDefaults()
		fmt.Printf("\nExample:\n")
		fmt.Printf("  %s -server localhost:5279 -interval 10s -verbose\n", os.Args[0])
		fmt.Printf("  %s -server 192.168.1.100:5279 -serial SPH5000TL3230001 -interval 30s\n", os.Args[0])
		fmt.Printf("\nThe simulator uses real Growatt protocol data from e2e tests\n")
		fmt.Printf("and varies the data slightly to simulate changing sensor readings.\n")
		os.Exit(0)
	}

	// Validate server address
	if _, _, err := net.SplitHostPort(*serverAddr); err != nil {
		log.Fatalf("❌ Invalid server address '%s': %v", *serverAddr, err)
	}

	// Create simulator
	sim := NewInverterSimulator(*serverAddr, *deviceSerial, *interval, *verbose)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("\n⚠️  Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Test initial connection
	log.Printf("🔍 Testing connection to %s...", *serverAddr)
	conn, err := net.DialTimeout("tcp", *serverAddr, 5*time.Second)
	if err != nil {
		log.Fatalf("❌ Cannot connect to go-grott server at %s: %v\n"+
			"   Make sure your go-grott server is running and listening on this address.", *serverAddr, err)
	}
	conn.Close()
	log.Printf("✅ Connection successful!")
	log.Printf("")

	// Run simulator
	if err := sim.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("❌ Simulator error: %v", err)
	}
}
