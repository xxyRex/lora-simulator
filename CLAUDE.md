# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

LoRaWAN device and gateway simulator written in Go, designed to test ChirpStack Application Server (AS) and Network Server (NS). Simulates OTAA device activation, uplink/downlink, and advanced features (FUOTA, BACnet, Modbus).

## Build and Run

```bash
# Build
make build
# Binary output: cmd/lora-simulator/lora-simulator

# Run (MUST be from cmd/lora-simulator/ due to relative paths for configs, JS codecs, test data)
cd cmd/lora-simulator
./lora-simulator -c config/lora-simulator.toml

make clean
```

## Tests

```bash
# Run codec tests (only test suite in the project)
go test ./internal/test_payload_codec/... -v
go test ./internal/test_payload_codec/... -v -run TestParseExcelSheet
```

## Configuration

Main config: `cmd/lora-simulator/config/lora-simulator.toml`

**Config file hierarchy** (all relative to `cmd/lora-simulator/`):
1. `config/lora-simulator.toml` - Server URLs, credentials, test features
2. `config/simulation-config.json` - Multi-device type simulation (device types + counts)
3. `payload_en_decoder/codec-release/vendors/milesight-iot/devices.json` - Device type catalog
4. `devices_dynamic.json` - FUOTA-specific device config (optional)
5. `api/fuota_req.json`, `api/modbus_server_create.json` - API request templates
6. `temp/device_stored_info.json` - Runtime device state persistence

**Key TOML sections:**
- `[general]` - `log_level` (5=debug..1=fatal), `channel_plan` (EU868/CN470/etc.)
- `[lora-simulator.api]` - ChirpStack AS: `server`, `username`, `password`, `insecure`, `test_feature`
- `[[simulator]]` - Array for multi-tenant; device count, uplink interval, gateway min/max
- `[prometheus]` - Metrics bind address (default `:9000`)
- `[lora-simulator.test_payload_codec]` - Codec comparison testing mode

**`test_feature`** (comma-separated): `normal`, `bacnet`, `fuota`, `modbus`

**`insecure`**: `true` = HTTP, `false` = HTTPS (certificate verification skipped)

**`api_test = true`**: Pure API testing mode, skips device simulation entirely

**Sequential join** (avoids overwhelming NS with large device counts):
```toml
sequence_join = true
sequence_join_interval = "2s"
sequence_device_number = 1
```

## Architecture

### Startup Pipeline (`cmd/root_run.go`)

Sequential tasks, each can abort on failure:
1. `setupASAPIClient` - JWT or CGI cookie auth with ChirpStack AS
2. `setupASIntegration` - Create/verify AS resources (apps, gateways, device profiles)
3. `setupNSIntegration` - MQTT client connect to NS
4. `setupPrometheus` - Start `:9000/metrics`
5. `startPayloadCodecTest` - Optional codec comparison mode
6. `startSimulator` - Launch simulation goroutines

### Core Components

```
Simulation (internal/simulator/simulator.go, ~1300 lines)
  ├── Gateway instances (min_count–max_count per simulation)
  │   ├── MQTT subscribe: gateway/{id}/tx  (downlink from NS)
  │   ├── MQTT publish:   gateway/{id}/rx  (uplink to NS)
  │   └── Device registry: map[DevEUI]chan DownlinkFrame
  └── Device instances
      ├── uplinkLoop() goroutine  - Periodic uplink transmission
      ├── downlinkLoop() goroutine - Process NS responses
      └── States: deviceStateOTAA → deviceStateActivated
```

**Key packages:**
- `internal/simulator/` - Orchestration, FUOTA/BACnet/Modbus setup
- `internal/device/` - OTAA activation, session key derivation, uplink/downlink
- `internal/gateway/` - MQTT-based gateway simulation
- `internal/as/api_client.go` - ChirpStack AS REST wrapper (wraps auto-generated swagger client)
- `internal/ns/mqtt.go` - Singleton MQTT client for NS
- `internal/deviceconfig/` - `DeviceTypeRegistry` + `DeviceConfigLoader` (loads devices.json + simulation-config.json)
- `internal/as_api/` - Auto-generated go-swagger client (do not manually edit)
- `internal/test_payload_codec/` - Codec comparison test runner (Excel-based)

### Multi-Device Type

`simulation-config.json` drives which device types to simulate:
```json
{
  "device_types": [
    { "device_id": "wt201", "count": 1, "uplink_paused": false, "uplink_confirm": false, "uplink_interval": 10000 }
  ]
}
```

Each device instance gets its own encoder JS, test data, FPort, and uplink interval from `devices.json`. Legacy mode: single global `payload` hex string in TOML.

### Payload Encoding (Goja JS engine)

- Encoder: `function Encode(fPort, obj)` → `{ bytes: [...] }`
- Decoder: `function Decode(fPort, bytes)` → object
- Scripts in: `payload_en_decoder/codec-release/vendors/{vendor}/{device}/{device}-encoder.js`
- Test data: `payload_en_decoder/codec-release/vendors/milesight-iot/{device}/device-test-data.json`

### OTAA Flow

Device → JoinRequest → Gateway → MQTT `gateway/{id}/rx` → ChirpStack NS → JoinAccept → MQTT `gateway/{id}/tx` → Device channel → derive AppSKey/NwkSKey → `deviceStateActivated`

### Concurrency

Each device has two goroutines (`uplinkLoop`, `downlinkLoop`). Shutdown via `context.Context`. Always check `ctx.Done()` in loops.

## Adding a New Device Type

1. Add encoder/decoder JS: `payload_en_decoder/codec-release/vendors/{vendor}/{device}/`
2. Add entry to `devices.json` with `id`, `encoder_script`, `decoder_script`, `test_data`, `default_fport`
3. Add to `simulation-config.json` with `device_id`, `count`, `uplink_interval`

## Adding a New Test Feature

1. Add config field to `internal/config/config.go`
2. Implement `setup{Feature}()` in `internal/simulator/simulator.go`
3. Call in `init()` based on `test_feature` string check

## API Client Regeneration

```bash
cd api_tools
./generate_api_client.sh
```

Runs `fix_mixin_operation_id.py` → `convert_swagger_to_camel_case.py` → `go-swagger generate client` → outputs to `internal/as_api/`.

## Logging

- File: `simulator.log` (in working directory `cmd/lora-simulator/`)
- `log_level`: 5=debug, 4=info, 3=warning, 2=error
- Library: logrus

## Troubleshooting

| Symptom | Check |
|---|---|
| Devices not joining | `activation_time`, AppKey match, MQTT connectivity, increase `sequence_join_interval` |
| No uplinks after join | JoinAccept MIC in logs, `uplink_paused=false`, `deviceStateActivated` |
| Codec errors | JS syntax, test data JSON format, `default_fport` matches codec |
| Auth failures | `username`/`password`, `server` reachable, `insecure` TLS setting |
