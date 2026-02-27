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

# Run with --workdir (多实例场景，指定工作目录，所有相对路径基于该目录)
./lora-simulator --workdir instances/server1 -c config/lora-simulator.toml

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

**Config file hierarchy** (all relative to working directory):
1. `config/lora-simulator.toml` - Server URLs, credentials, test features
2. `config/simulation-config.json` - Multi-device type simulation (device types + counts)
3. `payload_en_decoder/codec-release/vendors/milesight-iot/devices.json` - Device type catalog
4. `config/devices_dynamic.json` - FUOTA per-device debug config (hot-reloaded every 5s)
5. `api/fuota_req.json`, `api/modbus_server_create.json` - API request templates

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

**FUOTA task options:**
```toml
fuota_task_device_count = 1       # 每个 FUOTA 任务包含的设备数量
fuota_task_create_interval = "10s" # 多任务时的创建间隔，防止 NS 队列拥塞（status:16）
```

## Multi-Instance (Multiple Servers)

使用 `--workdir` 参数同时测试多台服务器，各实例完全隔离：

```
cmd/lora-simulator/
  lora-simulator.exe        ← 唯一二进制
  payload_en_decoder/       ← 唯一 codec（共享只读）
  instances/
    server1/
      config/lora-simulator.toml   # server=IP1, prometheus bind=":9001"
      config/devices_dynamic.json
      config/simulation-config.json
      api/fuota_req.json
    server2/
      config/lora-simulator.toml   # server=IP2, prometheus bind=":9002"
      ...
```

启动：
```powershell
.\lora-simulator.exe --workdir instances\server1 -c config\lora-simulator.toml
.\lora-simulator.exe --workdir instances\server2 -c config\lora-simulator.toml
```

各实例的 `simulator.log`、`config/devices_dynamic.json` 完全独立，互不干扰。

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

- File: `simulator.log` (in working directory, i.e. `cmd/lora-simulator/` or `--workdir` path)
- `log_level`: 5=debug, 4=info, 3=warning, 2=error
- Library: logrus

## FUOTA Implementation

### Overview

Complete implementation of LoRaWAN FUOTA (Firmware Update Over The Air) protocol including:
- Remote Multicast Setup (fPort=200)
- Fragmented Data Block Transport (fPort=201)
- Application Layer Clock Synchronization (fPort=202)

### FUOTA Flow Sequence

```
1. Multicast Package Version Check (fPort=200)
   Server → PackageVersionReq (CID=0x00)
   Device → PackageVersionAns (PackageID=2, Version=1)

2. Fragmentation Package Version Check (fPort=201)
   Server → PackageVersionReq (CID=0x00)
   Device → PackageVersionAns (PackageID=3, Version=1)

3. Clock Synchronization (fPort=202)
   Server → ForceDeviceResyncReq (CID=0x03, NbTransmissions)
   Device → DeviceAppTimeReq (CID=0x01, DeviceTime=UnixTimestamp)
   Server → DeviceAppTimeAns (CID=0x01, TimeCorrection)

4. Multicast Group Setup (fPort=200)
   Server → McGroupSetupReq (CID=0x02, McAddr, McKey, ...)
   Device → McGroupSetupAns (CID=0x02, McGroupID)

5. Class C Session Setup (fPort=200)
   Server → McClassCSessionReq (CID=0x04, SessionTime, Frequency, DR)
   Device → McClassCSessionAns (CID=0x04, TimeToStart)

6. Fragmentation Session Setup (fPort=201)
   Server → FragSessionSetupReq (CID=0x02, NbFrag, FragSize, ...)
   Device → FragSessionSetupAns (CID=0x02, StatusBitMask)

7. Fragment Transmission (Multicast)
   Server → DataFragment packets (fPort=201)
   Device → Receives and assembles fragments

8. Session Status Query (fPort=201)
   Server → FragSessionStatusReq (CID=0x03)
   Device → FragSessionStatusAns (CID=0x03, NbFragReceived, MissingFrag)
```

### Protocol Packages

**`internal/clocksync/`** - Application Layer Clock Synchronization v1.0.0
- `clocksync.go` - Protocol definitions and command structures
- Commands: PackageVersionReq/Ans, DeviceAppTimeReq/Ans, ForceDeviceResyncReq/Ans
- DefaultFPort: 202

**`internal/multicastsetup/`** - Remote Multicast Setup (existing)
- Commands: PackageVersionReq/Ans, McGroupSetupReq/Ans, McClassCSessionReq/Ans
- DefaultFPort: 200

**`internal/fragmentation/`** - Fragmented Data Block Transport (existing)
- Commands: PackageVersionReq/Ans, FragSessionSetupReq/Ans, FragSessionStatusReq/Ans
- DefaultFPort: 201

### Device-Side Handlers

**`internal/device/device_fuota_handler.go`** - FUOTA command processing

Key functions:
```go
// Multicast Setup (fPort=200)
handleMulticastSetupCommand()
handlePackageVersionReq()           // PackageID=2, Version=1
handleMcGroupSetupReq()
handleMcClassCSessionReq()

// Fragmentation (fPort=201)
handleFragmentationSessionSetupCommand()
handleFragmentationPackageVersionReq()  // PackageID=3, Version=1
handleFragSessionSetupReq()
handleFragSessionStatusReq()

// Clock Sync (fPort=202)
handleClockSyncCommand()
handleClockSyncPackageVersionReq()      // PackageID=3, Version=1（固定，无版本校验）
sendDeviceAppTimeReq()                  // Send Unix timestamp
handleDeviceAppTimeAns()                // Receive time correction
handleForceDeviceResyncReq()            // Trigger time sync
```

**`internal/device/device.go`** - Downlink routing

fPort routing in `downlinkHandler()`:
```go
case 200: // multicastsetup.DefaultFPort
    d.handleMulticastSetupCommand(data)
case 201: // fragmentation.DefaultFPort
    d.handleFragmentationSessionSetupCommand(data)
case 202: // clocksync.DefaultFPort
    d.handleClockSyncCommand(data)
```

### PackageID Reference

| Protocol | fPort | PackageID | Version |
|---|---|---|---|
| Remote Multicast Setup | 200 | 2 | 1 |
| Fragmented Data Block Transport | 201 | 3 | 1 |
| Clock Synchronization | 202 | 3 | 1 |

**Note**: Fragmentation uses PackageID=3 (vendor-specific) instead of standard LoRaWAN PackageID=1.

### FUOTA Debug Configuration (`config/devices_dynamic.json`)

支持按设备 EUI 范围配置调试行为，热重载（每 5 秒），无需重启模拟器。

**单组模式**（`use_groups: false`）：所有设备使用 `devices` 段配置。
**多组模式**（`use_groups: true`）：按 `device_groups` 中的 `deveui_range` 分别匹配。
两段配置可同时保留，通过 `use_groups` 切换，无需删除任何内容。

```json
{
  "use_groups": false,
  "devices": {
    "deveui_range": "all",
    "fuota_debug": {
      "package_version_ans": {
        "skip_package_version_ans": false,
        "package_identifier": 2,
        "package_version": 1,
        "random_delay_max_sec": 0,
        "send_double_ans": false
      },
      "frag_package_version_ans": {
        "skip_frag_package_version_ans": false,
        "package_identifier": 3,
        "package_version": 1,
        "random_delay_max_sec": 0
      },
      "clock_sync_package_version_ans": {
        "skip_clock_sync_package_version_ans": false,
        "random_delay_max_sec": 0
      },
      "mg_group_setup_ans": { "skip_mg_group_setup_ans": false },
      "mc_class_c_session_ans": { "skip_mc_class_c_session_ans": false, "time_to_start": 10 },
      "frag_session_setup_ans": { "skip_frag_session_setup_ans": false },
      "frag_session_status_ans": { "skip_frag_session_status_ans": true }
    }
  },
  "device_groups": []
}
```

**各阶段可模拟的失败场景：**

| 阶段 | 配置字段 | 失败场景 |
|---|---|---|
| 组播包版本确认 (fPort=200) | `package_version_ans` | `skip=true`(超时) / `package_identifier≠2`(ID错误) / `package_version<1`(版本不兼容) |
| 分片包版本确认 (fPort=201) | `frag_package_version_ans` | `skip=true`(超时) / `package_identifier≠3`(ID错误) / `package_version<1`(版本不兼容) |
| 时钟同步 (fPort=202) | `clock_sync_package_version_ans` | `skip=true`(超时)，无版本校验 |
| 组播组建立 (fPort=200) | `mg_group_setup_ans` | `skip=true`(超时) / `id_error=true` |
| Class C 会话建立 (fPort=200) | `mc_class_c_session_ans` | `skip=true`(超时) / `mc_group_undefined/freq_error/dr_error` |
| 分片会话建立 (fPort=201) | `frag_session_setup_ans` | `skip=true`(超时) / 各 status_bit_mask 错误标志 |
| 分片状态上报 (fPort=201) | `frag_session_status_ans` | `skip=true`(跳过) / 自定义 nb_frag_received / CRC 上报 |

### Key Crypto Functions

**`device_fuota_handler.go`** - Multicast key derivation
```go
GetMcRootKeyForGenAppKey()  // Derive McRootKey from GenAppKey (LoRaWAN 1.0.x)
GetMcKEKey()                // Derive McKEKey from McRootKey
GetMcAppSKey()              // Derive McAppSKey from McKey + McAddr
GetMcNetSKey()              // Derive McNetSKey from McKey + McAddr
```

### Testing FUOTA

1. Enable FUOTA in config:
```toml
[lora-simulator.api]
test_feature = "fuota"
```

2. Create FUOTA task via API or let simulator auto-create (see `simulator.setupFuota()`)

3. Monitor logs for flow progression:
```bash
cd cmd/lora-simulator
tail -f simulator.log | grep -i "fuota\|clock"
```

4. Expected log sequence:
```
fuota: package-version-req received (fPort=200)
fuota: sending package-version-ans (identifier=2, version=1)
fuota: fragmentation package-version-req received (fPort=201)
fuota: sending fragmentation package-version-ans (identifier=3, version=1)
fuota: received force-device-resync-req (fPort=202)
fuota: sending device-app-time-req (device_time=...)
fuota: received device-app-time-ans (time_correction=...)
fuota: multicast-setup command received (McGroupSetup)
fuota: multicast-class-c-session command received
fuota: fragmentation-session-setup command received
```

## Troubleshooting

| Symptom | Check |
|---|---|
| Devices not joining | `activation_time`, AppKey match, MQTT connectivity, increase `sequence_join_interval` |
| No uplinks after join | JoinAccept MIC in logs, `uplink_paused=false`, `deviceStateActivated` |
| Codec errors | JS syntax, test data JSON format, `default_fport` matches codec |
| Auth failures | `username`/`password`, `server` reachable, `insecure` TLS setting |
| FUOTA not starting | Verify `test_feature="fuota"`, devices joined, check server-side task creation |
| status:16 timeout | NS 下行队列拥塞，设置 `fuota_task_create_interval = "10s"` 错开任务创建 |
| Clock sync skipped | Normal if Fragmentation PackageVersionCheck fails; server decides sync necessity |
| Fragment timeout | Check multicast keys derived correctly, Class C session active, frequency/DR match |
| 多实例端口冲突 | 每个实例设置不同的 `prometheus.bind` 端口（9001, 9002...） |
