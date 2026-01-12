# 多设备类型配置迁移指南

本指南介绍如何从旧的单一设备类型配置迁移到新的多设备类型配置系统。

## 概述

### 旧配置方式（单一设备类型）
- 所有设备配置在 `config/base_devices_export.csv`
- 全局测试数据在 `payload_en_decoder/test-data.json`
- 一次仿真只能配置一种 payloadcodec 设备类型

### 新配置方式（多设备类型）
- 设备类型定义在 `payload_en_decoder/codec-release/vendors/milesight-iot/devices.json`
- 仿真配置在 `config/simulation-config.json`
- 每个设备类型有独立的测试数据文件
- 支持同时仿真多种设备类型

## 迁移步骤

### 步骤 1: 检查设备类型配置

确认 `devices.json` 中包含您需要的设备类型配置。新增字段包括：

```json
{
    "id": "vs330",
    "name": "VS330",
    "description": "Bathroom Occupancy Sensor",
    "catalog": "vs",
    "sn": "6617",
    "deveui": "24e124617",
    "device_profile": ["ClassA-OTAA"],
    "codec": "vendors/milesight-iot/vs330/vs330-codec.json",
    "decoder_script": "vendors/milesight-iot/vs330/vs330-decoder.js",
    "encoder_script": "vendors/milesight-iot/vs330/vs330-encoder.js",
    "test_data": "vendors/milesight-iot/vs330/vs330-test-data.json",
    "default_fport": 85,
    "simulation": {
        "uplink_interval": "120s",
        "confirmed_uplink": false
    }
}
```

新增字段说明：
- `test_data`: 设备特定测试数据文件路径
- `default_fport`: 默认 FPort
- `simulation`: 仿真默认参数

### 步骤 2: 创建设备特定测试数据文件

为每个设备类型创建测试数据文件。文件位置：
```
payload_en_decoder/codec-release/vendors/milesight-iot/{device-name}/{device-name}-test-data.json
```

示例 `vs330-test-data.json`:
```json
{
    "occupancy": 1,
    "battery": 100,
    "temperature": 25.5
}
```

### 步骤 3: 创建仿真配置文件

创建 `config/simulation-config.json`:

```json
{
    "version": "1.0.0",
    "simulation": {
        "name": "multi-device-simulation",
        "duration": "0s",
        "activation_time": "60s",
        "sequence_join": true,
        "sequence_join_interval": "2s"
    },
    "device_types": [
        {
            "device_id": "vs330",
            "count": 2,
            "uplink_interval": "120s"
        },
        {
            "device_id": "wt201",
            "count": 1,
            "uplink_interval": "180s"
        },
        {
            "device_id": "em300_th",
            "count": 3,
            "uplink_interval": "300s"
        }
    ]
}
```

配置说明：
- `device_id`: 对应 devices.json 中的 id 字段
- `count`: 该类型设备数量
- `uplink_interval`: 上行间隔（覆盖设备类型默认值）
- `test_data_override`: 可选，覆盖测试数据路径

### 步骤 4: 更新 base_devices_export.csv（可选）

如果需要自定义设备配置，可以在 `base_devices_export.csv` 中为每个设备类型添加模板行：

```csv
deveui,name,description,application,deviceprofile,payloadcodec,fport,appkey,devaddr,nwkskey,appskey
24e1246170000001,VS330-1,VS330 Template,LoRa Simulator,ClassA-OTAA,VS330,85,,,,
24e1247150000001,WT201-1,WT201 Template,LoRa Simulator,ClassC-OTAA,WT201,85,,,,
24e1241360000001,EM300-TH-1,EM300-TH Template,LoRa Simulator,ClassA-OTAA,EM300-TH,85,,,,
```

## 向后兼容性

新系统完全向后兼容：

1. **无 simulation-config.json**: 使用旧的单设备类型模式
2. **无设备特定测试数据**: 回退到全局 `test-data.json`
3. **无新字段在 devices.json**: 使用默认值

## 配置优先级

### 测试数据优先级
1. `DeviceInstance.TestData` (运行时设置)
2. `DeviceTypeConfig.TestData` (devices.json)
3. 全局 `payload_en_decoder/test-data.json`

### 上行间隔优先级
1. `DeviceTypeInstance.UplinkInterval` (simulation-config.json)
2. `DeviceTypeConfig.Simulation.UplinkInterval` (devices.json)
3. 全局配置 `lora-simulator.toml`

### FPort 优先级
1. `DeviceTypeConfig.DefaultFPort` (devices.json)
2. 默认值 (1)

## 验证迁移

运行仿真器并检查日志：

```bash
./lora-simulator -c lora-simulator.toml
```

成功日志示例：
```
INFO Registered 100 device types from devices.json
INFO Loaded simulation config with 3 device types
INFO Generated 6 device instances from simulation config
INFO Generated 2 instances for device type VS330
INFO Generated 1 instances for device type WT201
INFO Generated 3 instances for device type EM300-TH
```

## 常见问题

### Q: 如何禁用多设备类型模式？
删除或重命名 `config/simulation-config.json` 文件。

### Q: 如何为单个设备覆盖测试数据？
在 `simulation-config.json` 的 `device_types` 中添加 `test_data_override` 字段：
```json
{
    "device_id": "vs330",
    "count": 2,
    "test_data_override": "path/to/custom-test-data.json"
}
```

### Q: 设备类型 ID 在哪里找？
查看 `devices.json` 文件中每个设备的 `id` 字段。

## 文件结构

迁移后的文件结构：

```
lora-simulator/
├── config/
│   ├── base_devices_export.csv      # 设备模板（可选）
│   ├── simulation-config.json       # 仿真配置（新）
│   └── lora-simulator.toml          # 主配置
├── payload_en_decoder/
│   ├── test-data.json               # 全局测试数据（回退）
│   └── codec-release/
│       └── vendors/
│           └── milesight-iot/
│               ├── devices.json     # 设备类型定义（扩展）
│               ├── vs330/
│               │   ├── vs330-encoder.js
│               │   ├── vs330-decoder.js
│               │   └── vs330-test-data.json  # 设备特定测试数据
│               └── wt201/
│                   ├── wt201-encoder.js
│                   ├── wt201-decoder.js
│                   └── wt201-test-data.json
└── internal/
    └── deviceconfig/                # 新模块
        ├── types.go
        ├── registry.go
        └── loader.go