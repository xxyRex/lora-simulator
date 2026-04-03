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
	"github.com/brocaar/lora-simulator/internal/deviceconfig"
	"github.com/brocaar/lora-simulator/internal/gateway"
	"github.com/brocaar/lora-simulator/internal/ns"
	"github.com/brocaar/lora-simulator/internal/test_payload_codec"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

const (
	BACNET_SPECIFIC_WRITE_JSON = "bacnet_script/specific_write/objects.json"
	BACNET_FEATURE             = "bacnet"
	FUOTA_FEATURE              = "fuota"
	FUOTA_REQ_FILE             = "api/fuota_req.json"
	MODBUS_SERVER_CREATE_FILE  = "api/modbus_server_create.json"
	DEVICE_STORED_INFO_FILE    = "temp/device_stored_info.json"
	SIMULATION_CONFIG_FILE     = "config/simulation-config.json"
	DEVICES_JSON_FILE          = "payload_en_decoder/codec-release/vendors/milesight-iot/devices.json"
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
			waitDeviceStableTime: c.Device.WaitDeviceStableTime,
			duration:             c.Duration,
			gatewayMinCount:      c.Gateway.MinCount,
			gatewayMaxCount:      c.Gateway.MaxCount,
			eventTopicTemplate:   c.Gateway.EventTopicTemplate,
			commandTopicTemplate: c.Gateway.CommandTopicTemplate,
		}

		go sim.start()
	}

	return nil
}

type Simulation struct {
	ctx             context.Context
	wg              *sync.WaitGroup
	tenantID        string
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
	waitDeviceStableTime time.Duration

	deviceProfileID      uuid.UUID
	applicationID        string
	gatewayIDs           []lorawan.EUI64
	eventTopicTemplate   string
	commandTopicTemplate string

	deviceProfiles []*models.APIProfileData
	applications   []*models.APIAppListItem
	payloadCodecs  []*models.APIPayloadCodecItem

	// New fields for multi-device type support
	deviceConfigLoader *deviceconfig.DeviceConfigLoader
	deviceTypeRegistry *deviceconfig.DeviceTypeRegistry
	deviceInstances    []*deviceconfig.DeviceInstance

	// In-memory device list (no CSV file needed)
	generatedDevices []Device
}

func (s *Simulation) start() {

	if err := s.init(); err != nil {
		log.WithError(err).Error("simulator: init Simulation error")
	}

	if config.C.LoraSimulator.API.ApiTest {
		go s.ApiTest()
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

	if config.C.LoraSimulator.API.RestartAs {
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

	if config.C.LoraSimulator.API.UseNewDevice {
		if err := as.DeleteAllDevices(); err != nil {
			log.WithError(err).Warn("simulator: failed to delete all devices, continuing anyway")
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

		go func() {
			if err := s.setupFuota(); err != nil {
				log.WithError(err).Error("setupFuota failed")
			}
		}()
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
	if s.waitDeviceStableTime != 0 {
		log.Infof("wait device stable time: %v", s.waitDeviceStableTime)
		time.Sleep(s.waitDeviceStableTime)
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
	for i, dev := range s.generatedDevices {
		var gws []*gateway.Gateway
		gws = gateways

		var otaaDuration time.Duration
		if s.sequenceJoin {
			otaaDuration = time.Duration(int64(s.sequenceJoinInterval/time.Second)*int64(batchCount)) * time.Second
			count++
			batchCount = count / s.sequenceDeviceNumber
			log.Infof("deveui: %v otaaDuration: %v", dev.DevEUI, otaaDuration)
		} else {
			otaaDuration = time.Duration(mrand.Int63n(int64(s.activationTime)))
		}

		// Build device options
		deviceOpts := []device.DeviceOption{
			device.WithDevEUI(dev.DevEUI),
			device.WithAppKey(dev.AppKey),
			device.WithUplinkInterval(s.uplinkInterval),
			device.WithOTAADelay(otaaDuration),
			device.WithUplinkPayload(true, s.fPort, s.payload),
			device.WithGateways(gws),
			device.WithDeviceIndex(i),
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
			device.WithDeviceTypeConfig(dev.DeviceTypeConfig),
		}

		// Override uplink settings from device instance (if using multi-type simulation)
		// Uplink config is now per-device-type in simulation-config.json
		if s.deviceInstances != nil {
			for _, instance := range s.deviceInstances {
				if strings.ToLower(instance.DevEUI) == strings.ToLower(dev.DevEUI.String()) {
					if instance.UplinkInterval > 0 {
						deviceOpts = append(deviceOpts, device.WithUplinkInterval(instance.UplinkInterval))
					}
					// Pass per-device-type uplink configuration
					deviceOpts = append(deviceOpts, device.WithUplinkPaused(instance.UplinkPaused))
					deviceOpts = append(deviceOpts, device.WithUplinkConfirm(instance.UplinkConfirm))
					// Pass overridden test data (from test_data_override in simulation-config.json)
					if len(instance.TestData) > 0 {
						deviceOpts = append(deviceOpts, device.WithDeviceTestData(instance.TestData))
					}
					break
				}
			}
		}

		d, err := device.NewDevice(ctx, &wg, deviceOpts...)
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

	macs, err := as.GetGateway()
	if err != nil {
		return errors.Wrap(err, "get gateway error")
	}

	s.gatewayIDs = macs

	return nil
}

func (s *Simulation) tearDownGateways() error {
	log.Info("simulator: tear-down gateways")
	// 使用现有网关，不需要删除
	return nil
}

func (s *Simulation) setupDeviceProfile() error {
	log.Info("simulator: creating device-profile")

	profiles, err := as.GetProfiles()
	if err != nil {
		return err
	}

	s.deviceProfiles = profiles

	return nil
}

func (s *Simulation) tearDownDeviceProfile() error {
	log.Info("simulator: tear-down device-profile")
	// 使用现有配置文件，不需要删除
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

	return nil
}

func (s *Simulation) tearDownApplication() error {
	log.Info("simulator: tear-down application")

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

// Device represents a device configuration (used in memory, no CSV needed)
type Device struct {
	DevEUI           lorawan.EUI64
	Name             string
	Description      string
	Application      string
	DeviceProfile    string
	PayloadCodec     string
	FPort            string
	AppKey           lorawan.AES128Key
	DevAddr          lorawan.DevAddr
	NwkSKey          lorawan.AES128Key
	AppSKey          lorawan.AES128Key
	DeviceTypeConfig *deviceconfig.DeviceTypeConfig
}

// generateMultiTypeDevices generates devices from simulation-config.json supporting multiple device types
// All device configuration is derived from devices.json - no CSV file needed
func (s *Simulation) generateMultiTypeDevices() error {
	// Check if simulation config file exists
	if _, err := os.Stat(SIMULATION_CONFIG_FILE); os.IsNotExist(err) {
		return fmt.Errorf("simulation-config.json not found")
	}

	// Load device config - use BaseDir (original CWD before any --workdir chdir) to locate shared resources
	loader := deviceconfig.NewDeviceConfigLoader(config.BaseDir)
	if err := loader.Load(); err != nil {
		return fmt.Errorf("failed to load device config: %w", err)
	}

	s.deviceConfigLoader = loader
	s.deviceTypeRegistry = loader.GetRegistry()

	// Generate device instances
	instances, err := loader.GenerateDeviceInstances()
	if err != nil {
		return fmt.Errorf("failed to generate device instances: %w", err)
	}
	s.deviceInstances = instances

	log.Infof("Generated %d device instances from simulation config", len(instances))

	// Determine default application name
	defaultAppName := as.APPLICATION_NAME
	if len(s.applications) > 0 {
		defaultAppName = s.applications[0].Name
	}

	// Generate devices from instances using devices.json info directly (stored in memory)
	s.generatedDevices = nil
	for _, instance := range instances {
		// Get device type config from the instance itself
		deviceTypeConfig := instance.DeviceType
		if deviceTypeConfig == nil {
			log.Warnf("device type config not found for %s, skipping", instance.Name)
			continue
		}

		// Determine device profile from devices.json
		deviceProfile := "ClassA-OTAA"
		if len(deviceTypeConfig.DeviceProfile) > 0 {
			deviceProfile = deviceTypeConfig.DeviceProfile[0]
		}

		// Determine fPort from devices.json
		fPort := "85"
		if deviceTypeConfig.DefaultFPort > 0 {
			fPort = strconv.Itoa(deviceTypeConfig.DefaultFPort)
		}

		// Convert string DevEUI to lorawan.EUI64
		var devEUI lorawan.EUI64
		if err := devEUI.UnmarshalText([]byte(instance.DevEUI)); err != nil {
			log.Errorf("failed to parse DevEUI %s: %v", instance.DevEUI, err)
			continue
		}

		// Convert string AppKey to lorawan.AES128Key
		var appKey lorawan.AES128Key
		if err := appKey.UnmarshalText([]byte(instance.AppKey)); err != nil {
			log.Errorf("failed to parse AppKey for device %s: %v", instance.DevEUI, err)
			continue
		}

		newDevice := Device{
			DevEUI:           devEUI,
			Name:             instance.Name,
			Description:      instance.DevEUI,
			Application:      defaultAppName,
			DeviceProfile:    deviceProfile,
			PayloadCodec:     deviceTypeConfig.Name,
			FPort:            fPort,
			AppKey:           appKey,
			DevAddr:          lorawan.DevAddr{},   // Empty for OTAA devices
			NwkSKey:          lorawan.AES128Key{}, // Empty for OTAA devices
			AppSKey:          lorawan.AES128Key{}, // Empty for OTAA devices
			DeviceTypeConfig: deviceTypeConfig,
		}

		s.generatedDevices = append(s.generatedDevices, newDevice)
	}

	log.Infof("Generated %d devices for multi-type simulation", len(s.generatedDevices))

	return nil
}

// hasSimulationConfig checks if simulation-config.json exists
func hasSimulationConfig() bool {
	_, err := os.Stat(SIMULATION_CONFIG_FILE)
	return err == nil
}

func (s *Simulation) createDevices() error {
	// Use simulation-config.json for device generation
	// All device info is derived from devices.json - no CSV file needed
	if !hasSimulationConfig() {
		return fmt.Errorf("simulation-config.json is required for device generation")
	}

	if err := s.generateMultiTypeDevices(); err != nil {
		return fmt.Errorf("failed to generate devices: %w", err)
	}

	// Use in-memory device list directly (no CSV file needed)
	for _, dev := range s.generatedDevices {
		profileID := ""
		for _, p := range s.deviceProfiles {
			if p.Name == dev.DeviceProfile {
				profileID = p.Profile.ProfileID
				break
			}
		}

		if profileID == "" {
			return errors.Errorf("can not find device profile: %s", dev.DeviceProfile)
		}

		applicationID := ""
		for _, a := range s.applications {
			if a.Name == dev.Application {
				applicationID = a.ID
				break
			}
		}

		if applicationID == "" {
			return errors.Errorf("can not find application: %s", dev.Application)
		}

		payloadCodecID := ""
		for _, c := range s.payloadCodecs {
			if c.Name == dev.PayloadCodec {
				payloadCodecID = c.ID
				break
			}
		}

		if payloadCodecID == "" {
			return errors.Errorf("can not find payload codec: %s", dev.PayloadCodec)
		}

		err := as.CreateDevices(dev.DevEUI, dev.Name, profileID, dev.AppKey, payloadCodecID, applicationID)

		if err != nil {
			log.Error(err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	return nil
}

func (s *Simulation) tearDownDevices() error {
	log.Info("simulator: tear-down devices")

	for _, dev := range s.generatedDevices {
		err := as.DeleteDevices(dev.DevEUI.String())
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

// buildDevEUIToTestDataKeysMap builds a map from DevEUI to test data keys for each device instance
// DevEUI is normalized to lowercase for case-insensitive matching
func (s *Simulation) buildDevEUIToTestDataKeysMap() map[string]map[string]struct{} {
	devEUIToTestDataKeys := make(map[string]map[string]struct{})

	if len(s.deviceInstances) == 0 {
		return nil
	}

	for _, instance := range s.deviceInstances {
		if instance.TestData != nil && len(instance.TestData) > 0 {
			testDataKeys := make(map[string]struct{})
			for k := range instance.TestData {
				testDataKeys[k] = struct{}{}
			}
			// Normalize DevEUI to lowercase for case-insensitive matching
			normalizedDevEUI := strings.ToLower(instance.DevEUI)
			devEUIToTestDataKeys[normalizedDevEUI] = testDataKeys
		}
	}

	if len(devEUIToTestDataKeys) == 0 {
		return nil
	}

	log.Infof("Built DevEUI to test data keys map for %d device instances", len(devEUIToTestDataKeys))
	return devEUIToTestDataKeys
}

func (s *Simulation) setupBACnet() error {
	log.Info("simulator: creating BACnet objects")

	if !strings.Contains(config.C.LoraSimulator.API.TestFeature, "bacnet") {
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

	// Build DevEUI to test data keys map for filtering (optional)
	devEUIToTestDataKeys := s.buildDevEUIToTestDataKeysMap()
	if devEUIToTestDataKeys == nil {
		log.Info("no test data found in device instances, adding all BACnet objects without filtering")
	}

	for i := 0; i < int(objects.Total); i += MAX_ADD_DATUM {
		log.Info("add from ", i, " to ", i+MAX_ADD_DATUM)
		objects, err := as.GetAvailableBACnetObjects("", "asc", i, MAX_ADD_DATUM)
		if err != nil {
			return err
		}

		// Filter objects by device-specific test data keys if available
		if devEUIToTestDataKeys != nil {
			for _, bacnetDevice := range objects.Data {
				if bacnetDevice.DevEui == "" {
					continue
				}

				// Normalize DevEUI to lowercase for case-insensitive matching
				normalizedDevEUI := strings.ToLower(bacnetDevice.DevEui)

				// Get test data keys for this specific device
				testDataKeys, exists := devEUIToTestDataKeys[normalizedDevEUI]
				if !exists || len(testDataKeys) == 0 {
					// No test data for this device, skip filtering (keep all objects)
					continue
				}

				// Filter objects by test data keys for this device
				newObjs := []*models.APIPCO{}
				for _, obj := range bacnetDevice.Objects {
					if _, exists := testDataKeys[obj.LoraName]; exists {
						newObjs = append(newObjs, obj)
					}
				}
				bacnetDevice.Objects = newObjs
				log.Debugf("Filtered BACnet objects for device %s: %d objects after filtering", bacnetDevice.DevEui, len(newObjs))
			}
		}

		err = as.AddBACnetObjects(objects.Data)
		if err != nil {
			log.Error(err)
			continue
		}
		log.Info("added ", len(objects.Data), " devices with BACnet objects")
	}

	return nil
}

func (s *Simulation) setupFuota() error {
	log.Info("simulator: creating FUOTA task")

	if !strings.Contains(config.C.LoraSimulator.API.TestFeature, "fuota") || config.C.LoraSimulator.API.FuotaTaskDeviceCount == 0 {
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
		log.WithError(err).Error("setupFuota: delete existing fuota tasks failed, please wait for the previous task to finish")
		return err
	}

	// 等待所有节点入网后
	for {
		if device.GetJoinAcceptCount() == len(s.generatedDevices) {
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
	for _, dev := range s.generatedDevices {
		allDeveuiList = append(allDeveuiList, dev.DevEUI.String())
	}

	sort.Strings(allDeveuiList)

	deveuiList := []string{}
	for _, deveui := range allDeveuiList {
		deveuiList = append(deveuiList, deveui)
		if len(deveuiList) == config.C.LoraSimulator.API.FuotaTaskDeviceCount {
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
			if config.C.LoraSimulator.API.FuotaTaskCreateInterval > 0 {
				log.Infof("setupFuota waiting %v before creating next task", config.C.LoraSimulator.API.FuotaTaskCreateInterval)
				time.Sleep(config.C.LoraSimulator.API.FuotaTaskCreateInterval)
			}
		}
	}

	return nil
}

func (s *Simulation) setupModbus() error {
	log.Info("simulator: creating modbus servers")

	if !strings.Contains(config.C.LoraSimulator.API.TestFeature, "modbus") {
		return nil
	}

	modbusServers, err := as.GetModbusServer(math.MaxInt32, 0, "")
	if err != nil {
		return err
	}

	if len(modbusServers) > 0 {
		id := 0
		for _, server := range modbusServers {
			err = as.DeleteModbusServer(server.ID)

			if err != nil {
				log.Error("failed to delete modbus servers: ", err)
				return err
			}

			log.Info("deleted modbus servers: ", server.ID)
			id += 1
		}
	}

	for k := 0; k < 1; k++ {
		modbusServerCreateReq := models.APIModbusServer{
			ID:                 strconv.Itoa(k + 1),
			Enable:             1,
			Interface:          "eth0",
			ConnectType:        "modbus_tcp",
			Name:               "test" + strconv.Itoa(k),
			Port:               10000 + int32(k),
			SlaveID:            int32(k),
			Description:        "test" + strconv.Itoa(k),
			SlaveIDType:        1,
			GlobalObjectEnable: true,
			GlobalObjects: []string{
				"frequency",
				"rssi",
				"snr",
			},
		}

		err = as.CreateModbusServer(&modbusServerCreateReq)
		if err != nil {
			log.Error("failed to create modbus servers: ", err)
			return err
		}

		log.Info("created modbus servers: ", modbusServerCreateReq)
	}

	modbusServers, err = as.GetModbusServer(math.MaxInt32, 0, "")
	if err != nil {
		return err
	}

	log.Infof("modbus servers: %v", modbusServers)

	serverIDs := []string{}
	if len(modbusServers) < 1 {
		log.Error("no modbus server found")
		return errors.New("no modbus server found")
	}

	for _, server := range modbusServers {
		serverIDs = append(serverIDs, server.ID)
	}

	log.Infof("modbus server ids: %v", serverIDs)

	modbusObjects, err := as.GetAllAvaliableModbusObjects(&models.APIGetModbusObjectRequest{
		ServerID: serverIDs[0],
		Limit:    int32(1),
		Offset:   0,
		Search:   "",
	})

	if err != nil {
		log.Error("failed to get modbus objects: ", err)
		return err
	}

	const MAX_ADD_DATUM = 30

	// Build DevEUI to test data keys map for filtering (optional)
	devEUIToTestDataKeys := s.buildDevEUIToTestDataKeysMap()
	if devEUIToTestDataKeys == nil {
		log.Info("no test data found in device instances, adding all Modbus objects without filtering")
	}

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

		maxDevice := 2
		newData := []*models.APIModbusDevice{}
		for k := range modbusObjects.Data {
			if k > maxDevice {
				break
			}

			modbusDevice := modbusObjects.Data[k]

			// Filter objects by device-specific test data keys if available
			if devEUIToTestDataKeys != nil && modbusDevice.DevEui != "" {
				// Normalize DevEUI to lowercase for case-insensitive matching
				normalizedDevEUI := strings.ToLower(modbusDevice.DevEui)

				// Get test data keys for this specific device
				testDataKeys, exists := devEUIToTestDataKeys[normalizedDevEUI]
				if exists && len(testDataKeys) > 0 {
					newObjs := []*models.APIModbusObject{}
					for _, o := range modbusDevice.Objects {
						if _, exists := testDataKeys[o.LoraName]; exists {
							newObjs = append(newObjs, o)
						}
					}
					modbusDevice.Objects = newObjs
					log.Debugf("Filtered Modbus objects for device %s: %d objects after filtering", modbusDevice.DevEui, len(newObjs))
				}
			}

			newData = append(newData, modbusDevice)
		}

		modbusObjects.Data = newData

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

func (s *Simulation) ApiTest() error {
	log.Info("simulator: payload codec test")

	csv, err := as.ExportBulkDevice()
	if err != nil {
		return err
	}

	csvFile, err := os.Create("bulk_device.csv")
	if err != nil {
		return err
	}
	defer csvFile.Close()

	csvFile.WriteString(csv)

	return nil
}

func (s *Simulation) TestPayloadCodec() error {
	log.Info("simulator: test payload codec")

	for _, deviceSheet := range config.C.LoraSimulator.TestPayloadCodec.TestDeviceSheet {
		log.Info("simulator: test payload codec sheet: ", deviceSheet.Sheet)
		log.Info("simulator: test payload codec device: ", deviceSheet.Device)

		suite, err := test_payload_codec.ParseExcelSheet(config.C.LoraSimulator.TestPayloadCodec.TestCaseFile, deviceSheet.Sheet)
		if err != nil {
			log.Error("TestPayloadCodec: failed to parse excel sheet: ", err)
			continue
		}

		decodeScriptFile := fmt.Sprintf(config.C.LoraSimulator.TestPayloadCodec.TestCodecDir+"/vendors/milesight-iot/%s/%s-decoder.js", deviceSheet.Device, deviceSheet.Device)
		encodeScriptFile := fmt.Sprintf(config.C.LoraSimulator.TestPayloadCodec.TestCodecDir+"/vendors/milesight-iot/%s/%s-encoder.js", deviceSheet.Device, deviceSheet.Device)

		decodeScript, err := os.ReadFile(decodeScriptFile)
		if err != nil {
			log.Error("TestPayloadCodec: failed to read decode script: ", err)
			continue
		}

		encodeScript, err := os.ReadFile(encodeScriptFile)
		if err != nil {
			log.Error("TestPayloadCodec: failed to read encode script: ", err)
			continue
		}

		successTestCase := test_payload_codec.PayloadCodecTestSuite{}
		failedTestCase := test_payload_codec.PayloadCodecTestSuite{}
		for i := range suite.TestCases {
			time.Sleep(3 * time.Second)

			testCase := &suite.TestCases[i]
			encodeApiResult := ""
			if testCase.JSONContent != nil {
				jsonContent, err := json.Marshal(testCase.JSONContent)
				if err != nil {
					log.Error("TestPayloadCodec: failed to marshal json content: ", err)
					continue
				}

				testCase.APIENResult = true
				testCase.APIDEResult = true

				encodePayloadCodecReq := &models.APITestPayloadCodecRequest{
					Data:   string(jsonContent),
					FPort:  1,
					Script: string(encodeScript),
					Type:   "encode",
				}

				encodeApiResult, err = as.PayloadCodecTest(encodePayloadCodecReq)
				if err != nil {
					log.Errorf("TestPayloadCodec: failed to encode payload codec: %v, jsonContent: %s, description: %s", err, string(jsonContent), testCase.Description)
					continue
				}
			} else {
				testCase.APIENResult = false
				testCase.APIENResultMsg = "jsonContent is null"
			}

			encodedHex := strings.ToLower(encodeApiResult)
			if encodedHex != testCase.Command {
				testCase.APIENResult = false
				testCase.APIENResultMsg = fmt.Sprintf("encoded hex: %s, expected: %s", encodedHex, testCase.Response)
			}

			decodePayloadCodecReq := &models.APITestPayloadCodecRequest{
				Data:   testCase.Response,
				FPort:  1,
				Script: string(decodeScript),
				Type:   "decode",
			}

			time.Sleep(3 * time.Second)

			decodeApiResult, err := as.PayloadCodecTest(decodePayloadCodecReq)
			if err != nil {
				log.Errorf("TestPayloadCodec: failed to decode payload codec: %v, response: %s", err, testCase.Response)
				testCase.APIDEResult = false
				testCase.APIDEResultMsg = err.Error()
				continue
			}

			// 解析解码结果并与期望值比较
			testCase.APIDEResult, err = s.compareDecodeResult(decodeApiResult, testCase.JSONContent)
			if err != nil {
				log.WithError(err).Error("failed to compare decode result, decodeApiResult: ", decodeApiResult)
				testCase.APIDEResult = false
				testCase.APIDEResultMsg = err.Error()
			}

			if testCase.APIDEResult && testCase.APIENResult {
				successTestCase.TestCases = append(successTestCase.TestCases, *testCase)
			} else {
				failedTestCase.TestCases = append(failedTestCase.TestCases, *testCase)
			}

			successTestCase.SaveToJSON(config.C.LoraSimulator.TestPayloadCodec.TestResultDir + "/" + deviceSheet.Device + "-success.json")
			failedTestCase.SaveToJSON(config.C.LoraSimulator.TestPayloadCodec.TestResultDir + "/" + deviceSheet.Device + "-failed.json")
		}
	}

	return nil
}

// compareDecodeResult 比较解码结果与期望的JSON内容
func (s *Simulation) compareDecodeResult(apiResult interface{}, expectedContent interface{}) (bool, error) {
	// 直接将 apiResult 转换为字符串（因为 PayloadCodecTest 返回的就是 JSON 字符串）
	apiResultStr, ok := apiResult.(string)
	if !ok {
		return false, fmt.Errorf("API result is not a string: %T", apiResult)
	}

	// 解析API返回的JSON字符串为 interface{}
	var actualContent interface{}
	if err := json.Unmarshal([]byte(apiResultStr), &actualContent); err != nil {
		return false, fmt.Errorf("failed to unmarshal API result string: %w", err)
	}

	// 深度比较两个interface{}
	isEqual := s.deepEqual(actualContent, expectedContent)
	if !isEqual {
		return false, fmt.Errorf("actual content: %v, expected content: %v", actualContent, expectedContent)
	}

	return true, nil
}

// deepEqual 递归地比较两个interface{}对象
func (s *Simulation) deepEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// 将两个值都转换为JSON再比较，确保类型一致
	aBytes, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		return false
	}

	// 解析为map进行结构化比较
	var aMap, bMap interface{}
	if err := json.Unmarshal(aBytes, &aMap); err != nil {
		return false
	}
	if err := json.Unmarshal(bBytes, &bMap); err != nil {
		return false
	}

	return s.deepEqualValue(aMap, bMap)
}

// deepEqualValue 递归比较两个值
func (s *Simulation) deepEqualValue(a, b interface{}) bool {
	switch aVal := a.(type) {
	case map[string]interface{}:
		bVal, ok := b.(map[string]interface{})
		if !ok {
			return false
		}

		// 检查字段数量是否相同
		if len(aVal) != len(bVal) {
			return false
		}

		// 逐个比较字段
		for key, aValue := range aVal {
			bValue, exists := bVal[key]
			if !exists {
				return false
			}
			if !s.deepEqualValue(aValue, bValue) {
				return false
			}
		}
		return true

	case []interface{}:
		bVal, ok := b.([]interface{})
		if !ok {
			return false
		}

		if len(aVal) != len(bVal) {
			return false
		}

		for i, aValue := range aVal {
			if !s.deepEqualValue(aValue, bVal[i]) {
				return false
			}
		}
		return true

	default:
		// 对于基本类型，直接比较
		return aVal == b
	}
}
