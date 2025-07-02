# go-grott: Growatt Inverter Monitor in Go

This is a Go port of the Grott (Growatt Inverter Monitor) functionality, focusing on the server component that receives data from Growatt inverters.

## Quick Start

1. **Install dependencies**:
   ```bash
   # Install Task runner (optional but recommended)
   go install github.com/go-task/task/v3/cmd/task@latest
   
   # Install Mockery (for development)
   go install github.com/vektra/mockery/v2@latest
   ```

2. **Clone and build**:
   ```bash
   git clone <repository-url>
   cd go-grott
   task build
   ```

3. **Configure**:
   ```bash
   cp config.yaml.example config.yaml
   # Edit config.yaml to match your setup
   ```

4. **Run**:
   ```bash
   task run
   ```

5. **Test** (optional):
   ```bash
   task test
   task coverage
   ```

## Features

- TCP server that receives data from Growatt inverters
- Parsing and decoding of Growatt data packets using layout-driven extraction
- MQTT publishing of inverter data
- PVOutput.org integration
- HTTP API for monitoring and management
- Clean architecture with proper interface-based design
- Comprehensive test suite with 84%+ coverage
- Mock generation and dependency injection for testability
- Modern Go development tooling with Task runner

## Requirements

- Go 1.24 or later
- [Task runner](https://taskfile.dev/) (recommended for development)
- [Mockery](https://vektra.github.io/mockery/) (for generating mocks during development)
- MQTT broker (optional, for MQTT publishing)

## Configuration

The configuration is stored in a YAML file (`config.yaml`). All settings are well-documented with comments in the file itself.

Key configuration sections:

- **General Settings**: Control logging verbosity, decryption, and inverter type
- **Server Settings**: Configure the TCP server that receives data from inverters
- **API Settings**: Configure the HTTP API server for monitoring and management
- **MQTT Settings**: Configure connection to an MQTT broker for publishing data
- **PVOutput Settings**: Configure integration with PVOutput.org
- **Layout Settings**: Configure record layout mappings for different inverter models

## Usage

### Building and Running

Using Task (recommended):
```bash
# Build the application
task build

# Run with default config
task run

# Run with custom config file
task run-with-config CONFIG=./custom-config.yaml

# Development mode (builds and runs)
task dev
```

### Development Commands

```bash
# Run all tests
task test

# Run tests with coverage analysis
task coverage

# Generate HTML coverage report
task coverage-html

# Run code quality checks
task check          # Runs fmt, vet, and test
task fmt            # Format code
task vet            # Run go vet
task lint           # Run golangci-lint

# Generate mocks (for development)
task mocks          # Generate mocks
task mocks-clean    # Clean and regenerate all mocks

# Dependency management
task deps           # Download dependencies
task deps-update    # Update dependencies
```

Manual build and run:
```bash
go build -o go-grott ./cmd
./go-grott -config=config.yaml
```

### Command-line Arguments

- `-config`: Path to configuration file (default: `config.yaml`)
- `-version`: Show version information

## HTTP API

The HTTP API provides endpoints to:

- Check server status (`/api/v1/status`)
- List connected dataloggers (`/api/v1/dataloggers`)
- List inverters for a specific datalogger (`/api/v1/dataloggers/{id}/inverters`)

## How It Works

1. The DataCollectionServer listens for TCP connections from Growatt dataloggers
2. When data is received, it is decrypted and parsed using the appropriate layout
3. The parsed data is published to MQTT and optionally to PVOutput.org
4. The APIServer provides HTTP endpoints to monitor the server status and connected devices

## Architecture

- **Clean Architecture**: The application follows clean architecture principles with a clear separation of concerns
- **Interface-Driven Design**: Components interact through interfaces, making the code flexible and testable
- **Layout-Driven Parsing**: All field extraction is driven by JSON layout files, no hardcoded parsing logic
- **Dependency Injection**: Dependencies are injected into components, improving testability and flexibility
- **Context Usage**: The application properly uses Go contexts for cancellation and timeouts
- **Structured Logging**: Utilizes structured logging with zerolog for better observability
- **Graceful Shutdown**: All components handle graceful shutdowns with proper resource cleanup
- **Comprehensive Testing**: 84%+ test coverage with mockery-generated mocks for all interfaces

The application consists of two primary servers:
1. **DataCollectionServer**: Handles TCP connections from inverters, processes data, and publishes to configured endpoints
2. **APIServer**: Provides HTTP API endpoints for monitoring and management

## Project Structure

```
go-grott/
├── cmd/                # Application entry point (main.go)
├── internal/
│   ├── api/            # HTTP API implementation (APIServer)
│   ├── config/         # Configuration handling
│   ├── domain/         # Domain models and interfaces
│   ├── parser/         # Protocol parser for Growatt data
│   ├── pubsub/         # MQTT publisher implementation
│   └── service/        # Core services
│       ├── pvoutput/   # PVOutput.org integration
│       └── server.go   # DataCollectionServer implementation
├── layouts/            # Record layout definitions (JSON)
├── mocks/              # Generated mocks for testing (mockery)
├── coverage/           # Test coverage reports (generated)
├── build/              # Compiled binaries (generated)
├── Taskfile.yml        # Task runner configuration
├── .mockery.yaml       # Mockery configuration
└── config.yaml         # Configuration file
```

## Quick Start

1. **Install dependencies**:
   ```bash
   # Install Task runner (optional but recommended)
   go install github.com/go-task/task/v3/cmd/task@latest
   
   # Install Mockery (for development)
   go install github.com/vektra/mockery/v2@latest
   ```

2. **Clone and build**:
   ```bash
   git clone <repository-url>
   cd go-grott
   task build
   ```

3. **Configure**:
   ```bash
   cp config.yaml.example config.yaml
   # Edit config.yaml to match your setup
   ```

4. **Run**:
   ```bash
   task run
   ```

5. **Test** (optional):
   ```bash
   task test
   task coverage
   ```

## Testing and Development

### Test Coverage

The project maintains high test coverage across all packages:

- **Overall Coverage**: 84%+ (excluding generated mocks)
- **internal/api**: 98.2%
- **internal/domain**: 100.0%
- **internal/pubsub**: 100.0%
- **internal/service**: 92.6%
- **internal/service/pvoutput**: 92.1%
- **internal/config**: 91.5%
- **internal/parser**: 85.4%

### Running Tests

```bash
# Run all tests
task test

# Run tests with coverage
task coverage

# Generate HTML coverage report
task coverage-html

# Run specific package tests
task test-parser    # Parser tests only
```

### Mock Generation

The project uses [Mockery](https://vektra.github.io/mockery/) to generate mocks for all interfaces:

```bash
# Generate mocks
task mocks

# Clean and regenerate all mocks
task mocks-clean
```

Mocks are automatically generated for:
- Domain interfaces (DataParser, MessagePublisher, etc.)
- External dependencies (MQTT Client, net.Listener, net.Conn)

### Code Quality

```bash
# Run all quality checks
task check

# Individual checks
task fmt     # Format code
task vet     # Run go vet
task lint    # Run golangci-lint
```

## Design Patterns Used

- **Repository Pattern**: For device registry management
- **Strategy Pattern**: For different data parsing strategies
- **Null Object Pattern**: For handling disabled features (e.g., NoopPublisher, NoopClient)
- **Dependency Injection**: For providing dependencies to components
- **Facade Pattern**: The server acts as a facade, coordinating all components

## Future Improvements

- Additional monitoring service integrations
- Support for more inverter models and data formats
- Advanced data analytics and visualization
- Performance optimizations and benchmarking
- Container deployment with Docker/Kubernetes
- Distributed deployment capabilities

## Configuration Examples

### Basic Configuration (TCP Server Only)
```yaml
log_level: info  # debug, info, warn, error, fatal
decrypt: true
inverter_type: default
server:
  host: 0.0.0.0
  port: 5279
api:
  enabled: false
mqtt:
  enabled: false
pvoutput:
  enabled: false
```

### MQTT Integration
```yaml
# ... other settings ...
mqtt:
  enabled: true
  host: mqtt.example.com
  port: 1883
  username: "myuser"
  password: "mypassword"
  topic: home/solar/inverter
  include_inverter_id: true
  retain: true
```

### PVOutput.org Integration
```yaml
# ... other settings ...
pvoutput:
  enabled: true
  api_key: "your-api-key"
  system_id: "12345"
  update_limit_minutes: 5
  use_inverter_temp: true
```

### Multiple Inverters with Different PVOutput Systems
```yaml
# ... other settings ...
pvoutput:
  enabled: true
  api_key: "your-api-key"
  system_id: "default-system-id"
  multiple_inverters: true
  inverter_mappings:
    - inverter_serial: "NLD123456"
      system_id: "12345"
    - inverter_serial: "NLD789012"
      system_id: "67890"
```

## Testing and Development

### Test Coverage

The project maintains high test coverage across all packages:

- **Overall Coverage**: 84%+ (excluding generated mocks)
- **internal/api**: 98.2%
- **internal/domain**: 100.0%
- **internal/pubsub**: 100.0%
- **internal/service**: 92.6%
- **internal/service/pvoutput**: 92.1%
- **internal/config**: 91.5%
- **internal/parser**: 85.4%

### Running Tests

```bash
# Run all tests
task test

# Run tests with coverage
task coverage

# Generate HTML coverage report
task coverage-html

# Run specific package tests
task test-parser    # Parser tests only
```

### Mock Generation

The project uses [Mockery](https://vektra.github.io/mockery/) to generate mocks for all interfaces:

```bash
# Generate mocks
task mocks

# Clean and regenerate all mocks
task mocks-clean
```

Mocks are automatically generated for:
- Domain interfaces (DataParser, MessagePublisher, etc.)
- External dependencies (MQTT Client, net.Listener, net.Conn)

### Code Quality

```bash
# Run all quality checks
task check

# Individual checks
task fmt     # Format code
task vet     # Run go vet
task lint    # Run golangci-lint
```
