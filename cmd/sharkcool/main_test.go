package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCLICommands(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		want  string
		fails bool
	}{
		{[]string{"version"}, "SharkCool", false},
		{[]string{"help"}, "sharkcool mode 2", false},
		{[]string{"status", "--demo"}, `"demo":true`, false},
		{[]string{"mode", "--demo", "3"}, `"acknowledged":true`, false},
		{[]string{"speed", "--demo", "1800"}, `"acknowledged":true`, false},
		{[]string{"speed", "--demo", "100"}, "", true},
		{[]string{"rgb", "--demo", "--color", "#112233"}, "", true},
		{[]string{"rgb", "--brightness", "101"}, "", true},
		{[]string{"rgb", "--effect", "audio"}, "", true},
		{[]string{"status", "--transport", "invalid", "--demo"}, "", true},
		{[]string{"mode", "--demo", "1"}, "", true},
		{[]string{"gui", "--listen", "0.0.0.0:17865"}, "", true},
		{[]string{"status", "--timeout", "0s"}, "", true},
		{[]string{"cooling", "--slot", "0", "--rpm", "1800"}, "", true},
		{[]string{"cooling", "--slot", "1", "--rpm", "0"}, "", true},
		{[]string{"cooling", "--slot", "1", "--points", "20:1200,20:1800,60:2600,80:3600"}, "", true},
		{[]string{"cooling", "--slot", "256"}, "", true},
		{[]string{"source", "bad"}, "", true},
		{[]string{"source", "--demo", "gpu"}, "", true},
		{[]string{"device-options", "on"}, "", true},
		{[]string{"device-options", "--demo", "--smart-start-stop=false"}, "", true},
		{[]string{"device-options", "--smart-start-stop=bad"}, "", true},
		{[]string{"audio", "--demo"}, "", true},
		{[]string{"audio", "--duration=-1s"}, "", true},
		{[]string{"audio", "unexpected"}, "", true},
		{[]string{"reactive", "--demo"}, "", true},
		{[]string{"reactive", "--duration=-1s"}, "", true},
		{[]string{"reactive", "unexpected"}, "", true},
		{[]string{"input-monitor", "--demo"}, "", true},
		{[]string{"input-monitor", "--duration=-1s"}, "", true},
		{[]string{"rgb-preset", "--effect=bad"}, "", true},
		{[]string{"rgb-preset", "--effect=flow", "--palette=16"}, "", true},
		{[]string{"rgb-preset", "--demo", "--effect=audio"}, "", true},
		{[]string{"screen-info", "--demo"}, "", true},
		{[]string{"screen-info", "unexpected"}, "", true},
		{[]string{"screen-backup"}, "", true},
		{[]string{"scenes"}, "", true},
		{[]string{"scenes", "--config=missing.json", "--poll=100ms"}, "", true},
		{[]string{"scenes", "--config=missing.json", "--demo"}, "", true},
		{[]string{"scene-monitor", "--config=missing.json", "--duration=-1s"}, "", true},
		{[]string{"scene-monitor", "--config=missing.json", "extra"}, "", true},
		{[]string{"screen-backup", "--output=main_test.go"}, "", true},
		{[]string{"unknown"}, "", true},
	} {
		var out bytes.Buffer
		err := run(context.Background(), tc.args, &out)
		if (err != nil) != tc.fails || !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v: %s %v", tc.args, out.String(), err)
		}
	}
}
