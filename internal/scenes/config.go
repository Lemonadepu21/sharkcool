package scenes

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"sharkcool-linux/brb02"
)

// Profiles are evaluated in array order. Program selectors are exact executable
// paths or basenames, not command lines, shell patterns or window titles.
type Config struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}
type Profile struct {
	Name     string               `json:"name"`
	Programs []string             `json:"programs"`
	Lighting *brb02.RGBPatch      `json:"lighting,omitempty"`
	Cooling  *brb02.CoolingConfig `json:"cooling,omitempty"`
}

func Load(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 65537))
	if err != nil {
		return Config{}, err
	}
	if len(data) > 65536 {
		return Config{}, fmt.Errorf("场景配置不能超过 64 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var config Config
	if err = decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("场景 JSON: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return Config{}, fmt.Errorf("场景配置需要单个 JSON 对象")
	}
	return config, config.Validate()
}
func (config Config) Validate() error {
	if config.Version != 1 || len(config.Profiles) == 0 || len(config.Profiles) > 64 {
		return fmt.Errorf("场景 version 需要为 1，profiles 需要 1～64 项")
	}
	names := map[string]bool{}
	for _, profile := range config.Profiles {
		if profile.Name == "" || len(profile.Name) > 128 || !utf8.ValidString(profile.Name) || strings.TrimSpace(profile.Name) != profile.Name || strings.ContainsAny(profile.Name, "\r\n\x00") || names[profile.Name] {
			return fmt.Errorf("场景名称需要唯一且为有效文本")
		}
		names[profile.Name] = true
		if len(profile.Programs) == 0 || len(profile.Programs) > 16 {
			return fmt.Errorf("场景 %q 需要 1～16 个程序", profile.Name)
		}
		for _, program := range profile.Programs {
			if program == "" || len(program) > 4096 || !utf8.ValidString(program) || strings.ContainsAny(program, "\r\n\x00") || strings.TrimSpace(program) != program || program == "." || program == ".." || strings.Contains(program, "\\") || (strings.Contains(program, "/") && (!strings.HasPrefix(program, "/") || path.Clean(program) != program || program == "/")) {
				return fmt.Errorf("程序需要可执行文件名或规范的绝对路径: %q", program)
			}
		}
		if profile.Lighting == nil && profile.Cooling == nil {
			return fmt.Errorf("场景 %q 需要灯光或散热设置", profile.Name)
		}
		if profile.Lighting != nil {
			if err := profile.Lighting.Validate(); err != nil {
				return fmt.Errorf("场景 %q: %w", profile.Name, err)
			}
		}
		if profile.Cooling != nil {
			if _, err := profile.Cooling.Report(); err != nil {
				return fmt.Errorf("场景 %q: %w", profile.Name, err)
			}
		}
	}
	return nil
}

// Match takes a fresh set from the platform reader. The first configured match
// wins when multiple applications run; -1 means return to the session baseline.
func (config Config) Match(programs map[string]bool) int {
	for i, profile := range config.Profiles {
		for _, program := range profile.Programs {
			if programs[program] {
				return i
			}
		}
	}
	return -1
}
