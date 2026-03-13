package device

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brocaar/lorawan"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type DevicesDynamicConfig struct {
	Devices Devices `json:"devices"`
}

type Devices struct {
	DeveuiRange              string                        `json:"deveui_range"`
	FuotaDebug               FuotaDebug                    `json:"fuota_debug"`
	TestData                 map[string]interface{}        `json:"test_data"`
	DeveuiMap                map[lorawan.EUI64]interface{} `json:"-"`
	GlobalUplinkInterval     int64                         `json:"global_uplink_interval"`
	GlobalUplinkIntervalTime time.Duration                 `json:"-"`
}

type FuotaDebug struct {
	PackageVersionAns          *PackageVersionAns          `json:"package_version_ans"`
	FragPackageVersionAns      *FragPackageVersionAns      `json:"frag_package_version_ans"`
	ClockSyncDebug             *ClockSyncDebug             `json:"clock_sync_debug"`
	MgGroupSetupAns            *MgGroupSetupAns            `json:"mg_group_setup_ans"`
	McClassCSessionAns         *McClassCSessionAns         `json:"mc_class_c_session_ans"`
	FragSessionSetupAns        *FragSessionSetupAns        `json:"frag_session_setup_ans"`
	FragSessionStatusAns       *FragSessionStatusAns       `json:"frag_session_status_ans"`
	UpgradeDebug               *UpgradeDebug               `json:"upgrade_debug"`
}

type FragPackageVersionAns struct {
	SkipFragPackageVersionAns bool  `json:"skip_frag_package_version_ans"`
	PackageIdentifier         uint8 `json:"package_identifier"`
	PackageVersion            uint8 `json:"package_version"`
	OmitPackageVersion        bool  `json:"omit_package_version"`
	RandomDelayMaxSec         int64 `json:"random_delay_max_sec"`
}

type ClockSyncDebug struct {
	SkipDeviceAppTimeReq bool   `json:"skip_device_app_time_req"`
	DeviceTimeOverride   *int64 `json:"device_time_override"`   // 覆盖 DeviceTime，单位秒（GPS epoch），nil 表示使用当前时间
	MalformedPayload     string `json:"malformed_payload"`      // 发送原始错误字节（hex），非空时绕过正常编码直接发送
}

type UpgradeDebug struct {
	VersionReportDelaySec int64  `json:"version_report_delay_sec"` // 分片完成后延迟多少秒上报新版本（0=不延迟）
	NewFirmwareVersion    string `json:"new_firmware_version"`     // 新固件版本号，如 "v1.3"
}

type FragSessionSetupAns struct {
	SkipFragSessionSetupAns bool          `json:"skip_frag_session_setup_ans"`
	StatusBitMask           StatusBitMask `json:"status_bit_mask"`
}

type StatusBitMask struct {
	FragIndex                    int64 `json:"frag_index"`
	WrongDescriptor              bool  `json:"wrong_descriptor"`
	FragSessionIndexNotSupported bool  `json:"frag_session_index_not_supported"`
	NotEnoughMemory              bool  `json:"not_enough_memory"`
	EncodingUnsupported          bool  `json:"encoding_unsupported"`
}

type FragSessionStatusAns struct {
	SkipFragSessionStatusAns bool             `json:"skip_frag_session_status_ans"`
	ReceivedAndIndex         ReceivedAndIndex `json:"received_and_index"`
	MissingFrag              int64            `json:"missing_frag"`
	Status                   Status           `json:"status"`
	CrcCheck                 CrcCheck         `json:"crc_check"`
}

type CrcCheck struct {
	UseCrcCheck bool   `json:"use_crc_check"`
	Delay       int64  `json:"delay"`
	Value       string `json:"value"`
}

type ReceivedAndIndex struct {
	FragIndex      int64 `json:"frag_index"`
	NbFragReceived int64 `json:"nb_frag_received"`
}

type Status struct {
	NotEnoughMatrixMemory bool `json:"not_enough_matrix_memory"`
}

type McClassCSessionAns struct {
	SkipMcClassCSessionAns bool               `json:"skip_mc_class_c_session_ans"`
	StatusAndMcGroupID     StatusAndMcGroupID `json:"status_and_mc_group_id"`
	TimeToStart            int64              `json:"time_to_start"`
}

type StatusAndMcGroupID struct {
	McGroupUndefined bool  `json:"mc_group_undefined"`
	FreqError        bool  `json:"freq_error"`
	DRError          bool  `json:"dr_error"`
	McGroupID        int64 `json:"mc_group_id"`
}

type PackageVersionAns struct {
	SkipPackageVersionAns bool  `json:"skip_package_version_ans"`
	PackageIdentifier     uint8 `json:"package_identifier"`
	PackageVersion        uint8 `json:"package_version"`
	SendDoubleAns         bool  `json:"send_double_ans"`
	RandomDelayMaxSec     int64 `json:"random_delay_max_sec"`
}

type MgGroupSetupAns struct {
	SkipMgGroupSetupAns bool  `json:"skip_mg_group_setup_ans"`
	IDError             bool  `json:"id_error"`
	McGroupID           int64 `json:"mc_group_id"`
}

type TemperatureControl struct {
	Mode        int64   `json:"mode"`
	Temperature float64 `json:"temperature"`
}

// devicesDynamicConfigFile is the top-level JSON structure.
// use_groups=true  → 使用 device_groups 多组配置
// use_groups=false → 使用 devices 单组配置（默认）
// 两份配置可以同时保留在文件中，通过 use_groups 切换，无需删除任何内容。
type devicesDynamicConfigFile struct {
	UseGroups    bool      `json:"use_groups"`    // true=使用 device_groups，false=使用 devices
	Devices      *Devices  `json:"devices"`       // 单组配置（适用于所有设备或指定范围）
	DeviceGroups []Devices `json:"device_groups"` // 多组配置（每组独立规则）
}

// resolvedGroup holds a parsed device group with its EUI set and config.
type resolvedGroup struct {
	allDeveui bool
	euiMap    map[lorawan.EUI64]interface{}
	devices   Devices
}

type DynamicDevicesConfigManager struct {
	mu       sync.RWMutex
	groups   []resolvedGroup
	filePath string
}

var instance *DynamicDevicesConfigManager
var once sync.Once

func (m *DynamicDevicesConfigManager) LoadFromFile() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return errors.Wrap(err, "read config file error")
	}

	var fileConfig devicesDynamicConfigFile
	if err := json.Unmarshal(data, &fileConfig); err != nil {
		return errors.Wrap(err, "unmarshal config error")
	}

	// 根据 use_groups 选择激活的配置段
	var devicesList []Devices
	if fileConfig.UseGroups {
		devicesList = fileConfig.DeviceGroups
	} else if fileConfig.Devices != nil {
		devicesList = []Devices{*fileConfig.Devices}
	}

	var groups []resolvedGroup
	for _, dev := range devicesList {
		dev.GlobalUplinkIntervalTime = time.Duration(dev.GlobalUplinkInterval) * time.Millisecond

		var group resolvedGroup
		group.devices = dev

		if dev.DeveuiRange == "all" {
			group.allDeveui = true
		} else if dev.DeveuiRange != "" {
			deveuiList := strings.Split(dev.DeveuiRange, "-")
			if len(deveuiList) != 2 {
				return errors.New("invalid deveui range: " + dev.DeveuiRange)
			}

			beginEuiInt, err := strconv.ParseUint(deveuiList[0], 16, 64)
			if err != nil {
				return errors.Wrap(err, "parse begin eui error")
			}
			endEuiInt, err := strconv.ParseUint(deveuiList[1], 16, 64)
			if err != nil {
				return errors.Wrap(err, "parse end eui error")
			}

			if endEuiInt < beginEuiInt {
				return errors.New("end eui is less than begin eui")
			}

			group.euiMap = make(map[lorawan.EUI64]interface{})
			for i := beginEuiInt; i <= endEuiInt; i++ {
				euiStr := fmt.Sprintf("%016x", i)
				var eui lorawan.EUI64
				if err := eui.UnmarshalText([]byte(euiStr)); err != nil {
					return errors.Wrap(err, "unmarshal eui error")
				}
				group.euiMap[eui] = struct{}{}
			}
		}

		groups = append(groups, group)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups = groups

	return nil
}

func GetDynamicDevicesConfig(eui lorawan.EUI64) *DevicesDynamicConfig {
	once.Do(func() {
		filePath := "config/devices_dynamic.json"
		instance = &DynamicDevicesConfigManager{
			filePath: filePath,
		}
		if err := instance.LoadFromFile(); err != nil {
			fmt.Printf("Initial load of dynamic devices config failed: %v\n", err)
		}

		go func() {
			for {
				time.Sleep(5 * time.Second)
				if err := instance.LoadFromFile(); err != nil {
					fmt.Printf("Error reloading dynamic devices config: %v\n", err)
				}
			}
		}()
	})

	instance.mu.RLock()
	defer instance.mu.RUnlock()

	for _, group := range instance.groups {
		if group.allDeveui {
			return &DevicesDynamicConfig{Devices: group.devices}
		}
		if _, ok := group.euiMap[eui]; ok {
			return &DevicesDynamicConfig{Devices: group.devices}
		}
	}

	log.Debugf("no matching device group for eui: %v", eui)
	return nil
}
