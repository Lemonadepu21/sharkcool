package brb02

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

type CoolingSource byte

const (
	SourceCPU CoolingSource = 0 // EXE 0x57ccc8 / radioButtonCpu at UI +0x2c.
	SourceGPU CoolingSource = 1 // EXE 0x57ccd4 / radioButtonGpu at UI +0x30.
)

func ParseCoolingSource(value string) (CoolingSource, error) {
	switch value {
	case "cpu":
		return SourceCPU, nil
	case "gpu":
		return SourceGPU, nil
	}
	return 0, fmt.Errorf("散热来源必须为 cpu 或 gpu")
}

// CurvePoint is one byte of Celsius and a little-endian RPM value. The
// official UI builds four three-byte points at EXE 0x57bab0..0x57bb40.
type CurvePoint struct {
	Temperature byte   `json:"temperature"`
	RPM         uint16 `json:"rpm"`
}

// CoolingConfig represents a configuration slot. A successful slot read-back
// alone does not establish that this slot became the active configuration.
type CoolingConfig struct {
	Source CoolingSource `json:"source"`
	Slot   byte          `json:"slot"`
	RPM    uint16        `json:"rpm,omitempty"`
	Points []CurvePoint  `json:"points,omitempty"`
}

func coolingSelector(source CoolingSource, slot byte) error {
	if source > SourceGPU || slot < 1 || slot > 4 {
		return fmt.Errorf("散热来源需要 CPU/GPU，配置槽需要 1～4")
	}
	return nil
}

// Query 26 selects fixed (0) or curve (1), then slot. It does not select
// CPU/GPU. Firmware captures confirm 1/1 returns source=0, format=1, slot=1.
func CoolingPresetReport(curve bool, slot byte) ([]byte, error) {
	if slot < 1 || slot > 4 {
		return nil, fmt.Errorf("散热配置槽需要 1～4")
	}
	format := byte(0)
	if curve {
		format = 1
	}
	p, _ := BuildPacket(0x26, []byte{format, slot})
	return PacketReport(p)
}

func (c *Controller) GetCoolingPreset(ctx context.Context, curve bool, slot byte) (Frame, error) {
	report, err := CoolingPresetReport(curve, slot)
	if err != nil {
		return Frame{}, err
	}
	return c.exchangeRead(ctx, report)
}

func (config CoolingConfig) payload() ([]byte, error) {
	if err := coolingSelector(config.Source, config.Slot); err != nil {
		return nil, err
	}
	p := []byte{byte(config.Source), 0, config.Slot}
	if len(config.Points) == 0 {
		if config.RPM < 1200 || config.RPM > 4000 {
			return nil, fmt.Errorf("官方固定转速范围为 1200～4000 RPM")
		}
		p = append(p, byte(config.RPM), byte(config.RPM>>8))
	} else {
		if len(config.Points) != 4 || config.RPM != 0 {
			return nil, fmt.Errorf("曲线需要四个温度/转速点，不能同时指定固定转速")
		}
		p[1] = 1
		for i, point := range config.Points {
			if point.Temperature > 100 || point.RPM < 1200 || point.RPM > 4000 || i > 0 && point.Temperature <= config.Points[i-1].Temperature {
				return nil, fmt.Errorf("曲线温度需要在 0～100 °C 严格递增，转速需要在 1200～4000 RPM")
			}
			p = append(p, point.Temperature, byte(point.RPM), byte(point.RPM>>8))
		}
	}
	return p, nil
}

func (config CoolingConfig) Report() ([]byte, error) {
	p, err := config.payload()
	if err != nil {
		return nil, err
	}
	packet, _ := BuildPacket(0x24, p)
	return PacketReport(packet)
}

func (f Frame) CoolingConfig() (CoolingConfig, error) {
	if f.Command != 0x25 && f.Command != 0x26 || len(f.Payload) < 3 {
		return CoolingConfig{}, fmt.Errorf("不是散热配置响应")
	}
	p := f.Payload
	config := CoolingConfig{Source: CoolingSource(p[0]), Slot: p[2]}
	if err := coolingSelector(config.Source, config.Slot); err != nil {
		return CoolingConfig{}, err
	}
	switch {
	case p[1] == 0 && len(p) == 5:
		config.RPM = binary.LittleEndian.Uint16(p[3:])
	case p[1] == 1 && len(p) == 15:
		for i := 3; i < len(p); i += 3 {
			config.Points = append(config.Points, CurvePoint{p[i], binary.LittleEndian.Uint16(p[i+1:])})
		}
	default:
		return CoolingConfig{}, fmt.Errorf("未知的散热配置格式 %x", p)
	}
	return config, nil
}

func (c *Controller) GetCooling(ctx context.Context) (CoolingConfig, error) {
	f, err := c.Query(ctx, "cooling")
	if err != nil {
		return CoolingConfig{}, err
	}
	return f.CoolingConfig()
}

type CoolingResult struct {
	Config    CoolingConfig `json:"config"`
	Confirmed bool          `json:"confirmed"`
	Active    bool          `json:"active"`
	Message   string        `json:"message"`
}

// SetCooling confirms the addressed configuration slot via 26, then checks
// 25 independently. Curve selection can briefly appear in 25 before firmware
// falls back to fixed mode; require a second observation after one second.
// Active describes these observations, not indefinite temperature regulation.
func (c *Controller) SetCooling(ctx context.Context, config CoolingConfig) (CoolingResult, error) {
	report, err := config.Report()
	if err != nil {
		return CoolingResult{}, err
	}
	query, _ := CoolingPresetReport(len(config.Points) > 0, config.Slot)
	if err := c.commandMu.Lock(ctx); err != nil {
		return CoolingResult{}, err
	}
	defer c.commandMu.Unlock()
	c.mu.Lock()
	generation := c.lost
	c.mu.Unlock()
	expected, _ := config.payload()
	if err := c.verifyLocked(ctx, report, query, expected); err != nil {
		return CoolingResult{Config: config}, err
	}
	currentReport, _ := QueryReport("cooling")
	f, err := c.exchangeReadLocked(ctx, currentReport)
	result := CoolingResult{Config: config, Confirmed: true}
	if err != nil {
		result.Message = "配置槽已读回，但当前生效配置查询失败"
		return result, err
	}
	initialMatches := true
	if len(config.Points) > 0 {
		initialMatches = bytes.Equal(f.Payload, expected)
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			result.Message = "配置槽已读回，但曲线生效观察已取消"
			return result, ctx.Err()
		case <-generation:
			result.Message = "配置槽已读回，但曲线生效观察期间设备断开"
			return result, fmt.Errorf("曲线生效观察期间设备断开")
		case <-timer.C:
		}
		f, err = c.exchangeReadLocked(ctx, currentReport)
		if err != nil {
			result.Message = "配置槽已读回，但延迟生效配置查询失败"
			return result, err
		}
		if initialMatches && !bytes.Equal(f.Payload, expected) {
			result.Message = "曲线槽已保存，但设备随后回退到其他配置；温控未确认运行"
		} else if !initialMatches && bytes.Equal(f.Payload, expected) {
			result.Message = "曲线槽已保存，延迟读回匹配但尚未持续确认生效"
		}
	}
	c.mu.Lock()
	if c.lost != generation || c.dev == nil || c.linkError != nil {
		c.mu.Unlock()
		return result, fmt.Errorf("配置保存后连接已更换，无法确认当前生效状态")
	}
	result.Active = initialMatches && bytes.Equal(f.Payload, expected)
	if result.Active {
		c.status.RequestedMode = 0
		c.status.AcknowledgedMode = 0
		c.status.TargetRPM = int(config.RPM)
		c.status.ConfirmedRPM = int(config.RPM)
	}
	c.mu.Unlock()
	if result.Message == "" {
		result.Message = "配置槽已读回，当前生效配置与该配置不同"
	}
	if result.Active {
		result.Message = "配置槽与当前生效配置均已读回匹配"
	}
	return result, nil
}

func (c *Controller) GetCoolingSource(ctx context.Context) (CoolingSource, error) {
	f, err := c.Query(ctx, "source")
	if err != nil {
		return 0, err
	}
	if len(f.Payload) != 1 || f.Payload[0] > 1 {
		return 0, fmt.Errorf("未知散热来源响应 %x", f.Payload)
	}
	return CoolingSource(f.Payload[0]), nil
}

func (c *Controller) SetCoolingSource(ctx context.Context, source CoolingSource) error {
	if source > SourceGPU {
		return fmt.Errorf("散热来源需要 CPU/GPU")
	}
	p, _ := BuildPacket(0x22, []byte{byte(source)})
	report, _ := PacketReport(p)
	query, _ := QueryReport("source")
	if err := c.commandMu.Lock(ctx); err != nil {
		return err
	}
	defer c.commandMu.Unlock()
	if err := c.verifyLocked(ctx, report, query, []byte{byte(source)}); err != nil {
		return err
	}
	c.mu.Lock()
	c.status.TargetRPM = 0
	c.status.ConfirmedRPM = 0
	c.status.RequestedMode = 0
	c.status.AcknowledgedMode = 0
	c.mu.Unlock()
	return nil
}

// FanEnabled decodes command 21. Recovered from the packaged firmware dispatch
// at 0x1e1b902 and verified against USB firmware 3.0.3 (01 while running).
func (f Frame) FanEnabled() (bool, error) {
	if f.Long || f.Command != 0x21 || len(f.Payload) != 1 || f.Payload[0] > 1 {
		return false, fmt.Errorf("不是有效的风扇开关状态响应")
	}
	return f.Payload[0] == 1, nil
}
