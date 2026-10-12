//go:build linux

package brb02

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Brb02Service.exe sets these three UUIDs at VA 0x4013e6..0x4013f5.
const BLEServiceUUID = "0000ae30-0000-1000-8000-00805f9b34fb"
const BLENotifyUUID = "0000ae04-0000-1000-8000-00805f9b34fb"
const BLEWriteUUID = "0000ae10-0000-1000-8000-00805f9b34fb"

type bluezObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

type BLEDevice struct {
	Path             dbus.ObjectPath `json:"path"`
	Address          string          `json:"address"`
	Name             string          `json:"name"`
	Paired           bool            `json:"paired"`
	Connected        bool            `json:"connected"`
	ServicesResolved bool            `json:"servicesResolved"`
	ControlReady     bool            `json:"controlReady"`
}

func stringProp(p map[string]dbus.Variant, k string) string { v, _ := p[k].Value().(string); return v }
func boolProp(p map[string]dbus.Variant, k string) bool     { v, _ := p[k].Value().(bool); return v }
func flagsProp(p map[string]dbus.Variant) []string          { v, _ := p["Flags"].Value().([]string); return v }
func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

func managedObjects(ctx context.Context, conn *dbus.Conn) (bluezObjects, error) {
	var objects bluezObjects
	err := conn.Object("org.bluez", "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects)
	return objects, err
}

func bleDevices(objects bluezObjects) []BLEDevice {
	devices := []BLEDevice{}
	for path, interfaces := range objects {
		p, ok := interfaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		name := stringProp(p, "Name")
		if name != "BS BRB02 Cooler Pro" {
			continue
		}
		_, _, _, err := controlCharacteristics(objects, path)
		ready := err == nil && boolProp(p, "Paired") && boolProp(p, "Connected") && boolProp(p, "ServicesResolved")
		devices = append(devices, BLEDevice{path, stringProp(p, "Address"), name, boolProp(p, "Paired"), boolProp(p, "Connected"), boolProp(p, "ServicesResolved"), ready})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Address < devices[j].Address })
	return devices
}

func DiscoverBLE(ctx context.Context) ([]BLEDevice, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("连接系统 D-Bus: %w", err)
	}
	defer conn.Close()
	objects, err := managedObjects(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("查询 BlueZ: %w", err)
	}
	return bleDevices(objects), nil
}

func controlCharacteristics(objects bluezObjects, device dbus.ObjectPath) (notify, write dbus.ObjectPath, writeType string, err error) {
	var service dbus.ObjectPath
	for path, interfaces := range objects {
		p, ok := interfaces["org.bluez.GattService1"]
		if ok && strings.HasPrefix(string(path), string(device)+"/") && strings.ToLower(stringProp(p, "UUID")) == BLEServiceUUID {
			service = path
			break
		}
	}
	if service == "" {
		return "", "", "", fmt.Errorf("尚未发现 AE30 控制服务")
	}
	for path, interfaces := range objects {
		p, ok := interfaces["org.bluez.GattCharacteristic1"]
		if !ok {
			continue
		}
		s, _ := p["Service"].Value().(dbus.ObjectPath)
		if s != service {
			continue
		}
		flags := flagsProp(p)
		switch strings.ToLower(stringProp(p, "UUID")) {
		case BLENotifyUUID:
			if hasFlag(flags, "notify") || hasFlag(flags, "indicate") {
				notify = path
			}
		case BLEWriteUUID:
			if hasFlag(flags, "write-without-response") {
				write = path
				writeType = "command"
			} else if hasFlag(flags, "write") {
				write = path
				writeType = "request"
			}
		}
	}
	if notify == "" || write == "" {
		return "", "", "", fmt.Errorf("AE30 已发现，但 AE04 通知或 AE10 写入特征尚未就绪")
	}
	return notify, write, writeType, nil
}

type bleConn struct {
	conn                  *dbus.Conn
	device, notify, write dbus.ObjectPath
	writeType             string
	signals               chan *dbus.Signal
	mu                    sync.Mutex
	closed                bool
}

func OpenBLE(ctx context.Context, address string) (Transport, string, error) {
	return openBLE(ctx, address, dbus.ConnectSystemBus)
}

func openBLE(ctx context.Context, address string, connect func(...dbus.ConnOption) (*dbus.Conn, error)) (Transport, string, error) {
	conn, err := connect()
	if err != nil {
		return nil, "", fmt.Errorf("连接系统 D-Bus: %w", err)
	}
	owned := false
	defer func() {
		if !owned {
			_ = conn.Close()
		}
	}()
	objects, err := managedObjects(ctx, conn)
	if err != nil {
		return nil, "", fmt.Errorf("查询 BlueZ: %w", err)
	}
	var device BLEDevice
	for _, d := range bleDevices(objects) {
		if address == "" || strings.EqualFold(address, d.Address) {
			device = d
			break
		}
	}
	if device.Path == "" {
		return nil, "", fmt.Errorf("未发现已识别的蓝牙 BRB02；请先用系统蓝牙设置配对 BS BRB02 Cooler Pro")
	}
	if !device.Paired {
		return nil, "", fmt.Errorf("BRB02 尚未配对，请先在系统蓝牙设置完成配对")
	}
	if !device.Connected {
		err = conn.Object("org.bluez", device.Path).CallWithContext(ctx, "org.bluez.Device1.Connect", 0).Err
		if err != nil && !strings.Contains(err.Error(), "InProgress") && !strings.Contains(err.Error(), "AlreadyConnected") {
			return nil, "", fmt.Errorf("连接 BRB02 蓝牙: %w", err)
		}
	}
	var notify, write dbus.ObjectPath
	var writeType string
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		objects, err = managedObjects(ctx, conn)
		if err == nil {
			properties := objects[device.Path]["org.bluez.Device1"]
			if !boolProp(properties, "Connected") {
				err = fmt.Errorf("蓝牙连接尚未建立")
			} else if !boolProp(properties, "ServicesResolved") {
				err = fmt.Errorf("蓝牙 GATT 服务尚未完成解析")
			} else {
				notify, write, writeType, err = controlCharacteristics(objects, device.Path)
			}
		}
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, "", fmt.Errorf("蓝牙已配对，但控制 GATT 服务未就绪（可能反复断连）：%w；%v", ctx.Err(), err)
		case <-tick.C:
		}
	}
	c := &bleConn{conn: conn, device: device.Path, notify: notify, write: write, writeType: writeType, signals: make(chan *dbus.Signal, 256)}
	conn.Signal(c.signals)
	if err = conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.bluez"), dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchPathNamespace(device.Path)); err != nil {
		return nil, "", err
	}
	if err = conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.bluez"), dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"), dbus.WithMatchMember("InterfacesRemoved")); err != nil {
		return nil, "", err
	}
	if err = conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.bluez"), dbus.WithMatchInterface("org.bluez.Device1"), dbus.WithMatchMember("Disconnected"), dbus.WithMatchObjectPath(device.Path)); err != nil {
		return nil, "", err
	}
	if err = conn.Object("org.bluez", notify).CallWithContext(ctx, "org.bluez.GattCharacteristic1.StartNotify", 0).Err; err != nil {
		return nil, "", fmt.Errorf("订阅 AE04 通知: %w", err)
	}
	owned = true
	return c, "ble://" + device.Address, nil
}

func (c *bleConn) Read(ctx context.Context, b []byte) (int, error) {
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-c.conn.Context().Done():
			return 0, io.EOF
		case signal, ok := <-c.signals:
			if !ok {
				return 0, io.EOF
			}
			if signal == nil {
				continue
			}
			if signal.Name == "org.bluez.Device1.Disconnected" && len(signal.Body) == 2 {
				return 0, fmt.Errorf("BRB02 蓝牙断开: %v (%v)", signal.Body[0], signal.Body[1])
			}
			if signal.Name == "org.freedesktop.DBus.ObjectManager.InterfacesRemoved" && len(signal.Body) > 0 {
				p, _ := signal.Body[0].(dbus.ObjectPath)
				if p == c.device || p == c.notify || p == c.write {
					return 0, fmt.Errorf("蓝牙控制接口已移除")
				}
			}
			if signal.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" || len(signal.Body) < 2 {
				continue
			}
			iface, _ := signal.Body[0].(string)
			properties, _ := signal.Body[1].(map[string]dbus.Variant)
			if signal.Path == c.device && iface == "org.bluez.Device1" {
				if v, exists := properties["Connected"]; exists {
					connected, _ := v.Value().(bool)
					if !connected {
						return 0, fmt.Errorf("BRB02 蓝牙已断开")
					}
				}
				if v, exists := properties["ServicesResolved"]; exists {
					resolved, _ := v.Value().(bool)
					if !resolved {
						return 0, fmt.Errorf("BRB02 蓝牙控制服务已失效")
					}
				}
			}
			if signal.Path != c.notify || iface != "org.bluez.GattCharacteristic1" {
				continue
			}
			if v, exists := properties["Notifying"]; exists {
				on, _ := v.Value().(bool)
				if !on {
					return 0, fmt.Errorf("蓝牙通知已停止")
				}
			}
			data, _ := properties["Value"].Value().([]byte)
			if len(data) == 0 {
				continue
			}
			if len(data) > len(b) {
				return 0, fmt.Errorf("蓝牙通知长度 %d 超出缓冲区 %d", len(data), len(b))
			}
			return copy(b, data), nil
		}
	}
}

func (c *bleConn) Write(b []byte) (int, error) {
	packet, err := reportPacket(b)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = c.conn.Object("org.bluez", c.write).CallWithContext(ctx, "org.bluez.GattCharacteristic1.WriteValue", 0, packet, map[string]dbus.Variant{"type": dbus.MakeVariant(c.writeType)}).Err
	if err != nil {
		return 0, fmt.Errorf("写入 AE10: %w", err)
	}
	return len(b), nil
}

func (c *bleConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Stop only our notification subscription. Leave the system's pairing intact.
	_ = c.conn.Object("org.bluez", c.notify).CallWithContext(ctx, "org.bluez.GattCharacteristic1.StopNotify", 0).Err
	return c.conn.Close()
}
