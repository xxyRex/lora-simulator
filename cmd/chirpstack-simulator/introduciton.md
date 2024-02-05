## 使用方法
### 节点列表的生成方式
```
python3 generate_devices.py 2000
```
这条命令会基于base_devices_export.csv生成一个有2000条节点数据的device_import.csv

### 模拟器使用方法
**注意：使用前需要先保证LNS内无数据，防止冲突，同时将信道方案切换为EU868**
- 打开对应文件夹，根据平台执行对应命令即可
```shell
# Linux平台
./lns_simulator -c chirpstack-simulator.toml
# Windows平台
lns_simulator.exe -c chirpstack-simulator.toml
```
**使用该命令启动模拟器后，模拟器工作流程如下**
- 通过http API在LNS创建对应的application(simulator_test), profile(simulator_test), 网关, 然后解析device_import.csv的内容，将里面的所有节点添加到LNS
- 所有节点添加完毕后, 所有节点开始发送入网请求, 并开启下行数据接收循环, 等待接收下行数据
- 当节点接收到下行数据, 节点状态被设置为activiated, 并间隔10s(可在配置文件chirpstack-simulator.toml中的activation_time中配置)发送一条上行数据

**测试处理入网请求的性能时，使用lns_simulator_one_join.exe，可以把chirpstack-simulator.toml中的activation_time改0s，这样节点就会同一时间入网，如果设置为例如2min的话，节点的入网请求会随机分布在两分钟内发出**

### 根据lora-app-server.log日志统计每个eui上行包数量
**注意：要在模拟器关闭后进行才是准确的**

**步骤**
1. 将lora-app-server.log复制到当前文件夹
2. 执行以下命令
```shell
# 2000为需要需要统计的设备的个数
python statistics_data.py 2000
```
3. 统计结果会展示在statistics.csv中，包括eui, devAddr, 节点上行数量, 节点下行数据量, lns收到的上行数据量
