package simulator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/chirpstack-simulator/internal/as"
	"github.com/brocaar/chirpstack-simulator/internal/config"
	"github.com/brocaar/chirpstack-simulator/internal/ns"
	"github.com/brocaar/chirpstack-simulator/simulator"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
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
	var gateways []*simulator.Gateway
	var devices []*simulator.Device

	for _, gatewayID := range s.gatewayIDs {
		gw, err := simulator.NewGateway(
			simulator.WithGatewayID(gatewayID),
			simulator.WithMQTTClient(ns.Client()),
			simulator.WithEventTopicTemplate(s.eventTopicTemplate),
			simulator.WithCommandTopicTemplate(s.commandTopicTemplate),
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
		devGateways := make(map[int]*simulator.Gateway)
		devNumGateways := s.gatewayMinCount + mrand.Intn(s.gatewayMaxCount-s.gatewayMinCount+1)

		for len(devGateways) < devNumGateways {
			// pick random gateway index
			n := mrand.Intn(len(gateways))
			devGateways[n] = gateways[n]
		}

		var gws []*simulator.Gateway
		for k := range devGateways {
			gws = append(gws, devGateways[k])
		}

		zeroDuration := time.Duration(0)
		otaaDuration := time.Duration(0)
		if s.activationTime != zeroDuration {
			otaaDuration = time.Duration(mrand.Int63n(int64(s.activationTime)))
		}

		d, err := simulator.NewDevice(ctx, &wg,
			simulator.WithDevEUI(devEUI),
			simulator.WithAppKey(appKey),
			simulator.WithUplinkInterval(s.uplinkInterval),
			simulator.WithOTAADelay(otaaDuration),
			simulator.WithUplinkPayload(true, s.fPort, s.payload),
			simulator.WithGateways(gws),
			simulator.WithUplinkTXInfo(gw.UplinkTxInfo{
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
		)
		if err != nil {
			return errors.Wrap(err, "new device error")
		}

		devices = append(devices, d)
	}

	log.Info("len(devices): ", len(devices))

	go func() {
		sigChan := make(chan os.Signal)
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

			err := as.LNSCreateGateway(gatewayID.String(), gatewayID.String())

			if err != nil {
				return errors.Wrap(err, "create gateway error")
			}

			s.gatewayIDs = append(s.gatewayIDs, gatewayID)
		}

		return nil
	}

	macs, err := as.LNSGetGateway()
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
		err := as.LNSDeleteGateway(gatewayID.String())
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

	profiles, err := as.LNSGetProfiles()
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

	err := as.LNSDeleteDeviceProfile(s.deviceProfileID.String())
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func (s *LNSSimulation) setupApplication() error {
	log.Info("simulator: init application")

	if config.C.ChirpStack.API.UseNewApp {
		id, err := as.LNSCreateApplication()
		if err != nil {
			return errors.Wrap(err, "create applicaiton error")
		}

		s.applicationID = id

		return nil
	}

	apps, err := as.LNSGetApplications()
	if err != nil {
		return err
	}
	s.applications = apps

	return nil
}

func (s *LNSSimulation) tearDownApplication() error {
	log.Info("simulator: tear-down application")

	if !config.C.ChirpStack.API.UseNewApp {
		return nil
	}

	err := as.LNSDeleteApplication(s.applicationID)
	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

func (s *LNSSimulation) setupDevices() error {
	log.Info("simulator: init devices")

	ret, records := as.LoadLoRaWANDevCfg("devices_import.csv", 1)
	if !ret {
		return errors.Errorf("failed to setupDevices")
	}

	for _, ldcfg := range records {
		// TODO: 需要使用get获取对应的profileId和payloadCodecId
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

		eui := ldcfg.DevEUI
		appKey := ldcfg.AppKey

		err := as.LNSCreateDevices(eui, profileID, appKey, payloadCodecID, applicationID)

		if err != nil {
			log.Error(err)
		}

		var devEUI lorawan.EUI64
		var appKeyAES lorawan.AES128Key

		devEUI.UnmarshalText([]byte(eui))
		appKeyAES.UnmarshalText([]byte(appKey))

		s.deviceAppKeys[devEUI] = appKeyAES
	}

	return nil
}

func (s *LNSSimulation) tearDownDevices() error {
	log.Info("simulator: tear-down devices")

	for k := range s.deviceAppKeys {
		err := as.LNSDeleteDevices(k.String())
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

func (s *LNSSimulation) setupPayloadCodec() error {
	log.Info("simulator: creating gateways")

	codecs, err := as.LNSGetPayloadCoedc()
	if err != nil {
		return nil
	}

	s.payloadCodecs = codecs

	return nil
}
