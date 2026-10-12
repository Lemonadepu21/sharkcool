//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"sharkcool-linux/brb02"
	"sharkcool-linux/internal/desktop"
	"sharkcool-linux/internal/events"
)

func runInputMonitor(ctx context.Context, out io.Writer, duration time.Duration) error {
	source, err := events.OpenActivity(ctx)
	if err != nil {
		return err
	}
	defer source.Close()
	if duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}
	encoder := json.NewEncoder(out)
	if err = encoder.Encode(map[string]any{"source": source.Name(), "monitoring": true, "message": "只观察输入空闲/活动状态，不读取键值；不连接散热器"}); err != nil {
		return err
	}
	count := 0
	err = source.Capture(ctx, func() error { count++; return nil })
	if outputErr := encoder.Encode(map[string]any{"activityReports": count}); outputErr != nil {
		return outputErr
	}
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return nil
	}
	return err
}

func runReactive(ctx context.Context, out io.Writer, options brb02.Options, timeout, duration time.Duration) (resultErr error) {
	// Validate the desktop source before touching device state. No root or raw
	// key device permissions are acquired when the compositor lacks support.
	source, err := events.OpenActivity(ctx)
	if err != nil {
		return err
	}
	defer source.Close()
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := brb02.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	controllerCtx, stopController := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(controllerCtx); close(done) }()
	defer func() { stopController(); <-done }()
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !c.Snapshot().Connected {
		select {
		case <-connectCtx.Done():
			return fmt.Errorf("响应灯效等待设备失败: %s: %w", c.Snapshot().Error, connectCtx.Err())
		case <-tick.C:
		}
	}
	original, err := c.GetRGB(connectCtx)
	if err != nil {
		return err
	}
	if _, err = original.Report(); err != nil {
		return fmt.Errorf("无法完整备份原灯效: %w", err)
	}
	device := c.Snapshot().Device
	encoder := json.NewEncoder(out)
	defer func() {
		if c.Snapshot().Device != device {
			resultErr = errors.Join(resultErr, fmt.Errorf("连接的设备已更换，未恢复原灯效"))
			return
		}
		restoreCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_, err := c.SetRGB(restoreCtx, original)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("恢复原灯效失败: %w", err))
		} else {
			resultErr = errors.Join(resultErr, encoder.Encode(map[string]any{"reactive": false, "restored": true, "config": original}))
		}
	}()
	reactive := original
	reactive.Effect = 6
	// Interval has meaning in this effect; preserve a valid previous interval,
	// otherwise choose the middle of the official 300..1000 range.
	if reactive.Interval < 300 || reactive.Interval > 1000 {
		reactive.Interval = 650
	}
	if _, err = c.SetRGB(connectCtx, reactive); err != nil {
		return err
	}
	if err = encoder.Encode(map[string]any{"reactive": true, "device": device, "source": source.Name(), "message": "监测输入活动，最多每 100 ms 通知；停止后恢复原灯效，RGB 开关需开启"}); err != nil {
		return err
	}
	captureCtx := ctx
	if duration > 0 {
		var stop context.CancelFunc
		captureCtx, stop = context.WithTimeout(ctx, duration)
		defer stop()
	}
	reports, sent := 0, 0
	wasConnected := true
	err = source.Capture(captureCtx, func() error {
		reports++
		status := c.Snapshot()
		if !status.Connected {
			wasConnected = false
			return nil
		}
		if status.Device != device {
			return fmt.Errorf("响应灯效连接的设备已更换")
		}
		sendCtx, stop := context.WithTimeout(captureCtx, 3*time.Second)
		defer stop()
		if !wasConnected {
			if _, err := c.SetRGB(sendCtx, reactive); err != nil {
				return nil
			}
			wasConnected = true
		}
		if _, err := c.SendInputActivity(sendCtx); err != nil {
			wasConnected = false
			return nil
		}
		sent++
		return nil
	})
	if outputErr := encoder.Encode(map[string]any{"activityReports": reports, "sent": sent}); outputErr != nil {
		return outputErr
	}
	if reports > 0 && sent == 0 {
		return fmt.Errorf("已检测到输入活动，但没有向设备成功发送通知")
	}
	if captureCtx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return nil
	}
	return err
}
