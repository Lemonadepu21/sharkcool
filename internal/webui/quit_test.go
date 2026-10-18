package webui

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	cooler "sharkcool-linux/brb02"
)

func TestQuitRequiresAuthenticatedLocalPost(t *testing.T) {
	quits := 0
	h, err := NewHandlerWithSystemInfo(cooler.New("", true), "127.0.0.1:17865", nil, nil, func() { quits++ })
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:17865/", nil))
	token := regexp.MustCompile(`name="sharkcool-token" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())[1]
	for _, tc := range []struct {
		method, token, origin, body string
		status                      int
	}{
		{"GET", "", "", "", 405}, {"POST", "", "", "{}", 403},
		{"POST", token, "https://example.com", "{}", 403},
		{"POST", token, "", `{"unknown":true}`, 400},
		{"POST", token, "", "{}", 200},
		{"POST", token, "", "{}", 200},
	} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1:17865/api/quit", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-SharkCool-Token", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.method, w.Code, w.Body.String())
		}
	}
	if quits != 1 {
		t.Fatalf("quit called %d times", quits)
	}
}

func TestScreenTransferCannotStartDuringShutdown(t *testing.T) {
	m := &screenManager{quitting: true}
	for _, restore := range []bool{false, true} {
		if err := m.start("draft", restore); err == nil || !strings.Contains(err.Error(), "退出") {
			t.Fatal("screen transfer was not blocked during shutdown", err)
		}
	}
}
