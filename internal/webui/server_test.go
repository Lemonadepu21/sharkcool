package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	cooler "sharkcool-linux/brb02"
	"sharkcool-linux/internal/sensors"
)

func TestSystemInfoHTTPPolicy(t *testing.T) {
	c := cooler.New("", false)
	p := sensors.NewPublisher(c, func(context.Context) cooler.SystemInfo { return nil })
	h, err := NewHandlerWithSystemInfo(c, "127.0.0.1:17865", p, func() map[string]int64 { return nil })
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:17865/api/system-info", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-SharkCool-Token", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:17865/", nil))
	token := regexp.MustCompile(`name="sharkcool-token" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())[1]
	if w := request("GET", "", ""); w.Code != 200 || p.Snapshot().Enabled {
		t.Fatal(w.Code)
	}
	if w := request("POST", "", `{"enabled":true}`); w.Code != 403 || p.Snapshot().Enabled {
		t.Fatal("unauthorized start")
	}
	if w := request("POST", token, `{}`); w.Code != 400 || p.Snapshot().Enabled {
		t.Fatal("missing value enabled push")
	}
	if w := request("POST", token, `{"enabled":true}`); w.Code != 200 || !p.Snapshot().Enabled {
		t.Fatal(w.Code)
	}
	if w := request("GET", "", `{"enabled":false}`); w.Code != 200 || !p.Snapshot().Enabled {
		t.Fatal("GET altered push policy")
	}
	if w := request("POST", token, `{"enabled":false}`); w.Code != 200 || p.Snapshot().Enabled {
		t.Fatal(w.Code)
	}
}

func TestDemoHTTPFlowAndLocalAccessBoundary(t *testing.T) {
	c := cooler.New("", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	h, err := NewHandler(c, "127.0.0.1:17865")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, host, origin, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("X-SharkCool-Token", token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	page := request("GET", "/", "127.0.0.1:17865", "", "", "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "黑鲨风神Pro") {
		t.Fatal(page.Code)
	}
	token := regexp.MustCompile(`name="sharkcool-token" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())
	if len(token) != 2 {
		t.Fatal("session token missing")
	}
	for _, tc := range []struct {
		method, path, host, origin, token, body string
		want                                    int
	}{
		{"POST", "/api/speed", "127.0.0.1:17865", "", token[1], `{"rpm":1800}`, 200},
		{"POST", "/api/speed", "127.0.0.1:17865", "", "", `{"rpm":1800}`, 403},
		{"POST", "/api/cooling", "127.0.0.1:17865", "", "", `{"source":0,"slot":1,"rpm":1800}`, 403},
		{"POST", "/api/source", "127.0.0.1:17865", "", "", `{"source":"gpu"}`, 403},
		{"POST", "/api/device-options", "127.0.0.1:17865", "", "", `{"smartStartStop":true}`, 403},
		{"POST", "/api/device-options", "127.0.0.1:17865", "https://evil.example", token[1], `{"smartStartStop":true}`, 403},
		{"POST", "/api/device-options", "127.0.0.1:17865", "", token[1], `{}`, 400},
		{"POST", "/api/device-options", "127.0.0.1:17865", "", token[1], `{"smartStartStop":false}`, 409},
		{"GET", "/api/device-options", "127.0.0.1:17865", "", "", "", 405},
		{"POST", "/api/cooling", "127.0.0.1:17865", "https://evil.example", token[1], `{"source":0,"slot":1,"rpm":1800}`, 403},
		{"POST", "/api/source", "127.0.0.1:17865", "", token[1], `{"source":"bad"}`, 400},
		{"POST", "/api/cooling", "127.0.0.1:17865", "", token[1], `{"source":0,"slot":1,"rpm":1800}`, 409},
		{"GET", "/api/cooling", "127.0.0.1:17865", "", "", "", 405},
		{"POST", "/api/query", "127.0.0.1:17865", "", token[1], `{"kind":"cooling-preset","slot":256}`, 400},
		{"POST", "/api/rgb", "127.0.0.1:17865", "", "", `{"color":"#112233"}`, 403},
		{"POST", "/api/lcd-layout", "127.0.0.1:17865", "", "", `{"rows":["cpu-temperature","fan-rpm","time"]}`, 403},
		{"GET", "/api/lcd-layout", "127.0.0.1:17865", "", "", "", 405},
		{"POST", "/api/system-info", "127.0.0.1:17865", "", "", `{"enabled":true}`, 403},
		{"POST", "/api/system-info", "127.0.0.1:17865", "https://evil.example", token[1], `{"enabled":true}`, 403},
		{"POST", "/api/rgb", "127.0.0.1:17865", "", token[1], `{"color":"#112233"}`, 409},
		{"POST", "/api/query", "127.0.0.1:17865", "", "", `{"kind":"rgb"}`, 403},
		{"POST", "/api/query", "127.0.0.1:17865", "", token[1], `{"kind":"rgb"}`, 409},
		{"GET", "/api/query", "127.0.0.1:17865", "", "", "", 405},
		{"POST", "/api/switch", "127.0.0.1:17865", "", "", `{"kind":"rgb","on":true}`, 403},
		{"POST", "/api/switch", "127.0.0.1:17865", "https://evil.example", token[1], `{"kind":"rgb","on":true}`, 403},
		{"POST", "/api/speed", "127.0.0.1:17865", "", token[1], `{"rpm":999999}`, 409},
		{"POST", "/api/mode", "127.0.0.1:17865", "", token[1], `{"mode":3}`, 200},
		{"POST", "/api/mode", "127.0.0.1:17865", "", "", `{"mode":3}`, 403},
		{"POST", "/api/mode", "127.0.0.1:17865", "https://evil.example", token[1], `{"mode":3}`, 403},
		{"GET", "/api/status", "evil.example", "", "", "", 403},
		{"GET", "/api/mode", "127.0.0.1:17865", "", "", "", 405},
		{"POST", "/api/mode", "127.0.0.1:17865", "", token[1], `{"mode":1}`, 409},
		{"POST", "/api/mode", "127.0.0.1:17865", "", token[1], `{"mode":2,"other":true}`, 400},
		{"POST", "/api/mode", "127.0.0.1:17865", "", token[1], `{"mode":2}{"mode":3}`, 400},
		{"POST", "/api/autostart", "127.0.0.1:17865", "", token[1], `{"enabled":true}`, 409},
		{"GET", "/missing", "127.0.0.1:17865", "", "", "", 404},
	} {
		w := request(tc.method, tc.path, tc.host, tc.origin, tc.token, tc.body)
		if w.Code != tc.want {
			t.Errorf("%s %s: %d want %d: %s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	for _, path := range []string{"/app.js", "/style.css", "/api/status", "/api/settings"} {
		w := request(http.MethodGet, path, "127.0.0.1:17865", "", "", "")
		if w.Code != 200 {
			t.Errorf("asset %s: %d", path, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	limit := time.After(time.Second)
	for {
		w := request("GET", "/api/status", "127.0.0.1:17865", "", "", "")
		var s cooler.Status
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if s.Connected && s.Demo && s.AcknowledgedMode == 3 {
			break
		}
		select {
		case <-limit:
			t.Fatal("demo status not updated")
		case <-time.After(time.Millisecond):
		}
	}
}
