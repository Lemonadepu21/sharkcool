//go:build windows

package scenes

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"
)

const ProcessSource = "windows-toolhelp-executable"

func ReadProcesses(ctx context.Context) (map[string]bool, error) {
	k := syscall.NewLazyDLL("kernel32.dll")
	h, _, err := k.NewProc("CreateToolhelp32Snapshot").Call(2, 0)
	if h == ^uintptr(0) {
		return nil, err
	}
	defer k.NewProc("CloseHandle").Call(h)
	type entry struct {
		Size, Usage, PID        uint32
		Heap                    uintptr
		Module, Threads, Parent uint32
		Priority                int32
		Flags                   uint32
		Exe                     [260]uint16
	}
	e := entry{}
	e.Size = uint32(unsafe.Sizeof(e))
	first := k.NewProc("Process32FirstW")
	next := k.NewProc("Process32NextW")
	found := map[string]bool{}
	ok, _, err := first.Call(h, uintptr(unsafe.Pointer(&e)))
	if ok == 0 && err != syscall.Errno(18) {
		return nil, fmt.Errorf("枚举进程: %w", err)
	}
	for ok != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := syscall.UTF16ToString(e.Exe[:])
		if name != "" {
			found[name] = true
		}
		ok, _, err = next.Call(h, uintptr(unsafe.Pointer(&e)))
	}
	if err != syscall.Errno(18) {
		return nil, err
	}
	return found, nil
}
