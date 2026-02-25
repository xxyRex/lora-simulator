package device

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"sync/atomic"

	"github.com/brocaar/lora-simulator/internal/as_api/models"
	"github.com/brocaar/lora-simulator/internal/config"
	"github.com/brocaar/lora-simulator/internal/deviceconfig"
	"github.com/brocaar/lora-simulator/internal/fragmentation"
	"github.com/brocaar/lora-simulator/internal/gateway"
	"github.com/brocaar/lora-simulator/internal/multicastsetup"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

var (
	globalLastUplinkTime      = time.Now()
	globalLastUplinkTimeMutex sync.Mutex
)

func allowUplink(devEUI lorawan.EUI64) bool {
	config := GetDynamicDevicesConfig(devEUI)
	globalLastUplinkTimeMutex.Lock()
	allow := false
	if time.Since(globalLastUplinkTime) > config.Devices.GlobalUplinkIntervalTime {
		allow = true
		globalLastUplinkTime = time.Now()
	}
	globalLastUplinkTimeMutex.Unlock()
	return allow
}

// DeviceOption is the interface for a device option.
type DeviceOption func(*Device) error

const (
	AFTER_JOIN_DELAY   = 6 * time.Second
	UPLINK_TYPE_UP_UNC = "UpUnc"
	UPLINK_TYPE_UP_CON = "UpCnf"
)

type deviceState int

const (
	deviceStateOTAA deviceState = iota
	deviceStateActivated
)

type fuotaProperties struct {
	McGroupSetupReqPayload      *multicastsetup.McGroupSetupReqPayload
	McClassCSessionReqPayload   *multicastsetup.McClassCSessionReqPayload
	FragSessionSetupReqPayload  *fragmentation.FragSessionSetupReqPayload
	FragSessionStatusReqPayload *fragmentation.FragSessionStatusReqPayload
}

type multicastKeys struct {
	McKEKey   lorawan.AES128Key
	McAppSKey lorawan.AES128Key
	McNetSKey lorawan.AES128Key
}

// Device contains the state of a simulated LoRaWAN OTAA device (1.0.x).
type Device struct {
	sync.RWMutex

	// Context to cancel device.
	ctx context.Context

	// Cancel function.
	cancel context.CancelFunc

	// Waitgroup to wait until simulation has been fully cancelled.
	wg *sync.WaitGroup

	// DevEUI.
	devEUI lorawan.EUI64

	// JoinEUI.
	joinEUI lorawan.EUI64

	// AppKey.
	appKey lorawan.AES128Key

	// Interval in which device sends uplinks.
	uplinkInterval time.Duration

	// Total number of uplinks to send, before terminating.
	uplinkCount uint32

	// Device sends uplink as confirmed.
	confirmed bool

	// Per-device uplink configuration (from simulation-config.json)
	uplinkPaused  bool
	uplinkConfirm bool

	dynamicPayload []byte

	// Payload (plaintext) which the device sends as uplink.
	payload []byte

	encoderScriptFileModTime time.Time

	// FPort used for sending uplinks.
	fPort uint8

	// Assigned device address.
	devAddr lorawan.DevAddr

	// DevNonce.
	devNonce lorawan.DevNonce

	// Uplink frame-counter.
	fCntUp uint32

	// Downlink frame-counter.
	fCntDown uint32

	// Application session-key.
	appSKey lorawan.AES128Key

	// Network session-key.
	nwkSKey lorawan.AES128Key

	// Activation state.
	state deviceState

	// Downlink frames channel (used by the gateway). Note that the gateway
	// forwards downlink frames to all associated devices, as only the device
	// is able to validate the addressee.
	downlinkFrames chan *gw.DownlinkFrame

	// The associated gateway through which the device simulates its uplinks.
	gateways []*gateway.Gateway

	// Random DevNonce
	randomDevNonce bool

	// TXInfo for uplink
	uplinkTXInfo *gw.UplinkTxInfo

	// Downlink handler function.
	downlinkHandlerFunc func(confirmed, ack bool, fCntDown uint32, fPort uint8, data []byte) error

	// OTAA delay.
	otaaDelay time.Duration

	// join windows flag
	joinWindowFlag int32

	// dataUp count
	dataUpCount uint64

	// datadown count
	datadownCount uint64

	joinReqSent bool

	fuotaProperties fuotaProperties

	multicastKeys multicastKeys

	lastJoinRequestTime time.Time

	minJoinRequestInterval time.Duration

	joinRequestCount uint64

	// Device type configuration from devices.json
	deviceTypeConfig *deviceconfig.DeviceTypeConfig

	// Device-specific test data (loaded from device-specific test data file)
	deviceTestData map[string]interface{}
}

func WithDeviceStoredInfo(item *models.APIDeviceItem) DeviceOption {
	return func(d *Device) error {
		if item == nil {
			return nil
		}
		d.devEUI.UnmarshalBinary([]byte(item.DevEUI))
		appSKeyBytes, err := hex.DecodeString(item.AppSKey)
		if err != nil {
			log.Errorf("decode appSKey error: %v, item.AppsKey: %v", err, item.AppSKey)
		}
		err = d.appSKey.UnmarshalBinary(appSKeyBytes)
		if err != nil {
			log.Errorf("decode appSKey error: %v, item.AppsKey: %v", err, item.AppSKey)
		}
		nwkSKeyBytes, err := hex.DecodeString(item.NwkSKey)
		if err != nil {
			log.Errorf("decode nwkSKey error: %v, item.NwkSKey: %v", err, item.NwkSKey)
		}
		err = d.nwkSKey.UnmarshalBinary(nwkSKeyBytes)
		if err != nil {
			log.Errorf("decode nwkSKey error: %v, item.NwkSKey: %v", err, item.NwkSKey)
		}
		devAddrBytes, err := hex.DecodeString(item.DevAddr)
		if err != nil {
			log.Errorf("decode devAddr error: %v, item.DevAddr: %v", err, item.DevAddr)
		}
		err = d.devAddr.UnmarshalBinary(devAddrBytes)
		if err != nil {
			log.Errorf("decode devAddr error: %v, item.DevAddr: %v", err, item.DevAddr)
		}
		d.fCntUp = uint32(item.FCntUp)
		d.fCntDown = uint32(item.FCntDown)
		return nil
	}
}

// WithAppKey sets the AppKey.
func WithAppKey(appKey lorawan.AES128Key) DeviceOption {
	return func(d *Device) error {
		d.appKey = appKey
		return nil
	}
}

// WithDevEUI sets the DevEUI.
func WithDevEUI(devEUI lorawan.EUI64) DeviceOption {
	return func(d *Device) error {
		d.devEUI = devEUI
		return nil
	}
}

// WithJoinEUI sets the JoinEUI.
func WithJoinEUI(joinEUI lorawan.EUI64) DeviceOption {
	return func(d *Device) error {
		d.joinEUI = joinEUI
		return nil
	}
}

// WithOTAADelay sets the OTAA delay.
func WithOTAADelay(delay time.Duration) DeviceOption {
	return func(d *Device) error {
		d.otaaDelay = delay
		return nil
	}
}

// WithUplinkInterval sets the uplink interval.
func WithUplinkInterval(interval time.Duration) DeviceOption {
	return func(d *Device) error {
		d.uplinkInterval = interval
		return nil
	}
}

// WithUplinkCount sets the uplink count, after which the device simulation
// ends.
func WithUplinkCount(count uint32) DeviceOption {
	return func(d *Device) error {
		d.uplinkCount = count
		return nil
	}
}

// WithUplinkPayload sets the uplink payload.
func WithUplinkPayload(confirmed bool, fPort uint8, pl []byte) DeviceOption {
	return func(d *Device) error {
		d.fPort = fPort
		d.payload = pl
		d.confirmed = confirmed
		return nil
	}
}

// WithGateways adds the device to the given gateways.
// Use this function after WithDevEUI!
func WithGateways(gws []*gateway.Gateway) DeviceOption {
	return func(d *Device) error {
		d.gateways = gws

		for i := range d.gateways {
			d.gateways[i].AddDevice(d.devEUI, d.downlinkFrames)
		}
		return nil
	}
}

// WithRandomDevNonce randomizes the OTAA DevNonce instead of using a counter value.
func WithRandomDevNonce() DeviceOption {
	return func(d *Device) error {
		d.randomDevNonce = true
		return nil
	}
}

// WithUplinkTXInfo sets the TXInfo used for simulating the uplinks.
func WithUplinkTXInfo(txInfo *gw.UplinkTxInfo) DeviceOption {
	return func(d *Device) error {
		d.uplinkTXInfo = txInfo
		return nil
	}
}

// WithDownlinkHandlerFunc sets the downlink handler func.
func WithDownlinkHandlerFunc(f func(confirmed, ack bool, fCntDown uint32, fPort uint8, data []byte) error) DeviceOption {
	return func(d *Device) error {
		d.downlinkHandlerFunc = f
		return nil
	}
}

// WithDeviceTypeConfig sets the device type configuration from devices.json
func WithDeviceTypeConfig(cfg *deviceconfig.DeviceTypeConfig) DeviceOption {
	return func(d *Device) error {
		d.deviceTypeConfig = cfg
		if cfg != nil {
			// Set default fPort if specified
			if cfg.DefaultFPort > 0 {
				d.fPort = uint8(cfg.DefaultFPort)
			}
		}
		return nil
	}
}

// WithDeviceTestData sets the device-specific test data
func WithDeviceTestData(testData map[string]interface{}) DeviceOption {
	return func(d *Device) error {
		d.deviceTestData = testData
		return nil
	}
}

// WithUplinkPaused sets whether uplink is paused for this device
func WithUplinkPaused(paused bool) DeviceOption {
	return func(d *Device) error {
		d.uplinkPaused = paused
		return nil
	}
}

// WithUplinkConfirm sets whether this device uses confirmed uplinks
func WithUplinkConfirm(confirm bool) DeviceOption {
	return func(d *Device) error {
		d.uplinkConfirm = confirm
		return nil
	}
}

// NewDevice creates a new device simulation.
func NewDevice(ctx context.Context, wg *sync.WaitGroup, opts ...DeviceOption) (*Device, error) {
	ctx, cancel := context.WithCancel(ctx)

	d := &Device{
		ctx:    ctx,
		cancel: cancel,
		wg:     wg,

		downlinkFrames:         make(chan *gw.DownlinkFrame, 100),
		state:                  deviceStateOTAA,
		lastJoinRequestTime:    time.Now(),
		minJoinRequestInterval: 0,
		joinRequestCount:       0,
	}

	for _, o := range opts {
		if err := o(d); err != nil {
			return nil, err
		}
	}

	wg.Add(2)

	d.joinReqSent = false
	atomic.StoreInt32(&d.joinWindowFlag, 0)
	go d.uplinkLoop()
	go d.downlinkLoop()

	return d, nil
}

// uplinkLoop first handle the OTAA activation, after which it will periodically
// sends an uplink with the configured payload and fport.
func (d *Device) uplinkLoop() {
	defer d.cancel()
	defer d.wg.Done()

	var cancelled bool
	go func() {
		<-d.ctx.Done()
		cancelled = true
	}()

	time.Sleep(d.otaaDelay)

	for !cancelled {
		switch d.getState() {
		case deviceStateOTAA:
			d.joinRequest()
			time.Sleep(AFTER_JOIN_DELAY)
		case deviceStateActivated:
			// Use per-device uplink configuration (from simulation-config.json)
			paused := d.uplinkPaused
			uplinkType := UPLINK_TYPE_UP_UNC
			if d.uplinkConfirm {
				uplinkType = UPLINK_TYPE_UP_CON
			}

			// Optional: allow runtime override from devices_dynamic.json for global settings
			config := GetDynamicDevicesConfig(d.devEUI)
			if config != nil {
				// Only use global uplink interval if device-specific interval is not set
				if d.uplinkInterval == 0 && config.Devices.GlobalUplinkIntervalTime > 0 {
					d.uplinkInterval = config.Devices.GlobalUplinkIntervalTime
				}
			}
			if paused {
				continue
			} else {
				d.getEncoderData()
				if !allowUplink(d.devEUI) {
					time.Sleep(time.Second)
					continue
				}
				log.Infof("deveui: %v, uplinkType: %v", d.devEUI, uplinkType)
				if uplinkType == "UpUnc" {
					d.dataUp(lorawan.UnconfirmedDataUp, false)
				} else {
					d.dataUp(lorawan.ConfirmedDataUp, false)
				}

				if d.uplinkCount != 0 {
					if d.fCntUp >= d.uplinkCount {
						// d.cancel() also cancels the downlink loop. Wait one
						// second in order to process any potential downlink
						// response (e.g. and ack).
						time.Sleep(time.Second)
						d.cancel()
						return
					}
				}
			}
			time.Sleep(d.uplinkInterval)
		}
	}
}

// downlinkLoop handles the downlink messages.
// Note: as a gateway does not know the addressee of the downlink, it is up to
// the handling functions to validate the MIC etc..
func (d *Device) downlinkLoop() {
	defer d.cancel()
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return

		case pl := <-d.downlinkFrames:
			for _, item := range pl.Items {
				func() error {
					var phy lorawan.PHYPayload

					if err := phy.UnmarshalBinary(item.PhyPayload); err != nil {
						return errors.Wrap(err, "unmarshal phypayload error")
					}

					switch phy.MHDR.MType {
					case lorawan.JoinAccept:
						return d.joinAccept(phy)
					case lorawan.UnconfirmedDataDown, lorawan.ConfirmedDataDown:
						return d.downlinkData(phy)
					}

					return nil
				}()

				break
			}
		}
	}
}

// joinRequest sends the join-request.
func (d *Device) joinRequest() {
	if !config.C.LoraSimulator.API.UseNewDevice {
		d.setState(deviceStateActivated)
		return
	}

	if time.Since(d.lastJoinRequestTime) < d.minJoinRequestInterval {
		time.Sleep(time.Second * 5)
		return
	}

	if d.joinRequestCount >= 3 {
		return
	}

	phy := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.JoinRequest,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.JoinRequestPayload{
			DevEUI:   d.devEUI,
			JoinEUI:  d.joinEUI,
			DevNonce: d.getDevNonce(),
		},
	}

	if err := phy.SetUplinkJoinMIC(d.appKey); err != nil {
		return
	}

	d.sendUplink(phy)

	log.Info("deveui: ", d.devEUI.String(), " send join req")
	d.joinReqSent = true
	atomic.StoreInt32(&d.joinWindowFlag, 1)
	deviceJoinRequestCounter().Inc()

	d.lastJoinRequestTime = time.Now()
	d.joinRequestCount++
	d.minJoinRequestInterval = time.Duration(rand.Intn(60)) * time.Second
}

// encodePayload 执行 JS 编码器并返回编码后的字节数组
// Now supports per-device test data
func (d *Device) encodePayload(encoderScript string, testData map[string]interface{}) ([]byte, error) {
	// 创建新的 Goja VM 实例
	vm := goja.New()

	// 执行编码器脚本,注册 Encode 函数
	if _, err := vm.RunString(encoderScript); err != nil {
		return nil, fmt.Errorf("execute encoder script error: %v", err)
	}

	// 获取 Encode 函数
	encode, ok := goja.AssertFunction(vm.Get("Encode"))
	if !ok {
		return nil, fmt.Errorf("Encode is not a function")
	}

	// 调用 Encode 函数
	result, err := encode(goja.Undefined(), goja.Null(), vm.ToValue(testData))
	if err != nil {
		return nil, fmt.Errorf("call encode function error: %v", err)
	}

	// 将结果转换为字节数组
	exportedValue := result.Export()

	var bytes []byte

	// 处理不同类型的返回值
	switch v := exportedValue.(type) {
	case []interface{}:
		for _, byteVal := range v {
			switch num := byteVal.(type) {
			case int64:
				bytes = append(bytes, byte(num))
			case float64:
				bytes = append(bytes, byte(num))
			default:
				return nil, fmt.Errorf("unexpected type for byte value: %T", byteVal)
			}
		}
	case []int64:
		for _, num := range v {
			bytes = append(bytes, byte(num))
		}
	case []float64:
		for _, num := range v {
			bytes = append(bytes, byte(num))
		}
	default:
		return nil, fmt.Errorf("unexpected result type: %T", exportedValue)
	}

	return bytes, nil
}

// loadTestData loads test data from the appropriate path
// Priority: device-specific test data > device type test data
// Returns empty map if no test data is available (no longer requires default test data file)
func (d *Device) loadTestData() (map[string]interface{}, error) {
	// If device already has pre-loaded test data, use it
	if d.deviceTestData != nil {
		return d.deviceTestData, nil
	}

	// If no device-specific test data path is configured, return empty map
	if d.deviceTypeConfig.TestData == "" {
		log.Debugf("no test data path configured for device %s, using empty test data", d.devEUI)
		return make(map[string]interface{}), nil
	}

	// Try to open the device-specific test data file
	testDataFile, err := os.Open(d.deviceTypeConfig.TestData)
	if err != nil {
		log.Warnf("test data not found at %s for device %s, using empty test data: %v", d.deviceTypeConfig.TestData, d.devEUI, err)
		return make(map[string]interface{}), nil
	}
	defer testDataFile.Close()

	var testData map[string]interface{}
	if err := json.NewDecoder(testDataFile).Decode(&testData); err != nil {
		return nil, fmt.Errorf("decode test data error: %v", err)
	}

	return testData, nil
}


func (d *Device) getEncoderData() {
	// Get encoder script path (device-specific or legacy)
	ecPath := d.deviceTypeConfig.EncoderScript
	if ecPath == "" {
		log.Errorf("no encoder script path available for device %s", d.devEUI)
		return
	}

	// Check encoder script modification time to determine if re-encoding is needed
	var shouldReEncode bool
	if d.deviceTypeConfig.TestData != "" {
		// If device has a specific test data path, check its modification time
		testDataInfo, err := os.Stat(d.deviceTypeConfig.TestData)
		if err != nil {
			// Test data file doesn't exist, but we can still encode with empty data
			log.Debugf("test data file not found for device %s: %v, will use empty test data", d.devEUI, err)
			shouldReEncode = d.encoderScriptFileModTime.IsZero()
		} else if !d.encoderScriptFileModTime.Equal(testDataInfo.ModTime()) {
			d.encoderScriptFileModTime = testDataInfo.ModTime()
			shouldReEncode = true
		}
	} else {
		// No test data path configured, check encoder script modification time instead
		encoderInfo, err := os.Stat(ecPath)
		if err != nil {
			log.Errorf("stat encoder file error: %v", err)
			return
		}
		if !d.encoderScriptFileModTime.Equal(encoderInfo.ModTime()) {
			d.encoderScriptFileModTime = encoderInfo.ModTime()
			shouldReEncode = true
		}
	}

	if shouldReEncode {
		file, err := os.Open(ecPath)
		if err != nil {
			log.Errorf("open encoder file error: %v", err)
			return
		}
		defer file.Close()
		encoderScript, _ := io.ReadAll(file)

		// Load test data (device-specific or empty)
		testData, err := d.loadTestData()
		if err != nil {
			log.Errorf("load test data error: %v", err)
			return
		}

		bytes, err := d.encodePayload(string(encoderScript), testData)
		if err != nil {
			log.Errorf("encode payload error: %v", err)
			return
		}

		d.payload = bytes
		d.dynamicPayload = bytes
	} else {
		d.payload = d.dynamicPayload
	}

	// Use device-specific fPort if set, otherwise default to 1
	if d.fPort == 0 {
		d.fPort = 1
	}
}

// dataUp sends an data uplink.
func (d *Device) dataUp(mType lorawan.MType, ack bool) {
	if d.payload == nil || d.fPort == 0 {
		return
	}

	d.dataUpCount++

	phy := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: mType,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: d.devAddr,
				FCnt:    d.fCntUp,
				FCtrl: lorawan.FCtrl{
					ADR: false,
					ACK: ack,
				},
			},
			FPort: &d.fPort,
			FRMPayload: []lorawan.Payload{
				&lorawan.DataPayload{
					Bytes: d.payload,
				},
			},
		},
	}

	if err := phy.EncryptFRMPayload(d.appSKey); err != nil {
		return
	}

	if err := phy.SetUplinkDataMIC(lorawan.LoRaWAN1_0, 0, 0, 0, d.nwkSKey, d.nwkSKey); err != nil {
		return
	}

	d.fCntUp++

	d.sendUplink(phy)

	deviceUplinkCounter().Inc()

	d.payload = nil
	d.fPort = 0
}

// sendAck sends an ACK uplink (empty payload) in response to a confirmed downlink.
func (d *Device) sendAck() {
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
	}).Info("simulator: sending ACK for confirmed downlink")

	d.dataUpCount++

	phy := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.UnconfirmedDataUp,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: d.devAddr,
				FCnt:    d.fCntUp,
				FCtrl: lorawan.FCtrl{
					ADR: false,
					ACK: true,
				},
			},
		},
	}

	if err := phy.SetUplinkDataMIC(lorawan.LoRaWAN1_0, 0, 0, 0, d.nwkSKey, d.nwkSKey); err != nil {
		log.WithError(err).Error("simulator: set uplink data MIC error")
		return
	}

	d.fCntUp++

	d.sendUplink(phy)

	deviceUplinkCounter().Inc()
}

// joinAccept validates and handles the join-accept downlink.
func (d *Device) joinAccept(phy lorawan.PHYPayload) error {
	if atomic.LoadInt32(&d.joinWindowFlag) == 0 {
		return errors.New("not my join window " + d.devEUI.String())
	}

	err := phy.DecryptJoinAcceptPayload(d.appKey)
	if err != nil {
		return errors.Wrap(err, "decrypt join-accept payload error")
	}

	ok, err := phy.ValidateDownlinkJoinMIC(lorawan.JoinRequestType, d.joinEUI, d.devNonce, d.appKey)
	if err != nil {
		return nil
	}
	if !ok {
		return nil
	}

	jaPL, ok := phy.MACPayload.(*lorawan.JoinAcceptPayload)
	if !ok {
		return errors.New("expected *lorawan.JoinAcceptPayload")
	}

	d.appSKey, err = getAppSKey(jaPL.DLSettings.OptNeg, d.appKey, jaPL.HomeNetID, d.joinEUI, jaPL.JoinNonce, d.devNonce)
	if err != nil {
		return errors.Wrap(err, "get AppSKey error")
	}

	d.nwkSKey, err = getFNwkSIntKey(jaPL.DLSettings.OptNeg, d.appKey, jaPL.HomeNetID, d.joinEUI, jaPL.JoinNonce, d.devNonce)
	if err != nil {
		return errors.Wrap(err, "get NwkSKey error")
	}

	d.devAddr = jaPL.DevAddr

	log.Info("deveui: ", d.devEUI.String(), " received join accept")
	d.setState(deviceStateActivated)
	deviceJoinAcceptCounter().Inc()
	SetJoinAcceptCount(d.devEUI.String())

	return nil
}

func (d *Device) downlinkHandler(confirmed bool, ack bool, fCntDown uint32, fPort uint8, data []byte) error {
	log.WithFields(log.Fields{
		"dev_eui":   d.devEUI,
		"confirmed": confirmed,
		"ack":       ack,
		"fCntDown":  fCntDown,
		"fPort":     fPort,
		"data":      hex.EncodeToString(data),
	}).Info("simulator: downlink handler")

	switch fPort {
	case fragmentation.DefaultFPort:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("simulator: fragmentation")
		d.handleFragmentationSessionSetupCommand(data)
	case multicastsetup.DefaultFPort:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("simulator: multicast")
		d.handleMulticastSetupCommand(data)
	case 202: // clocksync.DefaultFPort
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("simulator: clock sync")
		d.handleClockSyncCommand(data)
	}

	return nil
}

// downlinkData validates and handles the downlink data.
func (d *Device) downlinkData(phy lorawan.PHYPayload) error {
	ok_single, err := phy.ValidateDownlinkDataMIC(lorawan.LoRaWAN1_0, 0, d.nwkSKey)
	if err != nil {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid downlink data MIC")
		return nil
	}

	ok_multicast, err := phy.ValidateDownlinkDataMIC(lorawan.LoRaWAN1_0, 0, d.multicastKeys.McNetSKey)
	if err != nil {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid downlink data MIC")
		return nil
	}

	if !ok_single && !ok_multicast {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid downlink data MIC")
		return nil
	}

	macPL, ok := phy.MACPayload.(*lorawan.MACPayload)
	if !ok {
		return fmt.Errorf("expected *lorawan.MACPayload, got: %T", phy.MACPayload)
	}

	for _, opt := range macPL.FHDR.FOpts {
		log.Infof("deveui: %v, opt: %v", d.devEUI, opt)
	}

	gap := uint32(uint16(macPL.FHDR.FCnt) - uint16(d.fCntDown%(1<<16)))
	d.fCntDown = d.fCntDown + gap

	var data []byte
	var fPort uint8
	if macPL.FPort != nil {
		fPort = *macPL.FPort
	}

	if fPort != 0 {
		if ok_single {
			err := phy.DecryptFRMPayload(d.appSKey)
			if err != nil {
				return errors.Wrap(err, "decrypt frmpayload error")
			}
		} else if ok_multicast {
			err := phy.DecryptFRMPayload(d.multicastKeys.McAppSKey)
			if err != nil {
				return errors.Wrap(err, "decrypt frmpayload error")
			}
		}

		if len(macPL.FRMPayload) != 0 {
			pl, ok := macPL.FRMPayload[0].(*lorawan.DataPayload)
			if !ok {
				return fmt.Errorf("expected *lorawan.DataPayload, got: %T", macPL.FRMPayload[0])
			}

			data = pl.Bytes
		}
	}
	d.datadownCount++

	// 如果收到 confirmed 下行，需要发送 ACK 响应
	if phy.MHDR.MType == lorawan.ConfirmedDataDown {
		d.sendAck()
	}

	d.downlinkHandler(phy.MHDR.MType == lorawan.ConfirmedDataDown, macPL.FHDR.FCtrl.ACK, d.fCntDown, fPort, data)

	return nil
}

// sendUplink sends
func (d *Device) sendUplink(phy lorawan.PHYPayload) error {
	b, err := phy.MarshalBinary()
	if err != nil {
		return errors.Wrap(err, "marshal phypayload error")
	}

	pl := gateway.RXPacketBytes{
		PHYPayload: b,
	}

	for i := range d.gateways {
		if err := d.gateways[i].SendUplinkFrame(pl); err != nil {
			log.WithError(err).WithFields(log.Fields{
				"dev_eui": d.devEUI,
			}).Error("simulator: send uplink frame error")
		}
	}

	return nil
}

// getDevNonce increments and returns a LoRaWAN DevNonce.
func (d *Device) getDevNonce() lorawan.DevNonce {
	if d.randomDevNonce {
		b := make([]byte, 2)
		_, _ = crand.Read(b)

		d.devNonce = lorawan.DevNonce(binary.BigEndian.Uint16(b))
	} else {
		d.devNonce++
	}

	return d.devNonce
}

// getState returns the current device state.
func (d *Device) getState() deviceState {
	d.RLock()
	defer d.RUnlock()

	return d.state
}

// setState sets the device to the given state.
func (d *Device) setState(s deviceState) {
	d.Lock()
	defer d.Unlock()

	d.state = s
}
