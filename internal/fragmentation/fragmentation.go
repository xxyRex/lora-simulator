/*
 * fragmentation.go - LoRaWAN分片数据块传输协议实现文件
 *
 * 该文件实现了LoRaWAN分片数据块传输协议（Fragmented Data Block Transport v1.0.0），
 * 用于FUOTA过程中的固件分片传输。主要功能包括：
 * - 定义分片会话相关的命令标识符（CID）
 * - 实现分片会话的设置、状态查询和删除等命令
 * - 提供数据分片的编码和传输功能
 * - 处理设备对分片命令的响应
 *
 * 该协议在FUOTA过程中负责将大型固件文件分割成小片段，并通过LoRaWAN网络可靠地传输到设备。
 */

//go:generate stringer -type=CID

// Package fragmentation implements the Fragmented Data Block Transport v1.0.0 over LoRaWAN.
package fragmentation

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// CID defines the command identifier.
type CID byte

// DefaultFPort defines the default fPort value for Fragmented Data Block Transport.
const DefaultFPort uint8 = 201

// Available command identifier.
const (
	PackageVersionReq    CID = 0x00
	PackageVersionAns    CID = 0x00
	FragSessionStatusReq CID = 0x01
	FragSessionStatusAns CID = 0x01
	FragSessionSetupReq  CID = 0x02
	FragSessionSetupAns  CID = 0x02
	FragSessionDeleteReq CID = 0x03
	FragSessionDeleteAns CID = 0x03
	FragCustomCrcAns     CID = 0x05
	DataFragment         CID = 0x08
)

// Errors
var (
	ErrNoPayloadForCID = errors.New("lorawan/applayer/fragmentation: no payload for given CID")
)

// map[uplink]...
var commandPayloadRegistry = map[bool]map[CID]func() CommandPayload{
	true: {
		PackageVersionAns:    func() CommandPayload { return &PackageVersionAnsPayload{} },
		FragSessionSetupAns:  func() CommandPayload { return &FragSessionSetupAnsPayload{} },
		FragSessionDeleteAns: func() CommandPayload { return &FragSessionDeleteAnsPayload{} },
		FragSessionStatusAns: func() CommandPayload { return &FragSessionStatusAnsPayload{} },
		FragCustomCrcAns:     func() CommandPayload { return &FragCustomCrcAnsPayload{} },
	},
	false: {
		FragSessionSetupReq:  func() CommandPayload { return &FragSessionSetupReqPayload{} },
		FragSessionDeleteReq: func() CommandPayload { return &FragSessionDeleteReqPayload{} },
		DataFragment:         func() CommandPayload { return &DataFragmentPayload{} },
		FragSessionStatusReq: func() CommandPayload { return &FragSessionStatusReqPayload{} },
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

// UnmarshalBinary decodes a slice of bytes into a command.
func (c *Command) UnmarshalBinary(uplink bool, data []byte) error {
	if len(data) == 0 {
		return errors.New("lorawan/applayer/fragmentation: at least 1 byte is expected")
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
	if err := c.Payload.UnmarshalBinary(data[1:]); err != nil {
		return err
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

// Commands defines a slice of commands.
type Commands []Command

// MarshalBinary encodes the commands to a slice of bytes.
func (c Commands) MarshalBinary() ([]byte, error) {
	var out []byte

	for _, cmd := range c {
		b, err := cmd.MarshalBinary()
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// UnmarshalBinary decodes a slice of bytes into a slice of commands.
func (c *Commands) UnmarshalBinary(uplink bool, data []byte) error {
	var i int

	for i < len(data) {
		var cmd Command
		if err := cmd.UnmarshalBinary(uplink, data[i:]); err != nil {
			return err
		}
		i += cmd.Size()
		*c = append(*c, cmd)
	}

	return nil
}

// PackageVersionAnsPayload implements the PackageVersionAns payload.
type PackageVersionAnsPayload struct {
	PackageIdentifier uint8
	PackageVersion    uint8
}

// Size returns the payload size in number of bytes.
func (p PackageVersionAnsPayload) Size() int {
	return 2
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p PackageVersionAnsPayload) MarshalBinary() ([]byte, error) {
	return []byte{
		p.PackageIdentifier,
		p.PackageVersion,
	}, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *PackageVersionAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d bytes are expected", p.Size())
	}

	p.PackageIdentifier = data[0]
	p.PackageVersion = data[1]
	return nil
}

// FragSessionSetupReqPayload implements the FragSessionSetupReq payload.
type FragSessionSetupReqPayload struct {
	FragSession FragSessionSetupReqPayloadFragSession
	NbFrag      uint16
	FragSize    uint8
	Control     FragSessionSetupReqPayloadControl
	Padding     uint8
	Descriptor  [4]byte
}

// FragSessionSetupReqPayloadFragSession implements the FragSessionSetupReq payload FragSession field.
type FragSessionSetupReqPayloadFragSession struct {
	FragIndex      uint8
	McGroupBitMask [4]bool
}

// FragSessionSetupReqPayloadControl implements the FragSessionSetupReq payload Control field.
type FragSessionSetupReqPayloadControl struct {
	FragmentationMatrix uint8
	BlockAckDelay       uint8
}

// Size returns the payload size in number of bytes.
func (p FragSessionSetupReqPayload) Size() int {
	return 10
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionSetupReqPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())

	// FragSession
	for i, mask := range p.FragSession.McGroupBitMask {
		if mask {
			b[0] |= (1 << uint8(i))
		}
	}
	b[0] |= (p.FragSession.FragIndex & 0x03) << 4

	// NbFrag
	binary.LittleEndian.PutUint16(b[1:3], p.NbFrag)

	// FragSize
	b[3] = p.FragSize

	// Control
	b[4] = p.Control.BlockAckDelay & 0x07
	b[4] |= (p.Control.FragmentationMatrix & 0x07) << 3

	// Padding
	b[5] = p.Padding

	// Descriptor
	copy(b[6:10], p.Descriptor[:])

	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionSetupReqPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d bytes are expected", p.Size())
	}

	// Fragmentation
	for i := range p.FragSession.McGroupBitMask {
		p.FragSession.McGroupBitMask[i] = data[0]&(1<<uint8(i)) != 0
	}
	p.FragSession.FragIndex = (data[0] >> 4) & 0x03

	// NbFrag
	p.NbFrag = binary.LittleEndian.Uint16(data[1:3])

	// FragSize
	p.FragSize = data[3]

	// Control
	p.Control.BlockAckDelay = data[4] & 0x07
	p.Control.FragmentationMatrix = (data[4] >> 3) & 0x07

	// Padding
	p.Padding = data[5]

	// Descriptor
	copy(p.Descriptor[:], data[6:10])

	return nil
}

// FragSessionSetupAnsPayload implements the FragSessionSetupAns payload.
type FragSessionSetupAnsPayload struct {
	StatusBitMask FragSessionSetupAnsPayloadStatusBitMask
}

// FragSessionSetupAnsPayloadStatusBitMask implements the FragSessionSetupAns payload StatusBitMask field.
type FragSessionSetupAnsPayloadStatusBitMask struct {
	FragIndex                    uint8
	WrongDescriptor              bool
	FragSessionIndexNotSupported bool
	NotEnoughMemory              bool
	EncodingUnsupported          bool
}

// Size returns the paylaod size in bytes.
func (p FragSessionSetupAnsPayload) Size() int {
	return 1
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionSetupAnsPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())

	if p.StatusBitMask.EncodingUnsupported {
		b[0] |= 0x01
	}

	if p.StatusBitMask.NotEnoughMemory {
		b[0] |= 0x02
	}

	if p.StatusBitMask.FragSessionIndexNotSupported {
		b[0] |= 0x04
	}

	if p.StatusBitMask.WrongDescriptor {
		b[0] |= 0x08
	}

	b[0] |= (p.StatusBitMask.FragIndex & 0x03) << 6

	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionSetupAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d byte is expected", p.Size())
	}

	p.StatusBitMask.EncodingUnsupported = data[0]&0x01 != 0
	p.StatusBitMask.NotEnoughMemory = data[0]&0x02 != 0
	p.StatusBitMask.FragSessionIndexNotSupported = data[0]&0x04 != 0
	p.StatusBitMask.WrongDescriptor = data[0]&0x08 != 0
	p.StatusBitMask.FragIndex = (data[0] >> 6) & 0x03

	return nil
}

// FragSessionDeleteReqPayload implements the FragSessionDeleteReq paylaod.
type FragSessionDeleteReqPayload struct {
	Param FragSessionDeleteReqPayloadParam
}

// FragSessionDeleteReqPayloadParam implements the FragSessionDeleteReq payload Param field.
type FragSessionDeleteReqPayloadParam struct {
	FragIndex uint8
}

// Size returns the payload size in bytes.
func (p FragSessionDeleteReqPayload) Size() int {
	return 1
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionDeleteReqPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())
	b[0] = p.Param.FragIndex & 0x03
	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionDeleteReqPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d byte is expected", p.Size())
	}
	p.Param.FragIndex = data[0] & 0x3
	return nil
}

// FragSessionDeleteAnsPayload implements the FragSessionDeleteAns payload.
type FragSessionDeleteAnsPayload struct {
	Status FragSessionDeleteAnsPayloadStatus
}

// FragSessionDeleteAnsPayloadStatus implements the FragSessionDeleteAns payload Status field.
type FragSessionDeleteAnsPayloadStatus struct {
	FragIndex           uint8
	SessionDoesNotExist bool
}

// Size returns the size of the payload in bytes.
func (p FragSessionDeleteAnsPayload) Size() int {
	return 1
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionDeleteAnsPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())
	b[0] = p.Status.FragIndex & 0x03
	if p.Status.SessionDoesNotExist {
		b[0] |= 0x04
	}
	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionDeleteAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d byte is expected", p.Size())
	}

	p.Status.FragIndex = data[0] & 0x03
	p.Status.SessionDoesNotExist = data[0]&0x04 != 0

	return nil
}

// DataFragmentPayload implements the DataFragment payload.
type DataFragmentPayload struct {
	IndexAndN DataFragmentPayloadIndexAndN
	Payload   []byte
}

// DataFragmentPayloadIndexAndN implements the DataFragment payload IndexAndN field.
type DataFragmentPayloadIndexAndN struct {
	FragIndex uint8
	N         uint16
}

// Size returns the payload size in bytes.
func (p DataFragmentPayload) Size() int {
	return 2 + len(p.Payload)
}

// MarshalBinary encodes the given payload to a slice of bytes.
func (p DataFragmentPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())

	binary.LittleEndian.PutUint16(b[0:2], p.IndexAndN.N&0x3fff)
	b[1] |= (p.IndexAndN.FragIndex & 0x03) << 6
	copy(b[2:], p.Payload)

	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *DataFragmentPayload) UnmarshalBinary(data []byte) error {
	if len(data) < 2 {
		return errors.New("lorawan/applayer/fragmentation: 2 bytes are expected")
	}

	p.IndexAndN.N = binary.LittleEndian.Uint16(data[0:2]) & 0x3fff // filter out the FragIndex
	p.IndexAndN.FragIndex = data[1] >> 6
	p.Payload = make([]byte, len(data[2:]))
	copy(p.Payload, data[2:])

	return nil
}

// FragSessionStatusReqPayload implements the FragSessionStatusReq payload.
type FragSessionStatusReqPayload struct {
	FragStatusReqParam FragSessionStatusReqPayloadFragStatusReqParam
}

// FragSessionStatusReqPayloadFragStatusReqParam implements the FragSessionStatusReq payload FragStatusReqParam field.
type FragSessionStatusReqPayloadFragStatusReqParam struct {
	FragIndex    uint8
	Participants bool
}

// Size returns the payload size in number of bytes.
func (p FragSessionStatusReqPayload) Size() int {
	return 1
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionStatusReqPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())

	if p.FragStatusReqParam.Participants {
		b[0] |= 0x01
	}

	b[0] |= (p.FragStatusReqParam.FragIndex & 0x03) << 1

	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionStatusReqPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d byte is expected", p.Size())
	}

	p.FragStatusReqParam.Participants = data[0]&0x01 != 0
	p.FragStatusReqParam.FragIndex = (data[0] >> 1) & 0x03

	return nil
}

// FragSessionStatusAnsPayload implements the FragSessionStatusAns payload.
type FragSessionStatusAnsPayload struct {
	ReceivedAndIndex FragSessionStatusAnsPayloadReceivedAndIndex
	MissingFrag      uint8
	Status           FragSessionStatusAnsPayloadStatus
}

// FragSessionStatusAnsPayloadReceivedAndIndex implements the FragSessionStatusAns payload ReceivedAndIndex field.
type FragSessionStatusAnsPayloadReceivedAndIndex struct {
	FragIndex      uint8
	NbFragReceived uint16
}

// FragSessionStatusAnsPayloadStatus implements the FragSessionStatusAns payload Status field.
type FragSessionStatusAnsPayloadStatus struct {
	NotEnoughMatrixMemory bool
}

// Size returns the payload size in number of bytes.
func (p FragSessionStatusAnsPayload) Size() int {
	return 4
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragSessionStatusAnsPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())

	binary.LittleEndian.PutUint16(b[0:2], p.ReceivedAndIndex.NbFragReceived&0x3fff)
	b[1] |= (p.ReceivedAndIndex.FragIndex & 0x03) << 6

	b[2] = p.MissingFrag
	if p.Status.NotEnoughMatrixMemory {
		b[3] |= 0x01
	}

	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragSessionStatusAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d bytes are expected", p.Size())
	}

	p.ReceivedAndIndex.NbFragReceived = binary.LittleEndian.Uint16(data[0:2]) & 0x3fff // filter out FragIndex
	p.ReceivedAndIndex.FragIndex = data[1] >> 6

	p.MissingFrag = data[2]
	p.Status.NotEnoughMatrixMemory = data[3]&0x01 != 0

	return nil
}

// FragCustomCrcAnsPayload implements the FragCustomCrcAns payload.
type FragCustomCrcAnsPayload struct {
	CRC uint32
}

// Size returns the payload size in bytes.
func (p FragCustomCrcAnsPayload) Size() int {
	return 4
}

// MarshalBinary encodes the payload to a slice of bytes.
func (p FragCustomCrcAnsPayload) MarshalBinary() ([]byte, error) {
	b := make([]byte, p.Size())
	binary.LittleEndian.PutUint32(b, p.CRC)
	return b, nil
}

// UnmarshalBinary decodes the payload from a slice of bytes.
func (p *FragCustomCrcAnsPayload) UnmarshalBinary(data []byte) error {
	if len(data) < p.Size() {
		return fmt.Errorf("lorawan/applayer/fragmentation: %d bytes are expected", p.Size())
	}

	p.CRC = binary.LittleEndian.Uint32(data[0:4])
	return nil
}

// Encode encodes the given slice of bytes to fragments including forward error correction.
// This is based on the proposed FEC code from the Fragmented Data Block Transport over
// LoRaWAN recommendation.
func Encode(data []byte, fragmentSize int, redundancyRatio float64) ([][]byte, error) {
	if redundancyRatio < 0 || redundancyRatio > 1 {
		return nil, errors.New("redundancyRatio must be between 0 and 1")
	}

	if len(data)%fragmentSize != 0 {
		return nil, errors.New("length of data must be a multiple of the given fragment-size")
	}

	// 计算原始分片数量
	originalFragments := len(data) / fragmentSize

	// 根据比例计算冗余分片数量
	redundancy := int(float64(originalFragments) * redundancyRatio)
	if redundancy < 1 {
		redundancy = 1 // 至少生成1个冗余分片
	}

	// 分片数据
	var dataRows [][]byte
	for i := 0; i < len(data)/fragmentSize; i++ {
		offset := i * fragmentSize
		dataRows = append(dataRows, data[offset:offset+fragmentSize])
	}
	w := len(dataRows)

	// 生成冗余分片
	for y := 0; y < redundancy; y++ {
		s := make([]byte, fragmentSize)
		a := matrixLine(y+1, w)

		for x := 0; x < w; x++ {
			if a[x] == 1 {
				for m := 0; m < fragmentSize; m++ {
					s[m] ^= dataRows[x][m]
				}
			}
		}

		dataRows = append(dataRows, s)
	}

	return dataRows, nil
}

func prbs23(x int) int {
	b0 := x & 1
	b1 := (x & 32) / 32
	return (x / 2) + (b0^b1)*(1<<22)
}

func isPower2(num int) bool {
	return num != 0 && (num&(num-1)) == 0
}

func matrixLine(n, m int) []int {
	line := make([]int, m)

	mm := 0
	if isPower2(m) {
		mm = 1
	}

	x := 1 + (1001 * n)

	for nbCoeff := 0; nbCoeff < m/2; nbCoeff++ {
		r := 1 << 16
		for r >= m {
			x = prbs23(x)
			r = x % (m + mm)
		}
		line[r] = 1
	}

	return line
}
