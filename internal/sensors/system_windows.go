//go:build windows

package sensors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	sample := readHardware(ctx)
	if validTemperature(sample.CPU) {
		result["cpu"] = *sample.CPU
	}
	if validTemperature(sample.GPU) {
		result["gpu"] = *sample.GPU
	} else if v, ok := nvidiaValue(ctx, "temperature.gpu"); ok && v > 0 {
		result["gpu"] = int64(v)
	}
	return result
}

type hardwareSample struct {
	CPU      *int64  `json:"cpu"`
	GPU      *int64  `json:"gpu"`
	GPUUsage *uint16 `json:"gpuUsage"`
}

func validTemperature(v *int64) bool { return v != nil && *v > 0 && *v <= 150 }

func readHardware(ctx context.Context) hardwareSample {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	pid, err := sensorServicePID()
	if err != nil {
		return hardwareSample{}
	}
	data, err := readSensorPipe(ctx, `\\.\pipe\SharkCool-CPU-Temperature`, pid)
	if err != nil {
		return hardwareSample{}
	}
	return parseHardware(data)
}

func parseHardware(data []byte) hardwareSample {
	var sample hardwareSample
	if len(data) > 1024 || json.Unmarshal(data, &sample) != nil {
		return hardwareSample{}
	}
	if !validTemperature(sample.CPU) {
		sample.CPU = nil
	}
	if !validTemperature(sample.GPU) {
		sample.GPU = nil
	}
	if sample.GPUUsage != nil && *sample.GPUUsage > 100 {
		sample.GPUUsage = nil
	}
	return sample
}

func sensorServicePID() (uint32, error) {
	api := syscall.NewLazyDLL("advapi32.dll")
	manager, _, err := api.NewProc("OpenSCManagerW").Call(0, 0, 1)
	if manager == 0 {
		return 0, err
	}
	defer api.NewProc("CloseServiceHandle").Call(manager)
	name, _ := syscall.UTF16PtrFromString("SharkCoolSensors")
	service, _, err := api.NewProc("OpenServiceW").Call(manager, uintptr(unsafe.Pointer(name)), 4)
	if service == 0 {
		return 0, err
	}
	defer api.NewProc("CloseServiceHandle").Call(service)
	var status [9]uint32
	var needed uint32
	ok, _, err := api.NewProc("QueryServiceStatusEx").Call(service, 0, uintptr(unsafe.Pointer(&status[0])), unsafe.Sizeof(status), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return 0, err
	}
	if status[1] != 4 || status[7] == 0 {
		return 0, fmt.Errorf("temperature service is not running")
	}
	return status[7], nil
}

// Read the service directly, avoiding a .NET subprocess on every sample.
func readSensorPipe(ctx context.Context, name string, expectedPID uint32) ([]byte, error) {
	path, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var handle syscall.Handle
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Identification-only access also prevents a rogue pipe from impersonating the client.
		handle, err = syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED|0x110000, 0)
		if err != syscall.Errno(231) {
			break
		} // ERROR_PIPE_BUSY
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(handle)
	var pid uint32
	ok, _, err := kernel.NewProc("GetNamedPipeServerProcessId").Call(uintptr(handle), uintptr(unsafe.Pointer(&pid)))
	if ok == 0 {
		return nil, err
	}
	if pid != expectedPID || expectedPID == 0 {
		return nil, fmt.Errorf("untrusted temperature pipe")
	}
	data := make([]byte, 1025)
	used := 0
	for used < len(data) {
		var count uint32
		var overlapped syscall.Overlapped
		err := syscall.ReadFile(handle, data[used:], &count, &overlapped)
		if err == syscall.ERROR_IO_PENDING {
			stopped := make(chan struct{})
			cancelIO := context.AfterFunc(ctx, func() { syscall.CancelIoEx(handle, &overlapped); close(stopped) })
			ok, _, readErr := kernel.NewProc("GetOverlappedResult").Call(uintptr(handle), uintptr(unsafe.Pointer(&overlapped)), uintptr(unsafe.Pointer(&count)), 1)
			if !cancelIO() {
				<-stopped
			}
			if ok == 0 {
				err = readErr
			} else {
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, fmt.Errorf("empty temperature response")
		}
		used += int(count)
		if bytes.IndexByte(data[:used], '\n') >= 0 {
			return data[:used], nil
		}
	}
	return nil, fmt.Errorf("temperature response exceeds 1024 bytes")
}
func NewSystemInfoReader(temperatures func() map[string]int64) func(context.Context) brb02.SystemInfo {
	var prevTotal, prevIdle uint64
	var have bool
	return func(ctx context.Context) brb02.SystemInfo {
		info := brb02.SystemInfo{}
		temps := temperatures()
		for name, metric := range map[string]brb02.Metric{"cpu": brb02.CPUTemperature, "gpu": brb02.GPUTemperature} {
			if v, ok := temps[name]; ok && v >= 0 && v <= 150 {
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
		if sample := readHardware(ctx); sample.GPUUsage != nil {
			info[brb02.GPUUsage] = *sample.GPUUsage
		} else if v, ok := nvidiaValue(ctx, "utilization.gpu"); ok && v <= 100 {
			info[brb02.GPUUsage] = v
		}
		now := time.Now()
		info[brb02.TimeOfDay] = uint16(now.Hour()*60 + now.Minute())
		return info
	}
}
