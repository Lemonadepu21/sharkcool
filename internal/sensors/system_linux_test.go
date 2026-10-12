//go:build linux

package sensors

import "testing"

func TestLinuxUsageUnitsAndUnavailableReadings(t *testing.T) {
	before, ok := parseCPUCounters("cpu 100 20 30 400 50 10 20 0 60 10\ncpu0 0")
	if !ok || before.total != 630 || before.idle != 450 {
		t.Fatalf("guest double-counted: %+v", before)
	}
	after, _ := parseCPUCounters("cpu 120 20 40 450 70 10 20 0 60 10")
	if usage, ok := cpuPercent(before, after); !ok || usage != 30 {
		t.Fatalf("CPU: %d %v", usage, ok)
	}
	for _, current := range []cpuCounters{before, {total: 10, idle: 1}, {total: 700, idle: 600}} {
		if _, ok := cpuPercent(before, current); ok {
			t.Fatal("invalid delta accepted")
		}
	}
	if usage, ok := memoryPercent("MemTotal: 1000 kB\nMemAvailable: 420 kB\n"); !ok || usage != 58 {
		t.Fatalf("memory: %d %v", usage, ok)
	}
	for _, text := range []string{"MemTotal: 0 kB", "MemTotal: 1000 kB", "MemTotal: 1000 kB\nMemAvailable: 1100 kB"} {
		if _, ok := memoryPercent(text); ok {
			t.Fatal("missing/invalid reading became fake zero")
		}
	}
	if _, ok := gpuPercent(t.TempDir()); ok {
		t.Fatal("missing GPU became fake zero")
	}
	if value, ok := parseGPUPercent("0\n14\n[N/A]\n"); !ok || value != 14 {
		t.Fatal(value, ok)
	}
	if value, ok := parseGPUPercent("0\n"); !ok || value != 0 {
		t.Fatal("measured zero lost")
	}
	if _, ok := parseGPUPercent("[N/A]\n101\n"); ok {
		t.Fatal("unsupported GPU became zero")
	}
}
