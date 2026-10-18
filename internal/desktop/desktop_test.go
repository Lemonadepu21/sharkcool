//go:build linux

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeWindowNeverFallsBackToBrowser(t *testing.T) {
	for _, available := range []bool{true, false} {
		for _, failed := range []bool{true, false} {
			t.Run(fmt.Sprintf("available=%t/failed=%t", available, failed), func(t *testing.T) {
				var calls [][]string
				lookup := func(name string) (string, error) {
					if available && name == "/test/sharkcool-window" {
						return name, nil
					}
					return "", os.ErrNotExist
				}
				start := func(name string, args ...string) error {
					calls = append(calls, append([]string{name}, args...))
					if failed {
						return os.ErrPermission
					}
					return nil
				}
				err := openWindow("http://127.0.0.1:17865", "/test", lookup, start)
				if (err == nil) != (available && !failed) {
					t.Fatalf("unexpected error: %v", err)
				}
				if available && !reflect.DeepEqual(calls, [][]string{{"/test/sharkcool-window", "--url", "http://127.0.0.1:17865"}}) {
					t.Fatalf("calls: %v", calls)
				}
				if !available && len(calls) != 0 {
					t.Fatalf("browser fallback attempted: %v", calls)
				}
			})
		}
	}
}

func TestWindowStartupFailureIsReported(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := startUI("/bin/sh", "-c", "exit 17"); err == nil || !strings.Contains(err.Error(), "exit status 17") {
		t.Fatalf("early startup failure hidden: %v", err)
	}
	if err := startUI("/bin/sh", "-c", "exit 0"); err != nil {
		t.Fatal(err)
	}
}

func TestAutostartAndExclusiveLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	if AutostartEnabled() {
		t.Fatal("unexpected autostart")
	}
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("autostart not created")
	}
	b, err := os.ReadFile(filepath.Join(root, "config/autostart/sharkcool.desktop"))
	if err != nil || !strings.Contains(string(b), "gui --tray") {
		t.Fatalf("%s %v", b, err)
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	if AutostartEnabled() {
		t.Fatal("autostart wasn't removed")
	}
	unlock, err := LockDevice()
	if err != nil {
		t.Fatal(err)
	}
	if unlock2, err := LockDevice(); err == nil {
		unlock2()
		t.Fatal("competing reader allowed")
	}
	unlock()
	unlock, err = LockDevice()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
