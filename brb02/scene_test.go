package brb02

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestSceneModeOfficialBooleanWire(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		wire    []byte
	}{
		{false, []byte{0xa5, 5, 8, 0, 0xb2}},
		{true, []byte{0xa5, 5, 8, 1, 0xb3}},
	} {
		report, err := SceneModeReport(tc.enabled)
		if err != nil || len(report) != 65 || report[0] != 0 || !bytes.Equal(report[1:6], tc.wire) {
			t.Fatalf("scene %v: %x %v", tc.enabled, report, err)
		}
	}
}

type sceneTransport struct {
	fakeTransport
	payload []byte
}

func (d *sceneTransport) Write(report []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, report...))
	f, _ := ParseReport(report)
	packet, _ := BuildPacket(f.Command, d.payload)
	d.reads <- packet
	return len(report), nil
}

func TestSceneModeRejectsFailureAndMalformedACK(t *testing.T) {
	for _, payload := range [][]byte{{0}, {1}, nil, {0, 0}} {
		d := &sceneTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, payload: payload}
		c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		waitStatus(t, c, func(s Status) bool { return s.Connected })
		commandCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		result, err := c.SetSceneMode(commandCtx, true)
		stop()
		cancel()
		<-done
		if bytes.Equal(payload, []byte{0}) {
			if err != nil || !result.Enabled || !result.Acknowledged {
				t.Fatal(result, err)
			}
		} else if err == nil || result.Acknowledged {
			t.Fatal("invalid ACK accepted", result, err)
		}
	}
	c := NewWithConnector(nil, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.SetSceneMode(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewWithConnector(nil, true).SetSceneMode(context.Background(), true); err == nil {
		t.Fatal("demo claimed acceptance")
	}
}
