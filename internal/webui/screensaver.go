package webui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	cooler "sharkcool-linux/brb02"
)

type screenJob struct {
	DraftID    string `json:"draftId"`
	Running    bool   `json:"running"`
	Phase      string `json:"phase"`
	Progress   int    `json:"progress"`
	Error      string `json:"error"`
	Confirmed  bool   `json:"confirmed"`
	CanRestore bool   `json:"canRestore"`
}

type screenManager struct {
	mu           sync.Mutex
	c            *cooler.Controller
	state        screenJob
	quitting     bool
	draft        []byte
	backup       *cooler.ScreenImage
	backupDevice string
}

func prepareScreen(data []byte) ([]byte, []byte, error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return nil, nil, fmt.Errorf("请选择 8 MB 以内的 PNG 或 JPG 图片")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" && format != "jpeg" {
		return nil, nil, fmt.Errorf("图片格式需要 PNG 或 JPG")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil, nil, fmt.Errorf("图片尺寸过大")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("无法读取图片")
	}
	pixels, err := cooler.EncodeScreenImage(source)
	if err != nil {
		return nil, nil, err
	}
	preview, err := cooler.DecodeScreenImage(pixels)
	if err != nil {
		return nil, nil, err
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, preview); err != nil {
		return nil, nil, err
	}
	return pixels, encoded.Bytes(), nil
}

func (m *screenManager) snapshot() screenJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.state
	state.CanRestore = m.backup != nil && m.backupDevice == m.c.Snapshot().Device
	return state
}
func (m *screenManager) update(phase string, progress int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Phase = phase
	m.state.Progress = progress
}

type storedScreenBackup struct {
	Info   cooler.ScreenImageInfo `json:"info"`
	Data   []byte                 `json:"data"`
	Device string                 `json:"device"`
}

func newScreenManager(c *cooler.Controller) *screenManager {
	m := &screenManager{c: c}
	dir, err := os.UserConfigDir()
	if err != nil {
		return m
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sharkcool", "screensaver-backups", "original-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, path := range files {
		info, err := os.Lstat(path)
		// Windows stores this file in the user's AppData directory and uses
		// inherited ACLs; Go does not expose those as POSIX permission bits.
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var backup storedScreenBackup
		if json.Unmarshal(data, &backup) != nil || backup.Device == "" || len(backup.Data) != cooler.ScreenImageBytes || backup.Info.Size != uint32(len(backup.Data)) || backup.Info.Reserved != 0 || cooler.ScreenCRC16(backup.Data) != backup.Info.CRC16 {
			continue
		}
		m.backup = &cooler.ScreenImage{Info: backup.Info, Data: backup.Data}
		m.backupDevice = backup.Device
		break
	}
	return m
}
func saveScreenBackup(image cooler.ScreenImage, device string) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "sharkcool", "screensaver-backups")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, fmt.Sprintf("original-%020d-*.json", time.Now().UnixNano()))
	if err != nil {
		return err
	}
	name := file.Name()
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(name)
		}
	}()
	// A single durable file keeps original metadata and pixels together.
	err = json.NewEncoder(file).Encode(storedScreenBackup{image.Info, image.Data, device})
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func (m *screenManager) start(id string, restore bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.quitting {
		return fmt.Errorf("应用正在退出")
	}
	if m.state.Running {
		return fmt.Errorf("屏保传输正在进行")
	}
	status := m.c.Snapshot()
	if !status.Connected || status.Demo {
		return fmt.Errorf("请先连接散热器")
	}
	var target cooler.ScreenImage
	if restore {
		if m.backup == nil || m.backupDevice != status.Device {
			return fmt.Errorf("没有这台设备的原屏保备份")
		}
		target = *m.backup
		target.Data = bytes.Clone(target.Data)
	} else {
		if id == "" || id != m.state.DraftID || len(m.draft) == 0 {
			return fmt.Errorf("图片预览已失效，请重新选择")
		}
		target.Data = bytes.Clone(m.draft)
		target.Info = cooler.ScreenImageInfo{Timestamp: uint32(time.Now().Unix()), Size: uint32(len(target.Data)), CRC16: cooler.ScreenCRC16(target.Data)}
	}
	m.state.Running = true
	m.state.Confirmed = false
	m.state.Error = ""
	m.state.Progress = -1
	m.state.Phase = "备份原图"
	if restore {
		m.state.Phase = "恢复中"
	}
	go m.transfer(target, status, restore)
	return nil
}

func (m *screenManager) transfer(target cooler.ScreenImage, initial cooler.Status, restore bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	var err error
	if !restore {
		var original cooler.ScreenImage
		original, err = m.c.GetScreenImage(ctx)
		if err == nil && len(original.Data) != cooler.ScreenImageBytes {
			err = fmt.Errorf("原屏保格式无法备份，尚未更改设备")
		}
		if err == nil {
			err = saveScreenBackup(original, initial.Device)
		}
		if err == nil {
			m.mu.Lock()
			m.backup = &original
			m.backupDevice = initial.Device
			m.state.CanRestore = true
			m.mu.Unlock()
		}
	}
	current := m.c.Snapshot()
	if err == nil && (!current.Connected || current.ConnectionGeneration != initial.ConnectionGeneration || current.Device != initial.Device) {
		err = fmt.Errorf("连接已变化，尚未更改屏保")
	}
	if err == nil {
		m.update("上传中", 0)
		_, err = m.c.SetScreenImage(ctx, target, func(done, total int) {
			if done == total {
				m.update("验证中", -1)
			} else {
				m.update("上传中", done*100/total)
			}
		})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Running = false
	m.state.Progress = 0
	if err != nil {
		m.state.Phase = "失败"
		m.state.Error = err.Error()
		return
	}
	m.state.Confirmed = true
	m.state.Progress = 100
	m.state.Phase = "已更换"
	if restore {
		m.state.Phase = "已恢复"
	}
}

func (m *screenManager) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/screensaver", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.snapshot()) })
	mux.HandleFunc("/api/screensaver/prepare", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Image []byte `json:"image"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeError(w, 400, fmt.Errorf("图片请求过大或格式错误"))
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			writeError(w, 400, fmt.Errorf("请求包含多余数据"))
			return
		}
		pixels, preview, err := prepareScreen(body.Image)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			writeError(w, 500, err)
			return
		}
		id := hex.EncodeToString(nonce[:])
		m.mu.Lock()
		if m.state.Running {
			m.mu.Unlock()
			writeError(w, 409, fmt.Errorf("请等待屏保传输完成"))
			return
		}
		m.draft = pixels
		m.state.DraftID = id
		m.state.Phase = ""
		m.state.Error = ""
		m.state.Confirmed = false
		m.mu.Unlock()
		writeJSON(w, 200, map[string]any{"draftId": id, "preview": preview, "width": cooler.ScreenWidth, "height": cooler.ScreenHeight})
	})
	for _, path := range []string{"/api/screensaver/apply", "/api/screensaver/restore"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				DraftID string `json:"draftId"`
			}
			if err := decode(w, r, &body); err != nil {
				writeError(w, 400, err)
				return
			}
			if err := m.start(body.DraftID, r.URL.Path == "/api/screensaver/restore"); err != nil {
				writeError(w, 409, err)
				return
			}
			writeJSON(w, 202, m.snapshot())
		})
	}
}
