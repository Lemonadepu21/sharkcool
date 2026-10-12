//go:build linux

package brb02

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// LinkEvent describes BlueZ link state without subscribing to device data.
type LinkEvent struct {
	Time       string         `json:"time"`
	Kind       string         `json:"kind"`
	Reason     string         `json:"reason,omitempty"`
	Message    string         `json:"message,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// MonitorBLE is read-only: no connect, pair, profile, or characteristic writes.
func MonitorBLE(ctx context.Context, address string, emit func(LinkEvent)) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	objects, err := managedObjects(ctx, conn)
	if err != nil {
		return err
	}
	var device BLEDevice
	for _, d := range bleDevices(objects) {
		if address == "" || strings.EqualFold(d.Address, address) {
			device = d
			break
		}
	}
	if device.Path == "" {
		return fmt.Errorf("未发现 BRB02 蓝牙设备")
	}
	signals := make(chan *dbus.Signal, 256)
	conn.Signal(signals)
	if err = conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.bluez"), dbus.WithMatchObjectPath(device.Path)); err != nil {
		return err
	}
	emit(LinkEvent{Time: time.Now().Format(time.RFC3339Nano), Kind: "snapshot", Properties: map[string]any{"address": device.Address, "paired": device.Paired, "connected": device.Connected, "servicesResolved": device.ServicesResolved, "controlReady": device.ControlReady}})
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-conn.Context().Done():
			return fmt.Errorf("系统 D-Bus 已断开")
		case signal, ok := <-signals:
			if !ok {
				return fmt.Errorf("D-Bus 信号流已关闭")
			}
			if signal == nil {
				continue
			}
			event := LinkEvent{Time: time.Now().Format(time.RFC3339Nano)}
			if signal.Name == "org.bluez.Device1.Disconnected" && len(signal.Body) == 2 {
				event.Kind = "disconnected"
				event.Reason, _ = signal.Body[0].(string)
				event.Message, _ = signal.Body[1].(string)
			} else if signal.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" && len(signal.Body) >= 2 {
				iface, _ := signal.Body[0].(string)
				if iface != "org.bluez.Device1" {
					continue
				}
				props, _ := signal.Body[1].(map[string]dbus.Variant)
				event.Kind = "properties"
				event.Properties = map[string]any{}
				for _, k := range []string{"Connected", "ServicesResolved", "Paired", "Bonded"} {
					if v, ok := props[k]; ok {
						event.Properties[k] = v.Value()
					}
				}
				if len(event.Properties) == 0 {
					continue
				}
			} else {
				continue
			}
			emit(event)
		}
	}
}
