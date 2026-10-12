package brb02

import (
	"context"
	"testing"
	"time"
)

type switchTransport struct {
	fakeTransport
	response []byte
}

func (d *switchTransport) Write(report []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, report...))
	packet, _ := BuildPacket(report[3], d.response)
	d.reads <- packet
	return len(report), nil
}

func TestSwitchRequiresSuccessfulACKAndNeverRetries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		success bool
	}{
		{"accepted", []byte{0}, true}, {"rejected", []byte{1}, false},
		{"empty", nil, false}, {"malformed", []byte{0, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &switchTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, response: tc.payload}
			c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected })
			commandCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			_, err := c.SetSwitch(commandCtx, "fan", false)
			if (err == nil) != tc.success {
				t.Fatalf("response %x, err %v", tc.payload, err)
			}
			d.mu.Lock()
			count := len(d.writes)
			d.mu.Unlock()
			if count != 1 {
				t.Fatalf("switch command replayed %d times", count)
			}
		})
	}
}

func TestFanStateDecoder(t *testing.T) {
	for _, value := range []byte{0, 1} {
		on, err := (Frame{Command: 0x21, Payload: []byte{value}}).FanEnabled()
		if err != nil || on != (value == 1) {
			t.Fatal(on, err)
		}
	}
	for _, frame := range []Frame{{Command: 0x20, Payload: []byte{0}}, {Command: 0x21}, {Command: 0x21, Payload: []byte{2}}, {Command: 0x21, Payload: []byte{0, 1}}, {Command: 0x21, Payload: []byte{1}, Long: true}} {
		if _, err := frame.FanEnabled(); err == nil {
			t.Fatal("invalid state accepted", frame)
		}
	}
}

func TestRGBStateDecoder(t *testing.T) {
	for _, value := range []byte{0, 1} {
		on, err := (Frame{Command: 0x11, Payload: []byte{value}}).RGBEnabled()
		if err != nil || on != (value == 1) {
			t.Fatal(on, err)
		}
	}
	for _, frame := range []Frame{{Command: 0x10, Payload: []byte{0}}, {Command: 0x11}, {Command: 0x11, Payload: []byte{2}}, {Command: 0x11, Payload: []byte{0, 1}}, {Command: 0x11, Payload: []byte{1}, Long: true}} {
		if _, err := frame.RGBEnabled(); err == nil {
			t.Fatal("invalid state accepted", frame)
		}
	}
}
