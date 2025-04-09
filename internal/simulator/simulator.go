package simulator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mrand "math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/chirpstack-simulator/internal/as"
	"github.com/brocaar/chirpstack-simulator/internal/config"
	device "github.com/brocaar/chirpstack-simulator/internal/device"
	"github.com/brocaar/chirpstack-simulator/internal/gateway"
	"github.com/brocaar/chirpstack-simulator/internal/ns"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/gocarina/gocsv"
)

const (
	BACNET_SPECIFIC_WRITE_JSON = "bacnet_script/specific_write/objects.json"
	BACNET_FEATURE             = "bacnet"
	FUOTA_FEATURE              = "fuota"
	DEVICES_IMPORT_FILE        = "config/devices_import.csv"
)

// Start starts the simulator.
func LNSStart(ctx context.Context, wg *sync.WaitGroup, c config.Config) error {
	for i, c := range c.Simulator {
		log.WithFields(log.Fields{
			"i": i,
		}).Info("LNS simulator: starting LNSSimulation")

		wg.Add(1)

		pl, err := hex.DecodeString(c.Device.Payload)
		if err != nil {
			return errors.Wrap(err, "decode payload error")
		}

		sim := LNSSimulation{
			ctx:                  ctx,
			wg:                   wg,
			tenantID:             c.TenantID,
			deviceCount:          c.Device.Count,
			activationTime:       c.ActivationTime,
			uplinkInterval:       c.Device.UplinkInterval,
			fPort:                c.Device.FPort,
			payload:              pl,
			frequency:            c.Device.Frequency,
			bandwidth:            c.Device.Bandwidth,
			spreadingFactor:      c.Device.SpreadingFactor,
			duration:             c.Duration,
			gatewayMinCount:      c.Gateway.MinCount,
			gatewayMaxCount:      c.Gateway.MaxCount,
			deviceAppKeys:        make(map[lorawan.EUI64]lorawan.AES128Key),
			eventTopicTemplate:   c.Gateway.EventTopicTemplate,
			commandTopicTemplate: c.Gateway.CommandTopicTemplate,
			euiCodecMap:          make(map[lorawan.EUI64]as.PayloadCodecItem),
		}

		go sim.start()
	}

	return nil
}

type LNSSimulation struct {
	ctx             context.Context
	wg              *sync.WaitGroup
	tenantID        string
	deviceCount     int
	gatewayMinCount int
	gatewayMaxCount int
	duration        time.Duration

	fPort           uint8
	payload         []byte
	activationTime  time.Duration
	uplinkInterval  time.Duration
	frequency       int
	bandwidth       int
	spreadingFactor int

	deviceProfileID      uuid.UUID
	applicationID        string
	gatewayIDs           []lorawan.EUI64
	deviceAppKeys        map[lorawan.EUI64]lorawan.AES128Key
	eventTopicTemplate   string
	commandTopicTemplate string

	deviceProfiles []as.ProfileResultJson
	applications   []as.ApplicationJson
	payloadCodecs  []as.PayloadCodecItem

	euiCodecMap map[lorawan.EUI64]as.PayloadCodecItem
}

func (s *LNSSimulation) start() {

	if err := s.init(); err != nil {
		log.WithError(err).Error("simulator: init LNSSimulation error")
	}

	if err := s.runSimulation(); err != nil {
		log.WithError(err).Error("simulator: LNSSimulation error")
	}

	log.Info("simulator: LNSSimulation completed")

	if err := s.tearDown(); err != nil {
		log.WithError(err).Error("simulator: tear-down LNSSimulation error")
	}

	s.wg.Done()

	log.Info("LNSSimulation: tear-down completed")
}

func (s *LNSSimulation) init() error {
	log.Info("LNSSimulation: setting up")

	if config.C.ChirpStack.API.RestartAs {
		as.RestartAppServer()
	}

	if err := as.DeleteAllDevices(); err != nil {
		return err
	}

	if err := s.setupGateways(); err != nil {
		return err
	}

	if err := s.setupDeviceProfile(); err != nil {
		return err
	}

	if err := s.setupApplication(); err != nil {
		return err
	}

	if err := s.setupPayloadCodec(); err != nil {
		return err
	}

	if err := s.setupDevices(); err != nil {
		return err
	}

	if err := s.setupBACnet(); err != nil {
		return err
	}

	return nil
}

func (s *LNSSimulation) tearDown() error {
	log.Info("LNSSimulation: cleaning up")

	if err := s.tearDownDevices(); err != nil {
		return err
	}

	if err := s.tearDownApplication(); err != nil {
		return err
	}

	if err := s.tearDownDeviceProfile(); err != nil {
		return err
	}

	if err := s.tearDownGateways(); err != nil {
		return err
	}

	return nil
}

func (s *LNSSimulation) runSimulation() error {
	var gateways []*gateway.Gateway
	var devices []*device.Device

	for _, gatewayID := range s.gatewayIDs {
		gw, err := gateway.NewGateway(
			gateway.WithGatewayID(gatewayID),
			gateway.WithMQTTClient(ns.Client()),
			gateway.WithEventTopicTemplate(s.eventTopicTemplate),
			gateway.WithCommandTopicTemplate(s.commandTopicTemplate),
		)
		if err != nil {
			return errors.Wrap(err, "new gateway error")
		}
		gateways = append(gateways, gw)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(s.ctx)
	if s.duration != 0 {
		ctx, cancel = context.WithTimeout(ctx, s.duration)
	}
	defer cancel()

	for devEUI, appKey := range s.deviceAppKeys {
		var gws []*gateway.Gateway
		if config.C.ChirpStack.API.UseNewGateway {
			devGateways := make(map[int]*gateway.Gateway)
			devNumGateways := s.gatewayMinCount + mrand.Intn(s.gatewayMaxCount-s.gatewayMinCount+1)

			for len(devGateways) < devNumGateways {
				// pick random gateway index
				n := mrand.Intn(len(gateways))
				devGateways[n] = gateways[n]
			}

			for k := range devGateways {
				gws = append(gws, devGateways[k])
			}
		} else {
			gws = gateways
		}

		zeroDuration := time.Duration(0)
		otaaDuration := time.Duration(0)
		if s.activationTime != zeroDuration {
			otaaDuration = time.Duration(mrand.Int63n(int64(s.activationTime)))
		}

		d, err := device.NewDevice(ctx, &wg,
			device.WithDevEUI(devEUI),
			device.WithAppKey(appKey),
			device.WithUplinkInterval(s.uplinkInterval),
			device.WithOTAADelay(otaaDuration),
			device.WithUplinkPayload(true, s.fPort, s.payload),
			device.WithGateways(gws),
			device.WithUplinkTXInfo(&gw.UplinkTxInfo{
				Frequency: uint32(s.frequency),
				Modulation: &gw.Modulation{
					Parameters: &gw.Modulation_Lora{
						Lora: &gw.LoraModulationInfo{
							Bandwidth:       uint32(s.bandwidth),
							SpreadingFactor: uint32(s.spreadingFactor),
							CodeRate:        gw.CodeRate_CR_4_5,
						},
					},
				},
			}),
			device.WithPayloadCodec(s.euiCodecMap[devEUI]),
		)
		if err != nil {
			return errors.Wrap(err, "new device error")
		}

		devices = append(devices, d)
	}

	log.Info("len(devices): ", len(devices))

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		select {
		case sig := <-sigChan:
			log.WithField("signal", sig).Info("signal received, stopping simulators")
			cancel()
		case <-ctx.Done():
		}
	}()

	wg.Wait()

	return nil
}

func (s *LNSSimulation) setupGateways() error {
	log.Info("simulator: creating gateways")

	if config.C.ChirpStack.API.UseNewGateway {
		for i := 0; i < s.gatewayMaxCount; i++ {
			var gatewayID lorawan.EUI64
			if _, err := rand.Read(gatewayID[:]); err != nil {
				return errors.Wrap(err, "read random bytes error")
			}

			err := as.CreateGateway(gatewayID.String(), gatewayID.String())

			if err != nil {
				return errors.Wrap(err, "create gateway error")
			}

			s.gatewayIDs = append(s.gatewayIDs, gatewayID)
		}

		return nil
	}

	macs, err := as.GetGateway()
	if err != nil {
		return errors.Wrap(err, "get gateway error")
	}

	s.gatewayIDs = macs

	return nil
}

func (s *LNSSimulation) tearDownGateways() error {
	log.Info("simulator: tear-down gateways")
	if !config.C.ChirpStack.API.UseNewGateway {
		return nil
	}

	for _, gatewayID := range s.gatewayIDs {
		err := as.DeleteGateway(gatewayID.String())
		if err != nil {
			return errors.Wrap(err, "delete gateway error")
		}
	}
	return nil
}

func (s *LNSSimulation) setupDeviceProfile() error {
	log.Info("simulator: creating device-profile")

	if config.C.ChirpStack.API.UseNewProfile {
		profileId, err := as.LNSCreateDeviceProfile()
		if err != nil {
			return errors.Wrap(err, "create device-profile error")
		}

		dpID, err := uuid.FromString(profileId)
		if err != nil {
			return err
		}
		s.deviceProfileID = dpID

		return nil
	}

	profiles, err := as.GetProfiles()
	if err != nil {
		return err
	}

	s.deviceProfiles = profiles

	return nil
}

func (s *LNSSimulation) tearDownDeviceProfile() error {
	log.Info("simulator: tear-down device-profile")

	if !config.C.ChirpStack.API.UseNewProfile {
		return nil
	}

	err := as.DeleteDeviceProfile(s.deviceProfileID.String())
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func (s *LNSSimulation) setupApplication() error {
	log.Info("simulator: init application")

	apps, err := as.GetApplications()
	if err != nil {
		return err
	}

	s.applications = apps

	for _, app := range apps {
		if app.Name == as.APPLICATION_NAME {
			s.applicationID = app.ID
			return nil
		}
	}

	if config.C.ChirpStack.API.UseNewApp {
		id, err := as.CreateApplication()
		if err != nil {
			return errors.Wrap(err, "create applicaiton error")
		}

		s.applicationID = id

		apps, err = as.GetApplications()
		if err != nil {
			return err
		}
		s.applications = apps
	}

	return nil
}

func (s *LNSSimulation) tearDownApplication() error {
	log.Info("simulator: tear-down application")

	if !config.C.ChirpStack.API.UseNewApp {
		return nil
	}

	err := as.DeleteApplication(s.applicationID)
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func generateRandomString() string {
	randomBytes := make([]byte, 16)
	_, err := rand.Read(randomBytes)
	if err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(randomBytes)
}

type Device struct {
	DevEUI        string `csv:"deveui"`
	Name          string `csv:"name"`
	Description   string `csv:"description"`
	Application   string `csv:"application"`
	DeviceProfile string `csv:"deviceprofile"`
	PayloadCodec  string `csv:"payloadcodec"`
	FPort         string `csv:"fport"`
	AppKey        string `csv:"appkey"`
	DevAddr       string `csv:"devaddr"`
	NwkSKey       string `csv:"nwkskey"`
	AppSKey       string `csv:"appskey"`
}

func generateDevices(num int) error {
	srcFile, err := os.Open("config/base_devices_export.csv")
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer srcFile.Close()

	var baseDevices []Device
	if err := gocsv.UnmarshalFile(srcFile, &baseDevices); err != nil {
		return fmt.Errorf("解析源CSV文件失败: %w", err)
	}
	if len(baseDevices) == 0 {
		return fmt.Errorf("基础设备模板为空")
	}

	baseDevice := baseDevices[0]
	baseEUI, err := strconv.ParseUint(baseDevice.DevEUI, 16, 64)
	if err != nil {
		return fmt.Errorf("解析基础DevEUI失败: %w", err)
	}

	devices := make([]Device, num)
	for i := 0; i < num; i++ {
		newEUI := baseEUI + uint64(i+1)
		newEUIStr := fmt.Sprintf("%016x", newEUI)
		newName := fmt.Sprintf("%s-%d", baseDevice.PayloadCodec, i+1)

		devices[i] = Device{
			DevEUI:        newEUIStr,
			Name:          newName,
			Description:   newEUIStr,
			Application:   baseDevice.Application,
			DeviceProfile: baseDevice.DeviceProfile,
			PayloadCodec:  baseDevice.PayloadCodec,
			FPort:         baseDevice.FPort,
			AppKey:        generateRandomString(),
			DevAddr:       baseDevice.DevAddr,
			NwkSKey:       baseDevice.NwkSKey,
			AppSKey:       baseDevice.AppSKey,
		}
	}

	dstFile, err := os.Create(DEVICES_IMPORT_FILE)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dstFile.Close()

	if err := gocsv.MarshalFile(&devices, dstFile); err != nil {
		return fmt.Errorf("写入CSV文件失败: %w", err)
	}

	return nil
}

func (s *LNSSimulation) setupDevices() error {
	log.Info("simulator: init devices")

	if err := generateDevices(s.deviceCount); err != nil {
		panic(err)
	}

	ret, records := as.LoadLoRaWANDevCfg(DEVICES_IMPORT_FILE, 1)
	if !ret {
		return errors.Errorf("failed to setupDevices")
	}

	for _, ldcfg := range records {
		profileID := ""
		for _, p := range s.deviceProfiles {
			if p.Name == ldcfg.DeviceProfile {
				profileID = p.Profile.ProfileID
				break
			}
		}

		if profileID == "" {
			return errors.Errorf("can not find device profile: %s", ldcfg.DeviceProfile)
		}

		applicationID := ""
		for _, a := range s.applications {
			if a.Name == ldcfg.Application {
				applicationID = a.ID
				break
			}
		}

		if applicationID == "" {
			return errors.Errorf("can not find application: %s", ldcfg.Application)
		}

		payloadCodecID := ""
		var codec as.PayloadCodecItem
		for _, c := range s.payloadCodecs {
			if c.Name == ldcfg.PayloadCodec {
				payloadCodecID = c.ID
				codec = c
				break
			}
		}

		if payloadCodecID == "" {
			return errors.Errorf("can not find payload codec: %s", ldcfg.PayloadCodec)
		}

		eui := ldcfg.DevEUI
		appKey := ldcfg.AppKey
		name := ldcfg.Name

		err := as.CreateDevices(eui, name, profileID, appKey, payloadCodecID, applicationID)

		if err != nil {
			log.Error(err)
		}

		var devEUI lorawan.EUI64
		var appKeyAES lorawan.AES128Key

		devEUI.UnmarshalText([]byte(eui))
		appKeyAES.UnmarshalText([]byte(appKey))

		s.deviceAppKeys[devEUI] = appKeyAES
		s.euiCodecMap[devEUI] = codec
	}

	return nil
}

func (s *LNSSimulation) tearDownDevices() error {
	log.Info("simulator: tear-down devices")

	for k := range s.deviceAppKeys {
		err := as.DeleteDevices(k.String())
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

func (s *LNSSimulation) setupPayloadCodec() error {
	log.Info("simulator: creating gateways")

	codecs, err := as.GetPayloadCoedc()
	if err != nil {
		return nil
	}

	s.payloadCodecs = codecs

	return nil
}

func (s *LNSSimulation) setupBACnet() error {
	log.Info("simulator: creating BACnet objects")

	if config.C.ChirpStack.API.TestFeature != "bacnet" {
		return nil
	}

	objects, err := as.GetAvailableBACnetObjects("", "asc", 0, 1)
	if err != nil {
		return err
	}

	// save objects to a json file BACNET_SPECIFIC_WRITE_JSON
	jsonData, err := json.Marshal(objects)
	if err != nil {
		return err
	}
	os.WriteFile(BACNET_SPECIFIC_WRITE_JSON, jsonData, 0644)

	log.Info("total count: ", objects.Total)

	const MAX_ADD_DATUM = 20

	for i := 0; i < int(objects.Total); i += MAX_ADD_DATUM {
		log.Info("add from ", i, " to ", i+MAX_ADD_DATUM)
		objects, err := as.GetAvailableBACnetObjects("", "asc", i, MAX_ADD_DATUM)
		if err != nil {
			return err
		}

		testDataFile, err := os.Open(device.TEST_DATA_PATH)
		if err != nil {
			return err
		}
		defer testDataFile.Close()

		var testData map[string]interface{}
		if err := json.NewDecoder(testDataFile).Decode(&testData); err != nil {
			return err
		}

		// 创建一个 map 来存储测试数据的键，提高查找效率
		testDataKeys := make(map[string]struct{})
		for k := range testData {
			testDataKeys[k] = struct{}{}
		}

		for i := range objects.Data {
			newObjs := []as.BACnetObject{}
			for _, obj := range objects.Data[i].Objects {
				if _, exists := testDataKeys[obj.LoraName]; exists {
					newObjs = append(newObjs, obj)
				}
			}
			objects.Data[i].Objects = newObjs
		}

		err = as.AddBACnetObjects(objects.Data)
		if err != nil {
			log.Error(err)
			continue
		}
		log.Info("added ", len(objects.Data), " objects")
	}

	return nil
}
