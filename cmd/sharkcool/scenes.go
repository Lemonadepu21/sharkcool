//go:build linux || windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"sharkcool-linux/brb02"
	"sharkcool-linux/internal/desktop"
	"sharkcool-linux/internal/scenes"
	"sharkcool-linux/internal/sensors"
)

func readSceneConfig(path string) (scenes.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return scenes.Config{}, err
	}
	defer file.Close()
	return scenes.Load(file)
}
func sceneName(config scenes.Config, index int) string {
	if index < 0 {
		return "默认"
	}
	return config.Profiles[index].Name
}

func runSceneMonitor(ctx context.Context, out io.Writer, config scenes.Config, poll, duration time.Duration) error {
	if duration > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, duration)
		defer stop()
	}
	encoder := json.NewEncoder(out)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	previous := -2
	observations, transitions := 0, 0
	for {
		programs, err := scenes.ReadProcesses(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return err
		}
		observations++
		index := config.Match(programs)
		if index != previous {
			if err = encoder.Encode(map[string]any{"readOnly": true, "profile": sceneName(config, index), "matched": index >= 0, "source": scenes.ProcessSource}); err != nil {
				return err
			}
			transitions++
			previous = index
		}
		select {
		case <-ctx.Done():
			return encoder.Encode(map[string]any{"observations": observations, "transitions": transitions})
		case <-ticker.C:
		}
	}
	return encoder.Encode(map[string]any{"observations": observations, "transitions": transitions})
}

func runScenes(ctx context.Context, out io.Writer, config scenes.Config, options brb02.Options, timeout, poll, duration time.Duration) (resultErr error) {
	// Check config/process access before taking ownership or changing the device.
	if _, err := scenes.ReadProcesses(ctx); err != nil {
		return err
	}
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := brb02.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	life, stopController := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(life); close(done) }()
	defer func() { stopController(); <-done }()
	connectCtx, stopConnect := context.WithTimeout(ctx, timeout)
	defer stopConnect()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for !c.Snapshot().Connected {
		select {
		case <-connectCtx.Done():
			return fmt.Errorf("场景等待设备失败: %s: %w", c.Snapshot().Error, connectCtx.Err())
		case <-ticker.C:
		}
	}
	manager, err := scenes.NewManager(connectCtx, c, config)
	if err != nil {
		return fmt.Errorf("场景备份失败，未改变设备: %w", err)
	}
	encoder := json.NewEncoder(out)
	defer func() {
		changed := manager.Dirty()
		restoreCtx, cancelRestore := context.WithTimeout(context.Background(), max(timeout, 10*time.Second))
		defer cancelRestore()
		// Preserve the controller until cleanup, including reconnection after Ctrl+C.
		for manager.Dirty() && !c.Snapshot().Connected {
			select {
			case <-restoreCtx.Done():
				resultErr = errors.Join(resultErr, fmt.Errorf("等待设备恢复连接以还原场景: %w", restoreCtx.Err()))
				return
			case <-ticker.C:
			}
		}
		if err := manager.Restore(restoreCtx); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("场景结束恢复失败: %w", err))
			return
		}
		resultErr = errors.Join(resultErr, encoder.Encode(map[string]any{"scenes": false, "restored": true, "changesToRestore": changed, "message": "被修改的配置已读回；退出场景标志仅确认成功响应"}))
	}()
	if err = encoder.Encode(map[string]any{"scenes": true, "device": c.Snapshot().Device, "profiles": len(config.Profiles), "source": scenes.ProcessSource, "message": "按配置顺序匹配当前用户程序；退出后恢复备份并退出场景"}); err != nil {
		return err
	}
	if duration > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, duration)
		defer stop()
	}
	pollTick := time.NewTicker(poll)
	defer pollTick.Stop()
	readInfo := sensors.NewSystemInfoReader(func() map[string]int64 { return sensors.ReadTemps(ctx) })
	infoTick := time.NewTicker(5 * time.Second)
	defer infoTick.Stop()
	observations, transitions := 0, 0
	linkWasUp := true
	nextVerify := time.Now().Add(5 * time.Second)
	for {
		programs, err := scenes.ReadProcesses(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		observations++
		status := c.Snapshot()
		if status.Connected {
			if !linkWasUp {
				if err = encoder.Encode(map[string]any{"connected": true}); err != nil {
					return err
				}
				linkWasUp = true
			}
			commandCtx, cancel := context.WithTimeout(ctx, timeout)
			result, err := manager.Select(commandCtx, config.Match(programs))
			cancel()
			if err != nil {
				if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
					return nil
				}
				return fmt.Errorf("场景切换失败: %w", err)
			}
			if !result.Changed && !time.Now().Before(nextVerify) {
				checkCtx, cancelCheck := context.WithTimeout(ctx, timeout)
				err := manager.VerifyActive(checkCtx)
				cancelCheck()
				if err != nil {
					return fmt.Errorf("场景持续读回失败: %w", err)
				}
				nextVerify = time.Now().Add(5 * time.Second)
			}
			if result.Changed {
				nextVerify = time.Now().Add(5 * time.Second)
				transitions++
				if err = encoder.Encode(result); err != nil {
					return err
				}
			}
		} else if linkWasUp {
			linkWasUp = false
			if err = encoder.Encode(map[string]any{"connected": false, "message": "等待自动重连，不积压旧进程状态"}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return encoder.Encode(map[string]any{"observations": observations, "transitions": transitions})
		case <-pollTick.C:
		case <-infoTick.C:
			if c.Snapshot().Connected {
				info := readInfo(ctx)
				if len(info) > 0 {
					sendCtx, stop := context.WithTimeout(ctx, timeout)
					_, err := c.SendSystemInfo(sendCtx, info)
					stop()
					if err != nil && ctx.Err() == nil {
						if err = encoder.Encode(map[string]any{"systemInfoError": err.Error()}); err != nil {
							return err
						}
					}
				}
			}
		}
	}
}
