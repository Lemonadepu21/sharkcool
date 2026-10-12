package brb02

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// The native bridge owns Windows HID/GATT handles. IPC is private stdio, never
// a network listener. The controller still verifies the cooler's own ACKs.
type bridgeMessage struct {
	Type   string `json:"type"`
	ID     int    `json:"id,omitempty"`
	Device string `json:"device,omitempty"`
	Data   string `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}
type bridgeConn struct {
	cmd       *exec.Cmd
	input     io.WriteCloser
	packets   chan []byte
	acks      chan bridgeMessage
	ready     chan bridgeMessage
	done      chan struct{}
	mu        sync.Mutex
	errMu     sync.Mutex
	err       error
	id        int
	closeOnce sync.Once
}

func bridgePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(exe), "sharkcool-bridge.exe")
	if _, err = os.Stat(p); err != nil {
		return "", fmt.Errorf("缺少 Windows 通信组件 sharkcool-bridge.exe，请解压完整桌面包: %w", err)
	}
	return p, nil
}
func openBridge(ctx context.Context, channel, target string) (Transport, string, error) {
	path, err := bridgePath()
	if err != nil {
		return nil, "", err
	}
	cmd := exec.Command(path, "--transport", channel, "--target", target)
	return startBridge(ctx, cmd)
}
func startBridge(ctx context.Context, cmd *exec.Cmd) (Transport, string, error) {
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, "", err
	}
	// Do not include device protocol data or pairing secrets in logs.
	cmd.Stderr = io.Discard
	prepareBridgeCommand(cmd)
	c := &bridgeConn{cmd: cmd, input: input, packets: make(chan []byte, 256), acks: make(chan bridgeMessage, 1), ready: make(chan bridgeMessage, 1), done: make(chan struct{})}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, "", err
	}
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 2048), 8192)
		for scanner.Scan() {
			var m bridgeMessage
			if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
				c.fail(fmt.Errorf("通信组件返回无效消息: %w", err))
				break
			}
			switch m.Type {
			case "ready":
				select {
				case c.ready <- m:
				default:
					c.fail(errors.New("通信组件重复就绪"))
				}
			case "packet":
				b, err := hex.DecodeString(m.Data)
				if err != nil || len(b) == 0 || len(b) > 4096 {
					c.fail(errors.New("通信组件返回无效数据包"))
					break
				}
				select {
				case c.packets <- b:
				default:
					c.fail(errors.New("设备数据队列已满"))
				}
			case "written":
				select {
				case c.acks <- m:
				default:
					c.fail(errors.New("通信组件返回多余写入确认"))
				}
			case "error":
				c.fail(errors.New(m.Error))
			default:
				c.fail(errors.New("通信组件返回未知消息"))
			}
			c.errMu.Lock()
			failed := c.err != nil
			c.errMu.Unlock()
			if failed {
				break
			}
		}
		if scanner.Err() != nil {
			c.fail(scanner.Err())
		}
		// A malformed or overflowing bridge must be terminated before waiting.
		c.errMu.Lock()
		failed := c.err != nil
		c.errMu.Unlock()
		if failed {
			_ = cmd.Process.Kill()
		}
		err := cmd.Wait()
		if err == nil {
			err = io.EOF
		}
		c.fail(err)
		close(c.done)
	}()
	select {
	case m := <-c.ready:
		if m.Device == "" {
			c.Close()
			return nil, "", errors.New("通信组件没有设备标识")
		}
		return c, m.Device, nil
	case <-ctx.Done():
		c.Close()
		return nil, "", ctx.Err()
	case <-c.done:
		return nil, "", c.failure()
	}
}
func (c *bridgeConn) fail(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		c.err = err
	}
}
func (c *bridgeConn) failure() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		return io.EOF
	}
	return c.err
}
func (c *bridgeConn) Read(ctx context.Context, b []byte) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-c.done:
		return 0, c.failure()
	case p := <-c.packets:
		if len(p) > len(b) {
			return 0, io.ErrShortBuffer
		}
		return copy(b, p), nil
	}
}
func (c *bridgeConn) Write(b []byte) (int, error) {
	if _, err := reportPacket(b); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return 0, c.failure()
	default:
	}
	c.id++
	payload, _ := json.Marshal(bridgeMessage{Type: "write", ID: c.id, Data: hex.EncodeToString(b)})
	// Pipe writes and native device writes are both bounded by the same deadline.
	sent := make(chan error, 1)
	go func() {
		n, err := c.input.Write(append(payload, '\n'))
		if err == nil && n != len(payload)+1 {
			err = io.ErrShortWrite
		}
		sent <- err
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case err := <-sent:
		if err != nil {
			c.Close()
			return 0, err
		}
	case <-timer.C:
		c.Close()
		return 0, context.DeadlineExceeded
	case <-c.done:
		return 0, c.failure()
	}
	select {
	case m := <-c.acks:
		if m.ID != c.id {
			c.Close()
			return 0, errors.New("通信组件写入序号不匹配")
		}
		return len(b), nil
	case <-timer.C:
		c.Close()
		return 0, context.DeadlineExceeded
	case <-c.done:
		return 0, c.failure()
	}
}
func (c *bridgeConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.input.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
	})
	return nil
}
