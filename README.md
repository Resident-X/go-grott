# go-grott: Growatt Inverter Monitor in Go

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Test Coverage](https://img.shields.io/badge/Coverage-84%25+-green)](./coverage)
[![Build Status](https://img.shields.io/badge/Build-Passing-green)](https://github.com/resident-x/go-grott)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A high-performance, clean architecture Go implementation of the Grott (Growatt Inverter Monitor) server that receives, processes, and distributes data from Growatt solar inverters.

## ✨ Key Features

- 🚀 **High Performance**: TCP server optimized for handling multiple inverter connections
- 🔍 **Layout-Driven Parsing**: JSON-based field extraction with no hardcoded parsing logic
- 📡 **Multiple Integrations**: MQTT publishing and PVOutput.org support
- 🌐 **HTTP API**: REST endpoints for monitoring and management
- 🏗️ **Clean Architecture**: Interface-driven design with comprehensive testing
- 🔧 **Developer-Friendly**: 84%+ test coverage with mock generation and modern tooling
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

```bash
# Edit the configuration file
cp config.yaml config.yaml.local
# Configure your settings in config.yaml.local
task run-with-config CONFIG=./config.yaml.local
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
  retain: false

# PVOutput.org Integration
pvoutput:
  enabled: false
  api_key: "your-api-key"
  system_id: "your-system-id"
  update_limit_minutes: 5
```

## 🔧 Development

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
│   ├── protocol/      # Protocol command handling
│   ├── pubsub/        # MQTT publishing
│   ├── service/       # Business logic services
│   ├── session/       # Connection session management
│   └── validation/    # Data validation
├── layouts/           # JSON parsing layouts
├── test/             # End-to-end tests
├── mocks/            # Generated test mocks
└── coverage/         # Coverage reports
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
- **Testability**: Comprehensive mocking and 84%+ test coverage
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

### Test Coverage by Package
- **Overall**: 84%+ (excluding generated mocks)
- **internal/api**: 98.2%
- **internal/domain**: 100.0%  
- **internal/pubsub**: 100.0%
- **internal/service**: 92.6%
- **internal/config**: 91.5%
- **internal/parser**: 85.4%

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

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Original [Grott project](https://github.com/johanmeijer/grott) by Johan Meijer
- Growatt inverter community for protocol documentation
- Contributors and testers who helped improve this implementation

## 📚 Additional Resources

- [Growatt Protocol Documentation](./docs/protocol.md)
- [Configuration Examples](./docs/configuration.md)  
- [Deployment Guide](./docs/deployment.md)
- [Troubleshooting Guide](./docs/troubleshooting.md)
