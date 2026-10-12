package brb02

import (
	"context"
	"fmt"
)

// Official UI +0x4c is checkBoxSmart_start_and_stop; +0x60 is
// checkBoxPowerOnSelfStarting. EXE 0x5701e0/0x570230 pass these in order.
type DeviceOptions struct {
	SmartStartStop bool `json:"smartStartStop"`
	PowerOnStart   bool `json:"powerOnStart"`
}

type DeviceOptionsPatch struct {
	SmartStartStop *bool `json:"smartStartStop,omitempty"`
	PowerOnStart   *bool `json:"powerOnStart,omitempty"`
}

func (patch DeviceOptionsPatch) Validate() error {
	if patch.SmartStartStop == nil && patch.PowerOnStart == nil {
		return fmt.Errorf("需要指定智能启停或上电自启动")
	}
	return nil
}

func (options DeviceOptions) payload() []byte {
	p := []byte{0, 0}
	if options.SmartStartStop {
		p[0] = 1
	}
	if options.PowerOnStart {
		p[1] = 1
	}
	return p
}

func (options DeviceOptions) Report() ([]byte, error) {
	p, _ := BuildPacket(2, options.payload())
	return PacketReport(p)
}

func (f Frame) DeviceOptions() (DeviceOptions, error) {
	if f.Command != 3 || len(f.Payload) != 2 || f.Payload[0] > 1 || f.Payload[1] > 1 {
		return DeviceOptions{}, fmt.Errorf("未知设备选项响应 %x", f.Payload)
	}
	return DeviceOptions{f.Payload[0] == 1, f.Payload[1] == 1}, nil
}

func (c *Controller) GetDeviceOptions(ctx context.Context) (DeviceOptions, error) {
	f, err := c.Query(ctx, "device")
	if err != nil {
		return DeviceOptions{}, err
	}
	return f.DeviceOptions()
}

type DeviceOptionsResult struct {
	Options   DeviceOptions `json:"options"`
	Confirmed bool          `json:"confirmed"`
	Message   string        `json:"message"`
}

// UpdateDeviceOptions reads before patching, so changing either checkbox
// preserves the other. Both bytes must match query 03 to confirm the write.
func (c *Controller) UpdateDeviceOptions(ctx context.Context, patch DeviceOptionsPatch) (DeviceOptionsResult, error) {
	if err := patch.Validate(); err != nil {
		return DeviceOptionsResult{}, err
	}
	if err := c.commandMu.Lock(ctx); err != nil {
		return DeviceOptionsResult{}, err
	}
	defer c.commandMu.Unlock()
	query, _ := QueryReport("device")
	f, err := c.exchangeLocked(ctx, query)
	if err != nil {
		return DeviceOptionsResult{}, err
	}
	options, err := f.DeviceOptions()
	if err != nil {
		return DeviceOptionsResult{}, err
	}
	if patch.SmartStartStop != nil {
		options.SmartStartStop = *patch.SmartStartStop
	}
	if patch.PowerOnStart != nil {
		options.PowerOnStart = *patch.PowerOnStart
	}
	report, _ := options.Report()
	if err := c.verifyLocked(ctx, report, query, options.payload()); err != nil {
		return DeviceOptionsResult{Options: options}, err
	}
	return DeviceOptionsResult{options, true, "设备智能启停与上电自启动选项已读回匹配"}, nil
}
