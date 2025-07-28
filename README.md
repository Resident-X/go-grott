# go-grott: Growatt Inverter Monitor

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Build Status](https://img.shields.io/badge/Build-Passing-green)](https://github.com/resident-x/go-grott)
[![License](https://img.shields.io/badge/License-Unlicense-blue.svg)](LICENSE)

> High-performance Growatt inverter monitoring with enhanced Home Assistant integration

A modern, clean architecture Go implementation of the Grott (Growatt Inverter Monitor) server that receives, processes, and distributes data from Growatt solar inverters with robust Home Assistant auto-discovery.

## Features

- **High Performance**: Optimized TCP server handling multiple inverter connections
- **Layout-Driven Parsing**: JSON-based field extraction with no hardcoded parsing logic
- **Enhanced Home Assistant Integration**: Robust MQTT auto-discovery with reconnection handling
- **Multiple Integrations**: MQTT publishing, PVOutput.org support, and HTTP API
- **Clean Architecture**: Interface-driven design with comprehensive testing
- **Developer-Friendly**: Modern tooling with fast test feedback

## Enhanced Home Assistant Auto-Discovery

The latest version includes significant reliability improvements for Home Assistant integration:

- **Connection Recovery**: Automatic sensor re-discovery after network disruptions
- **Birth Message Support**: Instant re-discovery when Home Assistant restarts
- **Periodic Rediscovery**: Configurable intervals to ensure long-term reliability
- **Configurable Categories**: Selective sensor discovery (diagnostic, battery, grid, PV)

## Quick Start

### Prerequisites

- Go 1.24 or later
- [Task runner](https://taskfile.dev/) (recommended)

### Installation

```bash
# Clone the repository
git clone https://github.com/resident-x/go-grott.git
cd go-grott

# Install Task runner (recommended)
go install github.com/go-task/task/v3/cmd/task@latest

# Build and run
task build
task run
```

### Basic Configuration

```bash
# Use default config.yaml in current directory
./go-grott

# Or specify a custom configuration file
./go-grott -config /path/to/your/config.yaml
```

> [!NOTE]
> Layout files for parsing Growatt data are embedded in the binary. If you need to modify them, they are located in `internal/parser/layouts/`. After making changes, rebuild with `task build`.

## Configuration

### Server Configuration

```yaml
# Data Collection Server
server:
  host: 0.0.0.0      # Bind address (0.0.0.0 = all interfaces)
  port: 5279         # Port for Growatt inverter connections

# HTTP API Server  
api:
  enabled: true      # Enable REST API
  host: 0.0.0.0
  port: 5280         # API port
```

### MQTT & Home Assistant Integration

```yaml
# MQTT Publishing
mqtt:
  enabled: false
  host: localhost
  port: 1883
  topic: energy/growatt
  include_inverter_id: false  # Add inverter serial to topic path
  retain: false
  publish_raw: true          # Publish raw JSON data
  
  # Enhanced Home Assistant Auto-Discovery
  home_assistant_auto_discovery:
    enabled: false           # Enable MQTT auto-discovery for Home Assistant
    discovery_prefix: homeassistant  # MQTT discovery prefix 
    device_name: "Growatt Inverter"  # Device name in Home Assistant
    device_manufacturer: "Growatt"   # Device manufacturer
    device_model: ""         # Device model (auto-detected if empty)
    retain_discovery: true   # Whether discovery messages should be retained
    
    # NEW: Enhanced Reliability Features
    listen_to_birth_message: true     # Listen for HA restart notifications
    rediscovery_interval: "1h"        # Periodic rediscovery (e.g., "1h", "30m", "0" to disable)
    
    # Sensor Categories
    include_diagnostic: true # Include diagnostic sensors (fault codes, etc.)
    include_battery: true    # Include battery-related sensors
    include_grid: true       # Include grid-related sensors  
    include_pv: true         # Include PV panel sensors
```

### PVOutput.org Integration

```yaml
# PVOutput.org Integration
pvoutput:
  enabled: false
  api_key: "your-api-key"
  system_id: "your-system-id"
  update_limit_minutes: 5
```

## Home Assistant Integration

When `home_assistant_auto_discovery.enabled` is `true`, go-grott automatically creates sensors in Home Assistant for:

- **PV Sensors**: Solar panel voltage, current, power, and energy production
- **Grid Sensors**: Grid voltage, current, frequency, and power export/import  
- **Battery Sensors**: Battery voltage, current, state of charge, and power flow
- **Diagnostic Sensors**: Inverter temperature, fault codes, and system status

### Enhanced Reliability Features

The enhanced auto-discovery includes several reliability improvements:

**Connection Recovery**: When MQTT connection is restored, discovery cache is cleared to trigger immediate sensor re-discovery.

**Home Assistant Birth Messages**: When HA restarts and sends its "online" birth message, all sensors are automatically re-discovered without waiting for the next data cycle.

**Periodic Rediscovery**: Configurable intervals (e.g., hourly) automatically refresh all discovery messages to ensure long-term reliability.

**Graceful Degradation**: If enhanced features fail, the system continues working with basic discovery functionality.

### Sensor Configuration

Sensor definitions are stored in `internal/homeassistant/layouts/homeassistant_sensors.json`:
- Device classes and units for proper Home Assistant integration
- State classes for energy dashboard compatibility  
- Icon assignments and category classifications
- Easy customization without modifying Go code

## Development

### Testing with Simulated Data

```bash
# Build the test inverter
go build -o test-inverter ./cmd/test-inverter

# Run with default settings (sends data every 10 seconds)
./test-inverter

# Run with custom settings
./test-inverter -server localhost:5279 -interval 30s -verbose

# Test with different inverter models
./test-inverter -serial SPH5000TL3230001 -verbose
./test-inverter -serial MIN3600TLXE230001 -interval 5s
```

### Quick Commands

```bash
# Development workflow
task dev              # Build and run with hot reload
task test             # Run all tests
task coverage         # Generate coverage reports
task check            # Code quality checks (fmt, vet, test)

# Building
task build            # Standard build
task build-release    # Optimized release build

# Testing
task test-unit        # Unit tests only
task test-integration # Integration tests
task test-e2e         # End-to-end tests
task test-all         # Complete test suite
```

## HTTP API

The REST API provides comprehensive monitoring and management:

### Core Endpoints
- `GET /api/v1/status` - Server status and metrics
- `GET /api/v1/dataloggers` - List connected dataloggers
- `GET /api/v1/dataloggers/{id}/inverters` - Inverters for a datalogger

### Device Communication
- `GET /datalogger` - Read datalogger register
- `PUT /datalogger` - Write datalogger register
- `GET /inverter` - Read inverter register
- `PUT /inverter` - Write inverter register
- `GET /multiregister` - Read multiple registers
- `PUT /multiregister` - Write multiple registers

### Format Support
All endpoints support multiple data formats via `?format=` parameter:
- `dec` - Decimal values
- `hex` - Hexadecimal values  
- `text` - Text representation

## Architecture

### Design Principles
- **Clean Architecture**: Clear separation of concerns with dependency inversion
- **Interface-Driven**: All components interact through well-defined interfaces
- **Testability**: Comprehensive mocking and testing infrastructure
- **Performance**: Optimized for high-throughput data processing
- **Reliability**: Graceful error handling and recovery mechanisms

### Core Components

1. **DataCollectionServer**: Handles TCP connections from Growatt dataloggers
2. **APIServer**: Provides HTTP API for monitoring and device interaction
3. **SessionManager**: Tracks device connections and state
4. **MessagePublisher**: Distributes data to MQTT and external services with enhanced HA discovery

### Data Flow
```
Growatt Inverter → TCP Connection → Protocol Parser → Data Processing → 
                                                   ↓
MQTT Broker ← Enhanced HA Discovery ← Message Publisher ← Parsed Data
                                                   ↓  
PVOutput.org ← PVOutput Client ← Formatted Data ← API Server
```

## Project Structure

```
go-grott/
├── cmd/                # Application entry point
├── internal/           # Core application code
│   ├── api/           # HTTP API server & endpoints
│   ├── config/        # Configuration management
│   ├── domain/        # Domain models & interfaces
│   ├── homeassistant/ # Enhanced HA auto-discovery
│   ├── parser/        # Growatt protocol parsing
│   │   └── layouts/   # JSON parsing layout files
│   ├── protocol/      # Protocol command handling
│   ├── pubsub/        # Enhanced MQTT publishing
│   ├── service/       # Business logic services
│   ├── session/       # Connection session management
│   └── validation/    # Data validation
├── test/             # End-to-end tests
├── mocks/            # Generated test mocks
├── config.yaml       # Default configuration
└── Taskfile.yml      # Task runner configuration
```

## Testing & Quality

### Testing Commands
```bash
# Run specific test suites
task test-unit        # Fast unit tests
task test-integration # Integration tests  
task test-e2e         # End-to-end system tests
task test-all         # Complete test suite

# Coverage analysis
task coverage         # Generate coverage reports
task coverage-html    # HTML coverage report
```

### Code Quality
```bash
task check           # Full quality check
task fmt             # Format code
task vet             # Static analysis
task lint            # Linting (requires golangci-lint)
```

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for:
- Development setup and workflow
- Code style and conventions
- Testing requirements
- Pull request process

## License

This project is released into the public domain under the Unlicense - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Original [Grott project](https://github.com/johanmeijer/grott) by Johan Meijer
- Growatt inverter community for protocol documentation
- Contributors and testers who helped improve this implementation
