//go:build linux

package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaylandWireAndStringBounds(t *testing.T) {
	for _, name := range []string{"wl_seat", "ext_idle_notifier_v1", "abc", "abcd"} {
		p := append(wlUint(42), wlString(name)...)
		p = append(p, wlUint(2)...)
		id, iface, version, err := waylandGlobal(p)
		if err != nil || id != 42 || iface != name || version != 2 {
			t.Fatalf("%d %s %d %v", id, iface, version, err)
		}
		var wire bytes.Buffer
		if err = writeWayland(&wire, 2, 0, p); err != nil {
			t.Fatal(err)
		}
		b := wire.Bytes()
		m, err := readWayland(io.MultiReader(bytes.NewReader(b[:3]), bytes.NewReader(b[3:7]), bytes.NewReader(b[7:])))
		if err != nil || m.object != 2 || m.opcode != 0 || !bytes.Equal(m.payload, p) {
			t.Fatalf("%+v %v", m, err)
		}
	}
	for _, p := range [][]byte{{}, wlUint(0), wlUint(0xffffffff), append(wlUint(2), 'a', 'b', 0, 0), wlString("a\x00b")} {
		if _, _, err := decodeWaylandString(p); err == nil {
			t.Fatalf("invalid string accepted: %x", p)
		}
	}
	for _, header := range [][]byte{append(wlUint(0), wlUint(8<<16)...), append(wlUint(1), wlUint(7<<16)...), append(wlUint(1), wlUint(9<<16)...)} {
		if _, err := readWayland(bytes.NewReader(header)); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
}

// A private in-memory compositor fixture checks the actual request envelopes.
// No keyboard/pointer object is requested and no real desktop is accessed.
func activityFixture(t *testing.T, version uint32) (*ActivitySource, net.Conn, <-chan error, error) {
	t.Helper()
	client, server := net.Pipe()
	result := make(chan error, 1)
	go func() {
		_ = server.SetDeadline(time.Now().Add(3 * time.Second))
		expect := func(object uint32, opcode uint16, payload []byte) error {
			m, err := readWayland(server)
			if err != nil {
				return err
			}
			if m.object != object || m.opcode != opcode || !bytes.Equal(m.payload, payload) {
				return errors.New("unexpected Wayland request")
			}
			return nil
		}
		steps := func() error {
			if err := expect(1, 1, wlUint(2)); err != nil {
				return err
			}
			if err := expect(1, 0, wlUint(3)); err != nil {
				return err
			}
			for _, g := range []struct {
				name    uint32
				iface   string
				version uint32
			}{{11, "wl_seat", 8}, {22, "ext_idle_notifier_v1", version}, {33, "wl_shm", 1}} {
				p := append(wlUint(g.name), wlString(g.iface)...)
				p = append(p, wlUint(g.version)...)
				if err := writeWayland(server, 2, 0, p); err != nil {
					return err
				}
			}
			if err := writeWayland(server, 3, 0, wlUint(99)); err != nil {
				return err
			}
			if version < 2 {
				return nil
			}
			if err := expect(2, 0, wlBind(11, "wl_seat", 1, 4)); err != nil {
				return err
			}
			if err := expect(2, 0, wlBind(22, "ext_idle_notifier_v1", 2, 5)); err != nil {
				return err
			}
			if err := expect(1, 0, wlUint(6)); err != nil {
				return err
			}
			if err := writeWayland(server, 6, 0, wlUint(100)); err != nil {
				return err
			}
			p := append(wlUint(7), wlUint(100)...)
			p = append(p, wlUint(4)...)
			if err := expect(5, 2, p); err != nil {
				return err
			}
			return server.SetDeadline(time.Time{})
		}
		result <- steps()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	source, err := openActivityConn(ctx, client)
	return source, server, result, err
}

func waitActivityFixture(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("compositor request timeout")
	}
}

func TestWaylandActivityAndSlowProducerDoesNotReplay(t *testing.T) {
	source, server, ready, err := activityFixture(t, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var count atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- source.Capture(ctx, func() error {
			if count.Add(1) == 1 {
				close(entered)
				<-release
			}
			return nil
		})
	}()
	waitActivityFixture(t, ready)
	if count.Load() != 0 {
		t.Fatal("opening source emitted activity")
	}
	if err = writeWayland(server, 7, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err = writeWayland(server, 7, 1, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("resume did not emit")
	}
	// While the callback is blocked, the reader must keep following the latest
	// state rather than queueing activity to replay after it becomes idle.
	for _, op := range []uint16{0, 1, 0} {
		if err = writeWayland(server, 7, op, nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(10 * time.Millisecond)
	releaseOnce.Do(func() { close(release) })
	time.Sleep(220 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("old input replayed: %d", count.Load())
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = source.Capture(context.Background(), func() error { return nil }); err == nil {
		t.Fatal("source reused")
	}
}

func TestWaylandCaptureFailureAndUnsupportedProtocol(t *testing.T) {
	source, server, ready, err := activityFixture(t, 1)
	if err == nil || source != nil {
		t.Fatal("idle inhibitors ignored on unsupported v1")
	}
	server.Close()
	waitActivityFixture(t, ready)
	for _, variant := range []string{"callback-error", "protocol-error", "removed", "disconnect"} {
		t.Run(variant, func(t *testing.T) {
			source, server, ready, err := activityFixture(t, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			sentinel := errors.New("producer callback failed")
			done := make(chan error, 1)
			go func() { done <- source.Capture(ctx, func() error { return sentinel }) }()
			waitActivityFixture(t, ready)
			switch variant {
			case "callback-error":
				_ = writeWayland(server, 7, 0, nil)
				_ = writeWayland(server, 7, 1, nil)
			case "protocol-error":
				_ = writeWayland(server, 7, 1, nil)
			case "removed":
				_ = writeWayland(server, 2, 1, wlUint(11))
			case "disconnect":
				_ = server.Close()
			}
			err = <-done
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failure ignored: %v", err)
			}
			if variant == "callback-error" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
		})
	}
}

func TestWaylandCancellationDuringWriteAndPartialRead(t *testing.T) {
	for _, partial := range []bool{false, true} {
		client, server := net.Pipe()
		source := &ActivitySource{conn: client}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		if partial {
			go func() { _, _ = readWayland(server); _, _ = server.Write(wlUint(7)) }()
		}
		err := source.Capture(ctx, func() error { return nil })
		cancel()
		server.Close()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("partial=%t: %v", partial, err)
		}
	}
}

func TestWaylandStringCountCannotOverflow(t *testing.T) {
	p := make([]byte, 4)
	binary.NativeEndian.PutUint32(p, 0xfffffffd)
	if _, _, err := decodeWaylandString(p); err == nil {
		t.Fatal("overflow accepted")
	}
}
