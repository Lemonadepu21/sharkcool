package brb02

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverUSBIdentity(t *testing.T) {
	root := t.TempDir()
	sys := filepath.Join(root, "sys")
	dev := filepath.Join(root, "dev")
	writeFixture(t, filepath.Join(sys, "hidraw3/device/uevent"), "HID_ID=0003:0000E2B7:00007001\nHID_NAME=BRB02\n")
	writeFixture(t, filepath.Join(sys, "hidraw4/device/uevent"), "HID_ID=0003:00001234:00007001\n")
	writeFixture(t, filepath.Join(sys, "hidraw5/device/uevent"), "HID_ID=0005:0000E2B7:00007001\n")
	writeFixture(t, filepath.Join(dev, "hidraw3"), "")
	devices, err := discover(sys, dev)
	if err != nil || len(devices) != 1 || devices[0].Path != filepath.Join(dev, "hidraw3") || devices[0].Name != "BRB02" {
		t.Fatalf("%+v %v", devices, err)
	}
}

func TestNonblockingReadCancellationAndClose(t *testing.T) {
	fds := []int{0, 0}
	if err := syscall.Pipe2(fds, syscall.O_NONBLOCK|syscall.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[1])
	c := &hidConn{fd: fds[0]}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := c.Read(ctx, make([]byte, 65)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), make([]byte, 65)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("%v", err)
	}
}

func TestHIDWritePreservesReportIDAndPayload(t *testing.T) {
	fds := []int{0, 0}
	if err := syscall.Pipe2(fds, syscall.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[0])
	c := &hidConn{fd: fds[1]}
	defer c.Close()
	r, _ := ModeReport(3)
	if n, err := c.Write(r); n != 65 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	buf := make([]byte, 65)
	n, err := syscall.Read(fds[0], buf)
	if err != nil || n != 65 || buf[0] != 0 || buf[1] != 0xa5 || buf[6] != 1 {
		t.Fatalf("%x %v", buf, err)
	}
	packet, _ := BuildLongPacket(0xc6, 1, true, bytes.Repeat([]byte{0xa4}, LongChunkSize))
	longReport, _ := PacketReport(packet)
	if n, err := c.Write(longReport); err != nil || n != 65 {
		t.Fatalf("long USB write: %d %v", n, err)
	}
	n, err = syscall.Read(fds[0], buf)
	if err != nil || n != 65 || !bytes.Equal(buf, longReport) {
		t.Fatalf("USB changed long report: %x %v", buf, err)
	}
	if _, err := c.Write(r[1:]); err == nil {
		t.Fatal("write without report ID accepted")
	}
}
