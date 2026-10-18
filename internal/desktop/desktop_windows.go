//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func nativeWindowName() string { return "sharkcool-window.exe" }
func prepareUICommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func OpenBrowser(url string) error {
	return startUI("rundll32.exe", "url.dll,FileProtocolHandler", url)
}

// A per-user Startup shortcut avoids administrator privileges or machine-wide services.
func autostartPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "SharkCool.lnk"), nil
}
func AutostartEnabled() bool {
	p, err := autostartPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
func SetAutostart(on bool) error {
	p, err := autostartPath()
	if err != nil {
		return err
	}
	if !on {
		err = os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:SHARKCOOL_SHORTCUT);$s.TargetPath=$env:SHARKCOOL_EXECUTABLE;$s.Arguments='gui --tray';$s.WorkingDirectory=[IO.Path]::GetDirectoryName($env:SHARKCOOL_EXECUTABLE);$s.IconLocation=[IO.Path]::Combine([IO.Path]::GetDirectoryName($env:SHARKCOOL_EXECUTABLE),'SharkCool.exe')+',0';$s.Save()`)
	cmd.Env = append(os.Environ(), "SHARKCOOL_SHORTCUT="+p, "SHARKCOOL_EXECUTABLE="+exe)
	prepareUICommand(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建启动快捷方式: %w: %s", err, out)
	}
	return nil
}
func LockDevice() (func(), error) {
	name, _ := syscall.UTF16PtrFromString("Local\\SharkCool-BRB02-device")
	kernel := syscall.NewLazyDLL("kernel32.dll")
	h, _, err := kernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, err
	}
	closeHandle := kernel.NewProc("CloseHandle")
	if err == syscall.Errno(183) {
		closeHandle.Call(h)
		return nil, fmt.Errorf("已有 SharkCool 进程正在使用设备；请使用已有控制界面")
	}
	return func() { closeHandle.Call(h) }, nil
}
