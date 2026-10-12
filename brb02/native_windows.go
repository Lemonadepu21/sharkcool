//go:build windows

package brb02

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const BLEServiceUUID = "0000ae30-0000-1000-8000-00805f9b34fb"
const BLENotifyUUID = "0000ae04-0000-1000-8000-00805f9b34fb"
const BLEWriteUUID = "0000ae10-0000-1000-8000-00805f9b34fb"

type Device struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Readable bool   `json:"readable"`
	Writable bool   `json:"writable"`
}
type BLEDevice struct {
	Path             string `json:"path"`
	Address          string `json:"address"`
	Name             string `json:"name"`
	Paired           bool   `json:"paired"`
	Connected        bool   `json:"connected"`
	ServicesResolved bool   `json:"servicesResolved"`
	ControlReady     bool   `json:"controlReady"`
}
type LinkEvent struct {
	Time       string         `json:"time"`
	Kind       string         `json:"kind"`
	Reason     string         `json:"reason,omitempty"`
	Message    string         `json:"message,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

func prepareBridgeCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func bridgeList(ctx context.Context, channel string, out any) error {
	path, err := bridgePath()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, "--list", channel)
	prepareBridgeCommand(cmd)
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("枚举 %s 设备: %w", channel, err)
	}
	return json.Unmarshal(b, out)
}
func Discover() ([]Device, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var devices []Device
	err := bridgeList(ctx, "usb", &devices)
	return devices, err
}
func DiscoverBLE(ctx context.Context) ([]BLEDevice, error) {
	var devices []BLEDevice
	err := bridgeList(ctx, "ble", &devices)
	return devices, err
}
func Open(path string) (Transport, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return openBridge(ctx, "usb", path)
}
func OpenBLE(ctx context.Context, address string) (Transport, string, error) {
	return openBridge(ctx, "ble", address)
}
func New(path string, demo bool) *Controller {
	return NewWithConnector(func(ctx context.Context) (Transport, string, error) { return openBridge(ctx, "usb", path) }, demo)
}
func NewWithOptions(o Options, demo bool) (*Controller, error) {
	if o.Transport == "" {
		o.Transport = "auto"
	}
	if o.Transport != "auto" && o.Transport != "usb" && o.Transport != "ble" {
		return nil, fmt.Errorf("transport 必须为 auto、usb 或 ble")
	}
	return NewWithConnector(func(ctx context.Context) (Transport, string, error) {
		if o.Transport == "usb" {
			return openBridge(ctx, "usb", o.Device)
		}
		if o.Transport == "ble" {
			return OpenBLE(ctx, o.Address)
		}
		usb, p, e := openBridge(ctx, "usb", o.Device)
		if e == nil {
			return usb, p, nil
		}
		ble, p, e2 := OpenBLE(ctx, o.Address)
		if e2 == nil {
			return ble, p, nil
		}
		return nil, "", errors.Join(fmt.Errorf("USB: %w", e), fmt.Errorf("BLE: %w", e2))
	}, demo), nil
}
func OpenWithOptions(ctx context.Context, o Options) (Transport, string, error) {
	c, err := NewWithOptions(o, false)
	if err != nil {
		return nil, "", err
	}
	return c.connector(ctx)
}

// Windows link diagnostics use discovery only, without pairing or GATT writes.
func MonitorBLE(ctx context.Context, address string, emit func(LinkEvent)) error {
	return fmt.Errorf("Windows 版暂未提供只读蓝牙链路日志；请用 doctor 检查设备，正常 BLE 控制不受影响")
}
