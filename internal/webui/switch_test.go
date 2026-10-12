package webui

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	cooler "sharkcool-linux/brb02"
	"strings"
	"testing"
	"time"
)

type switchStateTransport struct {
	packets chan []byte
	value   []byte
}

func (d *switchStateTransport) Read(ctx context.Context, b []byte) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case packet := <-d.packets:
		return copy(b, packet), nil
	}
}
func (d *switchStateTransport) Write(report []byte) (int, error) {
	packet, err := cooler.BuildPacket(report[3], d.value)
	if err != nil {
		return 0, err
	}
	d.packets <- packet
	return len(report), nil
}
func (d *switchStateTransport) Close() error { return nil }

// Use the real controller, protocol decoder and HTTP handler, not a fixture
// that invents the enabled field expected by the native window.
func TestRGBSwitchQueryExposesDeviceState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		enabled bool
		status  int
	}{{"on", []byte{1}, true, 200}, {"off", []byte{0}, false, 200}, {"invalid", []byte{2}, false, 409}, {"missing", nil, false, 409}} {
		t.Run(tc.name, func(t *testing.T) {
			d := &switchStateTransport{packets: make(chan []byte, 8), value: tc.payload}
			c := cooler.NewWithConnector(func(context.Context) (cooler.Transport, string, error) { return d, "fixture", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			deadline := time.Now().Add(time.Second)
			for !c.Snapshot().Connected && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !c.Snapshot().Connected {
				t.Fatal("controller did not connect")
			}
			h, err := NewHandler(c, "127.0.0.1:17865")
			if err != nil {
				t.Fatal(err)
			}
			page := httptest.NewRecorder()
			h.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:17865/", nil))
			token := regexp.MustCompile(`name="sharkcool-token" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())[1]
			req := httptest.NewRequest("POST", "http://127.0.0.1:17865/api/query", strings.NewReader(`{"kind":"rgb-switch"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-SharkCool-Token", token)
			response := httptest.NewRecorder()
			h.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatal(response.Code, response.Body.String())
			}
			var data map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if tc.status == 200 {
				enabled, ok := data["enabled"].(bool)
				if !ok || enabled != tc.enabled || data["command"] != float64(0x11) {
					t.Fatalf("actual protocol state missing: %s", response.Body.String())
				}
			} else if _, exists := data["enabled"]; exists {
				t.Fatal("invalid response produced a switch state")
			}
		})
	}
}
