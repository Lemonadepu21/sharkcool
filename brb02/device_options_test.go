package brb02

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestDeviceOptionsCapturedResponse(t *testing.T) {
	options, err := (Frame{Command: 3, Payload: []byte{0, 1}}).DeviceOptions()
	if err != nil || options.SmartStartStop || !options.PowerOnStart {
		t.Fatal(options, err)
	}
	report, _ := options.Report()
	if !bytes.Equal(report[1:7], []byte{0xa5, 6, 2, 0, 1, 0xae}) {
		t.Fatalf("official six-byte packet changed: %x", report)
	}
	for _, f := range []Frame{{Command: 2, Payload: []byte{0}}, {Command: 3, Payload: []byte{0}}, {Command: 3, Payload: []byte{2, 1}}, {Command: 3, Payload: []byte{0, 1, 0}}} {
		if _, err := f.DeviceOptions(); err == nil {
			t.Fatalf("accepted malformed options: %+v", f)
		}
	}
}

type optionsTransport struct {
	fakeTransport
	options []byte
	stale   bool
}

func (d *optionsTransport) Write(report []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, report...))
	f, _ := ParseReport(report)
	p := d.options
	if f.Command == 2 {
		if !d.stale {
			d.options = append([]byte{}, f.Payload...)
		}
		p = []byte{0} // An ACK must not confirm either checkbox.
	}
	packet, _ := BuildPacket(f.Command, p)
	d.reads <- packet
	return len(report), nil
}

func TestDeviceOptionsPreserveUnspecifiedAndRequireReadback(t *testing.T) {
	for _, stale := range []bool{false, true} {
		d := &optionsTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, options: []byte{0, 1}, stale: stale}
		c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		waitStatus(t, c, func(s Status) bool { return s.Connected })
		on := true
		commandCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
		result, err := c.UpdateDeviceOptions(commandCtx, DeviceOptionsPatch{SmartStartStop: &on})
		if stale {
			if err == nil || result.Confirmed {
				t.Fatal("ACK or stale state confirmed update", result, err)
			}
		} else if err != nil || !result.Confirmed || !result.Options.SmartStartStop || !result.Options.PowerOnStart {
			t.Fatal("unspecified power-on option changed", result, err)
		}
		stop()
		cancel()
		<-done
	}
}
