//go:build linux

package sensors

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func ReadTemps(ctx context.Context) map[string]int64 {
	temps := readSysTemps("/sys/class/hwmon", "/sys/class/thermal")
	if _, ok := temps["gpu"]; !ok && ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(queryCtx, "nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if v, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil && v > 0 && v < 150 {
					putMax(temps, "gpu", v)
				}
			}
		}
	}
	return temps
}

func readSysTemps(hwRoot, thermalRoot string) map[string]int64 {
	temps := map[string]int64{}
	entries, _ := os.ReadDir(hwRoot)
	for _, e := range entries {
		dir := filepath.Join(hwRoot, e.Name())
		name, _ := os.ReadFile(filepath.Join(dir, "name"))
		key := ""
		switch strings.TrimSpace(string(name)) {
		case "coretemp", "k10temp", "zenpower", "cpu_thermal":
			key = "cpu"
		case "amdgpu", "nouveau", "i915", "xe":
			key = "gpu"
		}
		if key == "" {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, path := range inputs {
			if v, ok := milliTemp(path); ok {
				putMax(temps, key, v)
			}
		}
	}
	if _, ok := temps["cpu"]; !ok {
		zones, _ := filepath.Glob(filepath.Join(thermalRoot, "thermal_zone*"))
		for _, zone := range zones {
			b, _ := os.ReadFile(filepath.Join(zone, "type"))
			switch strings.ToLower(strings.TrimSpace(string(b))) {
			case "x86_pkg_temp", "cpu-thermal", "cpu_thermal", "soc_thermal":
				if v, ok := milliTemp(filepath.Join(zone, "temp")); ok {
					putMax(temps, "cpu", v)
				}
			}
		}
	}
	return temps
}

func milliTemp(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return v / 1000, err == nil && v > 0 && v < 150000
}

func putMax(m map[string]int64, key string, value int64) {
	if old, ok := m[key]; !ok || value > old {
		m[key] = value
	}
}
