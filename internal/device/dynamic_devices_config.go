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
	DeveuiMap                map[lorawan.EUI64]interface{} `json:"-"`
	GlobalUplinkInterval     int64                         `json:"global_uplink_interval"`
	GlobalUplinkIntervalTime time.Duration                 `json:"-"`
}

type FuotaDebug struct {
	MgGroupSetupAns      *MgGroupSetupAns      `json:"mg_group_setup_ans"`
	McClassCSessionAns   *McClassCSessionAns   `json:"mc_class_c_session_ans"`
	FragSessionSetupAns  *FragSessionSetupAns  `json:"frag_session_setup_ans"`
	FragSessionStatusAns *FragSessionStatusAns `json:"frag_session_status_ans"`
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

type MgGroupSetupAns struct {
	SkipMgGroupSetupAns bool  `json:"skip_mg_group_setup_ans"`
	IDError             bool  `json:"id_error"`
	McGroupID           int64 `json:"mc_group_id"`
}

type TemperatureControl struct {
	Mode        int64   `json:"mode"`
	Temperature float64 `json:"temperature"`
}

type DynamicDevicesConfigManager struct {
	mu        sync.RWMutex
	config    DevicesDynamicConfig
	filePath  string
	allDeveui bool
}

var instance *DynamicDevicesConfigManager
var once sync.Once

func (m *DynamicDevicesConfigManager) LoadFromFile() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return errors.Wrap(err, "read config file error")
	}

	var config DevicesDynamicConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return errors.Wrap(err, "unmarshal config error")
	}

	config.Devices.GlobalUplinkIntervalTime = time.Duration(config.Devices.GlobalUplinkInterval) * time.Millisecond

	// 处理DeveuiRange逻辑
	if config.Devices.DeveuiRange == "all" {
		m.allDeveui = true
	} else {
		m.allDeveui = false
		deveuiList := strings.Split(config.Devices.DeveuiRange, "-")
		if len(deveuiList) != 2 {
			return errors.New("invalid deveui range")
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

		config.Devices.DeveuiMap = make(map[lorawan.EUI64]interface{})
		for i := beginEuiInt; i <= endEuiInt; i++ {
			euiStr := fmt.Sprintf("%016x", i)
			var eui lorawan.EUI64
			if err := eui.UnmarshalText([]byte(euiStr)); err != nil {
				return errors.Wrap(err, "unmarshal eui error")
			}
			config.Devices.DeveuiMap[eui] = struct{}{}
		}
	}

	// 所有校验通过后，原子性更新配置
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config

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

	if _, ok := instance.config.Devices.DeveuiMap[eui]; !ok && !instance.allDeveui {
		log.Infof("ok: %v, instance.allDeveui: %v", ok, instance.allDeveui)
		return nil
	}
	return &instance.config
}
