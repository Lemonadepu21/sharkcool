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

func runAudio(ctx context.Context, out io.Writer, options brb02.Options, timeout, duration time.Duration) (resultErr error) {
	encoder := json.NewEncoder(out)
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := brb02.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	// Keep the connection alive until the original RGB configuration has been
	// restored, including after Ctrl+C cancels the capture context.
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
			return fmt.Errorf("音频灯效等待设备失败: %s: %w", c.Snapshot().Error, connectCtx.Err())
		case <-tick.C:
		}
	}
	original, err := c.GetRGB(connectCtx)
	if err != nil {
		return err
	}
	if _, err := original.Report(); err != nil {
		return fmt.Errorf("无法完整备份原灯效: %w", err)
	}
	originalDevice := c.Snapshot().Device
	defer func() {
		if c.Snapshot().Device != originalDevice {
			resultErr = errors.Join(resultErr, fmt.Errorf("连接的设备已更换，未恢复原灯效"))
			return
		}
		restoreCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_, restoreErr := c.SetRGB(restoreCtx, original)
		if restoreErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("恢复原灯效失败: %w", restoreErr))
		} else {
			resultErr = errors.Join(resultErr, encoder.Encode(map[string]any{"audio": false, "restored": true, "config": original}))
		}
	}()
	audio := original
	audio.Effect = 7
	if _, err := c.SetRGB(connectCtx, audio); err != nil {
		return err
	}
	if err := encoder.Encode(map[string]any{"audio": true, "device": originalDevice, "source": "default-playback-monitor", "message": "监测播放音频，每 100 ms 推送强度；停止后恢复原灯效，RGB 开关需开启"}); err != nil {
		return err
	}
	captureCtx := ctx
	if duration > 0 {
		var stop context.CancelFunc
		captureCtx, stop = context.WithTimeout(ctx, duration)
		defer stop()
	}
	samples, sent := 0, 0
	maxLevel := byte(0)
	wasConnected := true
	err = events.CaptureAudio(captureCtx, func(level byte) error {
		samples++
		if level > maxLevel {
			maxLevel = level
		}
		status := c.Snapshot()
		if !status.Connected {
			wasConnected = false
			return nil // Drop old audio; resume fresh samples after reconnect.
		}
		if status.Device != originalDevice {
			return fmt.Errorf("音频灯效连接的设备已更换")
		}
		sendCtx, stop := context.WithTimeout(captureCtx, 3*time.Second)
		defer stop()
		if !wasConnected {
			if _, err := c.SetRGB(sendCtx, audio); err != nil {
				return nil
			}
			wasConnected = true
		}
		_, err := c.SendAudioLevel(sendCtx, level)
		if err != nil {
			wasConnected = false
			return nil
		}
		sent++
		return nil
	})
	if err := encoder.Encode(map[string]any{"samples": samples, "sent": sent, "maxLevel": maxLevel}); err != nil {
		return err
	}
	if samples > 0 && sent == 0 {
		return fmt.Errorf("已采集播放音频，但没有向设备成功发送强度")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && captureCtx.Err() != nil {
		return nil
	}
	return err
}
