# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a **LoRaWAN device and gateway simulator** written in Go, designed to test ChirpStack Application Server and Network Server functionality. It simulates OTAA device activation, uplink/downlink data transmission, and advanced features like FUOTA (Firmware Over-The-Air), BACnet, and Modbus protocol integration.

**Key capabilities:**
- Simulate hundreds of LoRaWAN devices with OTAA activation
- Multi-device type support with per-device configurations
- Gateway simulation via MQTT
- JavaScript-based payload encoding/decoding (Goja engine)
- Prometheus metrics for monitoring
- Support for EU868, CN470, AS923, AU915, IN865, RU864, KR920 frequency plans

## Build and Run Commands

```bash
# Build the simulator
make build
# Creates binary at: cmd/lora-simulator/lora-simulator

# Run the simulator (must be in cmd/lora-simulator directory)
cd cmd/lora-simulator
./lora-simulator -c config/lora-simulator.toml

# Clean build artifacts
make clean

# Build with custom flags (example)
GO_EXTRA_BUILD_ARGS="-v" make build
```

**Important:** The simulator expects to be run from the `cmd/lora-simulator/` directory due to relative paths to configuration files, JavaScript codecs, and test data.

## Configuration

The simulator uses **TOML** configuration files. Main config: `cmd/lora-simulator/config/lora-simulator.toml`

**Configuration hierarchy:**
1. `lora-simulator.toml` - Main configuration (server URLs, credentials, test features)
2. `simulation-config.json` - Multi-device type simulation configuration (NEW)
3. `devices.json` - Device type definitions with codec references (NEW)
4. `devices_dynamic.json` - FUOTA-specific device configuration (optional)
5. `base_devices_export.csv` - Legacy device import format (being phased out)

**Key configuration sections:**
- `[general]` - Log level and channel plan (frequency)
- `[lora-simulator.api]` - ChirpStack AS credentials, test features (`normal,bacnet,fuota,modbus`)
- `[[simulator]]` - Array of simulator instances (multi-tenant support)
- `[simulator.device]` - Device count, uplink interval, payload
- `[simulator.gateway]` - Gateway count range (min/max)
- `[prometheus]` - Metrics endpoint binding

**TLS/HTTPS behavior:**
- `insecure = true`: Uses HTTP (no TLS)
- `insecure = false`: Uses HTTPS with certificate verification skipped

## Architecture Overview

### Startup Flow (Task-Based Pipeline)

Entry point: `cmd/lora-simulator/main.go` → `cmd/root.go` → `cmd/root_run.go`

The simulator executes a **sequential task pipeline** in `root_run.go`:
1. `setLogLevel` - Configure logging
2. `printStartMessage` - Display startup info
3. `setupASAPIClient` - Authenticate with ChirpStack AS (JWT or CGI login)
4. `setupASIntegration` - Create/verify AS resources (applications, gateways, profiles)
5. `setupNSIntegration` - Connect MQTT client to Network Server
6. `setupPrometheus` - Start metrics endpoint (`:9000/metrics`)
7. `startPayloadCodecTest` - Optional codec testing mode
8. `startSimulator` - Launch simulation goroutines

Each task can abort the pipeline on failure.

### Core Components

```
Simulator (orchestrator)
  ├── Gateway instances (3-5 per simulation)
  │   ├── MQTT client (subscribe to gateway/{id}/tx)
  │   ├── Device registry (map[DevEUI]chan DownlinkFrame)
  │   └── Uplink forwarding (publish to gateway/{id}/rx)
  │
  └── Device instances (1-N per simulation)
      ├── uplinkLoop() goroutine - Periodic data transmission
      ├── downlinkLoop() goroutine - Handle AS/NS responses
      ├── Session keys (AppSKey, NwkSKey)
      └── Frame counters (fCntUp, fCntDown)
```

**Key files:**
- `internal/simulator/simulator.go` (1,253 lines) - Orchestrates initialization and simulation
- `internal/device/device.go` - OTAA activation, uplink/downlink handling
- `internal/gateway/gateway.go` - MQTT-based gateway simulation
- `internal/as/api_client.go` - ChirpStack AS REST API wrapper
- `internal/ns/mqtt.go` - MQTT client for Network Server integration

### Multi-Device Type Architecture (Recent Addition)

The codebase recently migrated from single-device to **multi-device type support**:

**New pattern:**
- `internal/deviceconfig/` package manages device types
- `DeviceTypeRegistry` - Lookup device types by ID or name
- `DeviceConfigLoader` - Loads `devices.json` + `simulation-config.json`
- Each device instance has its own: test data, encoder script, uplink interval, FPort

**Configuration files:**
- `payload_en_decoder/codec-release/vendors/milesight-iot/devices.json` - Device type catalog
- `config/simulation-config.json` - Specifies which device types to simulate and quantities
- `vendors/milesight-iot/{device}/device-test-data.json` - Per-device test data

**Device creation options (Option pattern):**
```go
device.WithDevEUI(eui)
device.WithAppKey(key)
device.WithPayloadCodec(codec)
device.WithDeviceTypeConfig(typeConfig)  // NEW: per-device type config
device.WithDeviceTestData(testData)      // NEW: device-specific test data
```

**Legacy pattern (still supported):**
- Global payload (`simulator.device.payload` in TOML)
- All devices use same encoder/decoder
- Test data from `payload_en_decoder/test-data.json`

## Important Architectural Patterns

### 1. Goroutine-Based Concurrency

Each device runs **two concurrent goroutines**:
- `uplinkLoop()` - Sleeps for `uplinkInterval`, then sends data
- `downlinkLoop()` - Blocks on `downlinkFrames` channel, processes AS responses

**Synchronization primitives:**
- `context.Context` - Cancellation signal (shutdown)
- `sync.WaitGroup` - Wait for all goroutines to finish
- Channels - Pass downlink frames from gateway to device
- `sync.RWMutex` - Protect device state (frame counters, session keys)

**Critical:** Always check `ctx.Done()` in loops to support graceful shutdown.

### 2. OTAA Activation Flow

```
Device                    Gateway               MQTT Broker           ChirpStack NS
  |                          |                        |                      |
  |--JoinRequest------------>|                        |                      |
  |  (encrypted w/ AppKey)   |                        |                      |
  |                          |--Publish-------------->|                      |
  |                          |  gateway/{id}/rx       |                      |
  |                          |                        |---Forward----------->|
  |                          |                        |                      |
  |                          |                        |<--JoinAccept---------|
  |                          |<-Publish---------------|                      |
  |                          |  gateway/{id}/tx       |                      |
  |<-Channel-----------------|                        |                      |
  |                          |                        |                      |
  |--Derive session keys--   |                        |                      |
  |  (AppSKey, NwkSKey)      |                        |                      |
  |--setState(Activated)--   |                        |                      |
```

**Device states:**
- `deviceStateOTAA` - Waiting for JoinAccept
- `deviceStateActivated` - Session keys established, can send data

### 3. Payload Encoding/Decoding

**JavaScript execution via Goja** (replaced Otto):
- Encoder: `function Encode(fPort, obj) { ... }` → returns `{ bytes: [...] }`
- Decoder: `function Decode(fPort, bytes) { ... }` → returns object

**Codec location:**
- Device-specific: `payload_en_decoder/codec-release/vendors/{vendor}/{device}/{device}-encoder.js`
- Registered in `devices.json` → `encoder_script` field

**Test data injection:**
- `device.getEncoderData()` retrieves test data from device-specific JSON or global `test-data.json`
- Data passed to JavaScript encoder: `Encode(fPort, testData)`

### 4. API Client Architecture

**ChirpStack AS Client** (`internal/as/api_client.go`):
- Uses **go-swagger generated client** (`internal/as_api/client/`)
- Two authentication modes:
  - JWT: `ASLogin()` → returns JWT token
  - CGI Cookie: `CGILogin()` → returns session cookie
- All API calls use authenticated transport

**MQTT Client** (`internal/ns/mqtt.go`):
- Singleton pattern: `ns.Client()` returns shared MQTT client
- Gateways use this client to subscribe to `gateway/{id}/tx` and publish to `gateway/{id}/rx`

## Test Features

Configure via `test_feature` in TOML (comma-separated):

**`normal`** - Basic device simulation
- Create gateways, devices, profiles
- OTAA activation
- Periodic uplink transmission

**`bacnet`** - BACnet protocol testing
- `setupBACnet()` in `simulator.go`
- Fetches available BACnet objects from AS
- Filters against test data
- Adds objects to BACnet server via AS API

**`modbus`** - Modbus protocol testing
- `setupModbus()` in `simulator.go`
- Creates Modbus server via AS API
- Maps Modbus registers to device data

**`fuota`** - Firmware Over-The-Air updates
- `setupFuota()` in `simulator.go`
- Waits for all devices to activate
- Creates FUOTA task with device groups
- Handles fragmentation (`internal/fragmentation/`)
- Handles multicast setup (`internal/multicastsetup/`)

## Key Configuration Options

**Sequential vs. Parallel Device Join:**
```toml
sequence_join = true               # Devices join in batches
sequence_join_interval = "2s"      # Interval between batches
sequence_device_number = 1         # Devices per batch
```
Use sequential join to avoid overwhelming the Network Server during large-scale simulations.

**Device Cleanup:**
```toml
clean_before_test = true           # Delete application/devices before starting
teardown_after_test = false        # Delete resources after simulation ends
use_new_device = true              # Create fresh devices vs. reuse existing
```

**Activation Time:**
```toml
activation_time = "100s"           # Time allocated for all devices to join
duration = "0s"                    # Total simulation duration (0 = infinite)
```

## Logging and Monitoring

**Logs:**
- Output file: `simulator.log` (created in working directory)
- Format: logrus with timestamps
- Log levels: `log_level = 4` (debug=5, info=4, warning=3, error=2)

**Prometheus Metrics** (`:9000/metrics`):
- `device_join_request_total` - Total join requests sent
- `device_join_accept_total` - Total successful joins
- `device_uplink_total` - Total uplinks sent by devices
- `gateway_uplink_total` - Total uplinks forwarded by gateways
- `gateway_downlink_total` - Total downlinks received

## API Client Generation

The AS API client is **auto-generated** from ChirpStack's Swagger spec:

```bash
# Regenerate API client (when ChirpStack API changes)
cd api_tools
./generate_api_client.sh
```

**Process:**
1. `fix_mixin_operation_id.py` - Fix duplicate operation IDs in swagger.yaml
2. `convert_swagger_to_camel_case.py` - Convert to camelCase naming
3. `go-swagger generate client` - Generate code in `internal/as_api/`

**Note:** Generated files (550+ files) should not be manually edited. Wrap generated API in `internal/as/api_client.go` instead.

## Common Development Patterns

### Adding a New Device Type

1. Create encoder/decoder JavaScript files:
   ```
   payload_en_decoder/codec-release/vendors/{vendor}/{device}/{device}-encoder.js
   payload_en_decoder/codec-release/vendors/{vendor}/{device}/{device}-decoder.js
   ```

2. Add device type to `devices.json`:
   ```json
   {
     "id": "new-device",
     "name": "New Device Name",
     "encoder_script": "path/to/encoder.js",
     "decoder_script": "path/to/decoder.js",
     "test_data": "path/to/test-data.json",
     "default_fport": 85,
     "simulation": {
       "uplink_interval": "300s",
       "frequency": 868100000
     }
   }
   ```

3. Add to `simulation-config.json`:
   ```json
   {
     "device_types": [
       {
         "device_type_id": "new-device",
         "count": 10,
         "enabled": true
       }
     ]
   }
   ```

### Adding a New Test Feature

1. Add configuration in `internal/config/config.go`
2. Implement `setup{Feature}()` in `internal/simulator/simulator.go`
3. Call in `init()` method based on `test_feature` config
4. Update TOML example with new feature name

## Important Notes

- **Working directory matters:** Always run from `cmd/lora-simulator/` directory
- **JavaScript engine:** Uses Goja (replaced Otto in recent commits)
- **Device keys:** AppKey is used for join, session keys (AppSKey/NwkSKey) derived from JoinAccept
- **Frame counters:** Must increment with each uplink/downlink to prevent replay attacks
- **MIC validation:** All LoRaWAN messages include Message Integrity Code validation
- **Gateway assignment:** Each device randomly selects 3-5 gateways to "hear" its transmissions
- **MQTT topics:** Use Go templates - `gateway/{{ .GatewayID }}/rx` and `gateway/{{ .GatewayID }}/tx`
- **Frequency plans:** Must match ChirpStack NS configuration (EU868, CN470, etc.)

## Troubleshooting

**Devices not joining:**
- Check `activation_time` is sufficient for device count
- Verify AppKey matches between simulator and ChirpStack AS
- Check MQTT broker connectivity
- Increase `sequence_join_interval` if NS is overwhelmed

**No uplinks after join:**
- Verify session keys were derived correctly (check logs for JoinAccept MIC validation)
- Check `uplink_interval` configuration
- Ensure device state is `deviceStateActivated`

**Codec errors:**
- Verify JavaScript encoder/decoder syntax (use test mode)
- Check test data JSON format matches encoder expectations
- Ensure `default_fport` matches codec implementation

**API authentication failures:**
- Verify `username` and `password` in TOML
- Ensure authentication credentials are correct
- Ensure `server` URL is accessible
- Check `insecure` TLS setting matches server configuration
