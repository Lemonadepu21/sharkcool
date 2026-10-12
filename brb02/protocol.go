package brb02

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const VID = 0xe2b7
const PID = 0x7001
const ReportSize = 64

// Official DLL VA 0x1000cf30: last byte = sum of preceding bytes modulo 256.
// Frame: A5, total length including header/checksum, command, data, checksum.
func BuildPacket(command byte, payload []byte) ([]byte, error) {
	if len(payload) > ReportSize-4 {
		return nil, fmt.Errorf("协议数据过长")
	}
	b := make([]byte, len(payload)+4)
	b[0] = 0xa5
	b[1] = byte(len(b))
	b[2] = command
	copy(b[3:], payload)
	b[len(b)-1] = checksum(b[:len(b)-1])
	return b, nil
}

func checksum(b []byte) byte {
	var sum byte
	for _, v := range b {
		sum += v
	}
	return sum
}

func PacketReport(packet []byte) ([]byte, error) {
	if len(packet) < 4 || (packet[0] != 0xa5 && packet[0] != 0xa4) {
		return nil, fmt.Errorf("需要不含报告 ID 的 A5/A4 协议帧")
	}
	if f, err := ParseReport(packet); err != nil {
		return nil, err
	} else if !f.ChecksumValid {
		return nil, fmt.Errorf("发送帧校验和不匹配")
	}
	r := make([]byte, ReportSize+1)
	copy(r[1:], packet[:int(packet[1])])
	return r, nil
}

// Mode 2/3 are Linux fixed-speed presets, not firmware preset numbers.
func ModeReport(mode int) ([]byte, error) {
	switch mode {
	case 2:
		return FixedSpeedReport(2912)
	case 3:
		return FixedSpeedReport(3534)
	}
	return nil, fmt.Errorf("仅支持 Linux 转速预设 2、3")
}

// Official short configuration and hardware read-back confirm fixed type = 1.
// Supported app range is conservative; it is not the device's advertised limit.
func FixedSpeedReport(rpm int) ([]byte, error) {
	if rpm < 1200 || rpm > 3600 {
		return nil, fmt.Errorf("当前支持固定转速 1200～3600 RPM")
	}
	b, _ := BuildPacket(0x24, []byte{0, 0, 1, byte(rpm), byte(rpm >> 8)})
	return PacketReport(b)
}

func (f Frame) FixedRPM() (int, bool) {
	if f.Command != 0x25 || len(f.Payload) != 5 || f.Payload[0] != 0 || f.Payload[1] != 0 || f.Payload[2] != 1 {
		return 0, false
	}
	return int(binary.LittleEndian.Uint16(f.Payload[3:])), true
}

// Builders recovered from official DLL exports. No factory/reset/boot commands.
func SwitchReport(kind string, on bool) ([]byte, error) {
	commands := map[string]byte{"fan": 0x20, "rgb": 0x10, "lcd": 0xc0}
	command, ok := commands[kind]
	if !ok {
		return nil, fmt.Errorf("开关必须为 fan、rgb 或 lcd")
	}
	value := byte(0)
	if on {
		value = 1
	}
	b, _ := BuildPacket(command, []byte{value})
	return PacketReport(b)
}

func QueryReport(kind string) ([]byte, error) {
	commands := map[string]byte{"firmware": 1, "device": 3, "cooling": 0x25, "rgb": 0x13, "rgb-switch": 0x11, "lcd-switch": 0xc1, "source": 0x23, "screen-info": 0xc5, "fan-switch": 0x21}
	command, ok := commands[kind]
	if !ok {
		return nil, fmt.Errorf("不支持查询 %q", kind)
	}
	b, _ := BuildPacket(command, nil)
	return PacketReport(b)
}

func reportPacket(report []byte) ([]byte, error) {
	if len(report) != ReportSize+1 || report[0] != 0 {
		return nil, fmt.Errorf("传输输入必须为 65 字节 HID 报告")
	}
	packet := report[1:]
	if _, err := ParseReport(packet); err != nil {
		return nil, err
	}
	if checksum(packet[:int(packet[1])-1]) != packet[int(packet[1])-1] {
		return nil, fmt.Errorf("发送帧校验和不匹配")
	}
	return append([]byte{}, packet[:int(packet[1])]...), nil
}

type Frame struct {
	Command       byte
	Payload       []byte
	ChecksumValid bool
	Long          bool
	First         bool
	Remaining     uint16
}

func ParseReport(b []byte) (Frame, error) {
	if len(b) > 1 && b[0] == 0 && (b[1] == 0xa5 || b[1] == 0xa4) {
		b = b[1:]
	}
	if len(b) < 4 || (b[0] != 0xa5 && b[0] != 0xa4) {
		return Frame{}, fmt.Errorf("无效的 A5/A4 报告")
	}
	n := int(b[1])
	minimum := 4
	if b[0] == 0xa4 {
		minimum = 6
	}
	if n < minimum || n > ReportSize || n > len(b) {
		return Frame{}, fmt.Errorf("帧长度无效或被截断")
	}
	if b[0] == 0xa4 {
		count := binary.LittleEndian.Uint16(b[2:4])
		if count&0x7fff == 0 {
			return Frame{}, fmt.Errorf("分片剩余数量不能为零")
		}
		return Frame{Command: b[4], Payload: append([]byte{}, b[5:n-1]...), ChecksumValid: checksum(b[:n-1]) == b[n-1], Long: true, First: count&0x8000 != 0, Remaining: count & 0x7fff}, nil
	}
	// Actual firmware replies do not consistently compute the trailing sum.
	// The official receive parser also uses header/length without rejecting it.
	return Frame{Command: b[2], Payload: append([]byte{}, b[3:n-1]...), ChecksumValid: checksum(b[:n-1]) == b[n-1]}, nil
}

func (f Frame) Heartbeat() (rpm, field uint16, ok bool) {
	if f.Long || f.Command != 6 || len(f.Payload) != 3 {
		return 0, 0, false
	}
	return binary.LittleEndian.Uint16(f.Payload), uint16(f.Payload[2]), true
}

func (f Frame) ConfigACK() bool {
	return !f.Long && f.Command == 0x24 && len(f.Payload) == 1 && f.Payload[0] == 0
}

// Reassemble BLE fragments/multiple frames and strip USB report ID/zero padding.
type Decoder struct{ pending []byte }

func (d *Decoder) Feed(chunk []byte) []Frame {
	d.pending = append(d.pending, chunk...)
	frames := []Frame{}
	for len(d.pending) > 0 {
		start := bytes.IndexByte(d.pending, 0xa5)
		if other := bytes.IndexByte(d.pending, 0xa4); other >= 0 && (start < 0 || other < start) {
			start = other
		}
		if start < 0 {
			d.pending = nil
			break
		}
		d.pending = d.pending[start:]
		if len(d.pending) < 2 {
			break
		}
		n := int(d.pending[1])
		minimum := 4
		if d.pending[0] == 0xa4 {
			minimum = 6
		}
		if n < minimum || n > ReportSize {
			d.pending = d.pending[1:]
			continue
		}
		if len(d.pending) < n {
			break
		}
		f, err := ParseReport(d.pending[:n])
		if err != nil {
			d.pending = d.pending[1:]
			continue
		}
		frames = append(frames, f)
		d.pending = d.pending[n:]
	}
	return frames
}
