//go:build windows

package sensors

import "testing"

func TestCPUServiceResponse(t *testing.T) {
	for _, tc := range []struct {
		data  string
		value int64
		valid bool
	}{
		{`{"cpu":56,"error":null}`, 56, true},
		{`{"cpu":150}`, 150, true},
		{`{"cpu":null,"error":"driver unavailable"}`, 0, false},
		{`{"cpu":56,"error":"read failed"}`, 0, false},
		{`{"cpu":0}`, 0, false}, {`{"cpu":-1}`, 0, false},
		{`{"cpu":151}`, 0, false}, {`{"cpu":55.5}`, 0, false},
		{`{}`, 0, false}, {`not JSON`, 0, false},
		{`{"cpu":56}{"cpu":57}`, 0, false},
	} {
		v, ok := parseCPU([]byte(tc.data))
		if v != tc.value || ok != tc.valid {
			t.Fatalf("%s: got %d/%t", tc.data, v, ok)
		}
	}
}
