package brb02

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// RGBConfig is the eight-byte configuration built by Brb02CoolerComm.dll
// VA 0x1000e4a0. Palette is retained as its firmware value because its labels
// depend on the effect. Interval is the official speed parameter, not RPM.
type RGBConfig struct {
	Effect     byte    `json:"effect"`
	Palette    byte    `json:"palette"`
	Interval   uint16  `json:"interval"`
	Brightness byte    `json:"brightness"`
	ColorCount byte    `json:"colorCount"`
	Color      [3]byte `json:"color"`
}

// Effect order recovered from the official UI's radio-button IDs at
// 0x56547d..0x5654d3 and response handling at 0x567a10..0x567a1e.
var effectIDs = map[string]byte{
	"flow": 1, "cycle": 2, "breathing": 3, "static": 4,
	"blinking": 5, "reactive": 6, "audio": 7, "refresh": 8,
}

// Official speed slider bounds, VA 0x565915..0x565b7e. Static and host-driven
// audio have no speed slider. Units are kept as firmware interval values.
func intervalBounds(effect byte) (min, max int, ok bool) {
	switch effect {
	case 1:
		return 1500, 4000, true
	case 2:
		return 5000, 15000, true
	case 3:
		return 1000, 5000, true
	case 5:
		return 450, 1050, true
	case 6:
		return 300, 1000, true
	case 8:
		return 250, 750, true
	}
	return 0, 0, false
}

func (c RGBConfig) payload() ([]byte, error) {
	if c.Effect < 1 || c.Effect > 8 || c.Palette > 15 || c.Brightness > 100 || (c.ColorCount != 1 && c.ColorCount != 10) {
		return nil, fmt.Errorf("RGB 配置超出官方字段范围")
	}
	p := []byte{c.Effect | c.Palette<<4, 0, 0, c.Brightness, c.ColorCount, c.Color[0], c.Color[1], c.Color[2]}
	binary.LittleEndian.PutUint16(p[1:3], c.Interval)
	return p, nil
}

func (c RGBConfig) Report() ([]byte, error) {
	p, err := c.payload()
	if err != nil {
		return nil, err
	}
	packet, err := BuildPacket(0x12, p)
	if err != nil {
		return nil, err
	}
	return PacketReport(packet)
}

func (f Frame) RGBConfig() (RGBConfig, error) {
	if f.Command != 0x13 && f.Command != 0x14 || len(f.Payload) != 8 {
		return RGBConfig{}, fmt.Errorf("不是八字节 RGB 配置响应")
	}
	p := f.Payload
	return RGBConfig{p[0] & 15, p[0] >> 4, binary.LittleEndian.Uint16(p[1:3]), p[3], p[4], [3]byte{p[5], p[6], p[7]}}, nil
}

// RGBPatch updates selected fields while preserving the others read from the
// device. Host-driven audio/reactive effects need additional event streaming
// and are intentionally excluded from this self-contained lighting control.
type RGBPatch struct {
	Effect     string `json:"effect,omitempty"`
	ColorMode  string `json:"colorMode,omitempty"`
	Color      string `json:"color,omitempty"`
	Brightness *int   `json:"brightness,omitempty"`
	Speed      *int   `json:"speed,omitempty"`
}

func (p RGBPatch) Validate() error {
	if p.Effect == "" && p.ColorMode == "" && p.Color == "" && p.Brightness == nil && p.Speed == nil {
		return fmt.Errorf("至少指定一项灯光设置")
	}
	if p.ColorMode != "" && p.ColorMode != "single" && p.ColorMode != "multi" {
		return fmt.Errorf("颜色模式需要 single 或 multi")
	}
	if p.ColorMode == "multi" && p.Color != "" {
		return fmt.Errorf("彩色模式无需指定单色")
	}
	if p.Effect != "" {
		if _, ok := effectIDs[p.Effect]; !ok || p.Effect == "audio" || p.Effect == "reactive" {
			return fmt.Errorf("灯效支持 flow、cycle、breathing、static、blinking、refresh")
		}
	}
	if p.Color != "" {
		if _, err := ParseColor(p.Color); err != nil {
			return err
		}
	}
	for name, value := range map[string]*int{"brightness": p.Brightness, "speed": p.Speed} {
		if value != nil && (*value < 0 || *value > 100) {
			return fmt.Errorf("%s 必须为 0～100", name)
		}
	}
	return nil
}

func ParseColor(color string) ([3]byte, error) {
	var rgb [3]byte
	s := strings.TrimPrefix(color, "#")
	if len(s) != 6 {
		return rgb, fmt.Errorf("颜色需要六位十六进制值，如 #00ff48")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return rgb, fmt.Errorf("颜色格式错误: %w", err)
	}
	copy(rgb[:], b)
	return rgb, nil
}

func (p RGBPatch) Apply(current RGBConfig) (RGBConfig, error) {
	if err := p.Validate(); err != nil {
		return RGBConfig{}, err
	}
	if p.Effect != "" {
		current.Effect = effectIDs[p.Effect]
		if min, max, ok := intervalBounds(current.Effect); ok {
			current.Interval = uint16((min + max) / 2)
		}
	}
	if p.Color != "" {
		current.Color, _ = ParseColor(p.Color)
		current.ColorCount = 1 // Official GUI: 单色 = 1, 彩色 = 10 at VA 0x579420.
	}
	if p.ColorMode == "single" {
		current.ColorCount = 1
	} else if p.ColorMode == "multi" {
		current.ColorCount = 10
	}
	if p.Brightness != nil {
		current.Brightness = byte(*p.Brightness)
	}
	if p.Speed != nil {
		min, max, ok := intervalBounds(current.Effect)
		if !ok {
			return RGBConfig{}, fmt.Errorf("当前灯效没有速度参数")
		}
		current.Interval = uint16(max - (max-min)*(*p.Speed)/100)
	}
	if _, err := current.payload(); err != nil {
		return RGBConfig{}, err
	}
	return current, nil
}

type RGBResult struct {
	Config    RGBConfig `json:"config"`
	Confirmed bool      `json:"confirmed"`
	Message   string    `json:"message"`
}

func (c *Controller) GetRGB(ctx context.Context) (RGBConfig, error) {
	f, err := c.Query(ctx, "rgb")
	if err != nil {
		return RGBConfig{}, err
	}
	return f.RGBConfig()
}

// RGBPresetReport uses the official DLL 0x1000d250 nibble packing.
// Palette remains a raw selector until its effect-specific labels are known.
func RGBPresetReport(effect, palette byte) ([]byte, error) {
	if effect < 1 || effect > 8 || palette > 15 {
		return nil, fmt.Errorf("灯效编号需要 1～8，配置选择值需要 0～15")
	}
	p, _ := BuildPacket(0x14, []byte{effect | palette<<4})
	return PacketReport(p)
}

func (c *Controller) GetRGBPreset(ctx context.Context, effect, palette byte) (RGBConfig, error) {
	report, err := RGBPresetReport(effect, palette)
	if err != nil {
		return RGBConfig{}, err
	}
	f, err := c.exchange(ctx, report)
	if err != nil {
		return RGBConfig{}, err
	}
	config, err := f.RGBConfig()
	if err == nil && (config.Effect != effect || config.Palette != palette) {
		err = fmt.Errorf("设备返回的灯效配置选择值不匹配: %x", f.Payload)
	}
	return config, err
}

func (c *Controller) UpdateRGB(ctx context.Context, patch RGBPatch) (RGBResult, error) {
	if err := patch.Validate(); err != nil {
		return RGBResult{}, err
	}
	if err := c.commandMu.Lock(ctx); err != nil {
		return RGBResult{}, err
	}
	defer c.commandMu.Unlock()
	query, _ := QueryReport("rgb")
	f, err := c.exchangeLocked(ctx, query)
	if err != nil {
		return RGBResult{}, err
	}
	current, err := f.RGBConfig()
	if err != nil {
		return RGBResult{}, err
	}
	config, err := patch.Apply(current)
	if err != nil {
		return RGBResult{}, err
	}
	return c.setRGBLocked(ctx, config)
}

// SetRGB restores or applies a complete configuration, confirming every byte
// with query 13. An ACK alone never counts as successful configuration.
func (c *Controller) SetRGB(ctx context.Context, config RGBConfig) (RGBResult, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return RGBResult{}, err
	}
	defer c.commandMu.Unlock()
	return c.setRGBLocked(ctx, config)
}

func (c *Controller) setRGBLocked(ctx context.Context, config RGBConfig) (RGBResult, error) {
	report, err := config.Report()
	if err != nil {
		return RGBResult{}, err
	}
	query, _ := QueryReport("rgb")
	if err := c.verifyLocked(ctx, report, query, report[4:12]); err != nil {
		return RGBResult{Config: config}, err
	}
	return RGBResult{config, true, "已读回匹配的 RGB 配置；可观察灯光效果"}, nil
}

// RGBEnabled decodes the independent light power state (11), not brightness.
func (f Frame) RGBEnabled() (bool, error) {
	if f.Long || f.Command != 0x11 || len(f.Payload) != 1 || f.Payload[0] > 1 {
		return false, fmt.Errorf("不是有效的灯效开关状态响应")
	}
	return f.Payload[0] == 1, nil
}
