# go-grott: Growatt Inverter Monitor

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Build Status](https://img.shields.io/badge/Build-Passing-green)](https://github.com/resident-x/go-grott)
[![License](https://img.shields.io/badge/License-Unlicense-blue.svg)](LICENSE)
[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)

Modern Go implementation of Grott - receives, processes, and distributes data from Growatt solar inverters.

## Why go-grott?

- **High-performance** concurrent TCP server for multiple inverters
- **Home Assistant auto-discovery** - sensors appear automatically with proper device classes
- **Multiple outputs** - MQTT, PVOutput.org, HTTP API
- **Startup data filtering** - prevents erroneous readings during inverter boot
- **Layout-driven parsing** - JSON configuration for easy model support

## Installation

### Recommended: Release Binary (SLSA L3 Verified)

Release binaries are built with [SLSA Level 3](https://slsa.dev) compliance - cryptographic proof the binary was built from this repository's source code using a trusted build process.

**Download:**
```bash
# Linux AMD64
wget https://github.com/resident-x/go-grott/releases/latest/download/go-grott-linux-amd64
wget https://github.com/resident-x/go-grott/releases/latest/download/go-grott-linux-amd64.intoto.jsonl

# Other platforms available: linux-arm64, darwin-amd64, darwin-arm64, windows-amd64.exe
```

**Verify (recommended):**
```bash
# Install slsa-verifier
go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@latest

# Verify binary authenticity
slsa-verifier verify-artifact \
  --provenance-path go-grott-linux-amd64.intoto.jsonl \
  --source-uri github.com/resident-x/go-grott \
  go-grott-linux-amd64

# Expected output: "Verified SLSA provenance"
```

**Run:**
```bash
chmod +x go-grott-linux-amd64
./go-grott-linux-amd64 -config config.yaml
```

### Alternative: Build from Source

**Using Task (recommended for development):**
```bash
go install github.com/go-task/task/v3/cmd/task@latest
git clone https://github.com/resident-x/go-grott.git
cd go-grott
task build
./go-grott
```

**Using go install:**
```bash
go install github.com/resident-x/go-grott@latest
```

## Quick Start

**1. Create `config.yaml`:**
```yaml
server:
  host: 0.0.0.0
  port: 5279

mqtt:
  enabled: true
  host: localhost
  port: 1883
  topic: energy/growatt

  homeassistant_autodiscovery:
    enabled: true
    discovery_prefix: homeassistant
    device_name: "Growatt Inverter"
```

**2. Configure your inverter's data logger:**
- Access ShineWiFi-X configuration
- Point server address to this machine's IP
- Port: 5279

**3. Run go-grott:**
```bash
./go-grott -config config.yaml
```

Data now flows to MQTT/Home Assistant automatically.

## Configuration

### Server

```yaml
server:
  host: 0.0.0.0      # Bind to all network interfaces
  port: 5279         # Growatt protocol port

api:
  enabled: true      # HTTP API for monitoring
  host: 0.0.0.0
  port: 8080
```

### MQTT & Home Assistant

```yaml
mqtt:
  enabled: true
  host: localhost
  port: 1883
  topic: energy/growatt
  retain: false

  # Filter spurious readings during inverter boot/reset
  startup_data_filter:
    enabled: true
    grace_period_seconds: 10           # Drop first N seconds of data
    data_gap_threshold_hours: 1        # Detect restarts via data gaps

  # Home Assistant auto-discovery
  homeassistant_autodiscovery:
    enabled: true
    discovery_prefix: homeassistant
    device_name: "Growatt Inverter"
    device_manufacturer: "Growatt"
    device_model: ""                   # Auto-detected from serial
    listen_to_birth_message: true      # Auto-rediscover on HA restart
    rediscovery_interval_hours: 24     # Periodic refresh
```

**Home Assistant features:**
- Sensors auto-discover with correct device classes (power, energy, voltage, etc.)
- Separate devices for inverter vs smart meter data
- Auto-reconnect on network disruptions
- Diagnostic sensors properly categorized

### PVOutput.org

```yaml
pvoutput:
  enabled: false
  api_key: "your-api-key"
  system_id: "your-system-id"
  update_limit_minutes: 5
```

## Key Concepts

### Startup Data Filtering

**Problem:** Inverters report invalid values (e.g., 32775 kWh) during boot due to memory initialization.

**Solution:**
- Filters data for first N seconds after startup
- Auto-detects restarts via data gaps
- Prevents false spikes in Home Assistant dashboards

### Layout-Driven Parsing

Field extraction defined in JSON (`internal/parser/layouts/*.json`):
- No hardcoded parsing logic
- Easy to add new inverter models
- Based on Growatt's Modbus register mappings

### Data Flow

```
Growatt Inverter → ShineWiFi-X → go-grott (port 5279) → MQTT/PVOutput/API
```

## Development

**Common tasks:**
```bash
task test              # Run all tests
task test-coverage     # With coverage report
task check             # Format, vet, test
task build             # Standard build
task build-release     # Optimized binary
```

**Project structure:**
```
internal/
├── api/               # HTTP API
├── config/            # Configuration
├── parser/            # Protocol parsing
│   └── layouts/       # Inverter model definitions (JSON)
├── protocol/          # TCP server
├── pubsub/            # MQTT publisher
└── service/           # PVOutput integration
```

**Adding new inverter model:**
1. Capture raw data (enable debug logging)
2. Create JSON layout in `internal/parser/layouts/`
3. Map field positions from Growatt register docs
4. Test with your inverter
5. Submit PR

## Troubleshooting

**Inverter not connecting:**
- Check firewall allows port 5279
- Verify data logger IP configuration
- Check logs: `./go-grott -config config.yaml`

**MQTT issues:**
- Verify broker accessibility
- Check credentials if using authentication
- App continues running if MQTT unavailable (non-blocking startup)

**Home Assistant sensors missing:**
- Enable `homeassistant_autodiscovery.enabled: true`
- Verify discovery prefix matches HA config
- Wait for first data packet or restart HA

**High/invalid energy readings:**
- Enable `startup_data_filter`
- Increase `grace_period_seconds` if needed

## HTTP API

Access at `http://localhost:8080`

**Main endpoints:**
- `GET /api/v1/status` - Server status
- `GET /api/v1/dataloggers` - Connected devices
- `GET /api/v1/dataloggers/{id}/inverters` - Inverter list

**Register access:**
- `GET/PUT /datalogger?serial=...&register=...&format=dec`
- `GET/PUT /inverter?serial=...&invserial=...&register=...`
- `GET/PUT /multiregister?serial=...&registers=1,2,3&values=...`

Formats: `dec` (decimal), `hex` (hexadecimal), `text`

## Contributing

Contributions welcome! Please:
- Add tests for new features
- Run `task check` before submitting
- Follow existing code style
- Update documentation

## License

[Unlicense](LICENSE) - public domain

## Acknowledgments

- Original [Grott](https://github.com/johanmeijer/grott) by Johan Meijer
- Growatt community for protocol documentation
