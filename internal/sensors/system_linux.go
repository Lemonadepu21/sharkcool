//go:build linux

package sensors

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sharkcool-linux/brb02"
)

// NewSystemInfoReader returns a sequential sampler. CPU usage needs two
// samples; it is omitted on the first call and when counters reset.
func NewSystemInfoReader(temperatures func() map[string]int64) func(context.Context) brb02.SystemInfo {
	var previous cpuCounters
	var havePrevious bool
	return func(ctx context.Context) brb02.SystemInfo {
		info := brb02.SystemInfo{}
		temps := temperatures()
		for key, metric := range map[string]brb02.Metric{"cpu": brb02.CPUTemperature, "gpu": brb02.GPUTemperature} {
			if value, ok := temps[key]; ok && value >= 0 && value <= 150 {
				info[metric] = uint16(value)
			}
		}
		if b, err := os.ReadFile("/proc/stat"); err == nil {
			if current, ok := parseCPUCounters(string(b)); ok {
				if havePrevious {
					if value, ok := cpuPercent(previous, current); ok {
						info[brb02.CPUUsage] = value
					}
				}
				previous, havePrevious = current, true
			}
		}
		if b, err := os.ReadFile("/proc/meminfo"); err == nil {
			if value, ok := memoryPercent(string(b)); ok {
				info[brb02.MemoryUsage] = value
			}
		}
		var fs syscall.Statfs_t
		if syscall.Statfs("/", &fs) == nil && fs.Blocks > 0 && fs.Bfree <= fs.Blocks {
			info[brb02.DiskUsage] = uint16((fs.Blocks - fs.Bfree) * 100 / fs.Blocks)
		}
		if value, ok := gpuPercent("/sys/class/drm"); ok {
			info[brb02.GPUUsage] = value
		} else if ctx.Err() == nil {
			queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			out, err := exec.CommandContext(queryCtx, "nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits").Output()
			cancel()
			if err == nil {
				if value, ok := parseGPUPercent(string(out)); ok {
					info[brb02.GPUUsage] = value
				}
			}
		}
		if ctx.Err() == nil {
			now := time.Now()
			info[brb02.TimeOfDay] = uint16(now.Hour()*60 + now.Minute())
		}
		return info
	}
}

type cpuCounters struct{ total, idle uint64 }

func parseCPUCounters(text string) (cpuCounters, bool) {
	fields := strings.Fields(strings.SplitN(text, "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, false
	}
	var counters cpuCounters
	// user/nice already include guest/guest_nice; count only the first eight.
	for i := 1; i < len(fields) && i <= 8; i++ {
		value, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuCounters{}, false
		}
		counters.total += value
		if i == 4 || i == 5 { // idle + iowait
			counters.idle += value
		}
	}
	return counters, true
}

func cpuPercent(previous, current cpuCounters) (uint16, bool) {
	if current.total <= previous.total || current.idle < previous.idle {
		return 0, false
	}
	total, idle := current.total-previous.total, current.idle-previous.idle
	if idle > total {
		return 0, false
	}
	return uint16((total - idle) * 100 / total), true
}

func memoryPercent(text string) (uint16, bool) {
	values := map[string]uint64{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && (fields[0] == "MemTotal:" || fields[0] == "MemAvailable:") {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, false
			}
			values[fields[0]] = value
		}
	}
	total, haveTotal := values["MemTotal:"]
	available, haveAvailable := values["MemAvailable:"]
	if !haveTotal || !haveAvailable || total == 0 || available > total {
		return 0, false
	}
	return uint16((total - available) * 100 / total), true
}

func gpuPercent(root string) (uint16, bool) {
	paths, _ := filepath.Glob(filepath.Join(root, "card[0-9]*", "device", "gpu_busy_percent"))
	var highest uint16
	found := false
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 16)
		if err == nil && value <= 100 {
			found = true
			if uint16(value) > highest {
				highest = uint16(value)
			}
		}
	}
	return highest, found
}

func parseGPUPercent(text string) (uint16, bool) {
	var highest uint16
	found := false
	for _, line := range strings.Split(text, "\n") {
		value, err := strconv.ParseUint(strings.TrimSpace(line), 10, 16)
		if err == nil && value <= 100 {
			found = true
			if uint16(value) > highest {
				highest = uint16(value)
			}
		}
	}
	return highest, found
}
