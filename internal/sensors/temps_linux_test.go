//go:build linux

package sensors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSensorSelectionAndAbsentReadings(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon")
	thermal := filepath.Join(root, "thermal")
	writeSensorFixture(t, filepath.Join(hw, "hwmon0/name"), "coretemp\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon0/temp1_input"), "45000\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon0/temp2_input"), "53000\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon1/name"), "nvme\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon1/temp1_input"), "90000\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon2/name"), "amdgpu\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon2/temp1_input"), "60000\n")
	writeSensorFixture(t, filepath.Join(hw, "hwmon2/temp2_input"), "N/A\n")
	temps := readSysTemps(hw, thermal)
	if temps["cpu"] != 53 || temps["gpu"] != 60 {
		t.Fatalf("%v", temps)
	}
	if temps := readSysTemps(filepath.Join(root, "absent"), thermal); len(temps) != 0 {
		t.Fatalf("absent sensors fabricated: %v", temps)
	}
}

func writeSensorFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPreferPackageDieAndGPUCore(t *testing.T) {
	root := t.TempDir()
	for name, files := range map[string]map[string]string{
		"hwmon0": {"name": "coretemp", "temp1_input": "55000", "temp1_label": "Package id 0", "temp2_input": "65000", "temp2_label": "Core 0"},
		"hwmon1": {"name": "amdgpu", "temp1_input": "61000", "temp1_label": "edge", "temp2_input": "95000", "temp2_label": "junction", "temp3_input": "90000", "temp3_label": "mem"},
		"hwmon2": {"name": "k10temp", "temp1_input": "85000", "temp1_label": "Tctl", "temp2_input": "60000", "temp2_label": "Tdie", "temp3_input": "90000", "temp3_label": "Tccd1"},
	} {
		for file, value := range files {
			writeSensorFixture(t, filepath.Join(root, name, file), value)
		}
	}
	temps := readSysTemps(root, filepath.Join(root, "absent"))
	if temps["cpu"] != 60 || temps["gpu"] != 61 {
		t.Fatalf("wrong sensor selected: %v", temps)
	}
}
