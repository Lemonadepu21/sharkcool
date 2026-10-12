//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"sharkcool-linux/brb02"
	"time"
)

func runAudio(context.Context, io.Writer, brb02.Options, time.Duration, time.Duration) error {
	return fmt.Errorf("Windows 版尚未接入主机音频采集；设备内置灯效仍可使用")
}
func runInputMonitor(context.Context, io.Writer, time.Duration) error {
	return fmt.Errorf("Windows 版尚未接入输入活动采集")
}
func runReactive(context.Context, io.Writer, brb02.Options, time.Duration, time.Duration) error {
	return fmt.Errorf("Windows 版尚未接入主机输入响应；设备内置灯效仍可使用")
}
