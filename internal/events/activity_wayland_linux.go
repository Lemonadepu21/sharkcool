//go:build linux

package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const ActivityInterval = 100 * time.Millisecond
const activityNotificationID uint32 = 7

// ActivitySource observes only compositor idle/resumed messages, without
// obtaining a wl_keyboard, wl_pointer, keymap, key values or device FDs.
// Version 2's get_input_idle_notification ignores idle inhibitors, so video
// playback does not get mistaken for continuous user input.
type ActivitySource struct {
	conn                       net.Conn
	seatGlobal, notifierGlobal uint32
	mu                         sync.Mutex
	used                       bool
}

func (*ActivitySource) Name() string   { return "wayland-input-idle-v2" }
func (s *ActivitySource) Close() error { return s.conn.Close() }

// OpenActivity validates support before callers change the device's effect.
// It uses the current user's normal Wayland socket, with no root/input group.
func OpenActivity(ctx context.Context) (*ActivitySource, error) {
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		return nil, fmt.Errorf("响应灯效需要 Wayland ext_idle_notifier_v1 v2；未设置 WAYLAND_DISPLAY")
	}
	if !filepath.IsAbs(display) {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if !filepath.IsAbs(runtimeDir) {
			return nil, fmt.Errorf("Wayland 需要有效的 XDG_RUNTIME_DIR")
		}
		display = filepath.Join(runtimeDir, display)
	}
	setupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(setupCtx, "unix", display)
	if err != nil {
		return nil, fmt.Errorf("连接当前 Wayland 会话: %w", err)
	}
	return openActivityConn(setupCtx, conn)
}

func openActivityConn(ctx context.Context, conn net.Conn) (source *ActivitySource, err error) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() {
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				err = ctx.Err()
			}
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err = writeWayland(conn, 1, 1, wlUint(2)); err != nil {
		return nil, err
	} // get_registry
	if err = writeWayland(conn, 1, 0, wlUint(3)); err != nil {
		return nil, err
	} // sync
	var seat, notifier, notifierVersion uint32
	for {
		var m waylandMessage
		m, err = readWayland(conn)
		if err != nil {
			return nil, err
		}
		if err = waylandFatal(m); err != nil {
			return nil, err
		}
		if m.object == 3 && m.opcode == 0 {
			if len(m.payload) != 4 {
				return nil, fmt.Errorf("无效的 Wayland 同步响应")
			}
			break
		}
		if m.object == 2 && m.opcode == 0 {
			var name, version uint32
			var iface string
			name, iface, version, err = waylandGlobal(m.payload)
			if err != nil {
				return nil, err
			}
			if iface == "wl_seat" && version >= 1 && seat == 0 {
				seat = name
			}
			if iface == "ext_idle_notifier_v1" {
				notifier, notifierVersion = name, version
			}
		}
		if m.object == 2 && m.opcode == 1 && len(m.payload) == 4 {
			name := binary.NativeEndian.Uint32(m.payload)
			if name == seat {
				seat = 0
			}
			if name == notifier {
				notifier = 0
			}
		}
	}
	if seat == 0 || notifier == 0 || notifierVersion < 2 {
		return nil, fmt.Errorf("当前桌面未提供 wl_seat 和 ext_idle_notifier_v1 v2，响应灯效不读取原始按键设备")
	}
	// Only these two interfaces are bound. wl_seat v1 has no keyboard/pointer
	// objects until explicitly requested; this client never requests them.
	if err = writeWayland(conn, 2, 0, wlBind(seat, "wl_seat", 1, 4)); err != nil {
		return nil, err
	}
	if err = writeWayland(conn, 2, 0, wlBind(notifier, "ext_idle_notifier_v1", 2, 5)); err != nil {
		return nil, err
	}
	if err = writeWayland(conn, 1, 0, wlUint(6)); err != nil {
		return nil, err
	}
	for {
		m, readErr := readWayland(conn)
		if readErr != nil {
			return nil, readErr
		}
		if err = waylandFatal(m); err != nil {
			return nil, err
		}
		if m.object == 6 && m.opcode == 0 {
			if len(m.payload) != 4 {
				return nil, fmt.Errorf("无效的 Wayland 同步响应")
			}
			break
		}
		if removedWaylandGlobal(m, seat, notifier) {
			return nil, fmt.Errorf("Wayland 活动服务已移除")
		}
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &ActivitySource{conn: conn, seatGlobal: seat, notifierGlobal: notifier}, nil
}

// Capture emits at most once per 100 ms while input is active. It first waits
// for an idled/resumed pair, so opening the source alone produces no activity.
// A source is single-use; cancellation closes its socket and joins its reader.
func (s *ActivitySource) Capture(ctx context.Context, emit func() error) error {
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return fmt.Errorf("活动采集源不能重复启动")
	}
	s.used = true
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return err
	}
	if emit == nil {
		_ = s.Close()
		return fmt.Errorf("活动回调不能为空")
	}
	readerCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(readerCtx, func() { _ = s.Close() })
	defer func() { cancel(); _ = s.Close(); stop() }()
	// ext_idle_notifier_v1.get_input_idle_notification, request opcode 2.
	request := append(wlUint(activityNotificationID), wlUint(uint32(ActivityInterval/time.Millisecond))...)
	request = append(request, wlUint(4)...)
	if err := writeWayland(s.conn, 5, 2, request); err != nil {
		_ = s.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	state := &activityState{}
	changed := make(chan struct{}, 1)
	errors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			m, err := readWayland(s.conn)
			if err == nil {
				err = waylandFatal(m)
			}
			if err == nil && removedWaylandGlobal(m, s.seatGlobal, s.notifierGlobal) {
				err = fmt.Errorf("Wayland 活动服务已移除")
			}
			if err == nil && m.object == activityNotificationID {
				err = state.update(m)
			}
			if err != nil {
				state.mu.Lock()
				state.active = false
				state.mu.Unlock()
				errors <- err
				return
			}
			if m.object == activityNotificationID {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}()
	defer func() { cancel(); _ = s.Close(); <-done }()
	tick := time.NewTicker(ActivityInterval)
	defer tick.Stop()
	var lastSent time.Time
	send := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state.mu.Lock()
		active := state.active
		state.mu.Unlock()
		if !active {
			return nil
		}
		now := time.Now()
		if !lastSent.IsZero() && now.Sub(lastSent) < ActivityInterval {
			return nil
		}
		if err := emit(); err != nil {
			return err
		}
		lastSent = now
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := send(); err != nil {
				return err
			}
		case <-changed:
			if err := send(); err != nil {
				return err
			}
		case err := <-errors:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("Wayland 活动连接已结束: %w", err)
		}
	}
}

// Keep only the newest activity state. A slow device write must not queue
// historic input notifications and replay them after the user becomes idle.
type activityState struct {
	mu           sync.Mutex
	idle, active bool
}

func (s *activityState) update(m waylandMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(m.payload) != 0 {
		return fmt.Errorf("无效的 Wayland 活动事件")
	}
	switch m.opcode {
	case 0:
		if s.idle {
			return fmt.Errorf("Wayland 重复空闲事件")
		}
		s.idle, s.active = true, false
	case 1:
		if !s.idle {
			return fmt.Errorf("Wayland 活动事件缺少之前的空闲事件")
		}
		s.idle, s.active = false, true
	default:
		return fmt.Errorf("未知的 Wayland 活动事件")
	}
	return nil
}

type waylandMessage struct {
	object  uint32
	opcode  uint16
	payload []byte
}

func wlUint(n uint32) []byte { p := make([]byte, 4); binary.NativeEndian.PutUint32(p, n); return p }
func wlString(s string) []byte {
	n := len(s) + 1
	p := make([]byte, 4+(n+3)&^3)
	binary.NativeEndian.PutUint32(p, uint32(n))
	copy(p[4:], s)
	return p
}
func wlBind(name uint32, iface string, version, id uint32) []byte {
	p := append(wlUint(name), wlString(iface)...)
	p = append(p, wlUint(version)...)
	return append(p, wlUint(id)...)
}

func writeWayland(w io.Writer, object uint32, opcode uint16, payload []byte) error {
	if len(payload)%4 != 0 || len(payload) > 65524 {
		return fmt.Errorf("无效的 Wayland 请求长度")
	}
	p := append(wlUint(object), wlUint(uint32(len(payload)+8)<<16|uint32(opcode))...)
	p = append(p, payload...)
	n, err := w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return err
}

func readWayland(r io.Reader) (waylandMessage, error) {
	h := make([]byte, 8)
	if _, err := io.ReadFull(r, h); err != nil {
		return waylandMessage{}, err
	}
	word := binary.NativeEndian.Uint32(h[4:])
	size := int(word >> 16)
	object := binary.NativeEndian.Uint32(h)
	if object == 0 || size < 8 || size%4 != 0 {
		return waylandMessage{}, fmt.Errorf("无效的 Wayland 消息头")
	}
	p := make([]byte, size-8)
	if _, err := io.ReadFull(r, p); err != nil {
		return waylandMessage{}, err
	}
	return waylandMessage{object: object, opcode: uint16(word), payload: p}, nil
}

func decodeWaylandString(p []byte) (string, int, error) {
	if len(p) < 4 {
		return "", 0, fmt.Errorf("Wayland 字符串被截断")
	}
	n := int(binary.NativeEndian.Uint32(p))
	padded := (n + 3) &^ 3
	if n < 1 || n > 4096 || len(p) < 4+padded || p[4+n-1] != 0 || bytes.IndexByte(p[4:4+n-1], 0) >= 0 {
		return "", 0, fmt.Errorf("无效的 Wayland 字符串")
	}
	return string(p[4 : 4+n-1]), 4 + padded, nil
}

func waylandGlobal(p []byte) (uint32, string, uint32, error) {
	if len(p) < 12 {
		return 0, "", 0, fmt.Errorf("Wayland global 被截断")
	}
	iface, n, err := decodeWaylandString(p[4:])
	if err != nil {
		return 0, "", 0, err
	}
	if len(p) != 4+n+4 {
		return 0, "", 0, fmt.Errorf("无效的 Wayland global 长度")
	}
	return binary.NativeEndian.Uint32(p), iface, binary.NativeEndian.Uint32(p[4+n:]), nil
}

func waylandFatal(m waylandMessage) error {
	if m.object != 1 || m.opcode != 0 {
		return nil
	}
	if len(m.payload) < 12 {
		return fmt.Errorf("无效的 Wayland error")
	}
	message, _, err := decodeWaylandString(m.payload[8:])
	if err != nil {
		return err
	}
	return fmt.Errorf("Wayland error 对象 %d 代码 %d: %s", binary.NativeEndian.Uint32(m.payload), binary.NativeEndian.Uint32(m.payload[4:]), message)
}

func removedWaylandGlobal(m waylandMessage, names ...uint32) bool {
	if m.object != 2 || m.opcode != 1 || len(m.payload) != 4 {
		return false
	}
	name := binary.NativeEndian.Uint32(m.payload)
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}
