package brb02

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type firmwareTransport struct{ fakeTransport }

func (d *firmwareTransport) Write(report []byte) (int, error) {
	n, err := d.fakeTransport.Write(report)
	if err == nil && report[3] == 1 {
		packet, _ := BuildPacket(1, []byte{3, 0, 3})
		d.reads <- packet
	}
	return n, err
}

func TestSilentLinkAndWriteFailureRecover(t *testing.T) {
	for _, failure := range []string{"silent", "short-write"} {
		t.Run(failure, func(t *testing.T) {
			first := &fakeTransport{reads: make(chan []byte, 8), short: failure == "short-write"}
			second := &firmwareTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}}
			var mu sync.Mutex
			opens := 0
			c := NewWithConnector(func(context.Context) (Transport, string, error) {
				mu.Lock()
				defer mu.Unlock()
				opens++
				if opens == 1 {
					return first, "same-device", nil
				}
				if opens == 2 {
					return nil, "", io.EOF
				} // first reopen can fail
				return second, "same-device", nil
			}, false)
			c.idleTimeout = 80 * time.Millisecond
			c.retryDelay = 5 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected && s.ConnectionGeneration == 1 })
			if failure == "short-write" {
				if _, err := c.Query(ctx, "firmware"); !errors.Is(err, io.ErrShortWrite) {
					t.Fatalf("short write: %v", err)
				}
			}
			waitStatus(t, c, func(s Status) bool { return s.Connected && s.ConnectionGeneration == 2 })
			heartbeat, _ := BuildPacket(6, []byte{0xce, 0x0d, 1})
			second.reads <- heartbeat
			waitStatus(t, c, func(s Status) bool { return !s.Stale && s.RPM == 3534 && s.Error == "" })
			queryCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			f, err := c.Query(queryCtx, "firmware")
			if err != nil || f.Command != 1 {
				t.Fatalf("query after recovery: %+v %v", f, err)
			}
			cancel()
			<-done
			first.mu.Lock()
			closed := first.closed
			first.mu.Unlock()
			second.mu.Lock()
			closed2 := second.closed
			second.mu.Unlock()
			mu.Lock()
			count := opens
			mu.Unlock()
			if closed != 1 || closed2 != 1 || count != 3 || c.Snapshot().Connected {
				t.Fatalf("leaked connections: %d %d %d", closed, closed2, count)
			}
		})
	}
}

func TestQueuedCommandDeadlineDoesNotWrite(t *testing.T) {
	d := &fakeTransport{reads: make(chan []byte, 8)}
	c := NewWithConnector(nil, false)
	c.dev = d
	if err := c.commandMu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Query(ctx, "firmware"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		c.commandMu.Unlock()
		t.Fatal("queued command ignored its deadline")
	}
	d.mu.Lock()
	writes := len(d.writes)
	d.mu.Unlock()
	if writes != 0 {
		t.Fatal("expired command wrote to the device")
	}
	c.commandMu.Unlock()
	if err := c.commandMu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.commandMu.Unlock()
}
