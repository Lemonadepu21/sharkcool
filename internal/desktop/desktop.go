package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// OpenWindow launches our native Qt client. Browsers are only opened by the
// explicit --browser command-line option, never as a silent fallback.
func OpenWindow(url string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return openWindow(url, filepath.Dir(executable), exec.LookPath, startUI)
}

func openWindow(url, binaryDir string, lookup func(string) (string, error), start func(string, ...string) error) error {
	path, err := lookup(filepath.Join(binaryDir, nativeWindowName()))
	if err != nil {
		path, err = lookup(nativeWindowName())
	}
	if err != nil {
		return fmt.Errorf("未找到原生应用 sharkcool-window，请安装完整桌面包；网页控制可显式使用 --browser")
	}
	return start(path, "--url", url)
}

func startUI(name string, args ...string) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "sharkcool")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(dir, "window.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	c := exec.Command(name, args...)
	prepareUICommand(c)
	c.Stdout, c.Stderr = log, log
	if err := c.Start(); err != nil {
		_ = log.Close()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait(); _ = log.Close() }()
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("应用窗口启动失败: %w；日志：%s", err, logPath)
		}
		return nil // A second launch may just activate the existing window.
	case <-timer.C:
		return nil
	}
}
