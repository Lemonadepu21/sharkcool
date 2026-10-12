package brb02

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	reads  chan []byte
	mu     sync.Mutex
	writes [][]byte
	closed int
	ack    bool
	short  bool
}

func (f *fakeTransport) Read(ctx context.Context, b []byte) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case r := <-f.reads:
		if r == nil {
			return 0, io.EOF
		}
		return copy(b, r), nil
	}
}
func (f *fakeTransport) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, append([]byte{}, b...))
	if f.short {
		return 1, nil
	}
	if f.ack {
		r := make([]byte, 64)
		if b[3] == 0x25 {
			packet, _ := BuildPacket(0x25, f.writes[0][4:9])
			copy(r, packet)
		} else {
			copy(r, []byte{0xa5, 5, 0x24, 0, 0xe1})
		}
		f.reads <- r
	}
	return len(b), nil
}
func (f *fakeTransport) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closed++; return nil }

func waitStatus(t *testing.T, c *Controller, fn func(Status) bool) {
	t.Helper()
	timeout := time.After(time.Second)
	for {
		if fn(c.Snapshot()) {
			return
		}
		select {
		case <-timeout:
			t.Fatalf("status timeout: %+v", c.Snapshot())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestControllerACKAndShutdown(t *testing.T) {
	dev := &fakeTransport{reads: make(chan []byte, 8), ack: true}
	c := NewWithConnector(nil, false)
	c.connector = func(context.Context) (Transport, string, error) { return dev, "test", nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	dev.reads <- []byte{0xa5, 7, 6, 0x60, 0x0b, 1, 0x1e, 0, 0}
	waitStatus(t, c, func(s Status) bool { return s.RPM == 2912 && !s.Stale })
	cmdCtx, cmdCancel := context.WithTimeout(ctx, time.Second)
	defer cmdCancel()
	res, err := c.SetMode(cmdCtx, 3)
	if err != nil || !res.Sent || !res.Acknowledged || c.Snapshot().AcknowledgedMode != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader didn't shut down")
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.closed != 1 || len(dev.writes) != 2 || c.Snapshot().Connected || c.Snapshot().RPM != 0 {
		t.Fatal("handle not released or stale status retained")
	}
}

func TestUnconfirmedAndShortWrites(t *testing.T) {
	for _, short := range []bool{false, true} {
		dev := &fakeTransport{reads: make(chan []byte, 8), short: short}
		c := NewWithConnector(nil, false)
		c.dev = dev
		c.status.Connected = true
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		res, err := c.SetMode(ctx, 2)
		cancel()
		if short {
			if err == nil || res.Sent {
				t.Fatal("short write reported as success")
			}
		} else if err != nil || !res.Sent || res.Acknowledged || c.Snapshot().AcknowledgedMode != 0 {
			t.Fatalf("%+v %v", res, err)
		}
	}
}

func TestDisconnectClearsTelemetry(t *testing.T) {
	dev := &fakeTransport{reads: make(chan []byte, 8)}
	c := NewWithConnector(nil, false)
	c.connector = func(context.Context) (Transport, string, error) { return dev, "test", nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	dev.reads <- []byte{0xa5, 7, 6, 0x60, 0x0b, 1, 0x1e, 0, 0}
	waitStatus(t, c, func(s Status) bool { return s.RPM == 2912 })
	dev.reads <- nil
	waitStatus(t, c, func(s Status) bool { return !s.Connected && s.RPM == 0 && s.Stale })
	cancel()
	<-done
}

func TestDemoDoesNotOpenDevice(t *testing.T) {
	c := NewWithConnector(nil, true)
	c.connector = func(context.Context) (Transport, string, error) { t.Error("demo accessed HID"); return nil, "", io.EOF }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	res, err := c.SetMode(ctx, 3)
	if err != nil || !res.Acknowledged {
		t.Fatal(err)
	}
	cancel()
	<-done
}

func TestReconnectClosesOldHandleWithoutDuplicateOpens(t *testing.T) {
	first := &fakeTransport{reads: make(chan []byte, 8)}
	second := &fakeTransport{reads: make(chan []byte, 8)}
	c := NewWithConnector(nil, false)
	var mu sync.Mutex
	opens := 0
	c.connector = func(context.Context) (Transport, string, error) {
		mu.Lock()
		defer mu.Unlock()
		opens++
		if opens == 1 {
			return first, "first", nil
		}
		return second, "second", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitStatus(t, c, func(s Status) bool { return s.Device == "first" && s.Connected })
	first.reads <- nil
	waitStatus(t, c, func(s Status) bool { return !s.Connected })
	deadline := time.After(4 * time.Second)
	for c.Snapshot().Device != "second" || !c.Snapshot().Connected {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("hotplug reconnect timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	second.reads <- []byte{0xa5, 7, 6, 0xce, 0x0d, 1, 0x8e, 0, 0}
	waitStatus(t, c, func(s Status) bool { return s.RPM == 3534 })
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if opens != 2 {
		t.Fatalf("unexpected opens: %d", opens)
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()
	if first.closed != 1 || second.closed != 1 {
		t.Fatal("device handles leaked")
	}
}

func TestQueryRejectsUnrelatedFramesAndDisconnectCancelsWait(t *testing.T) {
	dev := &fakeTransport{reads: make(chan []byte, 8)}
	c := NewWithConnector(nil, false)
	c.connector = func(context.Context) (Transport, string, error) { return dev, "ble://test", nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	result := make(chan error, 1)
	go func() {
		f, err := c.Query(ctx, "firmware")
		if err == nil && (f.Command != 1 || len(f.Payload) != 3) {
			err = io.ErrUnexpectedEOF
		}
		result <- err
	}()
	waitWrite := func() {
		t.Helper()
		deadline := time.After(time.Second)
		for {
			dev.mu.Lock()
			n := len(dev.writes)
			dev.mu.Unlock()
			if n > 0 {
				return
			}
			select {
			case <-deadline:
				t.Fatal("query not sent")
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitWrite()
	heartbeat, _ := BuildPacket(6, []byte{6, 9, 1})
	dev.reads <- heartbeat
	select {
	case err := <-result:
		t.Fatalf("unrelated heartbeat completed query: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	firmware, _ := BuildPacket(1, []byte{3, 0, 3})
	dev.reads <- firmware
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	dev.mu.Lock()
	dev.writes = nil
	dev.mu.Unlock()
	go func() { _, err := c.Query(ctx, "firmware"); result <- err }()
	waitWrite()
	dev.reads <- nil
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("disconnect accepted as response")
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel query")
	}
	cancel()
	<-done
}
