//go:build windows

package desktop

import (
	"os/exec"
	"strings"
	"testing"
)

func TestWindowsAutostartLaunchesTray(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if AutostartEnabled() {
		t.Fatal("unexpected autostart")
	}
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("shortcut not created")
	}
	path, err := autostartPath()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:SHARKCOOL_SHORTCUT);$s.Arguments`)
	cmd.Env = append(cmd.Environ(), "SHARKCOOL_SHORTCUT="+path)
	data, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(data)) != "gui --tray" {
		t.Fatalf("%s %v", data, err)
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	if AutostartEnabled() {
		t.Fatal("shortcut remains")
	}
}
