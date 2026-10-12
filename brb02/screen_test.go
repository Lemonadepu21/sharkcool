package brb02

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestSystemInfoAndLCDOfficialWire(t *testing.T) {
	// DLL count + ID/u16; the service uses ID 7 = hour*60 + minute.
	info := SystemInfo{TimeOfDay: 608, CPUTemperature: 35, CPUUsage: 0, MemoryUsage: 42}
	report, err := info.Report()
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseReport(report)
	if err != nil || f.Command != 7 || !f.ChecksumValid || !bytes.Equal(f.Payload, []byte{4, 0, 35, 0, 2, 0, 0, 6, 42, 0, 7, 0x60, 2}) {
		t.Fatalf("wrong system-info wire: %+v %v", f, err)
	}
	for _, bad := range []SystemInfo{{}, {FanRPM: 1350}, {Metric("unknown"): 1}, {CPUUsage: 101}, {TimeOfDay: 1440}, {GPUTemperature: 151}} {
		if _, err := bad.Report(); err == nil {
			t.Fatalf("accepted invalid info %v", bad)
		}
	}
	layout := LCDLayout{[]Metric{CPUTemperature, FanRPM, TimeOfDay}}
	report, err = layout.Report()
	if err != nil {
		t.Fatal(err)
	}
	f, err = ParseReport(report)
	if err != nil || f.Command != 0xc2 || !f.ChecksumValid || !bytes.Equal(f.Payload, []byte{0, 0, 3, 0, 10, 0, 100, 4, 143, 0, 100, 7, 20, 1, 100}) {
		t.Fatalf("wrong three-row layout: %+v %v", f, err)
	}
	for _, rows := range [][]Metric{nil, {CPUTemperature}, {CPUTemperature, FanRPM, FanRPM}, {CPUTemperature, FanRPM, Metric("bad")}} {
		if _, err := (LCDLayout{rows}).Report(); err == nil {
			t.Fatalf("accepted invalid layout %v", rows)
		}
	}
}

func TestSystemInfoDeliveryDoesNotNeedReply(t *testing.T) {
	dev := &fakeTransport{reads: make(chan []byte, 1)}
	c := NewWithConnector(nil, false)
	c.dev = dev
	result, err := c.SendSystemInfo(context.Background(), SystemInfo{TimeOfDay: 0})
	if err != nil || !result.Sent || len(dev.writes) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	dev.short = true
	if result, err := c.SendSystemInfo(context.Background(), SystemInfo{TimeOfDay: 1}); !errors.Is(err, io.ErrShortWrite) || result.Sent {
		t.Fatalf("short write: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.SendSystemInfo(ctx, SystemInfo{TimeOfDay: 2}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(dev.writes) != 2 {
		t.Fatal("canceled command wrote to device")
	}
	if _, err := NewWithConnector(nil, true).SendSystemInfo(context.Background(), SystemInfo{TimeOfDay: 0}); err == nil {
		t.Fatal("demo claimed hardware delivery")
	}
}

func TestLCDSwitchReadbackRejectsOtherResponses(t *testing.T) {
	for _, v := range []byte{0, 1} {
		on, err := (Frame{Command: 0xc1, Payload: []byte{v}}).LCDEnabled()
		if err != nil || on != (v == 1) {
			t.Fatalf("%d: %t %v", v, on, err)
		}
	}
	for _, frame := range []Frame{{Command: 0xc0, Payload: []byte{0}}, {Command: 0xc1}, {Command: 0xc1, Payload: []byte{2}}, {Command: 0xc1, Payload: []byte{1, 0}}} {
		if _, err := frame.LCDEnabled(); err == nil {
			t.Fatalf("accepted non-boolean query response: %+v", frame)
		}
	}
}
