//go:build linux

package scenes

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ReadProcesses observes only executable links for readable processes owned by
// this user. It never reads argv, environment, memory or input-device contents.
func ReadProcesses(ctx context.Context) (map[string]bool, error) {
	return readProcesses(ctx, "/proc", uint32(os.Getuid()))
}
func readProcesses(ctx context.Context, root string, uid uint32) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uid {
			continue
		}
		executable, err := os.Readlink(filepath.Join(root, entry.Name(), "exe"))
		if err != nil {
			continue
		}
		executable = strings.TrimSuffix(executable, " (deleted)")
		if !filepath.IsAbs(executable) {
			continue
		}
		found[executable] = true
		found[filepath.Base(executable)] = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return found, nil
}

const ProcessSource = "linux-proc-executable"
