package brb02

import (
	"context"
	"fmt"
	"io"
)

// Official EXE 0x5409a8 constructs A5 05 15 LEVEL CHECKSUM. Its
// callback at 0x565f70 maps the captured audio magnitude to levels 0..3.
func AudioLevelReport(level byte) ([]byte, error) {
	if level > 3 {
		return nil, fmt.Errorf("音频强度需要 0～3")
	}
	p, _ := BuildPacket(0x15, []byte{level})
	return PacketReport(p)
}

// Official EXE 0x540ae6 constructs A5 04 16 BF, with no key/button value.
func InputActivityReport() []byte {
	p, _ := BuildPacket(0x16, nil)
	r, _ := PacketReport(p)
	return r
}

// Host event delivery is one-way; no response establishes the visual effect.
// Event collection and effect selection belong to the calling application.
func (c *Controller) SendAudioLevel(ctx context.Context, level byte) (Delivery, error) {
	report, err := AudioLevelReport(level)
	if err != nil {
		return Delivery{}, err
	}
	return c.sendOneWay(ctx, report, "音频强度")
}

func (c *Controller) SendInputActivity(ctx context.Context) (Delivery, error) {
	return c.sendOneWay(ctx, InputActivityReport(), "输入活动")
}

func (c *Controller) sendOneWay(ctx context.Context, report []byte, label string) (Delivery, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return Delivery{}, err
	}
	defer c.commandMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Delivery{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.demo || c.dev == nil {
		return Delivery{}, fmt.Errorf("%s推送需要已连接的真实设备", label)
	}
	n, err := c.writeReportLocked(report)
	if err == nil && n != len(report) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("%s推送失败: %w", label, err)
	}
	return Delivery{true, label + "已发送；实际效果需在设备上观察"}, nil
}
