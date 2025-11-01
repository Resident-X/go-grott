# go-grott

A modern Go implementation of Grott - monitors Growatt solar inverters and publishes data to MQTT, PVOutput.org, and HTTP API.

## What It Does

- Receives data from Growatt inverters via TCP
- Decrypts and parses inverter telemetry
- Publishes to MQTT broker (with smart meter support)
- Uploads to PVOutput.org (generation + consumption data)
- Provides REST API for monitoring
- Automatically reconnects if MQTT/network fails

## Quick Start

### Option 1: Pre-built Binary (Recommended)

Download the latest release with SLSA Level 3 provenance:

```bash
# 1. Download binary for your platform
curl -LO https://github.com/Resident-X/go-grott/releases/latest/download/go-grott-linux-amd64

# 2. Download provenance attestation
curl -LO https://github.com/Resident-X/go-grott/releases/latest/download/go-grott-linux-amd64.intoto.jsonl

# 3. Verify SLSA provenance (optional but recommended)
# Install slsa-verifier: https://github.com/slsa-framework/slsa-verifier
slsa-verifier verify-artifact \
  --provenance-path go-grott-linux-amd64.intoto.jsonl \
  --source-uri github.com/Resident-X/go-grott \
  go-grott-linux-amd64

# 4. Make executable and run
chmod +x go-grott-linux-amd64
./go-grott-linux-amd64 -config config.yaml
```

**Available platforms:**
- `go-grott-linux-amd64` - Linux x86_64
- `go-grott-linux-arm64` - Linux ARM64
- `go-grott-darwin-amd64` - macOS Intel
- `go-grott-darwin-arm64` - macOS Apple Silicon
- `go-grott-windows-amd64.exe` - Windows x86_64

### Option 2: Go Install (Quick, requires Go)

If you have Go 1.24+ installed:

```bash
# Install directly to $GOPATH/bin
go install github.com/Resident-X/go-grott/cmd@latest

# Or pin to specific version
go install github.com/Resident-X/go-grott/cmd@v0.5.1

# Binary will be in $GOPATH/bin (usually ~/go/bin)
~/go/bin/cmd -config config.yaml
```

**Note:** This builds from source locally (no SLSA verification). Use Option 1 for production deployments.

### Option 3: Build from Source

```bash
# 1. Install Go 1.24+ and Task runner
go install github.com/go-task/task/v3/cmd/task@latest

# 2. Clone and build
git clone https://github.com/Resident-X/go-grott.git
cd go-grott
task build

# 3. Configure
cp config.yaml config.yaml.local
nano config.yaml  # Edit your settings

# 4. Run
task run
```

## Requirements

### Runtime (Pre-built Binary)
- **MQTT Broker** (optional) - e.g., Mosquitto
- **Growatt Inverter** configured to send data to your server IP:5279

### Go Install or Building from Source
- **Go 1.24+**
- [Task](https://taskfile.dev/) - Task runner (recommended)
- [Mockery](https://vektra.github.io/mockery/) - Mock generator (for development)

```bash
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/vektra/mockery/v2@latest
```

### SLSA Verification (Optional)
- [slsa-verifier](https://github.com/slsa-framework/slsa-verifier#installation) - Verify build provenance

```bash
go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@latest
```

## Which Installation Method?

| Method | Best For | Pros | Cons |
|--------|----------|------|------|
| **Pre-built Binary** | Production deployments | ✅ SLSA L3 verified<br>✅ No Go required<br>✅ Fast | Requires manual download |
| **Go Install** | Developers, quick testing | ✅ One command<br>✅ Latest version | ❌ No SLSA verification<br>❌ Requires Go |
| **Build from Source** | Development, customization | ✅ Full control<br>✅ Latest commits | ❌ Requires Go + Task<br>❌ Slower |

## Features

### Core
- ✅ TCP server (default port 5279)
- ✅ Automatic protocol detection and decryption
- ✅ Layout-driven parsing (supports multiple inverter models)
- ✅ Device registry (tracks dataloggers and inverters)

### MQTT
- ✅ Publish inverter data as JSON
- ✅ Publish smart meter data separately
- ✅ Auto-reconnect with exponential backoff
- ✅ Continuous self-healing (works after long outages)
- ✅ Thread-safe concurrent access

### PVOutput.org
- ✅ Upload generation data (v1/v2 parameters)
- ✅ Upload consumption data (v3/v4 dual-post for smart meters)
- ✅ Support multiple inverters with different system IDs
- ✅ Rate limiting (configurable update interval)

### Monitoring
- ✅ HTTP REST API
- ✅ Health check endpoint
- ✅ List connected devices
- ✅ Structured JSON logging

## Configuration

Edit `config.yaml` to configure:

```yaml
# Logging
log_level: info  # debug, info, warn, error

# TCP Server (receives inverter data)
server:
  host: 0.0.0.0
  port: 5279

# HTTP API (monitoring)
api:
  enabled: true
  host: 0.0.0.0
  port: 8080

# MQTT Publishing
mqtt:
  enabled: true
  host: localhost
  port: 1883
  username: ""
  password: ""
  topic: energy/growatt
  smart_meter_topic: energy/meter
  retain: false

# PVOutput.org
pvoutput:
  enabled: false
  api_key: your_api_key_here
  system_id: your_system_id_here
  update_limit_minutes: 5
```

### Smart Meter Configuration

If you have a smart meter connected to your inverter:

```yaml
mqtt:
  smart_meter_topic: energy/meter
  use_smart_meter_topic: true  # Publish to separate topic

pvoutput:
  enabled: true
  # Consumption data (v3/v4) automatically sent when available
```

### Multiple Inverters

```yaml
pvoutput:
  enabled: true
  api_key: your_api_key
  multiple_inverters: true
  inverter_mappings:
    - inverter_serial: "ABC123456"
      system_id: "12345"
    - inverter_serial: "XYZ789012"
      system_id: "67890"
```

## Usage

### Running

```bash
# Build and run
task run

# Run with custom config
task run-with-config CONFIG=./custom.yaml

# Build only
task build

# Development mode (auto-rebuild)
task dev
```

### Command Line

```bash
./build/go-grott -config config.yaml
./build/go-grott -version
```

### HTTP API Endpoints

```bash
# Health check
curl http://localhost:8080/api/v1/status

# List dataloggers
curl http://localhost:8080/api/v1/dataloggers

# List inverters for a datalogger
curl http://localhost:8080/api/v1/dataloggers/{serial}/inverters
```

## Development

### Tests

```bash
# Run all tests
task test

# With coverage report
task test-coverage

# Specific package
go test ./internal/parser/...
```

**Coverage:** 84%+ across all packages

### Code Quality

```bash
# All checks (format, vet, test)
task check

# Individual checks
task fmt   # Format code
task vet   # Static analysis
task lint  # golangci-lint
```

### Mocks

```bash
# Generate mocks for all interfaces
task mocks

# Clean and regenerate
task mocks-clean
```

### Common Tasks

```bash
task --list              # Show all available tasks
task deps                # Download dependencies
task deps-update         # Update dependencies
task clean               # Clean build artifacts
```

## Project Structure

```
go-grott/
├── cmd/                    # Entry point (main.go)
├── internal/
│   ├── api/                # HTTP API server
│   ├── config/             # Configuration loading
│   ├── domain/             # Core interfaces & models
│   ├── parser/             # Protocol parser
│   │   └── layouts/        # JSON layout definitions
│   ├── pubsub/             # MQTT publisher
│   ├── service/            # Business logic
│   │   └── pvoutput/       # PVOutput.org client
│   ├── session/            # Session management
│   └── validation/         # Data validation
├── mocks/                  # Generated mocks (mockery)
├── test/                   # Integration tests
├── build/                  # Compiled binaries
├── coverage/               # Test reports
├── config.yaml             # Main configuration
├── Taskfile.yml            # Task definitions
└── .mockery.yaml           # Mock generation config
```

## How It Works

1. **Datalogger connects** → TCP server accepts connection (port 5279)
2. **Data received** → Parser decrypts and extracts fields using JSON layouts
3. **Data validated** → Checks protocol headers and data integrity
4. **Publishing:**
   - **MQTT:** JSON message to broker (inverter + smart meter topics)
   - **PVOutput:** HTTP POST with generation (v1/v2) and consumption (v3/v4)
5. **API:** Exposes device registry and status via HTTP

### Data Flow

```
Inverter → TCP:5279 → Parser → {MQTT, PVOutput, Registry}
                                   ↓       ↓         ↓
                              Broker  Cloud API   HTTP API
```

## Architecture Highlights

- **Clean Architecture** - Separation of concerns, interface-driven
- **Dependency Injection** - All components use constructor injection
- **Context Propagation** - Proper cancellation and timeouts
- **Graceful Shutdown** - Clean resource cleanup
- **Self-Healing MQTT** - Continuous reconnection on failure
- **Structured Logging** - zerolog with component tagging

## Security & Supply Chain

### SLSA Level 3 Compliance

All releases are built with [SLSA Level 3](https://slsa.dev/spec/v1.0/levels) provenance using the official [SLSA Go builder](https://github.com/slsa-framework/slsa-github-generator). This provides:

- **Build Integrity** - Verifiable that binaries match source code
- **Non-forgeable Provenance** - Cryptographically signed attestations
- **Build Isolation** - Compiled on GitHub-hosted runners

### Verifying Downloads

Always verify binaries before running in production:

```bash
# Download binary and provenance
curl -LO https://github.com/Resident-X/go-grott/releases/download/v0.5.1/go-grott-linux-amd64
curl -LO https://github.com/Resident-X/go-grott/releases/download/v0.5.1/go-grott-linux-amd64.intoto.jsonl

# Verify provenance
slsa-verifier verify-artifact \
  --provenance-path go-grott-linux-amd64.intoto.jsonl \
  --source-uri github.com/Resident-X/go-grott \
  --source-tag v0.5.1 \
  go-grott-linux-amd64

# Expected output:
# Verified signature against tlog entry index <number> at URL: https://rekor.sigstore.dev/api/v1/log/entries/...
# Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@refs/tags/v2.1.0" at commit <sha>
# PASSED: Verified SLSA provenance
```

If verification fails, **do not run the binary** and report it as a security issue.

### What SLSA Verification Checks

- ✅ Binary was built by official GitHub Actions workflow
- ✅ Source code matches the specified commit/tag
- ✅ Build process was isolated and auditable
- ✅ Provenance signature is valid (signed by Sigstore)
- ✅ No tampering occurred after build

## Troubleshooting

### MQTT not connecting

Check logs for connection attempts:
```bash
# Watch logs
./build/go-grott -config config.yaml | grep mqtt
```

The system will automatically retry every 10 seconds. Check:
- MQTT broker is running: `sudo systemctl status mosquitto`
- Firewall allows port 1883
- Credentials are correct in config.yaml

### No data from inverter

- Verify inverter is configured to send to your server IP:5279
- Check firewall allows TCP port 5279
- Enable debug logging: `log_level: debug` in config.yaml

### PVOutput upload fails

- Verify API key and system ID
- Check rate limit (default: 5 minutes between updates)
- Enable debug logs to see request details

## License

[Add your license here]

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Run tests (`task test`)
4. Commit changes (`git commit -m 'Add amazing feature'`)
5. Push to branch (`git push origin feature/amazing-feature`)
6. Open a Pull Request

### Code Standards

- Run `task check` before committing
- Add tests for new features
- Update documentation as needed
- Follow existing code style

## Acknowledgments

- Original [Grott](https://github.com/johanmeijer/grott) project by Johan Meijer
- Go community for excellent tooling
