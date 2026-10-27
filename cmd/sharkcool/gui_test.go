package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cooler "sharkcool-linux/brb02"
	"sharkcool-linux/internal/buildinfo"
)

func TestGUIRejectsOldBackendInsteadOfReusingIt(t *testing.T) {
	for _, version := range []string{"1.12.1-windows", "1.13.0-windows", ""} {
		t.Run(version, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-SharkCool-App", "sharkcool-linux")
				json.NewEncoder(w).Encode(map[string]any{"demo": true, "version": version})
			}))
			defer server.Close()
			var out bytes.Buffer
			err := gui(context.Background(), &out, cooler.Options{Transport: "auto"}, true, strings.TrimPrefix(server.URL, "http://"), true, false, false)
			if err == nil || !strings.Contains(err.Error(), "后台版本") {
				t.Fatalf("old backend silently reused: output=%q error=%v", out.String(), err)
			}
		})
	}
}

func TestGUIReusesMatchingBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SharkCool-App", "sharkcool-linux")
		json.NewEncoder(w).Encode(map[string]any{"demo": true, "version": buildinfo.Version})
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := gui(context.Background(), &out, cooler.Options{Transport: "auto"}, true, strings.TrimPrefix(server.URL, "http://"), true, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "已运行") {
		t.Fatal(out.String())
	}
}
