//go:build linux

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// A shared per-user lock prevents competing readers from consuming ACKs.
func LockDevice() (func(), error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "sharkcool")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "device.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("已有 SharkCool 进程正在使用设备；请使用已有控制界面，或先退出该进程再运行命令行")
	}
	return func() { _ = f.Close() }, nil
}
