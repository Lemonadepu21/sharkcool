package brb02

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type Status struct {
	ConnectionGeneration uint64 `json:"connectionGeneration"`
	Connected            bool   `json:"connected"`
	Demo                 bool   `json:"demo"`
	Device               string `json:"device"`
	RPM                  uint16 `json:"rpm"`
	Field2               uint16 `json:"field2"`
	LastUpdate           int64  `json:"lastUpdate"`
	Stale                bool   `json:"stale"`
	RequestedMode        int    `json:"requestedMode"`
	AcknowledgedMode     int    `json:"acknowledgedMode"`
	Error                string `json:"error"`
	TargetRPM            int    `json:"targetRPM"`
	ConfirmedRPM         int    `json:"confirmedRPM"`
	Transport            string `json:"transport"`
}

type Controller struct {
	mu           sync.Mutex
	commandMu    commandLock
	status       Status
	dev          Transport
	reply        chan Frame
	replyCommand byte
	lost         chan struct{}
	linkCancel   context.CancelFunc
	linkError    error
	idleTimeout  time.Duration
	retryDelay   time.Duration
	screenRead   *screenReadTransfer
	demo         bool
	connector    Connector
}

func (c *Controller) Snapshot() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.Stale = !s.Connected || s.LastUpdate == 0 || time.Now().UnixMilli()-s.LastUpdate > 5000
	return s
}

// Run owns the only reader and reconnects after unplug; shutdown closes the FD.
func (c *Controller) Run(ctx context.Context) {
	if c.demo {
		c.runDemo(ctx)
		return
	}
	idleTimeout, retryDelay := c.idleTimeout, c.retryDelay
	if idleTimeout <= 0 {
		idleTimeout = 10 * time.Second
	}
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	for ctx.Err() == nil {
		var dev Transport
		var path string
		var err error
		if c.connector != nil {
			connectCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			dev, path, err = c.connector(connectCtx)
			cancel()
		} else {
			err = fmt.Errorf("未配置设备连接器")
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil && dev != nil {
			_ = dev.Close()
			dev = nil
		}
		if err == nil && dev == nil {
			err = fmt.Errorf("设备连接器返回空传输")
		}
		if err == nil {
			linkCtx, stopLink := context.WithCancel(ctx)
			c.mu.Lock()
			c.dev = dev
			c.linkCancel, c.linkError = stopLink, nil
			c.lost = make(chan struct{})
			c.status.Connected = true
			c.status.ConnectionGeneration++
			c.status.Device = path
			c.status.Transport = "usb"
			if len(path) >= 6 && path[:6] == "ble://" {
				c.status.Transport = "ble"
			}
			c.status.Error = ""
			c.status.LastUpdate = 0
			c.status.RPM = 0
			c.status.AcknowledgedMode = 0
			c.status.TargetRPM = 0
			c.status.ConfirmedRPM = 0
			c.status.RequestedMode = 0
			c.mu.Unlock()
			buf := make([]byte, 512)
			decoder := Decoder{}
			for ctx.Err() == nil {
				var n int
				readCtx, stopRead := context.WithTimeout(linkCtx, idleTimeout)
				n, err = dev.Read(readCtx, buf)
				stopRead()
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) && linkCtx.Err() == nil {
						err = fmt.Errorf("设备通信超时，正在重新连接: %w", err)
					}
					break
				}
				if n <= 0 || n > len(buf) {
					err = io.ErrUnexpectedEOF
					break
				}
				for _, f := range decoder.Feed(buf[:n]) {
					c.handle(f)
				}
			}
			c.mu.Lock()
			if c.linkError != nil {
				err = c.linkError
			}
			c.dev = nil
			c.linkCancel = nil
			c.screenRead = nil
			c.clearConnectionLocked(err, ctx)
			close(c.lost)
			c.mu.Unlock()
			stopLink()
			_ = dev.Close()
		} else {
			c.mu.Lock()
			c.clearConnectionLocked(err, ctx)
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (c *Controller) clearConnectionLocked(err error, ctx context.Context) {
	c.status.Connected = false
	c.status.RPM = 0
	c.status.LastUpdate = 0
	c.status.TargetRPM = 0
	c.status.ConfirmedRPM = 0
	c.status.RequestedMode = 0
	c.status.AcknowledgedMode = 0
	if err != nil && ctx.Err() == nil {
		c.status.Error = err.Error()
	}
}

// c.mu is held for a complete report. A failed write also wakes the reader,
// so a live file descriptor cannot leave a broken link connected indefinitely.
func (c *Controller) writeReportLocked(report []byte) (int, error) {
	if c.dev == nil {
		return 0, fmt.Errorf("设备未连接")
	}
	if c.linkError != nil {
		return 0, c.linkError
	}
	n, err := c.dev.Write(report)
	if err == nil && n != len(report) {
		err = io.ErrShortWrite
	}
	if err != nil && c.linkCancel != nil {
		c.linkError = err
		c.linkCancel()
	}
	return n, err
}

func (c *Controller) handle(f Frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.Long {
		c.handleScreenFragmentLocked(f)
		return
	}
	if c.screenRead != nil && f.Command == 0xc7 && (len(f.Payload) != 1 || f.Payload[0] != 0) {
		c.screenRead.done <- screenReadResult{err: fmt.Errorf("设备拒绝读取屏保: %x", f.Payload)}
		c.screenRead = nil
	}
	if rpm, field, ok := f.Heartbeat(); ok {
		c.status.RPM = rpm
		c.status.Field2 = field
		c.status.LastUpdate = time.Now().UnixMilli()
	}
	if c.reply != nil && f.Command == c.replyCommand {
		select {
		case c.reply <- f:
		default:
		}
	}

}

type ModeResult struct {
	Sent         bool   `json:"sent"`
	Acknowledged bool   `json:"acknowledged"`
	Message      string `json:"message"`
}

func (c *Controller) SetMode(ctx context.Context, mode int) (ModeResult, error) {
	report, err := ModeReport(mode)
	if err != nil {
		return ModeResult{}, err
	}
	return c.setFixedSpeed(ctx, report, mode)
}

func (c *Controller) SetFanRPM(ctx context.Context, rpm int) (ModeResult, error) {
	report, err := FixedSpeedReport(rpm)
	if err != nil {
		return ModeResult{}, err
	}
	return c.setFixedSpeed(ctx, report, 0)
}

func (c *Controller) setFixedSpeed(ctx context.Context, report []byte, preset int) (ModeResult, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return ModeResult{}, err
	}
	defer c.commandMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ModeResult{}, err
	}
	expected := report[4:9]
	rpm := int(expected[3]) | int(expected[4])<<8
	c.mu.Lock()
	if c.demo {
		c.status.RequestedMode = preset
		c.status.AcknowledgedMode = preset
		c.status.TargetRPM = rpm
		c.status.ConfirmedRPM = rpm
		c.mu.Unlock()
		return ModeResult{true, true, "演示：模拟切换成功，未向硬件发送任何数据"}, nil
	}
	dev, lost := c.dev, c.lost
	if dev == nil {
		c.mu.Unlock()
		return ModeResult{}, fmt.Errorf("设备未连接")
	}
	reply := make(chan Frame, 8)
	c.reply, c.replyCommand = reply, 0x25
	n, err := c.writeReportLocked(report)
	if err == nil && n != len(report) {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.reply = nil
		c.mu.Unlock()
		return ModeResult{}, fmt.Errorf("写入固定转速失败: %w", err)
	}
	c.status.RequestedMode = preset
	c.status.AcknowledgedMode = 0
	c.status.TargetRPM = rpm
	c.status.ConfirmedRPM = 0
	query, _ := QueryReport("cooling")
	n, err = c.writeReportLocked(query)
	if err == nil && n != len(query) {
		err = io.ErrShortWrite
	}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.reply = nil; c.mu.Unlock() }()
	if err != nil {
		return ModeResult{true, false, "转速配置已发送，但查询配置失败"}, err
	}
	for {
		select {
		case frame := <-reply:
			if !bytes.Equal(frame.Payload, expected) {
				continue
			}
			c.mu.Lock()
			if c.lost != lost || c.dev == nil || c.linkError != nil {
				c.mu.Unlock()
				return ModeResult{true, false, "设备随后断开"}, nil
			}
			c.status.AcknowledgedMode = preset
			c.status.ConfirmedRPM = rpm
			c.mu.Unlock()
			return ModeResult{true, true, fmt.Sprintf("已读回固定转速配置 %d RPM；实际转速会逐步变化", rpm)}, nil
		case <-lost:
			return ModeResult{true, false, "配置已发送，等待读回时设备断开"}, nil
		case <-ctx.Done():
			return ModeResult{true, false, "配置已发送，但未读回匹配的固定转速配置"}, nil
		}
	}
}

func (c *Controller) runDemo(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		c.mu.Lock()
		c.status.Connected = true
		c.status.Device = "演示设备（无硬件写入）"
		c.status.Transport = "demo"
		c.status.RPM = 2912
		if c.status.ConfirmedRPM != 0 {
			c.status.RPM = uint16(c.status.ConfirmedRPM)
		}
		c.status.LastUpdate = time.Now().UnixMilli()
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.status.Connected = false
			c.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

// Query returns the response bytes unchanged; payload semantics vary by firmware.
func (c *Controller) Query(ctx context.Context, kind string) (Frame, error) {
	report, err := QueryReport(kind)
	if err != nil {
		return Frame{}, err
	}
	return c.exchangeRead(ctx, report)
}

// SetSwitch waits for the device response and does not infer an observed state.
func (c *Controller) SetSwitch(ctx context.Context, kind string, on bool) (Frame, error) {
	report, err := SwitchReport(kind, on)
	if err != nil {
		return Frame{}, err
	}
	frame, err := c.exchange(ctx, report)
	if err != nil {
		return frame, err
	}
	if frame.Long || len(frame.Payload) != 1 || frame.Payload[0] != 0 {
		return frame, fmt.Errorf("设备拒绝 %s 开关设置（响应 %x）", kind, frame.Payload)
	}
	return frame, nil
}

func (c *Controller) exchange(ctx context.Context, report []byte) (Frame, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return Frame{}, err
	}
	defer c.commandMu.Unlock()
	return c.exchangeLocked(ctx, report)
}

func (c *Controller) exchangeLocked(ctx context.Context, report []byte) (Frame, error) {
	return c.exchangeCommandLocked(ctx, report, report[3])
}

// Retry a lost read response once. Mutating commands must never use this.
func (c *Controller) exchangeRead(ctx context.Context, report []byte) (Frame, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return Frame{}, err
	}
	defer c.commandMu.Unlock()
	return c.exchangeReadLocked(ctx, report)
}

func (c *Controller) exchangeReadLocked(ctx context.Context, report []byte) (Frame, error) {
	c.mu.Lock()
	generation := c.lost
	c.mu.Unlock()
	var frame Frame
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		frame, err = c.exchangeCommandOnLinkLocked(readCtx, report, report[3], generation)
		cancel()
		if err == nil || ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
			return frame, err
		}
	}
	return frame, err
}

// Long A4 frames carry the command at a different offset from A5 reports.
func (c *Controller) exchangeCommandLocked(ctx context.Context, report []byte, command byte) (Frame, error) {
	return c.exchangeCommandOnLinkLocked(ctx, report, command, nil)
}

func (c *Controller) exchangeCommandOnLinkLocked(ctx context.Context, report []byte, command byte, generation chan struct{}) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	c.mu.Lock()
	if generation != nil && c.lost != generation {
		c.mu.Unlock()
		return Frame{}, fmt.Errorf("查询期间设备连接已更换")
	}
	if c.demo {
		c.mu.Unlock()
		return Frame{}, fmt.Errorf("演示模式不生成伪造的设备查询响应")
	}
	dev := c.dev
	lost := c.lost
	if dev == nil {
		c.mu.Unlock()
		return Frame{}, fmt.Errorf("设备未连接")
	}
	reply := make(chan Frame, 1)
	c.reply, c.replyCommand = reply, command
	n, err := c.writeReportLocked(report)
	if err == nil && n != len(report) {
		err = io.ErrShortWrite
	}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.reply = nil; c.mu.Unlock() }()
	if err != nil {
		return Frame{}, err
	}
	select {
	case f := <-reply:
		c.mu.Lock()
		same := c.lost == lost && c.dev != nil && c.linkError == nil
		c.mu.Unlock()
		if !same {
			return Frame{}, fmt.Errorf("等待响应时设备已断开")
		}
		return f, nil
	case <-lost:
		return Frame{}, fmt.Errorf("指令已发送，等待响应时设备断开")
	case <-ctx.Done():
		return Frame{}, fmt.Errorf("指令已发送，未收到设备响应: %w", ctx.Err())
	}
}

// verifyLocked serializes a write and read-back on the same connection. The
// caller owns commandMu. Replies with an ACK or mismatching payload are ignored.
func (c *Controller) verifyLocked(ctx context.Context, report, query, expected []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.demo || c.dev == nil {
		c.mu.Unlock()
		return fmt.Errorf("此操作需要已连接的真实设备")
	}
	lost := c.lost
	reply := make(chan Frame, 8)
	c.reply, c.replyCommand = reply, query[3]
	defer func() { c.mu.Lock(); c.reply = nil; c.mu.Unlock() }()
	var err error
	for _, b := range [][]byte{report, query} {
		var n int
		n, err = c.writeReportLocked(b)
		if err == nil && n != len(b) {
			err = io.ErrShortWrite
		}
		if err != nil {
			break
		}
	}
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("发送配置或读回查询失败: %w", err)
	}
	for {
		select {
		case f := <-reply:
			if !bytes.Equal(f.Payload, expected) {
				continue
			}
			c.mu.Lock()
			same := c.lost == lost && c.dev != nil && c.linkError == nil
			c.mu.Unlock()
			if !same {
				return fmt.Errorf("读回配置时连接已更换")
			}
			return nil
		case <-lost:
			return fmt.Errorf("等待读回配置时设备断开")
		case <-ctx.Done():
			return fmt.Errorf("配置已发送，但未读回匹配的配置: %w", ctx.Err())
		}
	}
}
