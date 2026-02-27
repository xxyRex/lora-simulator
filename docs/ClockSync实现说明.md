# 时钟同步功能实现说明

## 概述
已实现 LoRaWAN Application Layer Clock Synchronization v1.0.0 协议，用于 FUOTA 过程中的设备时间同步。

## 实现的功能

### 1. Clock Sync Package (fPort=202)
创建了新的 `internal/clocksync` 包，实现以下命令：

#### 命令列表
- **PackageVersionReq/Ans (CID=0x00)**: 软件包版本查询
  - PackageIdentifier=3 (Clock Synchronization)
  - PackageVersion=1 (v1.0)

- **DeviceAppTimeReq/Ans (CID=0x01)**: 设备时间同步请求/响应
  - DeviceAppTimeReq: 设备发送当前时间（Unix时间戳）
  - DeviceAppTimeAns: 服务器返回时间校正值

- **DeviceTimeStampReq/Ans (CID=0x02)**: 时间戳查询（预留）

- **ForceDeviceResyncReq/Ans (CID=0x03)**: 强制设备重新同步

### 2. 设备端处理流程

#### 接收到 PackageVersionReq (fPort=202, data=00)
设备会自动发送 PackageVersionAns:
```
Payload: 00 03 01
- CID=0x00
- PackageIdentifier=3
- PackageVersion=1
