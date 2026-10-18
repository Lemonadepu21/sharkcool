//go:build linux

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart"), nil
}

func AutostartEnabled() bool {
	dir, err := configDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "sharkcool.desktop"))
	return err == nil
}

// Desktop Exec escaping has two stages: value escapes, then argument quotes.
func execQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(s)
	s = "\"" + s + "\""
	return strings.ReplaceAll(s, "\\", "\\\\")
}

func SetAutostart(on bool) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "sharkcool.desktop")
	if !on {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, "\n\r=") {
		return fmt.Errorf("桌面启动器不支持路径中的换行或等号")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	contents := "[Desktop Entry]\nType=Application\nName=SharkCool\nIcon=sharkcool\nComment=黑鲨风神Pro 控制器\nExec=" + execQuote(exe) + " gui --tray\nTerminal=false\nCategories=Utility;\n"
	f, err := os.CreateTemp(dir, ".sharkcool-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(contents); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func OpenBrowser(url string) error {
	return startUI("xdg-open", url)
}

func nativeWindowName() string { return "sharkcool-window" }

func prepareUICommand(cmd *exec.Cmd) {}
