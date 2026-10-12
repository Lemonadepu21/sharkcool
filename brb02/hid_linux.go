//go:build linux

package brb02

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Device struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Readable bool   `json:"readable"`
	Writable bool   `json:"writable"`
}

func Discover() ([]Device, error) { return discover("/sys/class/hidraw", "/dev") }

func discover(sysRoot, devRoot string) ([]Device, error) {
	entries, err := os.ReadDir(sysRoot)
	if err != nil {
		return nil, fmt.Errorf("读取 hidraw 列表: %w", err)
	}
	devices := []Device{}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(sysRoot, entry.Name(), "device", "uevent"))
		if err != nil {
			continue
		}
		var bus, vid, pid uint32
		name := "Black Shark BRB02"
		match := false
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "HID_ID=") {
				n, _ := fmt.Sscanf(line, "HID_ID=%x:%x:%x", &bus, &vid, &pid)
				match = n == 3 && bus == 3 && vid == VID && pid == PID
			}
			if strings.HasPrefix(line, "HID_NAME=") {
				name = strings.TrimPrefix(line, "HID_NAME=")
			}
		}
		if !match {
			continue
		}
		path := filepath.Join(devRoot, entry.Name())
		devices = append(devices, Device{path, name, syscall.Access(path, 4) == nil, syscall.Access(path, 2) == nil})
	}
	return devices, nil
}

type hidConn struct {
	mu sync.Mutex
	fd int
}

func Open(path string) (Transport, string, error) {
	devices, err := Discover()
	if err != nil {
		return nil, "", err
	}
	var lastErr error
	for _, d := range devices {
		if path != "" && d.Path != path {
			continue
		}
		fd, err := syscall.Open(d.Path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err == nil {
			return &hidConn{fd: fd}, d.Path, nil
		}
		if errors.Is(err, syscall.EACCES) {
			lastErr = fmt.Errorf("无权访问 %s；请按 README 安装 70-sharkcool.rules 后重新插拔设备", d.Path)
		} else {
			lastErr = fmt.Errorf("打开 %s: %w", d.Path, err)
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("未发现黑鲨风神Pro BRB02 (USB E2B7:7001)，请检查 USB 数据线和连接；指定设备也必须匹配此 ID")
}

func (c *hidConn) Read(ctx context.Context, b []byte) (int, error) {
	timer := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		// Raw nonblocking syscall: a Go os.File.Read can wait in the runtime poller.
		c.mu.Lock()
		if c.fd < 0 {
			c.mu.Unlock()
			return 0, os.ErrClosed
		}
		n, err := syscall.Read(c.fd, b)
		c.mu.Unlock()
		if err == nil {
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		}
		if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return 0, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *hidConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fd < 0 {
		return 0, os.ErrClosed
	}
	if len(b) != ReportSize+1 || b[0] != 0 {
		return 0, fmt.Errorf("输出报告必须为 65 字节，首字节为 0")
	}
	n, err := syscall.Write(c.fd, b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (c *hidConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fd < 0 {
		return nil
	}
	err := syscall.Close(c.fd)
	c.fd = -1
	return err
}
