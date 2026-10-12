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
