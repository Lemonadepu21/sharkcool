package webui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	cooler "sharkcool-linux/brb02"
)

func TestScreenPreparationAndDurableBackup(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			source.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	pixels, preview, err := prepareScreen(encoded.Bytes())
	if err != nil || len(pixels) != cooler.ScreenImageBytes {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(preview))
	if err != nil || config.Width != 428 || config.Height != 142 {
		t.Fatal("wrong preview size", err)
	}
	for _, invalid := range [][]byte{nil, []byte("not an image"), bytes.Repeat([]byte{0}, 8<<20+1)} {
		if _, _, err := prepareScreen(invalid); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("APPDATA", root)
	original := cooler.ScreenImage{Info: cooler.ScreenImageInfo{Timestamp: 123, Size: uint32(len(pixels)), CRC16: cooler.ScreenCRC16(pixels)}, Data: pixels}
	if err := saveScreenBackup(original, "/dev/test"); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "sharkcool/screensaver-backups/*.json"))
	if len(files) != 1 {
		t.Fatal("backup missing")
	}
	file, _ := os.Stat(files[0])
	if runtime.GOOS != "windows" && file.Mode().Perm() != 0o600 {
		t.Fatal("backup not private")
	}
	data, _ := os.ReadFile(files[0])
	var stored struct {
		Info cooler.ScreenImageInfo
		Data []byte
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.Info != original.Info || !bytes.Equal(stored.Data, pixels) {
		t.Fatal("backup lost data or metadata", err)
	}
	m := newScreenManager(cooler.New("", false))
	if m.backup == nil || m.backup.Info != original.Info || m.backupDevice != "/dev/test" {
		t.Fatal("backup lost across restart")
	}

}

func TestScreenHTTPPolicyAndOfflinePreview(t *testing.T) {
	c := cooler.New("", false)
	handler, err := NewHandler(c, "127.0.0.1:17865")
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "http://127.0.0.1:17865/", nil))
	token := regexp.MustCompile(`name="sharkcool-token" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())[1]
	request := func(method, path, auth, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:17865"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-SharkCool-Token", auth)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, auth, body, origin string
		status                           int
	}{
		{"GET", "/api/screensaver", "", "", "", 200},
		{"POST", "/api/screensaver/prepare", "", "{}", "", 403},
		{"POST", "/api/screensaver/apply", "", "{}", "", 403},
		{"POST", "/api/screensaver/restore", "", "{}", "", 403},
		{"GET", "/api/screensaver/apply", "", "", "", 405},
		{"POST", "/api/screensaver/prepare", token, "{}", "https://example.com", 403},
		{"POST", "/api/screensaver/prepare", token, "{}", "", 400},
		{"POST", "/api/screensaver/prepare", token, `{"image":"!"}`, "", 400},
		{"POST", "/api/screensaver/prepare", token, `{"image":"","path":"/etc/passwd"}`, "", 400},
		{"POST", "/api/screensaver/apply", token, `{"draftId":"expired"}`, "", 409},
	} {
		if w := request(tc.method, tc.path, tc.auth, tc.body, tc.origin); w.Code != tc.status {
			t.Fatalf("%s %s got %d: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	var encoded bytes.Buffer
	png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	body, _ := json.Marshal(map[string][]byte{"image": encoded.Bytes()})
	w := request("POST", "/api/screensaver/prepare", token, string(body), "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		DraftID       string
		Preview       []byte
		Width, Height int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.DraftID) != 32 || result.Width != 428 || result.Height != 142 {
		t.Fatal("invalid preparation result", err)
	}
	if _, err := png.Decode(bytes.NewReader(result.Preview)); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Connected {
		t.Fatal("preview connected hardware")
	}
}
