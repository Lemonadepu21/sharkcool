package brb02

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBridgeHelper(t *testing.T) {
	mode := os.Getenv("SHARKCOOL_TEST_BRIDGE")
	if mode == "" {
		return
	}
	emit := func(m bridgeMessage) { _ = json.NewEncoder(os.Stdout).Encode(m) }
	if mode == "invalid" {
		os.Stdout.WriteString("not-json\n")
		os.Exit(0)
	}
	if mode == "no-ready" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	emit(bridgeMessage{Type: "ready", Device: "ble://fixture"})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m bridgeMessage
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(4)
		}
		if mode == "reject" {
			emit(bridgeMessage{Type: "error", Error: "GATT write rejected"})
			os.Exit(1)
		}
		if mode == "wrong-id" {
			emit(bridgeMessage{Type: "written", ID: m.ID + 1})
			continue
		}
		emit(bridgeMessage{Type: "written", ID: m.ID})
		emit(bridgeMessage{Type: "packet", Data: "a5051101bc"})
	}
	os.Exit(0)
}
func testBridge(t *testing.T, mode string) (Transport, string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBridgeHelper$")
	cmd.Env = append(os.Environ(), "SHARKCOOL_TEST_BRIDGE="+mode)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return startBridge(ctx, cmd)
}
func TestNativeBridgeRoundTripAndReadCancellation(t *testing.T) {
	c, path, err := testBridge(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if path != "ble://fixture" {
		t.Fatal(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	b := make([]byte, 64)
	if _, err = c.Read(ctx, b); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read cancellation: %v", err)
	}
	report, _ := QueryReport("rgb-switch")
	if n, err := c.Write(report); err != nil || n != 65 {
		t.Fatalf("write %d %v", n, err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	n, err := c.Read(ctx2, b)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseReport(b[:n])
	if err != nil || f.Command != 0x11 || len(f.Payload) != 1 || f.Payload[0] != 1 {
		t.Fatalf("frame %v %v", f, err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Write(report); err == nil {
		t.Fatal("write after close accepted")
	}
}
func TestNativeBridgeRejectsInvalidMessagesAndDeadlines(t *testing.T) {
	for _, mode := range []string{"invalid", "no-ready"} {
		t.Run(mode, func(t *testing.T) {
			c, _, err := testBridge(t, mode)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("invalid bridge accepted")
			}
		})
	}
}
func TestNativeBridgeRejectsFailedOrMismatchedWrites(t *testing.T) {
	for _, mode := range []string{"reject", "wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			c, _, err := testBridge(t, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			report, _ := QueryReport("lcd-switch")
			if _, err = c.Write(report); err == nil {
				t.Fatal("failed write acknowledged")
			}
		})
	}
}
func TestNativeBridgeRejectsMalformedReport(t *testing.T) {
	c, _, err := testBridge(t, "ok")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte{0, 0xa5}); err == nil {
		t.Fatal("invalid report sent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err = c.Read(ctx, make([]byte, 64)); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
