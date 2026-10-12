//go:build linux

package scenes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessExecutableReader(t *testing.T) {
	root := t.TempDir()
	for pid, target := range map[string]string{"123": "/opt/game (deleted)", "456": "/usr/bin/tool", "789": "relative", "self": "/fake/ignore"} {
		if err := os.Mkdir(filepath.Join(root, pid), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, pid, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "999"), 0700); err != nil {
		t.Fatal(err)
	}
	found, err := readProcesses(context.Background(), root, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 4 || !found["/opt/game"] || !found["game"] || !found["tool"] || !found["/usr/bin/tool"] {
		t.Fatal(found)
	}
	found, err = readProcesses(context.Background(), root, uint32(os.Getuid()+1))
	if err != nil || len(found) != 0 {
		t.Fatal(found, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = readProcesses(ctx, root, uint32(os.Getuid())); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err = readProcesses(context.Background(), filepath.Join(root, "missing"), 0); err == nil {
		t.Fatal("missing proc accepted")
	}
}
