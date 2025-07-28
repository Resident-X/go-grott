# go-grott: Growatt Inverter Monitor in Go

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Build Status](https://img.shields.io/badge/Build-Passing-green)](https://github.com/resident-x/go-grott)
[![License](https://img.shields.io/badge/License-Unlicense-blue.svg)](LICENSE)

A high-performance, clean architecture Go implementation of the Grott (Growatt Inverter Monitor) server that receives, processes, and distributes data from Growatt solar inverters.

## ✨ Key Features

- 🚀 **High Performance**: TCP server optimized for handling multiple inverter connections
- 🔍 **Layout-Driven Parsing**: JSON-based field extraction with no hardcoded parsing logic
- 📡 **Multiple Integrations**: MQTT publishing, Home Assistant auto-discovery, and PVOutput.org support  
- 🏠 **Home Assistant Ready**: Automatic MQTT discovery with proper device classes and energy dashboard integration
- 🌐 **HTTP API**: REST endpoints for monitoring and management
- 🏗️ **Clean Architecture**: Interface-driven design with comprehensive testing
- 🔧 **Developer-Friendly**: Modern tooling with automated testing
- ⚡ **Fast Tests**: Optimized test timeouts for quick development feedback

## 🚀 Quick Start

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

By default, go-grott looks for `config.yaml` in the current working directory. You can specify a different configuration file using the `-config` flag:

```bash
# Use default config.yaml in current directory
./go-grott

# Or specify a custom configuration file
./go-grott -config /path/to/your/config.yaml
```

**Layout Files**: JSON layout files for parsing Growatt data are embedded in the binary by default. If you need to modify them, they are located in `internal/parser/layouts/`. After making changes, rebuild the binary:

```bash
task build  # Rebuilds with updated layouts
```

## 📋 Configuration

All configuration is managed through `config.yaml`. Key sections include:

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

### Integration Configuration
```yaml
# MQTT Publishing
mqtt:
  enabled: false
  host: localhost
  port: 1883
  topic: energy/growatt
  include_inverter_id: false  # Add inverter serial to topic path
  retain: false
  publish_raw: true          # Publish raw JSON data (original behavior)
  
  # Home Assistant Auto-Discovery
  homeassistant_autodiscovery:
    enabled: false           # Enable MQTT auto-discovery for Home Assistant
    discovery_prefix: homeassistant  # MQTT discovery prefix 
    device_name: "Growatt Inverter"  # Device name in Home Assistant
    device_manufacturer: "Growatt"   # Device manufacturer
    device_model: ""         # Device model (auto-detected if empty)
    retain_discovery: true   # Whether discovery messages should be retained
    include_diagnostic: true # Include diagnostic sensors (fault codes, etc.)
    include_battery: true    # Include battery-related sensors
    include_grid: true       # Include grid-related sensors  
    include_pv: true         # Include PV panel sensors

# PVOutput.org Integration
pvoutput:
  enabled: false
  api_key: "your-api-key"
  system_id: "your-system-id"
  update_limit_minutes: 5
```

#### Home Assistant Integration

When `homeassistant_autodiscovery.enabled` is `true`, go-grott automatically publishes MQTT discovery messages that allow Home Assistant to automatically create sensors for your Growatt inverter data. This includes:

- **PV Sensors**: Solar panel voltage, current, power, and energy production
- **Grid Sensors**: Grid voltage, current, frequency, and power export/import  
- **Battery Sensors**: Battery voltage, current, state of charge, and power flow (if available)
- **Diagnostic Sensors**: Inverter temperature, fault codes, and system status

The sensors automatically appear in Home Assistant with proper device classes, units of measurement, and state classes for energy dashboard integration.

**Sensor Configuration:**

Sensor definitions are stored in `internal/homeassistant/layouts/homeassistant_sensors.json`, which contains:
- Sensor names and device classes for proper Home Assistant integration
- Units of measurement and state classes for energy dashboard compatibility  
- Icon assignments and category classifications
- Easy customization without modifying Go code

**Configuration Options:**

- `publish_raw`: Enable/disable publishing of raw JSON data (can be disabled if only using Home Assistant)
- `include_*`: Control which categories of sensors to create
- `device_model`: Automatically detected from inverter type if not specified
- `retain_discovery`: Keeps discovery messages on broker for reliable sensor creation

## 🔧 Development

### Testing with Simulated Data

The project includes a test inverter simulator that sends realistic Growatt protocol data for testing:

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

The test inverter:
- Uses real Growatt protocol data from e2e tests
- Varies sensor readings to simulate realistic changes
- Supports different inverter serial patterns for device model testing
- Perfect for testing Home Assistant auto-discovery integration

### Running Tests

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

### Project Structure

```
go-grott/
├── cmd/                # Application entry point
├── internal/           # Core application code
│   ├── api/           # HTTP API server & endpoints
│   ├── config/        # Configuration management
│   ├── domain/        # Domain models & interfaces
│   ├── parser/        # Growatt protocol parsing
│   │   └── layouts/   # JSON parsing layout files
│   ├── protocol/      # Protocol command handling
│   ├── pubsub/        # MQTT publishing
│   ├── service/       # Business logic services
│   ├── session/       # Connection session management
│   └── validation/    # Data validation
├── test/             # End-to-end tests
├── mocks/            # Generated test mocks
├── config.yaml       # Default configuration
└── Taskfile.yml      # Task runner configuration
```

## 🌐 HTTP API

The REST API provides comprehensive monitoring and management endpoints:

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

## 🏗️ Architecture

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
4. **MessagePublisher**: Distributes data to MQTT and external services

### Data Flow
```
Growatt Inverter → TCP Connection → Protocol Parser → Data Processing → 
                                                   ↓
MQTT Broker ← Message Publisher ← Parsed Data ← Layout Engine
                                                   ↓  
PVOutput.org ← PVOutput Client ← Formatted Data ← API Server
```

## 📊 Testing & Quality

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

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for:
- Development setup and workflow
- Code style and conventions
- Testing requirements
- Pull request process

## 📄 License

This project is released into the public domain under the Unlicense - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Original [Grott project](https://github.com/johanmeijer/grott) by Johan Meijer
- Growatt inverter community for protocol documentation
- Contributors and testers who helped improve this implementation
