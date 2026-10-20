//go:build windows

package sensors

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestHardwareServiceResponse(t *testing.T) {
	for _, tc := range []struct {
		data           string
		cpu, gpu, load int64
	}{
		{`{"cpu":56,"gpu":61,"gpuUsage":0,"error":null}`, 56, 61, 0},
		{`{"cpu":null,"gpu":64,"gpuUsage":42,"error":"CPU unavailable"}`, -1, 64, 42},
		{`{"cpu":56,"gpu":null,"error":"GPU unavailable"}`, 56, -1, -1},
		{`{"cpu":150,"gpu":150,"gpuUsage":100}`, 150, 150, 100},
		{`{"cpu":0,"gpu":151,"gpuUsage":101}`, -1, -1, -1},
		{`{"cpu":-1,"gpu":0}`, -1, -1, -1},
		{`{"cpu":55.5}`, -1, -1, -1},
		{`{}`, -1, -1, -1}, {`not JSON`, -1, -1, -1},
		{`{"cpu":56}{"cpu":57}`, -1, -1, -1},
		{`{"cpu":56}` + strings.Repeat(" ", 1024), -1, -1, -1},
	} {
		got := parseHardware([]byte(tc.data))
		value := func(p *int64) int64 {
			if p == nil {
				return -1
			}
			return *p
		}
		load := int64(-1)
		if got.GPUUsage != nil {
			load = int64(*got.GPUUsage)
		}
		if value(got.CPU) != tc.cpu || value(got.GPU) != tc.gpu || load != tc.load {
			t.Fatalf("%s: got %+v", tc.data, got)
		}
	}
}

func TestSensorPipeTrustTimeoutAndFragmentation(t *testing.T) {
	for _, kind := range []string{"valid", "spoofed", "timeout", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			name := `\\.\pipe\SharkCool-test-` + kind + "-" + strings.ReplaceAll(t.Name(), "/", "-")
			path, _ := syscall.UTF16PtrFromString(name)
			raw, _, err := kernel.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(path)), 2|syscall.FILE_FLAG_OVERLAPPED, 8, 1, 2048, 0, 0, 0)
			handle := syscall.Handle(raw)
			if handle == syscall.InvalidHandle {
				t.Fatal(err)
			}
			defer syscall.CloseHandle(handle)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				var connect syscall.Overlapped
				ok, _, err := kernel.NewProc("ConnectNamedPipe").Call(raw, uintptr(unsafe.Pointer(&connect)))
				if ok == 0 && err == syscall.ERROR_IO_PENDING {
					stop := context.AfterFunc(ctx, func() { syscall.CancelIoEx(handle, &connect) })
					var count uint32
					ok, _, _ = kernel.NewProc("GetOverlappedResult").Call(raw, uintptr(unsafe.Pointer(&connect)), uintptr(unsafe.Pointer(&count)), 1)
					stop()
					if ok == 0 {
						return
					}
				} else if ok == 0 && err != syscall.Errno(535) {
					return
				}
				if kind == "timeout" {
					<-ctx.Done()
					return
				}
				parts := []string{`{"cpu":56,`, "\"gpu\":61}\n"}
				if kind == "oversized" {
					parts = []string{strings.Repeat("x", 1025)}
				}
				for _, part := range parts {
					var count uint32
					var write syscall.Overlapped
					payload := []byte(part)
					err := syscall.WriteFile(handle, payload, &count, &write)
					if err == syscall.ERROR_IO_PENDING {
						stop := context.AfterFunc(ctx, func() { syscall.CancelIoEx(handle, &write) })
						ok, _, _ := kernel.NewProc("GetOverlappedResult").Call(raw, uintptr(unsafe.Pointer(&write)), uintptr(unsafe.Pointer(&count)), 1)
						stop()
						if ok == 0 {
							return
						}
					} else if err != nil {
						return
					}
					runtime.KeepAlive(payload)
					time.Sleep(20 * time.Millisecond)
				}
			}()
			pid := uint32(os.Getpid())
			if kind == "spoofed" {
				pid++
			}
			timeout := 2 * time.Second
			if kind == "timeout" {
				timeout = 100 * time.Millisecond
			}
			readCtx, stop := context.WithTimeout(ctx, timeout)
			defer stop()
			data, err := readSensorPipe(readCtx, name, pid)
			if kind == "valid" {
				if err != nil || !json.Valid(data) || parseHardware(data).CPU == nil {
					t.Fatalf("%q: %v", data, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s pipe", kind)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("pipe fixture did not shut down")
			}
		})
	}
}

func TestInstalledSensorService(t *testing.T) {
	if os.Getenv("SHARKCOOL_TEST_SENSORS") != "1" {
		t.Skip("requires the installed Windows sensor service")
	}
	pid, err := sensorServicePID()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	data, err := readSensorPipe(ctx, `\\.\pipe\SharkCool-CPU-Temperature`, pid)
	if err != nil {
		t.Fatal(err)
	}
	var sample map[string]json.RawMessage
	if json.Unmarshal(data, &sample) != nil || sample["gpu"] == nil || sample["gpuUsage"] == nil {
		t.Fatalf("incomplete hardware sample: %s", data)
	}
}
