package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	cooler "sharkcool-linux/brb02"
	"sharkcool-linux/internal/buildinfo"
	"sharkcool-linux/internal/desktop"
	"sharkcool-linux/internal/sensors"
)

//go:embed assets/*
var assets embed.FS

func NewHandler(c *cooler.Controller, host string, temperatures ...func() map[string]int64) (http.Handler, error) {
	return newHandler(c, host, nil, temperatures...)
}

func NewHandlerWithSystemInfo(c *cooler.Controller, host string, publisher *sensors.Publisher, temperatures func() map[string]int64) (http.Handler, error) {
	return newHandler(c, host, publisher, temperatures)
}

func newHandler(c *cooler.Controller, host string, publisher *sensors.Publisher, temperatures ...func() map[string]int64) (http.Handler, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	static, _ := fs.Sub(assets, "assets")
	mux := http.NewServeMux()
	screen := newScreenManager(c)
	screen.register(mux)
	session := fmt.Sprint(time.Now().UnixNano())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("assets/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, strings.ReplaceAll(string(b), "__TOKEN__", token))
	})
	mux.Handle("/app.js", http.FileServer(http.FS(static)))
	mux.Handle("/style.css", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		temps := map[string]int64{}
		if len(temperatures) > 0 {
			temps = temperatures[0]()
		}
		writeJSON(w, 200, struct {
			cooler.Status
			Temps   map[string]int64 `json:"temps"`
			Session string           `json:"session"`
			Version string           `json:"version"`
		}{c.Snapshot(), temps, session, buildinfo.Version})
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"autostart": desktop.AutostartEnabled()})
	})
	mux.HandleFunc("/api/system-info", func(w http.ResponseWriter, r *http.Request) {
		if publisher == nil {
			writeError(w, 409, fmt.Errorf("系统信息采集未启动"))
			return
		}
		if r.Method == http.MethodPost {
			if c.Snapshot().Demo {
				writeError(w, 409, fmt.Errorf("演示模式不向设备推送数据"))
				return
			}
			var body struct {
				Enabled *bool `json:"enabled"`
			}
			if err := decode(w, r, &body); err != nil {
				writeError(w, 400, err)
				return
			}
			if body.Enabled == nil {
				writeError(w, 400, fmt.Errorf("需要 enabled 字段"))
				return
			}
			publisher.SetEnabled(*body.Enabled)
		}
		writeJSON(w, 200, publisher.Snapshot())
	})
	mux.HandleFunc("/api/lcd-layout", func(w http.ResponseWriter, r *http.Request) {
		var body cooler.LCDLayout
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		result, err := c.SetLCDLayout(ctx, body)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/query", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind  string `json:"kind"`
			Curve bool   `json:"curve"`
			Slot  int    `json:"slot"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var frame cooler.Frame
		var err error
		if body.Kind == "cooling-preset" {
			if body.Slot < 1 || body.Slot > 4 {
				writeError(w, 400, fmt.Errorf("配置槽需要 1～4"))
				return
			}
			frame, err = c.GetCoolingPreset(ctx, body.Curve, byte(body.Slot))
		} else {
			frame, err = c.Query(ctx, body.Kind)
		}
		if err != nil {
			writeError(w, 409, err)
			return
		}
		result := map[string]any{"command": frame.Command, "payloadHex": hex.EncodeToString(frame.Payload), "checksumValid": frame.ChecksumValid}
		if config, err := frame.RGBConfig(); err == nil {
			result["config"] = config
		}
		if config, err := frame.CoolingConfig(); err == nil {
			result["config"] = config
		}
		if options, err := frame.DeviceOptions(); err == nil {
			result["options"] = options
		}
		if body.Kind == "rgb-switch" {
			enabled, err := frame.RGBEnabled()
			if err != nil {
				writeError(w, 409, err)
				return
			}
			result["enabled"] = enabled
		}
		if enabled, err := frame.LCDEnabled(); err == nil {
			result["enabled"] = enabled
		}
		if enabled, err := frame.FanEnabled(); err == nil {
			result["enabled"] = enabled
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/device-options", func(w http.ResponseWriter, r *http.Request) {
		var patch cooler.DeviceOptionsPatch
		if err := decode(w, r, &patch); err != nil {
			writeError(w, 400, err)
			return
		}
		if err := patch.Validate(); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := c.UpdateDeviceOptions(ctx, patch)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/cooling", func(w http.ResponseWriter, r *http.Request) {
		var body cooler.CoolingConfig
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := c.SetCooling(ctx, body)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/source", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Source string `json:"source"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		source, err := cooler.ParseCoolingSource(body.Source)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := c.SetCoolingSource(ctx, source); err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]any{"confirmed": true, "source": source, "message": "温度来源已查询读回匹配；温控曲线需要相应主机温度持续推送"})
	})
	mux.HandleFunc("/api/rgb", func(w http.ResponseWriter, r *http.Request) {
		var body cooler.RGBPatch
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		result, err := c.UpdateRGB(ctx, body)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/mode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode int `json:"mode"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		res, err := c.SetMode(ctx, body.Mode)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("/api/speed", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RPM int `json:"rpm"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		result, err := c.SetFanRPM(ctx, body.RPM)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/switch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind string `json:"kind"`
			On   bool   `json:"on"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		frame, err := c.SetSwitch(ctx, body.Kind, body.On)
		if err != nil {
			writeError(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]any{"message": "设备已返回开关响应；请观察实际变化", "response": frame})
	})
	mux.HandleFunc("/api/autostart", func(w http.ResponseWriter, r *http.Request) {
		if c.Snapshot().Demo {
			writeError(w, 409, fmt.Errorf("演示模式不修改登录自启配置"))
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		if err := desktop.SetAutostart(body.Enabled); err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"autostart": desktop.AutostartEnabled()})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SharkCool-App", "sharkcool-linux")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != host {
			writeError(w, 403, fmt.Errorf("主机地址不匹配"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			writeError(w, 403, fmt.Errorf("拒绝跨站请求"))
			return
		}
		mutating := strings.HasPrefix(r.URL.Path, "/api/screensaver/") || r.URL.Path == "/api/device-options" || r.URL.Path == "/api/mode" || r.URL.Path == "/api/autostart" || r.URL.Path == "/api/speed" || r.URL.Path == "/api/switch" || r.URL.Path == "/api/query" || r.URL.Path == "/api/rgb" || r.URL.Path == "/api/cooling" || r.URL.Path == "/api/source" || r.URL.Path == "/api/lcd-layout" || r.URL.Path == "/api/system-info" && r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutating {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				writeError(w, 405, fmt.Errorf("需要 POST"))
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-SharkCool-Token")), []byte(token)) != 1 {
				writeError(w, 403, fmt.Errorf("缺少有效的会话令牌"))
				return
			}
			if r.Header.Get("Content-Type") != "application/json" {
				writeError(w, 415, fmt.Errorf("需要 application/json"))
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, 405, fmt.Errorf("需要 GET"))
			return
		}
		if mutating && screen.snapshot().Running && !strings.HasPrefix(r.URL.Path, "/api/screensaver") {
			writeError(w, 409, fmt.Errorf("请等待屏保传输完成"))
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("请求格式错误: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("请求包含多余数据")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
