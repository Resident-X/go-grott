package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// InverterSimulator simulates a Growatt inverter sending data to go-grott server
type InverterSimulator struct {
	serverAddr string
	interval   time.Duration
	verbose    bool
	conn       net.Conn // Persistent connection
	hexData    []string // Loaded hex data from file
	dataIndex  int      // Current position in hex data array
}

// loadHexDataFromFile reads hex data lines from the specified file
func loadHexDataFromFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	var hexData []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") { // Skip empty lines and comments
			hexData = append(hexData, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if len(hexData) == 0 {
		return nil, fmt.Errorf("no hex data found in file %s", filename)
	}

	return hexData, nil
}

// NewInverterSimulator creates a new inverter simulator
func NewInverterSimulator(serverAddr, hexDataFile string, interval time.Duration, verbose bool) (*InverterSimulator, error) {
	hexData, err := loadHexDataFromFile(hexDataFile)
	if err != nil {
		return nil, err
	}

	return &InverterSimulator{
		serverAddr: serverAddr,
		interval:   interval,
		verbose:    verbose,
		hexData:    hexData,
		dataIndex:  0,
	}, nil
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

// getNextHexData returns the next hex data string from the loaded data, cycling through
func (sim *InverterSimulator) getNextHexData() string {
	if len(sim.hexData) == 0 {
		return ""
	}

	hexData := sim.hexData[sim.dataIndex]
	sim.dataIndex = (sim.dataIndex + 1) % len(sim.hexData) // Cycle through data
	return hexData
}

// sendHexData sends a single hex data string using the persistent connection
func (sim *InverterSimulator) sendHexData(ctx context.Context, hexDataStr string) error {
	if !sim.isConnected() {
		return fmt.Errorf("not connected to server")
	}

	// Decode hex string to bytes
	data, err := hex.DecodeString(strings.ReplaceAll(hexDataStr, " ", ""))
	if err != nil {
		return fmt.Errorf("failed to decode hex data: %w", err)
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
		log.Printf("📤 Sent %d bytes - Data: %s...", n, hexDataStr[:min(40, len(hexDataStr))])
	}

	// Try to read response
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

// Run starts the inverter simulator
func (sim *InverterSimulator) Run(ctx context.Context) error {
	log.Printf("🔌 Starting Growatt Inverter Simulator (Real Data)")
	log.Printf("   Server Address: %s", sim.serverAddr)
	log.Printf("   Send Interval: %v", sim.interval)
	log.Printf("   Hex Data Lines: %d", len(sim.hexData))
	log.Printf("   Verbose: %v", sim.verbose)
	log.Printf("")

	// Establish persistent connection
	if err := sim.connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer sim.disconnect()

	ticker := time.NewTicker(sim.interval)
	defer ticker.Stop()

	packetCount := 0
	startTime := time.Now()

	log.Printf("📡 Starting data transmission every %v", sim.interval)
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

			// Get next hex data and send it
			hexData := sim.getNextHexData()
			if hexData == "" {
				log.Printf("❌ No hex data available")
				continue
			}

			if err := sim.sendHexData(ctx, hexData); err != nil {
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
		serverAddr  = flag.String("server", "localhost:5279", "go-grott server address (host:port)")
		hexDataFile = flag.String("file", "testHexData.txt", "File containing hex data (one hex string per line)")
		interval    = flag.Duration("interval", 10*time.Second, "Interval between data transmissions")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
		help        = flag.Bool("help", false, "Show help message")
	)
	flag.Parse()

	if *help {
		fmt.Printf("Test Inverter Simulator for go-grott (Real Data)\n\n")
		fmt.Printf("This tool reads real hex data from a file and sends it to your go-grott server\n")
		fmt.Printf("for testing Home Assistant auto-discovery and general server functionality.\n\n")
		fmt.Printf("Usage:\n")
		flag.PrintDefaults()
		fmt.Printf("\nExample:\n")
		fmt.Printf("  %s -server localhost:5279 -file testHexData.txt -interval 10s -verbose\n", os.Args[0])
		fmt.Printf("  %s -server 192.168.1.100:5279 -file mydata.txt -interval 5s\n", os.Args[0])
		fmt.Printf("\nThe hex data file should contain one hex string per line.\n")
		fmt.Printf("Empty lines and lines starting with # are ignored.\n")
		fmt.Printf("The simulator will cycle through all lines in the file continuously.\n")
		os.Exit(0)
	}

	// Validate server address
	if _, _, err := net.SplitHostPort(*serverAddr); err != nil {
		log.Fatalf("❌ Invalid server address '%s': %v", *serverAddr, err)
	}

	// Check if hex data file exists
	if _, err := os.Stat(*hexDataFile); os.IsNotExist(err) {
		log.Fatalf("❌ Hex data file '%s' does not exist", *hexDataFile)
	}

	// Create simulator
	sim, err := NewInverterSimulator(*serverAddr, *hexDataFile, *interval, *verbose)
	if err != nil {
		log.Fatalf("❌ Failed to create simulator: %v", err)
	}

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
