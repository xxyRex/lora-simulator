package simulator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	mrand "math/rand"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/lora-simulator/internal/as"
	"github.com/brocaar/lora-simulator/internal/as_api/models"
	"github.com/brocaar/lora-simulator/internal/config"
	device "github.com/brocaar/lora-simulator/internal/device"
	"github.com/brocaar/lora-simulator/internal/gateway"
	"github.com/brocaar/lora-simulator/internal/ns"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/gocarina/gocsv"
)

const (
	BACNET_SPECIFIC_WRITE_JSON = "bacnet_script/specific_write/objects.json"
	BACNET_FEATURE             = "bacnet"
	FUOTA_FEATURE              = "fuota"
	DEVICES_IMPORT_FILE        = "config/devices_import.csv"
	FUOTA_REQ_FILE             = "api/fuota_req.json"
	MODBUS_SERVER_CREATE_FILE  = "api/modbus_server_create.json"
	DEVICE_STORED_INFO_FILE    = "temp/device_stored_info.json"
)

// Start starts the simulator.
func Start(ctx context.Context, wg *sync.WaitGroup, c config.Config) error {
	for i, c := range c.Simulator {
		log.WithFields(log.Fields{
			"i": i,
		}).Info("simulator: starting Simulation")

		wg.Add(1)

		pl, err := hex.DecodeString(c.Device.Payload)
		if err != nil {
			return errors.Wrap(err, "decode payload error")
		}

		sim := Simulation{
			ctx:                  ctx,
			wg:                   wg,
			tenantID:             c.TenantID,
			deviceCount:          c.Device.Count,
			activationTime:       c.ActivationTime,
			sequenceJoin:         c.SequenceJoin,
			sequenceJoinInterval: c.SequenceJoinInterval,
			sequenceDeviceNumber: c.SequenceDeviceNumber,
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
			euiCodecMap:          make(map[lorawan.EUI64]*models.APIPayloadCodecItem),
		}

		go sim.start()
	}

	return nil
}

type Simulation struct {
	ctx             context.Context
	wg              *sync.WaitGroup
	tenantID        string
	deviceCount     int
	gatewayMinCount int
	gatewayMaxCount int
	duration        time.Duration

	fPort                uint8
	payload              []byte
	sequenceJoin         bool
	sequenceJoinInterval time.Duration
	sequenceDeviceNumber int
	activationTime       time.Duration
	uplinkInterval       time.Duration
	frequency            int
	bandwidth            int
	spreadingFactor      int

	deviceProfileID      uuid.UUID
	applicationID        string
	gatewayIDs           []lorawan.EUI64
	deviceAppKeys        map[lorawan.EUI64]lorawan.AES128Key
	eventTopicTemplate   string
	commandTopicTemplate string

	deviceProfiles []*models.APIProfileData
	applications   []*models.APIAppListItem
	payloadCodecs  []*models.APIPayloadCodecItem

	euiCodecMap map[lorawan.EUI64]*models.APIPayloadCodecItem
}

func (s *Simulation) start() {

	if err := s.init(); err != nil {
		log.WithError(err).Error("simulator: init Simulation error")
	}

	if err := s.runSimulation(); err != nil {
		log.WithError(err).Error("simulator: Simulation error")
	}

	log.Info("simulator: Simulation completed")

	if err := s.tearDown(); err != nil {
		log.WithError(err).Error("simulator: tear-down Simulation error")
	}

	s.wg.Done()

	log.Info("Simulation: tear-down completed")
}

func (s *Simulation) init() error {
	log.Info("Simulation: setting up")

	if config.C.ChirpStack.API.RestartAs {
		as.RestartAppServer()
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

	if config.C.ChirpStack.API.UseNewDevice {
		if err := as.DeleteAllDevices(); err != nil {
			return err
		}

		if err := s.createDevices(); err != nil {
			return err
		}

		if err := s.setupBACnet(); err != nil {
			return err
		}

		if err := s.setupModbus(); err != nil {
			return err
		}

		go s.setupFuota()
	}

	if err := s.setupDevices(); err != nil {
		return err
	}

	return nil
}

func (s *Simulation) tearDown() error {
	log.Info("Simulation: cleaning up")

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

func (s *Simulation) runSimulation() error {
	if config.C.ChirpStack.API.WaitDeviceStableTime != 0 {
		log.Infof("wait device stable time: %v", config.C.ChirpStack.API.WaitDeviceStableTime)
		time.Sleep(config.C.ChirpStack.API.WaitDeviceStableTime)
	}

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

	count := 0
	batchCount := 0
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

		var otaaDuration time.Duration
		if s.sequenceJoin {
			otaaDuration = time.Duration(int64(s.sequenceJoinInterval/time.Second)*int64(batchCount)) * time.Second
			count++
			batchCount = count / s.sequenceDeviceNumber
			log.Infof("deveui: %v otaaDuration: %v", devEUI, otaaDuration)
		} else {
			otaaDuration = time.Duration(mrand.Int63n(int64(s.activationTime)))
		}

		deviceResult := []*models.APIDeviceItem{}
		if !config.C.ChirpStack.API.UseNewDevice {
			for i := 0; i < s.deviceCount; i += 25 {
				ret, err := as.GetDevices(i, 25)
				if err != nil {
					return errors.Wrap(err, "get devices error")
				}
				deviceResult = append(deviceResult, ret...)
			}
		}
		devRetMap := make(map[string]*models.APIDeviceItem)
		for i := range deviceResult {
			devRetMap[strings.ToLower(deviceResult[i].DevEUI)] = deviceResult[i]
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
			device.WithDeviceStoredInfo(devRetMap[strings.ToLower(devEUI.String())]),
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

func (s *Simulation) setupGateways() error {
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

func (s *Simulation) tearDownGateways() error {
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

func (s *Simulation) setupDeviceProfile() error {
	log.Info("simulator: creating device-profile")

	if config.C.ChirpStack.API.UseNewProfile {
		profileId, err := as.CreateDeviceProfile()
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

func (s *Simulation) tearDownDeviceProfile() error {
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

func (s *Simulation) setupApplication() error {
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

func (s *Simulation) tearDownApplication() error {
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

func (s *Simulation) createDevices() error {
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
		for _, c := range s.payloadCodecs {
			if c.Name == ldcfg.PayloadCodec {
				payloadCodecID = c.ID
				break
			}
		}

		if payloadCodecID == "" {
			return errors.Errorf("can not find payload codec: %s", ldcfg.PayloadCodec)
		}

		err := as.CreateDevices(ldcfg.DevEUI, ldcfg.Name, profileID, ldcfg.AppKey, payloadCodecID, applicationID)

		if err != nil {
			log.Error(err)
		}
	}

	return nil
}

func (s *Simulation) setupDevices() error {
	log.Info("simulator: init devices")

	ret, records := as.LoadLoRaWANDevCfg(DEVICES_IMPORT_FILE, 1)
	if !ret {
		return errors.Errorf("failed to setupDevices")
	}

	for _, ldcfg := range records {

		eui := ldcfg.DevEUI
		appKey := ldcfg.AppKey

		var devEUI lorawan.EUI64
		var appKeyAES lorawan.AES128Key

		var codec *models.APIPayloadCodecItem
		for _, c := range s.payloadCodecs {
			if c.Name == ldcfg.PayloadCodec {
				codec = c
				break
			}
		}

		devEUI.UnmarshalText([]byte(eui))
		appKeyAES.UnmarshalText([]byte(appKey))

		s.deviceAppKeys[devEUI] = appKeyAES
		s.euiCodecMap[devEUI] = codec
	}

	return nil
}

func (s *Simulation) tearDownDevices() error {
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

func (s *Simulation) setupPayloadCodec() error {
	log.Info("simulator: creating payload codecs")

	codecs, err := as.GetPayloadCoedc()
	if err != nil {
		return nil
	}

	s.payloadCodecs = codecs

	return nil
}

func (s *Simulation) setupBACnet() error {
	log.Info("simulator: creating BACnet objects")

	if !strings.Contains(config.C.ChirpStack.API.TestFeature, "bacnet") {
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

	const MAX_ADD_DATUM = 30

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

		for k := range objects.Data {
			newObjs := []*models.APIPCO{}
			for _, obj := range objects.Data[k].Objects {
				if _, exists := testDataKeys[obj.LoraName]; exists {
					newObjs = append(newObjs, obj)
				}
			}
			objects.Data[k].Objects = newObjs
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

func (s *Simulation) setupFuota() error {
	log.Info("simulator: creating FUOTA task")

	if !strings.Contains(config.C.ChirpStack.API.TestFeature, "fuota") || config.C.ChirpStack.API.FuotaTaskDeviceCount == 0 {
		return nil
	}
	// 删除所有已存在的fuota任务
	log.Info("setupFuota deleting all existing fuota tasks")
	fuotaTasksRes, err := as.GetFuotaTask("", "asc", 0, math.MaxInt32)
	if err != nil {
		return err
	}

	deleteIDs := []int32{}
	for _, task := range fuotaTasksRes.Tasks {
		deleteIDs = append(deleteIDs, task.ID)
	}

	err = as.DeleteFuotaTask(deleteIDs)
	if err != nil {
		return err
	}

	// 等待所有节点入网后
	for {
		if device.GetJoinAcceptCount() == s.deviceCount {
			break
		}
		log.Info("setupFuota waiting for all nodes to join")
		time.Sleep(10 * time.Second)
	}

	jsonFile, err := os.Open(FUOTA_REQ_FILE)
	if err != nil {
		return err
	}
	defer jsonFile.Close()

	var fuotaTaskReqTemplate *models.APIFuotaTask
	if err := json.NewDecoder(jsonFile).Decode(&fuotaTaskReqTemplate); err != nil {
		return err
	}

	taskCount := 0
	allDeveuiList := []string{}
	for deveui := range s.deviceAppKeys {
		allDeveuiList = append(allDeveuiList, deveui.String())
	}

	sort.Strings(allDeveuiList)

	deveuiList := []string{}
	for _, deveui := range allDeveuiList {
		deveuiList = append(deveuiList, deveui)
		if len(deveuiList) == config.C.ChirpStack.API.FuotaTaskDeviceCount {
			var fuotaTaskReq *models.APIFuotaTask = fuotaTaskReqTemplate
			fuotaTaskReq.Name = fuotaTaskReq.Name + "-" + strconv.Itoa(taskCount)
			fuotaTaskReq.Deveui = deveuiList
			err = as.CreateFuotaTask(fuotaTaskReq)
			if err != nil {
				log.Error(err)
				return err
			}
			taskCount++
			log.Infof("setupFuota created fuota task: %s, deveuis: %v", fuotaTaskReq.Name, deveuiList)
			deveuiList = []string{}
		}
	}

	return nil
}

func (s *Simulation) setupModbus() error {
	log.Info("simulator: creating modbus servers")

	if !strings.Contains(config.C.ChirpStack.API.TestFeature, "modbus") {
		return nil
	}

	modbusServers, err := as.GetModbusServer(as.ModbusGetServerReq{
		ID:       int64(1),
		Execute:  int64(1),
		Core:     "yruo_modbus_slave",
		Function: "get",
		Values: []as.ModbusGetServerReqValue{
			{
				Base:   "server",
				Search: "",
				Order:  "asc",
				Offset: 0,
				Limit:  math.MaxInt32,
			},
		},
	})

	if err != nil {
		return err
	}

	if len(modbusServers.Result) > 0 {
		id := 0
		for _, server := range modbusServers.Result[0].Servers {
			ids := []string{}
			ids = append(ids, server.ID)
			err = as.DeleteModbusServer(as.ModbusServerDeleteReq{
				ID:       int64(id),
				Execute:  int64(1),
				Core:     "yruo_modbus_slave",
				Function: "del",
				Values: []as.ModbusServerDeleteReqValue{
					{
						Base: "server",
						IDS:  ids,
					},
				},
			})

			if err != nil {
				log.Error("failed to delete modbus servers: ", err)
				return err
			}

			log.Info("deleted modbus servers: ", ids)
			id += 1
		}
	}

	for k := 0; k < 10; k++ {
		modbusServerCreateReq := as.ModbusServerCreateReq{
			ID:       int64(k + 1),
			Execute:  int64(1),
			Core:     "yruo_modbus_slave",
			Function: "add",
			Values: []as.ModbusServerCreateReqValue{
				{
					Base: "server",
					Servers: []as.ModbusServerCreateReqServer{
						{
							Enable:      1,
							Interface:   "eth 0",
							ConnectType: "modbus_tcp",
							Name:        "test" + strconv.Itoa(k),
							Port:        10000 + int64(k),
							SlaveID:     int64(k),
							Description: "test" + strconv.Itoa(k),
						},
					},
				},
			},
		}

		err = as.CreateModbusServer(modbusServerCreateReq)
		if err != nil {
			log.Error("failed to create modbus servers: ", err)
			return err
		}

		log.Info("created modbus servers: ", modbusServerCreateReq.Values[0].Servers)
	}

	modbusServers, err = as.GetModbusServer(as.ModbusGetServerReq{
		ID:       int64(1),
		Execute:  int64(1),
		Core:     "yruo_modbus_slave",
		Function: "get",
		Values: []as.ModbusGetServerReqValue{
			{
				Base:   "server",
				Search: "",
				Order:  "asc",
				Offset: 0,
				Limit:  math.MaxInt32,
			},
		},
	})

	if err != nil {
		return err
	}

	log.Infof("modbus servers: %v", modbusServers)

	serverIDs := []string{}
	if len(modbusServers.Result) < 1 || len(modbusServers.Result[0].Servers) < 1 {
		log.Error("no modbus server found")
		return errors.New("no modbus server found")
	}

	for _, server := range modbusServers.Result[0].Servers {
		serverIDs = append(serverIDs, server.ID)
	}

	log.Infof("modbus server ids: %v", serverIDs)

	modbusObjects, err := as.GetAllAvaliableModbusObjects(&models.APIGetModbusObjectRequest{
		ServerID: "0",
		Limit:    int32(1),
		Offset:   0,
		Search:   "",
	})

	if err != nil {
		log.Error("failed to get modbus objects: ", err)
		return err
	}

	const MAX_ADD_DATUM = 30

	serverIdIndex := 0
	for i := 0; i < int(modbusObjects.Total); i += MAX_ADD_DATUM {
		log.Infof("add from %d to %d", i, i+MAX_ADD_DATUM)
		modbusObjects, err := as.GetAllAvaliableModbusObjects(&models.APIGetModbusObjectRequest{
			ServerID: serverIDs[serverIdIndex],
			Limit:    int32(MAX_ADD_DATUM),
			Offset:   int32(i),
			Search:   "",
		})

		if err != nil {
			log.Error("failed to get modbus objects: ", err)
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

		testDataKeys := make(map[string]struct{})
		for k := range testData {
			testDataKeys[k] = struct{}{}
		}

		for k := range modbusObjects.Data {
			newObjs := []*models.APIObject{}
			for _, o := range modbusObjects.Data[k].Objects {
				if _, exists := testDataKeys[o.LoraName]; exists {
					newObjs = append(newObjs, o)
				}
			}
			modbusObjects.Data[k].Objects = newObjs
		}

		err = as.AddModbusDatum(&models.APIAddModbusObjectRequest{
			ServerID: serverIDs[serverIdIndex],
			Data:     modbusObjects.Data,
		})

		if err != nil {
			log.Error("failed to add modbus datum: ", err)
			return err
		}

		log.Infof("added %d modbus datum", len(modbusObjects.Data))
		serverIdIndex++
		serverIdIndex = serverIdIndex % len(serverIDs)
	}

	return nil
}
