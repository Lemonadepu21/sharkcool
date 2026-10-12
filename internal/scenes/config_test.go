package scenes

import (
	"strings"
	"testing"
)

func TestStrictConfigAndPriority(t *testing.T) {
	valid := `{"version":1,"profiles":[{"name":"A","programs":["game","/opt/game"],"lighting":{"effect":"static","color":"#204060"}},{"name":"B","programs":["second"],"cooling":{"source":0,"slot":2,"rpm":1800}}]}`
	c, err := Load(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if c.Match(map[string]bool{"second": true, "/opt/game": true}) != 0 || c.Match(map[string]bool{"second": true}) != 1 || c.Match(nil) != -1 {
		t.Fatal("match priority")
	}
	for _, bad := range []string{valid + ` {}`, strings.Replace(valid, `"version":1`, `"version":2`, 1), strings.Replace(valid, `"game"`, `"./game"`, 1), strings.Replace(valid, `"game"`, `"/opt/../game"`, 1), strings.Replace(valid, `"effect":"static"`, `"effect":"audio"`, 1), strings.Replace(valid, `"name":"B"`, `"name":"A"`, 1), strings.Replace(valid, `"source":0`, `"source":"cpu"`, 1), strings.Replace(valid, `"rpm":1800`, `"rpm":1`, 1), strings.Replace(valid, `"version":1`, `"unknown":1,"version":1`, 1), strings.Repeat(" ", 65537)} {
		if _, err := Load(strings.NewReader(bad)); err == nil {
			t.Fatalf("accepted invalid config: %.120s", bad)
		}
	}
}
