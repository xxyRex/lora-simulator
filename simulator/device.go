package simulator

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/robertkrimen/otto"
	log "github.com/sirupsen/logrus"

	"sync/atomic"

	"github.com/brocaar/chirpstack-simulator/internal/as"
	"github.com/brocaar/chirpstack-simulator/internal/config"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

// DeviceOption is the interface for a device option.
type DeviceOption func(*Device) error

const (
	CODEC_PARENT_DIR = "payload_en_decoder"
	CODEC_DIR        = CODEC_PARENT_DIR + "/codec-release/vendors/milesight-iot/"
	TEST_DATA_PATH   = CODEC_PARENT_DIR + "/test-data.json"
)

type deviceState int

const (
	deviceStateOTAA deviceState = iota
	deviceStateActivated
)

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
	downlinkFrames chan gw.DownlinkFrame

	// The associated gateway through which the device simulates its uplinks.
	gateways []*Gateway

	// Random DevNonce
	randomDevNonce bool

	// TXInfo for uplink
	uplinkTXInfo gw.UplinkTxInfo

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

	payloadCodec as.PayloadCodecItem

	config *Configuration
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
func WithGateways(gws []*Gateway) DeviceOption {
	return func(d *Device) error {
		d.gateways = gws

		for i := range d.gateways {
			d.gateways[i].addDevice(d.devEUI, d.downlinkFrames)
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
func WithUplinkTXInfo(txInfo gw.UplinkTxInfo) DeviceOption {
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

func WithPayloadCodec(payloadCodec as.PayloadCodecItem) DeviceOption {
	return func(d *Device) error {
		d.payloadCodec = payloadCodec
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

		downlinkFrames: make(chan gw.DownlinkFrame, 100),
		state:          deviceStateOTAA,
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

type DeviceStatus struct {
	UplinkPaused bool   `json:"uplink_paused"`
	UplinkType   string `json:"uplink_type"`
}

type Configuration struct {
	DeviceStatus DeviceStatus           `json:"device_status"`
	Object       map[string]interface{} `json:"object"`
}

func getConfiguration(filePath string) (*Configuration, error) {
	// Read the file content
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	// Parse the JSON
	var config Configuration
	if err := json.Unmarshal(fileContent, &config); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	return &config, nil
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
			time.Sleep(6 * time.Second)
		case deviceStateActivated:
			config, err := getConfiguration("config/device.json")
			d.config = config
			if err == nil && config.DeviceStatus.UplinkPaused {
				continue
			} else {
				if d.config.DeviceStatus.UplinkType == "UpUnc" {
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
	if d.joinReqSent {
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
}

// encodePayload 执行 JS 编码器并返回编码后的字节数组
func (d *Device) encodePayload(encoderScript string) ([]byte, error) {
	// 读取测试数据
	testDataFile, err := os.Open(TEST_DATA_PATH)
	if err != nil {
		return nil, fmt.Errorf("open test data file error: %v", err)
	}
	defer testDataFile.Close()

	var testData map[string]interface{}
	if err := json.NewDecoder(testDataFile).Decode(&testData); err != nil {
		return nil, fmt.Errorf("decode test data error: %v", err)
	}

	// 创建新的 VM 实例
	vm := otto.New()

	// 执行编码器脚本,注册 Encode 函数
	if _, err := vm.Run(encoderScript); err != nil {
		return nil, fmt.Errorf("execute encoder script error: %v", err)
	}

	// 调用 Encode 函数
	encode, err := vm.Get("Encode")
	if err != nil {
		return nil, fmt.Errorf("get encode function error: %v", err)
	}

	result, err := encode.Call(otto.NullValue(), nil, testData)
	if err != nil {
		return nil, fmt.Errorf("call encode function error: %v", err)
	}

	// 将结果转换为字节数组
	exportedValue, err := result.Export()
	if err != nil {
		return nil, fmt.Errorf("export result error: %v", err)
	}

	var bytes []byte

	// 处理不同类型的返回值
	int32Slice, isInt32Slice := exportedValue.([]int32)
	if isInt32Slice {
		bytes = make([]byte, len(int32Slice))
		for i, num := range int32Slice {
			bytes[i] = byte(num)
		}
	} else {
		encodedBytes, ok := exportedValue.([]interface{})
		if !ok {
			return nil, fmt.Errorf("encodedBytes is not a []interface{}")
		}

		for _, byteVal := range encodedBytes {
			if num, ok := byteVal.(int32); ok {
				bytes = append(bytes, byte(num))
			} else if num, ok := byteVal.(int); ok {
				bytes = append(bytes, byte(num))
			} else if num, ok := byteVal.(int64); ok {
				bytes = append(bytes, byte(num))
			} else if num, ok := byteVal.(float64); ok {
				bytes = append(bytes, byte(num))
			} else if num, ok := byteVal.(float32); ok {
				bytes = append(bytes, byte(num))
			} else {
				return nil, fmt.Errorf("unexpected type for byte value")
			}
		}
	}

	return bytes, nil
}

// dataUp sends an data uplink.
func (d *Device) dataUp(mType lorawan.MType, ack bool) {
	d.dataUpCount++

	ecPath := CODEC_DIR + strings.ToLower(d.payloadCodec.Name) + "/" + strings.ToLower(d.payloadCodec.Name) + "-encoder.js"

	fileInfo, err := os.Stat(ecPath)
	if err != nil {
		log.Errorf("stat encoder file error: %v", err)
		return
	}

	if config.C.General.UseDynamicPayload && !d.encoderScriptFileModTime.Equal(fileInfo.ModTime()) {
		d.encoderScriptFileModTime = fileInfo.ModTime()

		file, err := os.Open(ecPath)
		if err != nil {
			log.Errorf("open encoder file error: %v", err)
			return
		}
		defer file.Close()
		encoderScript, _ := io.ReadAll(file)

		bytes, err := d.encodePayload(string(encoderScript))
		if err != nil {
			log.Errorf("encode payload error: %v", err)
			return
		}

		d.payload = bytes
	}

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

	return nil
}

// downlinkData validates and handles the downlink data.
func (d *Device) downlinkData(phy lorawan.PHYPayload) error {
	ok, err := phy.ValidateDownlinkDataMIC(lorawan.LoRaWAN1_0, 0, d.nwkSKey)
	if err != nil {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid downlink data MIC")
		return nil
	}

	if !ok {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid downlink data MIC")
		return nil
	}

	macPL, ok := phy.MACPayload.(*lorawan.MACPayload)
	if !ok {
		return fmt.Errorf("expected *lorawan.MACPayload, got: %T", phy.MACPayload)
	}

	gap := uint32(uint16(macPL.FHDR.FCnt) - uint16(d.fCntDown%(1<<16)))
	d.fCntDown = d.fCntDown + gap

	var data []byte
	var fPort uint8
	if macPL.FPort != nil {
		fPort = *macPL.FPort
	}

	if fPort != 0 {
		err := phy.DecryptFRMPayload(d.appSKey)
		if err != nil {
			return errors.Wrap(err, "decrypt frmpayload error")
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

	log.WithFields(log.Fields{
		"confirmed":     phy.MHDR.MType == lorawan.ConfirmedDataDown,
		"ack":           macPL.FHDR.FCtrl.ACK,
		"f_cnt":         d.fCntDown,
		"dev_eui":       d.devEUI,
		"f_port":        fPort,
		"data":          hex.EncodeToString(data),
		"datadownCount": d.datadownCount,
	}).Info("simulator: device received downlink data")

	if phy.MHDR.MType == lorawan.ConfirmedDataDown {
		d.dataUp(lorawan.UnconfirmedDataUp, true)
	}

	return nil
	// if d.downlinkHandlerFunc == nil {
	// 	return nil
	// }

	// return d.downlinkHandlerFunc(phy.MHDR.MType == lorawan.ConfirmedDataDown, macPL.FHDR.FCtrl.ACK, d.fCntDown, fPort, data)
}

// sendUplink sends
func (d *Device) sendUplink(phy lorawan.PHYPayload) error {
	b, err := phy.MarshalBinary()
	if err != nil {
		return errors.Wrap(err, "marshal phypayload error")
	}

	pl := LNSRXPacketBytes{
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
