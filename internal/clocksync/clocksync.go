/*
 * clocksync.go - LoRaWAN Application Layer Clock Synchronization
 *
 * 该文件实现了LoRaWAN应用层时钟同步协议（Application Layer Clock Synchronization v1.0.0），
 * 用于FUOTA过程中的设备时间同步。主要功能包括：
 * - 定义时钟同步相关的命令标识符（CID）
 * - 实现设备时间请求和应答命令
 * - 处理设备与服务器之间的时间校正
 *
 * 该协议在FUOTA过程中确保所有设备在同步的时间窗口内接收固件更新，
 * 从而提高多播传输的效率。
 */

//go:generate stringer -type=CID

// Package clocksync implements the LoRaWAN Application Layer Clock Synchronization v1.0.0.
package clocksync

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// CID defines the command identifier.
type CID byte

// DefaultFPort defines the default fPort value for Clock Synchronization.
const DefaultFPort uint8 = 202

// Available command identifiers.
const (
	PackageVersionReq    CID = 0x00
	PackageVersionAns    CID = 0x00
	DeviceAppTimeReq     CID = 0x01
	DeviceAppTimeAns     CID = 0x01
	DeviceTimeStampReq   CID = 0x02
	DeviceTimeStampAns   CID = 0x02
	ForceDeviceResyncReq CID = 0x03
	ForceDeviceResyncAns CID = 0x03
)

// Errors
var (
	ErrNoPayloadForCID = errors.New("lorawan/applayer/clocksync: no payload for given CID")
)

// map[uplink]...
// uplink=false: 下行命令（服务器→设备）
// uplink=true: 上行命令（设备→服务器）
var commandPayloadRegistry = map[bool]map[CID]func() CommandPayload{
	false: {
		// 服务器发给设备的命令
		PackageVersionReq:    func() CommandPayload { return &PackageVersionReqPayload{} },
		DeviceAppTimeAns:     func() CommandPayload { return &DeviceAppTimeAnsPayload{} },
		DeviceTimeStampReq:   func() CommandPayload { return &DeviceTimeStampReqPayload{} },
		ForceDeviceResyncReq: func() CommandPayload { return &ForceDeviceResyncReqPayload{} },
	},
	true: {
		// 设备发给服务器的命令
		PackageVersionAns:    func() CommandPayload { return &PackageVersionAnsPayload{} },
		DeviceAppTimeReq:     func() CommandPayload { return &DeviceAppTimeReqPayload{} },
		DeviceTimeStampAns:   func() CommandPayload { return &DeviceTimeStampAnsPayload{} },
		ForceDeviceResyncAns: func() CommandPayload { return &ForceDeviceResyncAnsPayload{} },
	},
}

// GetCommandPayload returns a new CommandPayload for the given CID.
func GetCommandPayload(uplink bool, c CID) (CommandPayload, error) {
	v, ok := commandPayloadRegistry[uplink][c]
	if !ok {
		return nil, ErrNoPayloadForCID
	}

	return v(), nil
}

// CommandPayload defines the interface that a command payload must implement.
type CommandPayload interface {
	MarshalBinary() (data []byte, err error)
	UnmarshalBinary(data []byte) error
	Size() int
}

// Command defines the Command structure.
type Command struct {
	CID     CID
	Payload CommandPayload
}

// MarshalBinary encodes the command to a slice of bytes.
func (c Command) MarshalBinary() ([]byte, error) {
	b := []byte{byte(c.CID)}
	if c.Payload != nil {
		p, err := c.Payload.MarshalBinary()
		if err != nil {
			return nil, err
		}
		b = append(b, p...)
	}
	return b, nil
}

// UnmarshalBinary decodes a command from a slice of bytes.
func (c *Command) UnmarshalBinary(uplink bool, data []byte) error {
	if len(data) == 0 {
		return errors.New("lorawan/applayer/clocksync: at least 1 byte is expected")
	}

	c.CID = CID(data[0])

	p, err := GetCommandPayload(uplink, c.CID)
	if err != nil {
		if err == ErrNoPayloadForCID {
			return nil
		}
		return err
	}

	c.Payload = p

	if len(data) > 1 {
		if err := c.Payload.UnmarshalBinary(data[1:]); err != nil {
			return err
		}
	}

	return nil
}

// Size returns the size of the command in bytes.
func (c Command) Size() int {
	if c.Payload != nil {
		return c.Payload.Size() + 1
	}
	return 1
}

// PackageVersionAnsPayload implements the PackageVersionAns payload.
type PackageVersionAnsPayload struct {
	PackageIdentifier uint8
	PackageVersion    uint8
}

// Size returns the size of the payload in bytes.
func (p PackageVersionAnsPayload) Size() int {
	return 2
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p PackageVersionAnsPayload) MarshalBinary() ([]byte, error) {
	return []byte{p.PackageIdentifier, p.PackageVersion}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *PackageVersionAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) != 2 {
		return fmt.Errorf("lorawan/applayer/clocksync: %d bytes are expected", 2)
	}
	p.PackageIdentifier = data[0]
	p.PackageVersion = data[1]
	return nil
}

// PackageVersionReqPayload implements the PackageVersionReq payload (empty).
type PackageVersionReqPayload struct{}

// Size returns the size of the payload in bytes.
func (p PackageVersionReqPayload) Size() int {
	return 0
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p PackageVersionReqPayload) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *PackageVersionReqPayload) UnmarshalBinary(data []byte) error {
	return nil
}

// DeviceAppTimeReqPayload implements the DeviceAppTimeReq payload.
// 设备发送此命令请求时间同步，包含设备当前时间（自1970-01-01 00:00:00 UTC起的秒数）
type DeviceAppTimeReqPayload struct {
	DeviceTime uint32 // 设备时间（秒）
	TokenReq   uint8  // 可选：用于关联请求和应答的令牌
}

// Size returns the size of the payload in bytes.
func (p DeviceAppTimeReqPayload) Size() int {
	return 4 + 1 // DeviceTime(4) + TokenReq(1)
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p DeviceAppTimeReqPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, 5)
	binary.LittleEndian.PutUint32(b[0:4], p.DeviceTime)
	b[4] = p.TokenReq
	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *DeviceAppTimeReqPayload) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("lorawan/applayer/clocksync: at least 4 bytes are expected, got %d", len(data))
	}
	p.DeviceTime = binary.LittleEndian.Uint32(data[0:4])
	if len(data) >= 5 {
		p.TokenReq = data[4]
	}
	return nil
}

// DeviceAppTimeAnsPayload implements the DeviceAppTimeAns payload.
// 服务器发送此命令响应时间同步请求，包含时间校正值
type DeviceAppTimeAnsPayload struct {
	TimeCorrection uint32 // 时间校正值（秒），设备需要加上此值来同步时间
	TokenAns       uint8  // 对应请求中的 TokenReq
}

// Size returns the size of the payload in bytes.
func (p DeviceAppTimeAnsPayload) Size() int {
	return 4 + 1 // TimeCorrection(4) + TokenAns(1)
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p DeviceAppTimeAnsPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, 5)
	binary.LittleEndian.PutUint32(b[0:4], p.TimeCorrection)
	b[4] = p.TokenAns
	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *DeviceAppTimeAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("lorawan/applayer/clocksync: at least 4 bytes are expected, got %d", len(data))
	}
	p.TimeCorrection = binary.LittleEndian.Uint32(data[0:4])
	if len(data) >= 5 {
		p.TokenAns = data[4]
	}
	return nil
}

// DeviceTimeStampReqPayload implements the DeviceTimeStampReq payload (empty).
type DeviceTimeStampReqPayload struct{}

// Size returns the size of the payload in bytes.
func (p DeviceTimeStampReqPayload) Size() int {
	return 0
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p DeviceTimeStampReqPayload) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *DeviceTimeStampReqPayload) UnmarshalBinary(data []byte) error {
	return nil
}

// DeviceTimeStampAnsPayload implements the DeviceTimeStampAns payload (empty).
type DeviceTimeStampAnsPayload struct{}

// Size returns the size of the payload in bytes.
func (p DeviceTimeStampAnsPayload) Size() int {
	return 0
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p DeviceTimeStampAnsPayload) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *DeviceTimeStampAnsPayload) UnmarshalBinary(data []byte) error {
	return nil
}

// ForceDeviceResyncReqPayload implements the ForceDeviceResyncReq payload.
type ForceDeviceResyncReqPayload struct {
	NbTransmissions uint8
}

// Size returns the size of the payload in bytes.
func (p ForceDeviceResyncReqPayload) Size() int {
	return 1
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p ForceDeviceResyncReqPayload) MarshalBinary() ([]byte, error) {
	return []byte{p.NbTransmissions}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *ForceDeviceResyncReqPayload) UnmarshalBinary(data []byte) error {
	if len(data) != 1 {
		return fmt.Errorf("lorawan/applayer/clocksync: 1 byte is expected, got %d", len(data))
	}
	p.NbTransmissions = data[0]
	return nil
}

// ForceDeviceResyncAnsPayload implements the ForceDeviceResyncAns payload (empty).
type ForceDeviceResyncAnsPayload struct{}

// Size returns the size of the payload in bytes.
func (p ForceDeviceResyncAnsPayload) Size() int {
	return 0
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p ForceDeviceResyncAnsPayload) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *ForceDeviceResyncAnsPayload) UnmarshalBinary(data []byte) error {
	return nil
}
