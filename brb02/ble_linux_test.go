package brb02

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const fixtureDevice dbus.ObjectPath = "/org/bluez/hci0/dev_04_33_85_5E_BD_4A"
const fixtureService dbus.ObjectPath = fixtureDevice + "/service0010"
const fixtureNotify dbus.ObjectPath = fixtureService + "/char0011"
const fixtureWrite dbus.ObjectPath = fixtureService + "/char0012"

func fixtureObjects() bluezObjects {
	return bluezObjects{
		fixtureDevice:  {"org.bluez.Device1": {"Name": dbus.MakeVariant("BS BRB02 Cooler Pro"), "Address": dbus.MakeVariant("04:33:85:5E:BD:4A"), "Paired": dbus.MakeVariant(true), "Connected": dbus.MakeVariant(true), "ServicesResolved": dbus.MakeVariant(true)}},
		fixtureService: {"org.bluez.GattService1": {"UUID": dbus.MakeVariant(BLEServiceUUID)}},
		fixtureNotify:  {"org.bluez.GattCharacteristic1": {"UUID": dbus.MakeVariant(BLENotifyUUID), "Service": dbus.MakeVariant(fixtureService), "Flags": dbus.MakeVariant([]string{"notify"})}},
		fixtureWrite:   {"org.bluez.GattCharacteristic1": {"UUID": dbus.MakeVariant(BLEWriteUUID), "Service": dbus.MakeVariant(fixtureService), "Flags": dbus.MakeVariant([]string{"write-without-response"})}},
	}
}

func TestBLEIdentifiesControlServiceAndWriteType(t *testing.T) {
	objects := fixtureObjects()
	notify, write, typ, err := controlCharacteristics(objects, fixtureDevice)
	if err != nil || notify != fixtureNotify || write != fixtureWrite || typ != "command" {
		t.Fatalf("%s %s %s %v", notify, write, typ, err)
	}
	objects[fixtureWrite]["org.bluez.GattCharacteristic1"]["Flags"] = dbus.MakeVariant([]string{"write"})
	_, _, typ, err = controlCharacteristics(objects, fixtureDevice)
	if err != nil || typ != "request" {
		t.Fatalf("%s %v", typ, err)
	}
	// AE40, HID, and identically named UUIDs on another device are never selected.
	objects[fixtureService]["org.bluez.GattService1"]["UUID"] = dbus.MakeVariant("0000ae40-0000-1000-8000-00805f9b34fb")
	if _, _, _, err = controlCharacteristics(objects, fixtureDevice); err == nil {
		t.Fatal("wrong service accepted")
	}
	if _, _, _, err = controlCharacteristics(fixtureObjects(), "/org/bluez/hci0/dev_OTHER"); err == nil {
		t.Fatal("cross-device service accepted")
	}
}

func TestBLECachedCharacteristicsDoNotImplyReadyConnection(t *testing.T) {
	for _, key := range []string{"Paired", "Connected", "ServicesResolved"} {
		objects := fixtureObjects()
		objects[fixtureDevice]["org.bluez.Device1"][key] = dbus.MakeVariant(false)
		devices := bleDevices(objects)
		if len(devices) != 1 || devices[0].ControlReady {
			t.Fatalf("cached GATT accepted with %s=false: %+v", key, devices)
		}
	}
}

type fakeObjectManager struct{ objects bluezObjects }

func (f *fakeObjectManager) GetManagedObjects() (bluezObjects, *dbus.Error) { return f.objects, nil }

type capturedWrite struct {
	data []byte
	typ  string
}
type fakeCharacteristic struct{ writes chan capturedWrite }

func (*fakeCharacteristic) StartNotify() *dbus.Error { return nil }
func (*fakeCharacteristic) StopNotify() *dbus.Error  { return nil }
func (f *fakeCharacteristic) WriteValue(b []byte, options map[string]dbus.Variant) *dbus.Error {
	f.writes <- capturedWrite{append([]byte{}, b...), stringProp(options, "type")}
	return nil
}

// Test the actual D-Bus calls and signals on an isolated session bus. No hardware.
func TestBLEDBusRoundTripAndDisconnect(t *testing.T) {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon unavailable")
	}
	// A filesystem socket stays inside the test's temporary directory. The
	// daemon's default abstract socket is unavailable in restricted sandboxes.
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--address=unix:path="+filepath.Join(t.TempDir(), "bus"), "--print-address=1")
	var daemonError bytes.Buffer
	cmd.Stderr = &daemonError
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = cmd.Wait()
		t.Fatalf("private D-Bus startup: %v: %s", err, daemonError.String())
	}
	address = strings.TrimSpace(address)
	server, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err = server.RequestName("org.bluez", dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if err = server.Export(&fakeObjectManager{fixtureObjects()}, "/", "org.freedesktop.DBus.ObjectManager"); err != nil {
		t.Fatal(err)
	}
	characteristic := &fakeCharacteristic{make(chan capturedWrite, 1)}
	for _, path := range []dbus.ObjectPath{fixtureNotify, fixtureWrite} {
		if err = server.Export(characteristic, path, "org.bluez.GattCharacteristic1"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport, path, err := openBLE(ctx, "04:33:85:5E:BD:4A", func(options ...dbus.ConnOption) (*dbus.Conn, error) { return dbus.Connect(address, options...) })
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if path != "ble://04:33:85:5E:BD:4A" {
		t.Fatal(path)
	}
	report, _ := ModeReport(3)
	if n, err := transport.Write(report); err != nil || n != 65 {
		t.Fatalf("%d %v", n, err)
	}
	write := <-characteristic.writes
	if !bytes.Equal(write.data, report[1:10]) || write.typ != "command" {
		t.Fatalf("BLE must strip report ID and padding: %+v", write)
	}
	longPacket, _ := BuildLongPacket(0xc6, 1, true, bytes.Repeat([]byte{0xa4}, LongChunkSize))
	longReport, _ := PacketReport(longPacket)
	if n, err := transport.Write(longReport); err != nil || n != 65 {
		t.Fatalf("long BLE write: %d %v", n, err)
	}
	write = <-characteristic.writes
	if !bytes.Equal(write.data, longPacket) || len(write.data) != 64 {
		t.Fatalf("long BLE envelope changed: %x", write.data)
	}
	heartbeat := []byte{0xa5, 7, 6, 6, 9, 1, 0xc2}
	if err = server.Emit(fixtureNotify, "org.freedesktop.DBus.Properties.PropertiesChanged", "org.bluez.GattCharacteristic1", map[string]dbus.Variant{"Value": dbus.MakeVariant(heartbeat)}, []string{}); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	n, err := transport.Read(ctx, b)
	if err != nil || !bytes.Equal(b[:n], heartbeat) {
		t.Fatalf("%x %v", b[:n], err)
	}
	if err = server.Emit(fixtureDevice, "org.freedesktop.DBus.Properties.PropertiesChanged", "org.bluez.Device1", map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)}, []string{}); err != nil {
		t.Fatal(err)
	}
	if _, err = transport.Read(ctx, b); err == nil || !strings.Contains(err.Error(), "断开") {
		t.Fatalf("disconnect ignored: %v", err)
	}
}
