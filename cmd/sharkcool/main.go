//go:build linux || windows

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	cooler "sharkcool-linux/brb02"
	"sharkcool-linux/internal/buildinfo"
	"sharkcool-linux/internal/desktop"
	"sharkcool-linux/internal/sensors"
	"sharkcool-linux/internal/webui"
)

var version = buildinfo.Version

func main() {
	cleanup := prepareApplication()
	defer cleanup()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "SharkCool:", err)
		showStartupError(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	command := "gui"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	if command == "version" {
		fmt.Fprintln(out, "SharkCool", version)
		return nil
	}
	if command == "help" || (len(args) > 0 && (args[0] == "--help" || args[0] == "-h")) {
		usage(out)
		return nil
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(out)
	demo := flags.Bool("demo", false, "演示模式，不访问设备")
	device := flags.String("device", "", "指定 /dev/hidrawN（仍校验 VID/PID）")
	transport := flags.String("transport", "auto", "连接通道 auto、usb 或 ble")
	address := flags.String("address", "", "指定已配对 BRB02 蓝牙地址")
	listen := flags.String("listen", "127.0.0.1:17865", "本机控制界面地址")
	noBrowser := flags.Bool("no-browser", false, "仅运行后台服务，不打开应用窗口")
	useBrowser := flags.Bool("browser", false, "使用网页控制（默认打开 Qt 原生应用）")
	tray := flags.Bool("tray", false, "启动后驻留托盘，不显示主窗口")
	timeout := flags.Duration("timeout", 12*time.Second, "命令行等待心跳或 ACK 的最长时间")
	effect := flags.String("effect", "", "RGB 灯效：flow、cycle、breathing、static、blinking、refresh")
	color := flags.String("color", "", "RGB 颜色，如 #00ff48")
	colorMode := flags.String("color-mode", "", "RGB 颜色模式 single（单色）或 multi（彩色）")
	brightness := flags.Int("brightness", -1, "RGB 亮度 0～100")
	lightingSpeed := flags.Int("lighting-speed", -1, "RGB 速度 0～100，越高越快")
	slot := flags.Int("slot", 0, "散热配置槽 1～4；0 查询当前配置")
	curve := flags.Bool("curve", false, "查询温控曲线槽，否则查询固定转速槽")
	coolingSource := flags.String("source", "cpu", "散热配置使用 cpu 或 gpu 温度")
	points := flags.String("points", "", "四点曲线，如 20:1200,40:1800,60:2600,80:3600")
	coolingRPM := flags.Int("rpm", 0, "保存固定转速配置槽，1200～4000 RPM")
	smart := flags.Bool("smart-start-stop", false, "设备智能启停，显式指定 true 或 false")
	powerOn := flags.Bool("power-on-start", false, "设备上电自启动，显式指定 true 或 false")
	audioDuration := flags.Duration("duration", 0, "主机灯效/场景/输入监测持续时间；0 等待 Ctrl+C")
	palette := flags.Int("palette", 0, "RGB 配置选择值 0～15，仅用于 rgb-preset 查询")
	sceneConfigPath := flags.String("config", "", "scenes / scene-monitor 的 JSON 配置文件")
	scenePoll := flags.Duration("poll", time.Second, "进程场景扫描周期，200ms～60s")
	outputPath := flags.String("output", "", "screen-backup 的原始图片备份路径，不能覆盖已有文件")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout 必须大于 0")
	}
	var err error
	options := cooler.Options{Transport: *transport, Device: *device, Address: *address}
	if _, err := cooler.NewWithOptions(options, *demo); err != nil {
		return err
	}
	switch command {
	case "scenes", "scene-monitor":
		if flags.NArg() != 0 || *sceneConfigPath == "" || *audioDuration < 0 || *scenePoll < 200*time.Millisecond || *scenePoll > 60*time.Second {
			return fmt.Errorf("用法: %s --config scenes.json [--poll 1s] [--duration 30s]；poll 范围 200ms～60s", command)
		}
		if *demo {
			return fmt.Errorf("场景入口需要真实进程，不生成演示匹配或设备响应")
		}
		config, err := readSceneConfig(*sceneConfigPath)
		if err != nil {
			return err
		}
		if command == "scene-monitor" {
			return runSceneMonitor(ctx, out, config, *scenePoll, *audioDuration)
		}
		return runScenes(ctx, out, config, options, *timeout, *scenePoll, *audioDuration)
	case "reactive", "input-monitor":
		if flags.NArg() != 0 || *audioDuration < 0 {
			return fmt.Errorf("用法: %s [--duration 30s]；持续时间不能为负", command)
		}
		if *demo {
			return fmt.Errorf("输入活动需要真实桌面，演示模式不监测输入")
		}
		if command == "input-monitor" {
			return runInputMonitor(ctx, out, *audioDuration)
		}
		return runReactive(ctx, out, options, *timeout, *audioDuration)
	case "screen-info":
		if flags.NArg() != 0 {
			return fmt.Errorf("screen-info 不需要位置参数")
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			return c.GetScreenImageInfo(commandCtx)
		})
	case "screen-backup":
		if flags.NArg() != 0 || *outputPath == "" {
			return fmt.Errorf("用法: screen-backup --output image.bin [--timeout 120s]")
		}
		if _, err := os.Lstat(*outputPath); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("备份路径必须是不存在的新文件")
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			image, err := c.GetScreenImage(commandCtx)
			if err != nil {
				return nil, err
			}
			file, err := os.OpenFile(*outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return nil, err
			}
			n, writeErr := file.Write(image.Data)
			if writeErr == nil && n != len(image.Data) {
				writeErr = io.ErrShortWrite
			}
			err = errors.Join(writeErr, file.Close())
			if err != nil {
				_ = os.Remove(*outputPath)
				return nil, err
			}
			return map[string]any{"verified": true, "output": *outputPath, "info": image.Info}, nil
		})
	case "audio":
		if flags.NArg() != 0 || *audioDuration < 0 {
			return fmt.Errorf("用法: sharkcool audio [--transport ble] [--duration 30s]；持续时间不能为负")
		}
		if *demo {
			return fmt.Errorf("音频灯效需要真实设备，演示模式不采集音频")
		}
		return runAudio(ctx, out, options, *timeout, *audioDuration)
	case "rgb-preset":
		effectIDs := map[string]byte{"flow": 1, "cycle": 2, "breathing": 3, "static": 4, "blinking": 5, "reactive": 6, "audio": 7, "refresh": 8}
		id, ok := effectIDs[*effect]
		if !ok || flags.NArg() != 0 || *palette < 0 || *palette > 15 {
			return fmt.Errorf("rgb-preset 需要 --effect 指定灯效，--palette 为 0～15")
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			return c.GetRGBPreset(commandCtx, id, byte(*palette))
		})
	case "device-options":
		if flags.NArg() != 0 {
			return fmt.Errorf("device-options 使用 --smart-start-stop=true|false、--power-on-start=true|false；无选项时只读取")
		}
		patch := cooler.DeviceOptionsPatch{}
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "smart-start-stop" {
				patch.SmartStartStop = smart
			}
			if f.Name == "power-on-start" {
				patch.PowerOnStart = powerOn
			}
		})
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			if patch.SmartStartStop != nil || patch.PowerOnStart != nil {
				return c.UpdateDeviceOptions(commandCtx, patch)
			}
			return c.GetDeviceOptions(commandCtx)
		})
	case "source":
		if flags.NArg() > 1 {
			return fmt.Errorf("用法: sharkcool source [选项] [cpu|gpu]")
		}
		var source cooler.CoolingSource
		if flags.NArg() == 1 {
			source, err = cooler.ParseCoolingSource(flags.Arg(0))
			if err != nil {
				return err
			}
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			if flags.NArg() == 1 {
				if err := c.SetCoolingSource(commandCtx, source); err != nil {
					return nil, err
				}
			}
			actual, err := c.GetCoolingSource(commandCtx)
			return map[string]any{"source": actual, "confirmed": err == nil}, err
		})
	case "cooling":
		if flags.NArg() != 0 || *slot < 0 || *slot > 4 {
			return fmt.Errorf("cooling 通过 --slot 1～4、--curve、--points 或 --rpm 设置，不接受位置参数")
		}
		config := cooler.CoolingConfig{Slot: byte(*slot)}
		writing := *points != "" || *coolingRPM != 0
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "points" || f.Name == "rpm" {
				writing = true
			}
		})
		if writing {
			config.Source, err = cooler.ParseCoolingSource(*coolingSource)
			if err != nil {
				return err
			}
			if *coolingRPM < 0 || *coolingRPM > 4000 {
				return fmt.Errorf("rpm 必须为 1200～4000")
			}
			config.RPM = uint16(*coolingRPM)
			if *points != "" {
				config.Points, err = parseCurvePoints(*points)
				if err != nil {
					return err
				}
			}
			if _, err := config.Report(); err != nil {
				return err
			}
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			if writing {
				return c.SetCooling(commandCtx, config)
			}
			if *slot == 0 {
				return c.GetCooling(commandCtx)
			}
			frame, err := c.GetCoolingPreset(commandCtx, *curve, byte(*slot))
			if err != nil {
				return nil, err
			}
			return frame.CoolingConfig()
		})
	case "lcd":
		if flags.NArg() != 3 {
			return fmt.Errorf("用法: sharkcool lcd [选项] cpu-temperature fan-rpm time；需要三个不同指标")
		}
		layout := cooler.LCDLayout{Rows: []cooler.Metric{cooler.Metric(flags.Arg(0)), cooler.Metric(flags.Arg(1)), cooler.Metric(flags.Arg(2))}}
		if _, err := layout.Report(); err != nil {
			return err
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			return c.SetLCDLayout(commandCtx, layout)
		})
	case "sysinfo":
		if flags.NArg() != 0 {
			return fmt.Errorf("sysinfo 不接受位置参数；采集一次并推送可用系统指标")
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			temps := sensors.ReadTemps(commandCtx)
			read := sensors.NewSystemInfoReader(func() map[string]int64 { return temps })
			read(commandCtx)
			select {
			case <-commandCtx.Done():
				return nil, commandCtx.Err()
			case <-time.After(time.Second):
			}
			info := read(commandCtx)
			result, err := c.SendSystemInfo(commandCtx, info)
			return struct {
				cooler.Delivery
				Values cooler.SystemInfo `json:"values"`
			}{result, info}, err
		})
	case "rgb":
		if flags.NArg() != 0 {
			return fmt.Errorf("rgb 通过 --effect、--color-mode、--color、--brightness、--lighting-speed 设置，不接受位置参数")
		}
		patch := cooler.RGBPatch{Effect: *effect, ColorMode: *colorMode, Color: *color}
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "brightness" {
				patch.Brightness = brightness
			}
			if f.Name == "lighting-speed" {
				patch.Speed = lightingSpeed
			}
		})
		if err := patch.Validate(); err != nil {
			return err
		}
		return runDeviceCommand(ctx, out, options, *demo, *timeout, func(commandCtx context.Context, c *cooler.Controller) (any, error) {
			return c.UpdateRGB(commandCtx, patch)
		})
	case "trace":
		if flags.NArg() != 1 {
			return fmt.Errorf("用法: sharkcool trace --transport ble firmware|device|cooling|rgb|rgb-switch|lcd-switch|source")
		}
		if *demo {
			return fmt.Errorf("trace 需要真实设备")
		}
		return trace(ctx, out, options, *timeout, flags.Arg(0))
	case "linklog":
		if flags.NArg() != 0 {
			return fmt.Errorf("linklog 不接受位置参数")
		}
		deadline, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		encoder := json.NewEncoder(out)
		var outputErr error
		err := cooler.MonitorBLE(deadline, *address, func(event cooler.LinkEvent) {
			if outputErr == nil {
				outputErr = encoder.Encode(event)
			}
		})
		return errors.Join(err, outputErr)
	case "doctor":
		if flags.NArg() != 0 {
			return fmt.Errorf("doctor 不接受位置参数")
		}
		return doctor(ctx, out)
	case "gui":
		if flags.NArg() != 0 {
			return fmt.Errorf("gui 不接受位置参数")
		}
		if *tray && (*noBrowser || *useBrowser) {
			return fmt.Errorf("--tray 不能与 --no-browser 或 --browser 同时使用")
		}
		return gui(ctx, out, options, *demo, *listen, *noBrowser, *useBrowser, *tray)
	case "speed":
		if flags.NArg() != 1 {
			return fmt.Errorf("用法: sharkcool speed [--transport ble] 1200～3600")
		}
		rpm, err := strconv.Atoi(flags.Arg(0))
		if err != nil {
			return err
		}
		if _, err = cooler.FixedSpeedReport(rpm); err != nil {
			return err
		}
		return cli(ctx, out, "speed", options, *demo, *timeout, rpm)
	case "query", "switch":
		var report []byte
		if command == "query" && flags.NArg() == 1 {
			report, err = cooler.QueryReport(flags.Arg(0))
		} else if command == "switch" && flags.NArg() == 2 {
			if flags.Arg(1) != "on" && flags.Arg(1) != "off" {
				return fmt.Errorf("开关取值为 on 或 off")
			}
			report, err = cooler.SwitchReport(flags.Arg(0), flags.Arg(1) == "on")
		} else {
			return fmt.Errorf("用法: query firmware|device|cooling|rgb|rgb-switch|fan-switch|lcd-switch|source|screen-info；switch fan|rgb|lcd on|off")
		}
		if err != nil {
			return err
		}
		return request(ctx, out, options, *demo, *timeout, report)
	case "status", "watch", "mode":
		mode := 0
		if command == "mode" {
			if flags.NArg() != 1 {
				return fmt.Errorf("用法: sharkcool mode [--timeout 5s] 2 或 3")
			}
			var err error
			mode, err = strconv.Atoi(flags.Arg(0))
			if err != nil {
				return err
			}
			if _, err = cooler.ModeReport(mode); err != nil {
				return err
			}
		} else if flags.NArg() != 0 {
			return fmt.Errorf("%s 不接受位置参数", command)
		}
		return cli(ctx, out, command, options, *demo, *timeout, mode)
	default:
		return fmt.Errorf("未知命令 %q；运行 sharkcool help 查看帮助", command)
	}
}

func parseCurvePoints(value string) ([]cooler.CurvePoint, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("需要四个温度:转速点")
	}
	points := make([]cooler.CurvePoint, 0, 4)
	for _, part := range parts {
		pair := strings.Split(part, ":")
		if len(pair) != 2 {
			return nil, fmt.Errorf("曲线格式需要 温度:转速")
		}
		temp, e1 := strconv.Atoi(pair[0])
		rpm, e2 := strconv.Atoi(pair[1])
		if e1 != nil || e2 != nil || temp < 0 || temp > 100 || rpm < 1200 || rpm > 4000 {
			return nil, fmt.Errorf("温度需要 0～100，转速需要 1200～4000")
		}
		points = append(points, cooler.CurvePoint{Temperature: byte(temp), RPM: uint16(rpm)})
	}
	return points, nil
}

func usage(out io.Writer) {
	fmt.Fprintln(out, `SharkCool Linux — 黑鲨风神Pro BRB02 控制器

sharkcool                          打开本机中文控制界面
sharkcool gui --no-browser         后台运行，不打开应用窗口
sharkcool gui --tray               后台驻留托盘
sharkcool gui --demo               无硬件演示（不访问设备）
sharkcool doctor                   检查设备及读写权限
sharkcool status                   等待一个心跳并输出 JSON
sharkcool watch                    每秒输出 JSON，Ctrl+C 退出
sharkcool linklog --timeout 20s    只读记录蓝牙状态和断连原因
sharkcool speed --transport ble 1800 设置固定转速，读回验证
sharkcool mode 2                   设置固定转速 2912，读回验证
sharkcool mode 3                   设置固定转速 3534，读回验证
sharkcool cooling                         查询当前散热配置
sharkcool cooling --slot 1 --curve         查询温控曲线槽
sharkcool cooling --slot 1 --source cpu --points 20:1200,40:1800,60:2600,80:3600  保存曲线槽
sharkcool source gpu                      切换温度来源并读回
sharkcool device-options                  读取设备智能启停和上电自启动
sharkcool device-options --smart-start-stop=true  只修改智能启停并读回
sharkcool lcd --transport ble cpu-temperature fan-rpm time  设置屏幕三行内容
sharkcool sysinfo --transport ble          采集并推送一次系统信息
sharkcool query --transport ble firmware  查询固件（返回原始响应）
sharkcool switch --transport ble rgb off  关闭灯光（等待设备响应）
sharkcool rgb --effect static --color '#00ff48' --brightness 80  设置单色灯效并读回
sharkcool rgb --color-mode multi             切换彩色，保留其他灯效参数
sharkcool rgb-preset --effect breathing --palette 0  查询指定灯效配置
sharkcool audio --transport ble --duration 30s  播放音频灯效，结束恢复原灯效
sharkcool reactive --transport ble --duration 30s  输入响应灯效，结束恢复原灯效
sharkcool scenes --config scenes.json --transport ble  程序场景，结束恢复原配置
sharkcool scene-monitor --config scenes.json --duration 10s  只检查程序匹配
sharkcool input-monitor --duration 5s      只检查 Wayland 输入活动来源
sharkcool screen-info                     查询屏保图片信息
sharkcool screen-backup --output image.bin --timeout 120s  读取并校验原始屏保备份
sharkcool version                  显示版本

连接选项：--transport auto|usb|ble、--address 蓝牙MAC、--device /dev/hidrawN
选项放在位置参数前：--timeout 12s、--demo
界面选项：--listen 127.0.0.1:17865、--no-browser、--browser
需要权限时，请按 README 安装 udev 规则，不必以 root 启动应用。`)
}

func start(ctx context.Context, c *cooler.Controller, monitors ...*sensors.Monitor) func() {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); c.Run(ctx) }()
	for _, monitor := range monitors {
		wg.Add(1)
		go func(m *sensors.Monitor) { defer wg.Done(); m.Run(ctx) }(monitor)
	}
	return func() { cancel(); wg.Wait() }
}

func cli(ctx context.Context, out io.Writer, command string, options cooler.Options, demo bool, timeout time.Duration, mode int) error {
	if !demo {
		unlock, err := desktop.LockDevice()
		if err != nil {
			return err
		}
		defer unlock()
	}
	c, err := cooler.NewWithOptions(options, demo)
	if err != nil {
		return err
	}
	temps := sensors.NewMonitor(sensors.ReadTemps)
	stop := start(ctx, c, temps)
	defer stop()
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		s := c.Snapshot()
		if s.Connected && (command == "mode" || command == "speed" || !s.Stale) {
			break
		}
		select {
		case <-deadline.Done():
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("等待设备超时: %s", s.Error)
		case <-tick.C:
		}
	}
	if command == "mode" || command == "speed" {
		// Give command ACK its own deadline after connection establishment.
		ackCtx, ackCancel := context.WithTimeout(ctx, timeout)
		defer ackCancel()
		var res cooler.ModeResult
		var err error
		if command == "speed" {
			res, err = c.SetFanRPM(ackCtx, mode)
		} else {
			res, err = c.SetMode(ackCtx, mode)
		}
		if err != nil {
			return err
		}
		if err = json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
		if !res.Acknowledged {
			return fmt.Errorf("未收到设备确认")
		}
		return nil
	}
	if command == "status" {
		return json.NewEncoder(out).Encode(applicationStatus(c, temps))
	}
	watch := time.NewTicker(time.Second)
	defer watch.Stop()
	for {
		if err := json.NewEncoder(out).Encode(applicationStatus(c, temps)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-watch.C:
		}
	}
}

func gui(ctx context.Context, out io.Writer, options cooler.Options, demo bool, address string, noBrowser, useBrowser, tray bool) error {
	ctx, cancelApp := context.WithCancel(ctx)
	defer cancelApp()
	openUI := func(url string) error {
		if tray {
			return desktop.OpenWindow(url, "--tray")
		}
		return desktop.OpenWindow(url)
	}
	if useBrowser {
		openUI = desktop.OpenBrowser
	}
	if !demo && desktop.AutostartEnabled() {
		if err := desktop.SetAutostart(true); err != nil {
			fmt.Fprintln(out, "更新托盘自启动失败：", err)
		}
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("控制界面只允许监听本机回环 IP，例如 127.0.0.1:17865")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		client := http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
		res, getErr := client.Get("http://" + address + "/api/status")
		if getErr == nil {
			defer res.Body.Close()
			var s struct {
				cooler.Status
				Version string `json:"version"`
			}
			if res.StatusCode == 200 && res.Header.Get("X-SharkCool-App") == "sharkcool-linux" && json.NewDecoder(res.Body).Decode(&s) == nil && s.Demo == demo && options.Device == "" && options.Address == "" && options.Transport == "auto" {
				if s.Version != buildinfo.Version {
					return fmt.Errorf("后台版本 %q 与当前版本 %q 不一致；请先完全退出旧版 SharkCool（Windows 可运行旧版目录的 Stop-SharkCool.cmd），再启动当前版本", s.Version, buildinfo.Version)
				}
				fmt.Fprintln(out, "SharkCool 已运行：http://"+address)
				if !noBrowser {
					return openUI("http://" + address)
				}
				return nil
			}
		}
		return fmt.Errorf("无法监听 %s（如已有演示服务，请先退出）：%w", address, err)
	}
	defer listener.Close()
	if !demo {
		unlock, err := desktop.LockDevice()
		if err != nil {
			return err
		}
		defer unlock()
	}
	c, err := cooler.NewWithOptions(options, demo)
	if err != nil {
		return err
	}
	temps := sensors.NewMonitor(sensors.ReadTemps)
	stop := start(ctx, c, temps)
	defer stop()
	publisher := sensors.NewPublisher(c, sensors.NewSystemInfoReader(temps.Snapshot))
	pushCtx, stopPush := context.WithCancel(ctx)
	pushDone := make(chan struct{})
	go func() { defer close(pushDone); publisher.Run(pushCtx) }()
	defer func() { stopPush(); <-pushDone }()
	handler, err := webui.NewHandlerWithSystemInfo(c, listener.Addr().String(), publisher, temps.Snapshot, cancelApp)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	url := "http://" + listener.Addr().String()
	fmt.Fprintln(out, "SharkCool 控制界面：", url)
	if demo {
		fmt.Fprintln(out, "演示模式：转速和控制结果为模拟数据")
	}
	if !noBrowser {
		if err := openUI(url); err != nil {
			fmt.Fprintln(out, "无法打开应用窗口；可检查安装或显式使用 gui --browser：", err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func doctor(ctx context.Context, out io.Writer) error {
	fmt.Fprintf(out, "SharkCool %s · USB + BLE\n", version)
	devices, usbErr := cooler.Discover()
	if usbErr != nil {
		fmt.Fprintln(out, "USB:", usbErr)
	}
	if len(devices) == 0 {
		fmt.Fprintln(out, "USB: 未枚举 E2B7:7001；电源接口可能仅供电")
	}
	ready := false
	for _, d := range devices {
		fmt.Fprintf(out, "USB %s · %s · 可读=%t 可写=%t\n", d.Path, d.Name, d.Readable, d.Writable)
		ready = ready || (d.Readable && d.Writable)
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ble, bleErr := cooler.DiscoverBLE(deadline)
	if bleErr != nil {
		fmt.Fprintln(out, "BLE:", bleErr)
	}
	if bleErr == nil && len(ble) == 0 {
		fmt.Fprintln(out, "BLE: 未发现已配对的 BS BRB02 Cooler Pro")
	}
	for _, d := range ble {
		fmt.Fprintf(out, "BLE %s · %s · 配对=%t 连接=%t 服务解析=%t 控制接口=%t\n", d.Address, d.Name, d.Paired, d.Connected, d.ServicesResolved, d.ControlReady)
		ready = ready || (d.Paired && d.Connected && d.ControlReady)
	}
	fmt.Fprintln(out, "BLE 控制服务 AE30 / 通知 AE04 / 写入 AE10；配对成功不等于控制接口就绪。")
	if !ready {
		return fmt.Errorf("当前没有就绪的控制通道；蓝牙反复断连时需要检查连接日志")
	}
	return nil
}

func request(ctx context.Context, out io.Writer, options cooler.Options, demo bool, timeout time.Duration, report []byte) error {
	if demo {
		return fmt.Errorf("查询/开关操作需要实际设备；请勿使用 --demo")
	}
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := cooler.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	stop := start(ctx, c)
	defer stop()
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !c.Snapshot().Connected {
		select {
		case <-deadline.Done():
			return fmt.Errorf("等待连接超时: %s", c.Snapshot().Error)
		case <-tick.C:
		}
	}
	commandCtx, commandCancel := context.WithTimeout(ctx, timeout)
	defer commandCancel()
	// The public API only exposes recovered query/switch builders.
	var f cooler.Frame
	switch report[3] {
	case 0x20:
		f, err = c.SetSwitch(commandCtx, "fan", report[4] == 1)
	case 0x10:
		f, err = c.SetSwitch(commandCtx, "rgb", report[4] == 1)
	case 0xc0:
		f, err = c.SetSwitch(commandCtx, "lcd", report[4] == 1)
	default:
		names := map[byte]string{1: "firmware", 3: "device", 0x25: "cooling", 0x13: "rgb", 0x11: "rgb-switch", 0x21: "fan-switch", 0xc1: "lcd-switch", 0x23: "source", 0xc5: "screen-info"}
		f, err = c.Query(commandCtx, names[report[3]])
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"transport": c.Snapshot().Transport, "command": f.Command, "payloadHex": hex.EncodeToString(f.Payload), "checksumValid": f.ChecksumValid, "received": true})
}

func trace(ctx context.Context, out io.Writer, options cooler.Options, timeout time.Duration, query string) error {
	report, err := cooler.QueryReport(query)
	if query == "mode2" {
		report, err = cooler.ModeReport(2)
	}
	if query == "mode3" {
		report, err = cooler.ModeReport(3)
	}
	if err != nil {
		return err
	}
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dev, path, err := cooler.OpenWithOptions(deadline, options)
	if err != nil {
		return err
	}
	defer dev.Close()
	encoder := json.NewEncoder(out)
	if err = encoder.Encode(map[string]any{"device": path, "txHex": hex.EncodeToString(report[1 : 1+int(report[2])])}); err != nil {
		return err
	}
	if _, err = dev.Write(report); err != nil {
		return err
	}
	buffer := make([]byte, 512)
	for {
		n, err := dev.Read(deadline, buffer)
		if err != nil {
			if deadline.Err() != nil {
				return nil
			}
			return err
		}
		if err = encoder.Encode(map[string]any{"time": time.Now().Format(time.RFC3339Nano), "rxHex": hex.EncodeToString(buffer[:n])}); err != nil {
			return err
		}
	}
}

func applicationStatus(c *cooler.Controller, temps *sensors.Monitor) any {
	return struct {
		cooler.Status
		Temps map[string]int64 `json:"temps"`
	}{c.Snapshot(), temps.Snapshot()}
}

func runDeviceCommand(ctx context.Context, out io.Writer, options cooler.Options, demo bool, timeout time.Duration, command func(context.Context, *cooler.Controller) (any, error)) error {
	if demo {
		return fmt.Errorf("此操作需要真实设备")
	}
	unlock, err := desktop.LockDevice()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := cooler.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	stop := start(ctx, c)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !c.Snapshot().Connected {
		select {
		case <-connectCtx.Done():
			return fmt.Errorf("等待连接超时: %s", c.Snapshot().Error)
		case <-tick.C:
		}
	}
	commandCtx, commandCancel := context.WithTimeout(ctx, timeout)
	defer commandCancel()
	result, err := command(commandCtx, c)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
