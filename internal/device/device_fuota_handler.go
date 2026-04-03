package device

import (
	"crypto/aes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/brocaar/lora-simulator/internal/clocksync"
	"github.com/brocaar/lora-simulator/internal/fragmentation"
	"github.com/brocaar/lora-simulator/internal/multicastsetup"
	"github.com/brocaar/lorawan"
	log "github.com/sirupsen/logrus"
)

func (d *Device) handleMulticastSetupCommand(b []byte) error {
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
		"b_len":   len(b),
		"b_hex":   fmt.Sprintf("%X", b),
	}).Info("fuota: handleMulticastSetupCommand raw bytes")
	// 特判：PackageVersionReq 命令没有 payload，只有 CID 字节
	if len(b) > 0 && b[0] == byte(multicastsetup.PackageVersionReq) {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
			"cid":     multicastsetup.PackageVersionReq,
		}).Info("fuota: multicast-setup command received")
		return d.handlePackageVersionReq()
	}

	// 其他命令的正常解析流程
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

func (d *Device) handlePackageVersionReq() error {
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
		"cid":     multicastsetup.PackageVersionReq,
	}).Info("fuota: package-version-req received")

	// 读取动态配置（用于调试/测试）
	config := GetDynamicDevicesConfig(d.devEUI)
	var packageVersionDebug *PackageVersionAns = nil

	if config != nil {
		packageVersionDebug = config.Devices.FuotaDebug.PackageVersionAns
	}

	// 默认值：PackageIdentifier=2（Remote Multicast Setup软件包标识），PackageVersion=1（v1.0）
	packageIdentifier := uint8(2)
	packageVersion := uint8(1)

	// 如果配置了调试参数，使用配置值
	if packageVersionDebug != nil {
		if packageVersionDebug.SkipPackageVersionAns {
			log.Info("fuota: package-version-req received, skipping response")
			return nil
		}

		packageIdentifier = packageVersionDebug.PackageIdentifier
		packageVersion = packageVersionDebug.PackageVersion

		// 随机延迟，模拟乱序
		if packageVersionDebug.RandomDelayMaxSec > 0 {
			delay := time.Duration(rand.Int63n(packageVersionDebug.RandomDelayMaxSec+1)) * time.Second
			log.WithFields(log.Fields{
				"dev_eui": d.devEUI,
				"delay":   delay,
			}).Info("fuota: package-version-ans random delay")
			time.Sleep(delay)
		}
	}

	// 构造应答消息
	cmd := multicastsetup.Command{
		CID: multicastsetup.PackageVersionAns,
		Payload: &multicastsetup.PackageVersionAnsPayload{
			PackageIdentifier: packageIdentifier,
			PackageVersion:    packageVersion,
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	log.WithFields(log.Fields{
		"dev_eui":            d.devEUI,
		"package_identifier": packageIdentifier,
		"package_version":    packageVersion,
		"payload":            fmt.Sprintf("% X", b),
	}).Info("fuota: sending package-version-ans")

	// 如果开启双包模式，先发一条正确的 Ans（PackageIdentifier=2, PackageVersion=1），再发配置的错误 Ans
	if packageVersionDebug != nil && packageVersionDebug.SendDoubleAns {
		correctCmd := multicastsetup.Command{
			CID: multicastsetup.PackageVersionAns,
			Payload: &multicastsetup.PackageVersionAnsPayload{
				PackageIdentifier: 2,
				PackageVersion:    1,
			},
		}
		correctB, err := correctCmd.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal correct command error: %w", err)
		}
		log.WithFields(log.Fields{
			"dev_eui":            d.devEUI,
			"package_identifier": 2,
			"package_version":    1,
			"payload":            fmt.Sprintf("% X", correctB),
		}).Info("fuota: sending double package-version-ans (correct, first)")
		d.payload = correctB
		d.fPort = multicastsetup.DefaultFPort
		d.dataUp(lorawan.UnconfirmedDataUp, false)
	}

	// 发送配置的 Ans（错误或正常）
	d.payload = b
	d.fPort = multicastsetup.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)

	return nil
}

// handleFragmentationPackageVersionReq 响应分片软件包版本请求（fPort=201, CID=0x00）
// 根据文档：PackageIdentifier=3，PackageVersion=1
func (d *Device) handleFragmentationPackageVersionReq() error {
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
		"cid":     fragmentation.PackageVersionReq,
	}).Info("fuota: fragmentation package-version-req received")

	// 读取动态配置（用于调试/测试）
	cfg := GetDynamicDevicesConfig(d.devEUI)
	var fragPkgVerDebug *FragPackageVersionAns

	if cfg != nil {
		fragPkgVerDebug = cfg.Devices.FuotaDebug.FragPackageVersionAns
	}

	// 默认值：PackageIdentifier=3（分片软件包标识），PackageVersion=1（v1.0）
	packageIdentifier := uint8(3)
	packageVersion := uint8(1)

	if fragPkgVerDebug != nil {
		if fragPkgVerDebug.SkipFragPackageVersionAns {
			log.Info("fuota: fragmentation package-version-req received, skipping response")
			return nil
		}

		packageIdentifier = fragPkgVerDebug.PackageIdentifier
		packageVersion = fragPkgVerDebug.PackageVersion

		// 随机延迟，模拟乱序
		if fragPkgVerDebug.RandomDelayMaxSec > 0 {
			delay := time.Duration(rand.Int63n(fragPkgVerDebug.RandomDelayMaxSec+1)) * time.Second
			log.WithFields(log.Fields{
				"dev_eui": d.devEUI,
				"delay":   delay,
			}).Info("fuota: fragmentation package-version-ans random delay")
			time.Sleep(delay)
		}

		// 省略 PackageVersion 字段：只发 [CID, PackageIdentifier]（截断报文）
		if fragPkgVerDebug.OmitPackageVersion {
			b := []byte{byte(fragmentation.PackageVersionAns), packageIdentifier}
			log.WithFields(log.Fields{
				"dev_eui":            d.devEUI,
				"package_identifier": packageIdentifier,
				"payload":            fmt.Sprintf("% X", b),
			}).Info("fuota: sending fragmentation package-version-ans (omit package_version)")
			d.payload = b
			d.fPort = fragmentation.DefaultFPort
			d.dataUp(lorawan.UnconfirmedDataUp, false)
			return nil
		}
	}

	cmd := fragmentation.Command{
		CID: fragmentation.PackageVersionAns,
		Payload: &fragmentation.PackageVersionAnsPayload{
			PackageIdentifier: packageIdentifier,
			PackageVersion:    packageVersion,
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal command error: %w", err)
	}

	log.WithFields(log.Fields{
		"dev_eui":            d.devEUI,
		"package_identifier": packageIdentifier,
		"package_version":    packageVersion,
		"payload":            fmt.Sprintf("% X", b),
	}).Info("fuota: sending fragmentation package-version-ans")

	d.payload = b
	d.fPort = fragmentation.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)

	return nil
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

	// 计算 TimeToStart：SessionTime 是 GPS 纪元（1980-01-06 00:00:00 UTC）以来的秒数，
	// TimeToStart 是设备距 ClassC session 开始的剩余秒数。
	// GPS epoch offset from Unix epoch = 315964800 seconds
	const gpsEpochOffset = int64(315964800)
	currentGPSTime := time.Now().Unix() - gpsEpochOffset
	var calculatedTimeToStart uint32
	if int64(pl.SessionTime) > currentGPSTime {
		calculatedTimeToStart = uint32(int64(pl.SessionTime) - currentGPSTime)
	} else {
		calculatedTimeToStart = 0
	}
	timeToStartPtr := &calculatedTimeToStart

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
	// 特判：Fragmentation PackageVersionReq 命令没有 payload，只有 CID 字节
	if len(data) > 0 && data[0] == byte(fragmentation.PackageVersionReq) {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
			"cid":     fragmentation.PackageVersionReq,
		}).Info("fuota: fragmentation PackageVersionReq received")
		return d.handleFragmentationPackageVersionReq()
	}

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

				// CRC 上报完成后，触发延迟版本上报
				d.scheduleVersionReport(config)
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

	// 使用设备索引计算固定延迟，每台设备间隔 2 秒
	// 设备索引 0 → 0 秒，设备索引 1 → 2 秒，设备索引 1999 → 3998 秒
	delay := time.Duration(d.deviceIndex*2) * time.Second
	log.WithFields(log.Fields{
		"dev_eui":      d.devEUI,
		"device_index": d.deviceIndex,
		"delay":        delay,
	}).Info("fuota: frag-session-status-ans fixed delay")
	time.Sleep(delay)

	d.dataUp(lorawan.UnconfirmedDataUp, false)
	d.fuotaProperties.FragSessionStatusReqPayload = pl

	// FragSessionStatusAns 发送完成后，触发延迟版本上报
	d.scheduleVersionReport(config)
	return nil
}

// scheduleVersionReport 在 FUOTA 完成后延迟上报新固件版本号
func (d *Device) scheduleVersionReport(config *DevicesDynamicConfig) {
	if config == nil || config.Devices.FuotaDebug.UpgradeDebug == nil {
		return
	}
	upgradeDebug := config.Devices.FuotaDebug.UpgradeDebug
	if upgradeDebug.NewFirmwareVersion == "" {
		return
	}

	go func() {
		if upgradeDebug.VersionReportDelaySec > 0 {
			log.WithFields(log.Fields{
				"dev_eui":     d.devEUI,
				"delay_sec":   upgradeDebug.VersionReportDelaySec,
				"new_version": upgradeDebug.NewFirmwareVersion,
			}).Info("fuota: scheduling firmware version report after delay")
			time.Sleep(time.Duration(upgradeDebug.VersionReportDelaySec) * time.Second)
		}

		// 更新内存中的测试数据，写入新固件版本号
		if d.deviceTestData == nil {
			d.deviceTestData = make(map[string]interface{})
		}
		d.deviceTestData["firmware_version"] = upgradeDebug.NewFirmwareVersion

		// 清除文件修改时间缓存，强制 getEncoderData 重新编码
		d.encoderScriptFileModTime = time.Time{}

		// 重置 fPort 为设备默认值，避免遗留 FUOTA fPort（200/201）
		defaultFPort := d.deviceTypeConfig.DefaultFPort
		if defaultFPort > 0 {
			d.fPort = uint8(defaultFPort)
		} else {
			d.fPort = 0
		}

		// 使用新版本号重新编码 payload
		d.getEncoderData()

		log.WithFields(log.Fields{
			"dev_eui":     d.devEUI,
			"new_version": upgradeDebug.NewFirmwareVersion,
			"fport":       d.fPort,
		}).Info("fuota: reporting new firmware version after upgrade")

		d.dataUp(lorawan.UnconfirmedDataUp, false)
	}()
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

// ==================== Clock Synchronization Functions ====================

// handleClockSyncCommand handles clock synchronization commands from the server.
func (d *Device) handleClockSyncCommand(b []byte) error {
	// 其他命令的正常解析流程
	var cmd clocksync.Command
	if err := cmd.UnmarshalBinary(false, b); err != nil {
		return fmt.Errorf("unmarshal clock sync command error: %w", err)
	}

	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
		"cid":     cmd.CID,
	}).Info("fuota: received clock sync command")

	switch cmd.CID {
	case clocksync.DeviceAppTimeAns:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("fuota: entering DeviceAppTimeAns case")
		pl, ok := cmd.Payload.(*clocksync.DeviceAppTimeAnsPayload)
		if !ok {
			return fmt.Errorf("expected *DeviceAppTimeAnsPayload, got: %T", cmd.Payload)
		}
		return d.handleDeviceAppTimeAns(pl)
	case clocksync.ForceDeviceResyncReq:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("fuota: entering ForceDeviceResyncReq case")
		pl, ok := cmd.Payload.(*clocksync.ForceDeviceResyncReqPayload)
		if !ok {
			log.WithFields(log.Fields{
				"dev_eui":      d.devEUI,
				"payload_type": fmt.Sprintf("%T", cmd.Payload),
			}).Error("fuota: ForceDeviceResyncReq type assertion failed")
			return fmt.Errorf("expected *ForceDeviceResyncReqPayload, got: %T", cmd.Payload)
		}
		return d.handleForceDeviceResyncReq(pl)
	default:
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
			"cid":     cmd.CID,
		}).Warn("fuota: unknown clock sync command")
		return fmt.Errorf("unknown clock sync command")
	}
}

// sendDeviceAppTimeReq sends a DeviceAppTimeReq to request time synchronization.
// 设备主动发送时间同步请求
func (d *Device) sendDeviceAppTimeReq() error {
	// 计算设备当前时间（GPS epoch：自1980-01-06 00:00:00 UTC起的秒数）
	// 与服务器保持一致，服务器使用 TimeSinceGPSEpoch()
	const gpsEpochOffset = int64(315964800)
	deviceTime := uint32(time.Now().Unix() - gpsEpochOffset)

	// 如果配置了覆盖值（用于测试），使用配置的固定时间
	cfg := GetDynamicDevicesConfig(d.devEUI)
	if cfg != nil && cfg.Devices.FuotaDebug.ClockSyncDebug != nil {
		debug := cfg.Devices.FuotaDebug.ClockSyncDebug

		// malformed_payload：直接发送原始错误字节，绕过正常编码
		if debug.MalformedPayload != "" {
			hexStr := strings.ReplaceAll(debug.MalformedPayload, " ", "")
			raw, err := hex.DecodeString(hexStr)
			if err != nil {
				return fmt.Errorf("fuota: malformed_payload hex decode error: %w", err)
			}
			log.WithFields(log.Fields{
				"dev_eui": d.devEUI,
				"payload": fmt.Sprintf("% X", raw),
			}).Info("fuota: sending malformed device-app-time-req")
			d.payload = raw
			d.fPort = clocksync.DefaultFPort
			d.dataUp(lorawan.UnconfirmedDataUp, false)
			return nil
		}

		// device_time_override：使用配置的固定时间
		if debug.DeviceTimeOverride != nil {
			deviceTime = uint32(*debug.DeviceTimeOverride)
			log.WithFields(log.Fields{
				"dev_eui":     d.devEUI,
				"device_time": deviceTime,
			}).Info("fuota: using device_time_override for device-app-time-req")
		}
	}

	cmd := clocksync.Command{
		CID: clocksync.DeviceAppTimeReq,
		Payload: &clocksync.DeviceAppTimeReqPayload{
			DeviceTime: deviceTime,
			TokenReq:   0, // 可以使用随机值或序号
		},
	}

	b, err := cmd.MarshalBinary()
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"dev_eui":     d.devEUI,
		"device_time": deviceTime,
	}).Info("fuota: sending device-app-time-req")

	// 设置payload并发送上行
	d.payload = b
	d.fPort = clocksync.DefaultFPort
	d.dataUp(lorawan.UnconfirmedDataUp, false)

	return nil
}

// handleDeviceAppTimeAns handles DeviceAppTimeAns from server.
// 设备接收服务器的时间校正响应
func (d *Device) handleDeviceAppTimeAns(pl *clocksync.DeviceAppTimeAnsPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":         d.devEUI,
		"time_correction": pl.TimeCorrection,
		"token_ans":       pl.TokenAns,
	}).Info("fuota: received device-app-time-ans")

	// 这里可以调整设备时间，但在模拟器中我们只记录日志
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
	}).Info("fuota: clock synchronization completed")

	return nil
}

// handleForceDeviceResyncReq handles ForceDeviceResyncReq from server.
// 服务器强制设备重新同步时间，设备收到后只需发送 AppTimeReq，不回复 ForceDeviceResyncAns
func (d *Device) handleForceDeviceResyncReq(pl *clocksync.ForceDeviceResyncReqPayload) error {
	log.WithFields(log.Fields{
		"dev_eui":          d.devEUI,
		"nb_transmissions": pl.NbTransmissions,
	}).Info("fuota: received force-device-resync-req")

	cfg := GetDynamicDevicesConfig(d.devEUI)
	if cfg != nil && cfg.Devices.FuotaDebug.ClockSyncDebug != nil &&
		cfg.Devices.FuotaDebug.ClockSyncDebug.SkipDeviceAppTimeReq {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Info("fuota: skipping device-app-time-req (timeout simulation)")
		return nil
	}

	// 根据文档：设备收到 ForceDeviceResyncReq 后只发送 AppTimeReq，等待服务器计算并回复 AppTimeAns
	return d.sendDeviceAppTimeReq()
}

