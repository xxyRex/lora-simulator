package simulator

import (
	"fmt"

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

	cmd := multicastsetup.Command{
		CID: multicastsetup.McGroupSetupAns,
		Payload: &multicastsetup.McGroupSetupAnsPayload{
			McGroupIDHeader: multicastsetup.McGroupSetupAnsPayloadMcGroupIDHeader{
				IDError:   false,
				McGroupID: pl.McGroupIDHeader.McGroupID,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = 200
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.McGroupSetupReqPayload = pl

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

	cmd := multicastsetup.Command{
		CID: multicastsetup.McClassCSessionAns,
		Payload: &multicastsetup.McClassCSessionAnsPayload{
			StatusAndMcGroupID: multicastsetup.McClassCSessionAnsPayloadStatusAndMcGroupID{
				McGroupUndefined: false,
				FreqError:        false,
				DRError:          false,
				McGroupID:        pl.McGroupIDHeader.McGroupID,
			},
			TimeToStart: &pl.SessionTime,
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = 200
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
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
			"cid":     cmd.CID,
		}).Warn("fuota: unknown command")
		return fmt.Errorf("unknown command")
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

	cmd := fragmentation.Command{
		CID: fragmentation.FragSessionSetupAns,
		Payload: &fragmentation.FragSessionSetupAnsPayload{
			StatusBitMask: fragmentation.FragSessionSetupAnsPayloadStatusBitMask{
				FragIndex:                    pl.FragSession.FragIndex,
				WrongDescriptor:              false,
				FragSessionIndexNotSupported: false,
				NotEnoughMemory:              false,
				EncodingUnsupported:          false,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = 200
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

	nbReceived := 0
	if d.fuotaProperties.FragSessionStatusReqPayload != nil {
		nbReceived = int(d.fuotaProperties.FragSessionSetupReqPayload.NbFrag)
	}

	cmd := fragmentation.Command{
		CID: fragmentation.FragSessionStatusAns,
		Payload: &fragmentation.FragSessionStatusAnsPayload{
			ReceivedAndIndex: fragmentation.FragSessionStatusAnsPayloadReceivedAndIndex{
				FragIndex:      pl.FragStatusReqParam.FragIndex,
				NbFragReceived: uint16(nbReceived),
			},
			MissingFrag: 0,
			Status: fragmentation.FragSessionStatusAnsPayloadStatus{
				NotEnoughMatrixMemory: false,
			},
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	d.payload = b
	d.fPort = 200
	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.FragSessionStatusReqPayload = pl
	return nil
}
