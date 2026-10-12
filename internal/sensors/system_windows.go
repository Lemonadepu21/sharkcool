//go:build windows

package sensors

import (
	"context"
	"os"
	"os/exec"
	"sharkcool-linux/brb02"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

func hiddenCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}
func nvidiaValue(ctx context.Context, query string) (uint16, bool) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	out, err := hiddenCommand(ctx, "nvidia-smi.exe", "--query-gpu="+query, "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	var value uint16
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		v, err := strconv.ParseUint(strings.TrimSpace(line), 10, 16)
		if err == nil && v <= 150 {
			if uint16(v) > value {
				value = uint16(v)
			}
			found = true
		}
	}
	return value, found
}
func ReadTemps(ctx context.Context) map[string]int64 {
	result := map[string]int64{}
	if v, ok := nvidiaValue(ctx, "temperature.gpu"); ok && v > 0 {
		result["gpu"] = int64(v)
	}
	return result
}
func NewSystemInfoReader(temperatures func() map[string]int64) func(context.Context) brb02.SystemInfo {
	var prevTotal, prevIdle uint64
	var have bool
	return func(ctx context.Context) brb02.SystemInfo {
		info := brb02.SystemInfo{}
		for name, metric := range map[string]brb02.Metric{"cpu": brb02.CPUTemperature, "gpu": brb02.GPUTemperature} {
			if v, ok := temperatures()[name]; ok && v >= 0 && v <= 150 {
				info[metric] = uint16(v)
			}
		}
		var idle, kern, user syscall.Filetime
		r, _, _ := kernel.NewProc("GetSystemTimes").Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user)))
		ft := func(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
		total := ft(kern) + ft(user)
		id := ft(idle)
		if r != 0 {
			if have && total > prevTotal && id >= prevIdle && id-prevIdle <= total-prevTotal {
				info[brb02.CPUUsage] = uint16((total - prevTotal - id + prevIdle) * 100 / (total - prevTotal))
			}
			prevTotal, prevIdle, have = total, id, true
		}
		type memory struct {
			Length, Load                                                                                  uint32
			Physical, AvailPhysical, PageFile, AvailPageFile, Virtual, AvailVirtual, AvailExtendedVirtual uint64
		}
		m := memory{}
		m.Length = uint32(unsafe.Sizeof(m))
		r, _, _ = kernel.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&m)))
		if r != 0 && m.Load <= 100 {
			info[brb02.MemoryUsage] = uint16(m.Load)
		}
		root := os.Getenv("SystemDrive") + "\\"
		path, _ := syscall.UTF16PtrFromString(root)
		var available, totalDisk, free uint64
		r, _, _ = kernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&totalDisk)), uintptr(unsafe.Pointer(&free)))
		if r != 0 && totalDisk > 0 && free <= totalDisk {
			info[brb02.DiskUsage] = uint16((totalDisk - free) * 100 / totalDisk)
		}
		if v, ok := nvidiaValue(ctx, "utilization.gpu"); ok && v <= 100 {
			info[brb02.GPUUsage] = v
		}
		now := time.Now()
		info[brb02.TimeOfDay] = uint16(now.Hour()*60 + now.Minute())
		return info
	}
}
