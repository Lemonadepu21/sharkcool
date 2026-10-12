//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func guiLaunch() bool {
	return len(os.Args) == 1 || os.Args[1] == "gui" || strings.HasPrefix(os.Args[1], "--")
}
func prepareApplication() func() {
	if !guiLaunch() {
		return func() {}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return func() {}
	}
	dir := filepath.Join(cache, "sharkcool")
	_ = os.MkdirAll(dir, 0700)
	f, err := os.OpenFile(filepath.Join(dir, "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return func() {}
	}
	os.Stdout = f
	os.Stderr = f
	return func() { _ = f.Close() }
}
func showStartupError(err error) {
	if !guiLaunch() {
		return
	}
	text, _ := syscall.UTF16PtrFromString("SharkCool 启动失败：\n" + err.Error())
	title, _ := syscall.UTF16PtrFromString("SharkCool")
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
