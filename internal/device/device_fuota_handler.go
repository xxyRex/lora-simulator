package device

import (
	"crypto/aes"
	"fmt"
	"strconv"
	"time"

	"github.com/brocaar/chirpstack-simulator/internal/fragmentation"
	"github.com/brocaar/chirpstack-simulator/internal/multicastsetup"
	"github.com/brocaar/lorawan"
	log "github.com/sirupsen/logrus"
)

func (d *Device) handleMulticastSetupCommand(b []byte) error {
	var cmd multicastsetup.Command
	if err := cmd.UnmarshalBinary(true, b); err != nil {
		return fmt.Errorf("unmarshal command error: %w", err)
	}

	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
		"cid":     cmd.CID,
	}).Info("fuota: multicast-setup command received")

	switch cmd.CID {
	case multicastsetup.McGroupSetupReq:
		pl, ok := cmd.Payload.(*multicastsetup.McGroupSetupReqPayload)
		if !ok {
			log.WithFields(log.Fields{
				"dev_eui": d.devEUI,
				"cid":     cmd.CID,
			}).Warnf("fuota: expected *McGroupSetupReqPayload, got: %T", cmd.Payload)
			return fmt.Errorf("expected *McGroupSetupReqPayload, got: %T", cmd.Payload)
		}
		return d.handleMcGroupSetupReq(pl)
	case multicastsetup.McClassCSessionReq:
		pl, ok := cmd.Payload.(*multicastsetup.McClassCSessionReqPayload)
		if !ok {
			return fmt.Errorf("expected *McClassCSessionReqPayload, got: %T", cmd.Payload)
		}
		return d.handleMcClassCSessionReq(pl)
	default:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
			"cid":     cmd.CID,
		}).Warn("fuota: unknown command")
		return fmt.Errorf("unknown command")
	}
}

func (d *Device) handleMcGroupSetupReq(pl *multicastsetup.McGroupSetupReqPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":                      d.devEUI,
		"cid":                          multicastsetup.McGroupSetupReq,
		"pl.MinMcFCnt":                 pl.MinMcFCnt,
		"pl.MaxMcFCnt":                 pl.MaxMcFCnt,
		"pl.McGroupIDHeader.McGroupID": pl.McGroupIDHeader.McGroupID,
		"pl.McAddr":                    pl.McAddr,
		"pl.McKeyEncrypted":            pl.McKeyEncrypted,
	}).Info("fuota: multicast-setup command received")

	config := GetDynamicDevicesConfig(d.devEUI)
	var mcGroupSetupDebug *MgGroupSetupAns = nil

	if config != nil {
		mcGroupSetupDebug = config.Devices.FuotaDebug.MgGroupSetupAns
	}

	idError := false
	mcGroupID := pl.McGroupIDHeader.McGroupID

	if mcGroupSetupDebug != nil {
		if mcGroupSetupDebug.SkipMgGroupSetupAns {
			log.Info("fuota: multicast-setup command received, skipping")
			return nil
		}

		idError = mcGroupSetupDebug.IDError
		mcGroupID = uint8(mcGroupSetupDebug.McGroupID)
	}

	mcRootKey, err := GetMcRootKeyForGenAppKey(d.appKey)
	if err != nil {
		log.Info("fuota: get mc root key for gen app key error: %w", err)
		return fmt.Errorf("fuota: get mc root key for gen app key error: %w", err)
	}

	mcKEKey, err := GetMcKEKey(mcRootKey)
	if err != nil {
		log.Info("fuota: get mc ke key error: %w", err)
		return fmt.Errorf("fuota: get mc ke key error: %w", err)
	}

	block, err := aes.NewCipher(mcKEKey[:])
	if err != nil {
		log.Info("fuota: new cipher error: %w", err)
		return fmt.Errorf("fuota: new cipher error: %w", err)
	}

	var mcKey lorawan.AES128Key
	block.Encrypt(mcKey[:], pl.McKeyEncrypted[:])

	mcAppSKey, err := GetMcAppSKey(mcKey, pl.McAddr)
	if err != nil {
		return fmt.Errorf("get McAppSKey error: %w", err)
	}

	mcNetSKey, err := GetMcNetSKey(mcKey, pl.McAddr)
	if err != nil {
		return fmt.Errorf("get McNetSKey error: %s", err)
	}

	cmd := multicastsetup.Command{
		CID: multicastsetup.McGroupSetupAns,
		Payload: &multicastsetup.McGroupSetupAnsPayload{
			McGroupIDHeader: multicastsetup.McGroupSetupAnsPayloadMcGroupIDHeader{
				IDError:   idError,
				McGroupID: mcGroupID,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = multicastsetup.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.McGroupSetupReqPayload = pl
	d.multicastKeys.McAppSKey = mcAppSKey
	d.multicastKeys.McNetSKey = mcNetSKey
	d.multicastKeys.McKEKey = mcKEKey
	return nil
}

func (d *Device) handleMcClassCSessionReq(pl *multicastsetup.McClassCSessionReqPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":                      d.devEUI,
		"cid":                          multicastsetup.McClassCSessionReq,
		"pl.McGroupIDHeader.McGroupID": pl.McGroupIDHeader.McGroupID,
		"pl.SessionTime":               pl.SessionTime,
		"pl.SessionTimeOut":            pl.SessionTimeOut,
		"pl.DLFrequency":               pl.DLFrequency,
		"pl.DR":                        pl.DR,
	}).Info("fuota: multicast-class-c-session command received")

	config := GetDynamicDevicesConfig(d.devEUI)
	var mcClassCSessionDebug *McClassCSessionAns = nil

	if config != nil {
		mcClassCSessionDebug = config.Devices.FuotaDebug.McClassCSessionAns
	}

	mcGroupUndefined := false
	freqError := false
	drError := false
	mcGroupID := pl.McGroupIDHeader.McGroupID
	timeToStartPtr := &pl.SessionTime

	if mcClassCSessionDebug != nil {
		if mcClassCSessionDebug.SkipMcClassCSessionAns {
			log.Info("fuota: multicast-class-c-session command received, skipping")
			return nil
		}

		mcGroupUndefined = mcClassCSessionDebug.StatusAndMcGroupID.McGroupUndefined
		freqError = mcClassCSessionDebug.StatusAndMcGroupID.FreqError
		drError = mcClassCSessionDebug.StatusAndMcGroupID.DRError
		mcGroupID = uint8(mcClassCSessionDebug.StatusAndMcGroupID.McGroupID)
		timeToStart := uint32(mcClassCSessionDebug.TimeToStart)
		timeToStartPtr = &timeToStart
	}

	if mcGroupUndefined || freqError || drError {
		timeToStartPtr = nil
	}

	cmd := multicastsetup.Command{
		CID: multicastsetup.McClassCSessionAns,
		Payload: &multicastsetup.McClassCSessionAnsPayload{
			StatusAndMcGroupID: multicastsetup.McClassCSessionAnsPayloadStatusAndMcGroupID{
				McGroupUndefined: mcGroupUndefined,
				FreqError:        freqError,
				DRError:          drError,
				McGroupID:        mcGroupID,
			},
			TimeToStart: timeToStartPtr,
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		log.Info("marshal command error: %w", err)
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = multicastsetup.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.McClassCSessionReqPayload = pl

	return nil
}

func (d *Device) handleFragmentationSessionSetupCommand(data []byte) error {
	var cmd fragmentation.Command
	if err := cmd.UnmarshalBinary(true, data); err != nil {
		return fmt.Errorf("unmarshal command error: %w", err)
	}

	switch cmd.CID {
	case fragmentation.FragSessionSetupReq:
		pl, ok := cmd.Payload.(*fragmentation.FragSessionSetupReqPayload)
		if !ok {
			return fmt.Errorf("expected *FragmentationSessionSetupReqPayload, got: %T", cmd.Payload)
		}
		return d.handleFragSessionSetupReq(pl)
	case fragmentation.FragSessionStatusReq:
		pl, ok := cmd.Payload.(*fragmentation.FragSessionStatusReqPayload)
		if !ok {
			return fmt.Errorf("expected *fragmentation.FragSessionStatusReqPayload, got: %T", cmd.Payload)
		}
		return d.handleFragSessionStatusReq(pl)
	default:
		return nil
	}
}

func (d *Device) handleFragSessionSetupReq(pl *fragmentation.FragSessionSetupReqPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":                        d.devEUI,
		"cid":                            fragmentation.FragSessionSetupReq,
		"pl.FragSession.FragIndex":       pl.FragSession.FragIndex,
		"pl.FragSession.McGroupBitMask":  pl.FragSession.McGroupBitMask,
		"pl.NbFrag":                      pl.NbFrag,
		"pl.FragSize":                    pl.FragSize,
		"pl.Control.FragmentationMatrix": pl.Control.FragmentationMatrix,
		"pl.Control.BlockAckDelay":       pl.Control.BlockAckDelay,
	}).Info("fuota: fragmentation-session-setup command received")

	config := GetDynamicDevicesConfig(d.devEUI)
	var fragSessionSetupDebug *FragSessionSetupAns = nil

	if config != nil {
		fragSessionSetupDebug = config.Devices.FuotaDebug.FragSessionSetupAns
	}

	fragIndex := pl.FragSession.FragIndex
	wrongDescriptor := false
	fragSessionIndexNotSupported := false
	notEnoughMemory := false
	encodingUnsupported := false
	if fragSessionSetupDebug != nil {
		if fragSessionSetupDebug.SkipFragSessionSetupAns {
			log.Info("fuota: fragmentation-session-setup command received, skipping")
			return nil
		}

		fragIndex = uint8(config.Devices.FuotaDebug.FragSessionSetupAns.StatusBitMask.FragIndex)
		wrongDescriptor = config.Devices.FuotaDebug.FragSessionSetupAns.StatusBitMask.WrongDescriptor
		fragSessionIndexNotSupported = config.Devices.FuotaDebug.FragSessionSetupAns.StatusBitMask.FragSessionIndexNotSupported
		notEnoughMemory = config.Devices.FuotaDebug.FragSessionSetupAns.StatusBitMask.NotEnoughMemory
		encodingUnsupported = config.Devices.FuotaDebug.FragSessionSetupAns.StatusBitMask.EncodingUnsupported
	}

	cmd := fragmentation.Command{
		CID: fragmentation.FragSessionSetupAns,
		Payload: &fragmentation.FragSessionSetupAnsPayload{
			StatusBitMask: fragmentation.FragSessionSetupAnsPayloadStatusBitMask{
				FragIndex:                    fragIndex,
				WrongDescriptor:              wrongDescriptor,
				FragSessionIndexNotSupported: fragSessionIndexNotSupported,
				NotEnoughMemory:              notEnoughMemory,
				EncodingUnsupported:          encodingUnsupported,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = fragmentation.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.FragSessionSetupReqPayload = pl

	return nil
}

func (d *Device) handleFragSessionStatusReq(pl *fragmentation.FragSessionStatusReqPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":                            d.devEUI,
		"cid":                                fragmentation.FragSessionStatusReq,
		"pl.FragStatusReqParam.FragIndex":    pl.FragStatusReqParam.FragIndex,
		"pl.FragStatusReqParam.Participants": pl.FragStatusReqParam.Participants,
	}).Info("fuota: fragmentation-session-status command received")
	config := GetDynamicDevicesConfig(d.devEUI)

	var fragSessionStatusDebug *FragSessionStatusAns = nil

	if config != nil && config.Devices.FuotaDebug.FragSessionStatusAns != nil {
		fragSessionStatusDebug = config.Devices.FuotaDebug.FragSessionStatusAns
	}

	nbReceived := 0
	if d.fuotaProperties.FragSessionStatusReqPayload != nil {
		nbReceived = int(d.fuotaProperties.FragSessionSetupReqPayload.NbFrag)
	}

	fragIndex := pl.FragStatusReqParam.FragIndex
	nbFragReceived := uint16(nbReceived)
	missingFrag := uint8(0)
	notEnoughMatrixMemory := false

	if fragSessionStatusDebug != nil {
		if fragSessionStatusDebug.SkipFragSessionStatusAns {
			log.Info("fuota: fragmentation-session-status command received, skipping")
			return nil
		}

		fragIndex = uint8(fragSessionStatusDebug.ReceivedAndIndex.FragIndex)
		nbFragReceived = uint16(fragSessionStatusDebug.ReceivedAndIndex.NbFragReceived)
		missingFrag = uint8(fragSessionStatusDebug.MissingFrag)
		notEnoughMatrixMemory = fragSessionStatusDebug.Status.NotEnoughMatrixMemory
		UseCrcCheck := fragSessionStatusDebug.CrcCheck.UseCrcCheck
		if UseCrcCheck {
			go func() {
				time.Sleep(time.Duration(fragSessionStatusDebug.CrcCheck.Delay) * time.Second)
				crc32Value, err := strconv.ParseUint(fragSessionStatusDebug.CrcCheck.Value, 16, 32)
				if err != nil {
					log.Info("parse crc32 value error: %w", err)
					return
				}

				cmd := fragmentation.Command{
					CID: fragmentation.FragCustomCrcAns,
					Payload: &fragmentation.FragCustomCrcAnsPayload{
						CRC: uint32(crc32Value),
					},
				}
				b, err := cmd.MarshalBinary()
				if err != nil {
					log.Info("marshal command error: %w", err)
					return
				}
				d.payload = b
				d.fPort = fragmentation.DefaultFPort
				d.dataUp(lorawan.UnconfirmedDataUp, false)
			}()
			return nil
		}
	}

	cmd := fragmentation.Command{
		CID: fragmentation.FragSessionStatusAns,
		Payload: &fragmentation.FragSessionStatusAnsPayload{
			ReceivedAndIndex: fragmentation.FragSessionStatusAnsPayloadReceivedAndIndex{
				FragIndex:      fragIndex,
				NbFragReceived: nbFragReceived,
			},
			MissingFrag: missingFrag,
			Status: fragmentation.FragSessionStatusAnsPayloadStatus{
				NotEnoughMatrixMemory: notEnoughMatrixMemory,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = fragmentation.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.FragSessionStatusReqPayload = pl
	return nil
}

// GetMcRootKeyForGenAppKey returns the McRootKey given a GenAppKey.
// Note: The GenAppKey is only used for LoRaWAN 1.0.x devices.
func GetMcRootKeyForGenAppKey(genAppKey lorawan.AES128Key) (lorawan.AES128Key, error) {
	return getKey(genAppKey, [16]byte{})
}

// GetMcRootKeyForAppKey returns the McRootKey given an AppKey.
// Note: The AppKey is only used for LoRaWAN 1.1.x devices.
func GetMcRootKeyForAppKey(appKey lorawan.AES128Key) (lorawan.AES128Key, error) {
	return getKey(appKey, [16]byte{0x20})
}

// GetMcKEKey returns the McKEKey given the McRootKey.
func GetMcKEKey(mcRootKey lorawan.AES128Key) (lorawan.AES128Key, error) {
	return getKey(mcRootKey, [16]byte{})
}

// GetMcAppSKey returns the McAppSKey given the McKey and McAddr.
func GetMcAppSKey(mcKey lorawan.AES128Key, mcAddr lorawan.DevAddr) (lorawan.AES128Key, error) {
	b := [16]byte{0x01}

	mcAddrB, err := mcAddr.MarshalBinary()
	if err != nil {
		return lorawan.AES128Key{}, err
	}
	copy(b[1:5], mcAddrB)

	return getKey(mcKey, b)
}

// GetMcNetSKey returns the McNetSKey given the McKey and McAddr.
func GetMcNetSKey(mcKey lorawan.AES128Key, mcAddr lorawan.DevAddr) (lorawan.AES128Key, error) {
	b := [16]byte{0x02}

	mcAddrB, err := mcAddr.MarshalBinary()
	if err != nil {
		return lorawan.AES128Key{}, err
	}
	copy(b[1:5], mcAddrB)

	return getKey(mcKey, b)
}

func getKey(key lorawan.AES128Key, b [16]byte) (lorawan.AES128Key, error) {
	var out lorawan.AES128Key

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return out, err
	}
	if block.BlockSize() != len(b) {
		return out, fmt.Errorf("block-size of %d bytes is expected", len(b))
	}

	block.Encrypt(out[:], b[:])
	return out, nil
}
