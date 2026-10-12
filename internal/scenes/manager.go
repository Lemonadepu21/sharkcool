package scenes

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"sharkcool-linux/brb02"
)

type Device interface {
	Snapshot() brb02.Status
	GetRGB(context.Context) (brb02.RGBConfig, error)
	GetRGBPreset(context.Context, byte, byte) (brb02.RGBConfig, error)
	GetCooling(context.Context) (brb02.CoolingConfig, error)
	GetCoolingPreset(context.Context, bool, byte) (brb02.Frame, error)
	GetCoolingSource(context.Context) (brb02.CoolingSource, error)
	SetRGB(context.Context, brb02.RGBConfig) (brb02.RGBResult, error)
	SetCooling(context.Context, brb02.CoolingConfig) (brb02.CoolingResult, error)
	SetCoolingSource(context.Context, brb02.CoolingSource) error
	SetSceneMode(context.Context, bool) (brb02.SceneModeResult, error)
}
type slotKey struct {
	curve bool
	slot  byte
}
type rgbKey struct{ effect, palette byte }

type plan struct {
	name     string
	lighting *brb02.RGBConfig
	cooling  *brb02.CoolingConfig
}

// Manager is sequential: the caller provides fresh process observations and
// owns the device for this session. A failed partial transition remains dirty
// until Restore succeeds; none of its ACKs alone establish an applied scene.
type Manager struct {
	device                             Device
	endpoint                           string
	rgb                                brb02.RGBConfig
	cooling                            brb02.CoolingConfig
	source                             brb02.CoolingSource
	plans                              []plan
	keys                               []slotKey
	slots                              map[slotKey]brb02.CoolingConfig
	touched                            map[slotKey]bool
	rgbKeys                            []rgbKey
	rgbSlots                           map[rgbKey]brb02.RGBConfig
	touchedRGB                         map[rgbKey]bool
	rgbDirty, coolingDirty, sceneDirty bool
	active                             int
	generation                         uint64
}
type Transition struct {
	Changed           bool   `json:"changed"`
	Profile           string `json:"profile"`
	SceneAcknowledged bool   `json:"sceneAcknowledged"`
	LightingConfirmed bool   `json:"lightingConfirmed"`
	CoolingConfirmed  bool   `json:"coolingConfirmed"`
}

func NewManager(ctx context.Context, device Device, config Config) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	status := device.Snapshot()
	if !status.Connected || status.Device == "" || status.Demo {
		return nil, fmt.Errorf("场景需要已连接的真实设备")
	}
	m := &Manager{device: device, endpoint: status.Device, active: -1, generation: status.ConnectionGeneration, slots: map[slotKey]brb02.CoolingConfig{}, touched: map[slotKey]bool{}, rgbSlots: map[rgbKey]brb02.RGBConfig{}, touchedRGB: map[rgbKey]bool{}}
	var err error
	if m.rgb, err = device.GetRGB(ctx); err != nil {
		return nil, err
	}
	if _, err = m.rgb.Report(); err != nil {
		return nil, err
	}
	if m.cooling, err = device.GetCooling(ctx); err != nil {
		return nil, err
	}
	if _, err = m.cooling.Report(); err != nil {
		return nil, err
	}
	m.cooling = cloneCooling(m.cooling)
	if m.source, err = device.GetCoolingSource(ctx); err != nil {
		return nil, err
	}
	if m.source != m.cooling.Source {
		return nil, fmt.Errorf("设备温度来源与当前配置不一致，未改变设备")
	}
	for _, profile := range config.Profiles {
		p := plan{name: profile.Name}
		if profile.Lighting != nil {
			v, err := profile.Lighting.Apply(m.rgb)
			if err != nil {
				return nil, fmt.Errorf("场景 %q: %w", profile.Name, err)
			}
			p.lighting = &v
			m.addRGBKey(rgbKey{v.Effect, v.Palette})
		}
		if profile.Cooling != nil {
			v := cloneCooling(*profile.Cooling)
			p.cooling = &v
			m.addKey(keyFor(v))
		}
		m.plans = append(m.plans, p)
	}
	{
		m.addRGBKey(rgbKey{m.rgb.Effect, m.rgb.Palette})
		for _, key := range m.rgbKeys {
			saved, e := device.GetRGBPreset(ctx, key.effect, key.palette)
			if e != nil {
				return nil, e
			}
			if saved.Effect != key.effect || saved.Palette != key.palette {
				return nil, fmt.Errorf("RGB 槽备份选择值不匹配")
			}
			if _, e = saved.Report(); e != nil {
				return nil, e
			}
			m.rgbSlots[key] = saved
		}
		if m.rgbSlots[rgbKey{m.rgb.Effect, m.rgb.Palette}] != m.rgb {
			return nil, fmt.Errorf("当前 RGB 与其配置槽不同，无法完整恢复，未改变设备")
		}
	}
	m.addKey(keyFor(m.cooling))
	for _, key := range m.keys {
		frame, err := device.GetCoolingPreset(ctx, key.curve, key.slot)
		if err != nil {
			return nil, err
		}
		saved, err := frame.CoolingConfig()
		if err != nil {
			return nil, err
		}
		if keyFor(saved) != key {
			return nil, fmt.Errorf("配置槽备份选择值不匹配")
		}
		if _, err = saved.Report(); err != nil {
			return nil, err
		}
		m.slots[key] = cloneCooling(saved)
	}
	if !equalCooling(m.slots[keyFor(m.cooling)], m.cooling) {
		return nil, fmt.Errorf("当前散热与其配置槽不同，无法完整恢复，未改变设备")
	}
	if err = m.guard(status.ConnectionGeneration); err != nil {
		return nil, err
	}
	return m, nil
}
func keyFor(c brb02.CoolingConfig) slotKey { return slotKey{len(c.Points) > 0, c.Slot} }
func cloneCooling(c brb02.CoolingConfig) brb02.CoolingConfig {
	c.Points = append([]brb02.CurvePoint(nil), c.Points...)
	return c
}
func equalCooling(a, b brb02.CoolingConfig) bool {
	aa, ae := a.Report()
	bb, be := b.Report()
	return ae == nil && be == nil && bytes.Equal(aa, bb)
}
func (m *Manager) addKey(key slotKey) {
	for _, known := range m.keys {
		if known == key {
			return
		}
	}
	m.keys = append(m.keys, key)
}
func (m *Manager) addRGBKey(key rgbKey) {
	for _, known := range m.rgbKeys {
		if known == key {
			return
		}
	}
	m.rgbKeys = append(m.rgbKeys, key)
}

func (m *Manager) guard(generation uint64) error {
	status := m.device.Snapshot()
	if !status.Connected {
		return fmt.Errorf("场景操作期间设备断开")
	}
	if status.Device != m.endpoint {
		return fmt.Errorf("场景连接的设备已更换，未向新设备写入备份")
	}
	if status.ConnectionGeneration != generation {
		return fmt.Errorf("场景操作期间连接已更换")
	}
	return nil
}
func (m *Manager) Dirty() bool { return m.rgbDirty || m.coolingDirty || m.sceneDirty }

func (m *Manager) Select(ctx context.Context, index int) (Transition, error) {
	if index < -1 || index >= len(m.plans) {
		return Transition{}, fmt.Errorf("场景选择超出范围")
	}
	status := m.device.Snapshot()
	if err := m.guard(status.ConnectionGeneration); err != nil {
		return Transition{}, err
	}
	if m.active == index && m.generation == status.ConnectionGeneration && (index >= 0 || !m.Dirty()) {
		return Transition{}, nil
	}
	if m.Dirty() {
		if err := m.Restore(ctx); err != nil {
			return Transition{}, err
		}
	}
	result := Transition{Changed: true, Profile: "默认"}
	if index == -1 {
		m.active = -1
		m.generation = status.ConnectionGeneration
		return result, nil
	}
	p := m.plans[index]
	generation := m.device.Snapshot().ConnectionGeneration
	result.Profile = p.name
	// Firmware may reset cooling when entering scene mode. Set the flag
	// before applying independent settings, then verify all domains.
	m.sceneDirty = true
	m.rgbDirty = true
	m.coolingDirty = true
	r, err := m.device.SetSceneMode(ctx, true)
	if err != nil {
		return result, err
	}
	if !r.Acknowledged {
		return result, fmt.Errorf("场景模式未获成功响应")
	}
	result.SceneAcknowledged = true
	if p.lighting != nil {
		if err := m.guard(generation); err != nil {
			return result, err
		}
		m.rgbDirty = true
		m.touchedRGB[rgbKey{p.lighting.Effect, p.lighting.Palette}] = true
		r, err := m.device.SetRGB(ctx, *p.lighting)
		if err != nil {
			return result, err
		}
		if !r.Confirmed {
			return result, fmt.Errorf("场景灯光未读回匹配")
		}
		result.LightingConfirmed = true
	}
	if p.cooling != nil {
		if err := m.guard(generation); err != nil {
			return result, err
		}
		m.coolingDirty = true
		m.touched[keyFor(*p.cooling)] = true
		if err := m.device.SetCoolingSource(ctx, p.cooling.Source); err != nil {
			return result, err
		}
		if err := m.guard(generation); err != nil {
			return result, err
		}
		r, err := m.device.SetCooling(ctx, *p.cooling)
		if err != nil {
			return result, err
		}
		if !r.Confirmed || !r.Active {
			return result, fmt.Errorf("场景散热未确认生效: %s", r.Message)
		}
		result.CoolingConfirmed = true
	}
	if err := m.guard(generation); err != nil {
		return result, err
	}
	desiredRGB, desiredCooling := m.rgb, m.cooling
	if p.lighting != nil {
		desiredRGB = *p.lighting
	}
	if p.cooling != nil {
		desiredCooling = *p.cooling
	}
	if err := m.verify(ctx, &desiredRGB, &desiredCooling, generation); err != nil {
		return result, err
	}
	m.active = index
	m.generation = generation
	return result, nil
}
func (m *Manager) verify(ctx context.Context, lighting *brb02.RGBConfig, cooling *brb02.CoolingConfig, generation uint64) error {
	if err := m.guard(generation); err != nil {
		return err
	}
	if lighting != nil {
		actual, err := m.device.GetRGB(ctx)
		if err != nil {
			return err
		}
		if actual != *lighting {
			return fmt.Errorf("场景标志响应后灯光配置发生变化")
		}
	}
	if cooling != nil {
		actual, err := m.device.GetCooling(ctx)
		if err != nil {
			return err
		}
		if !equalCooling(actual, *cooling) {
			return fmt.Errorf("场景散热配置不匹配: 期望 %+v，读回 %+v", *cooling, actual)
		}
		source, err := m.device.GetCoolingSource(ctx)
		if err != nil {
			return err
		}
		if source != cooling.Source {
			return fmt.Errorf("场景温度来源未匹配")
		}
	}
	return m.guard(generation)
}

// VerifyActive detects device-side changes during a long-running scene.
func (m *Manager) VerifyActive(ctx context.Context) error {
	if m.active < 0 {
		return nil
	}
	p := m.plans[m.active]
	rgb, cooling := m.rgb, m.cooling
	if p.lighting != nil {
		rgb = *p.lighting
	}
	if p.cooling != nil {
		cooling = *p.cooling
	}
	return m.verify(ctx, &rgb, &cooling, m.generation)
}

// Restore recovers all touched slots plus the original active config, source
// and lighting after exiting scene mode. It attempts the independent steps even
// after an error. The original scene flag is not queryable: exit explicitly
// means default mode, rather than claiming a flag read-back or original flag.
func (m *Manager) Restore(ctx context.Context) error {
	if !m.Dirty() {
		m.active = -1
		return nil
	}
	generation := m.device.Snapshot().ConnectionGeneration
	if err := m.guard(generation); err != nil {
		return err
	}
	var result error
	if m.sceneDirty {
		if err := m.guard(generation); err != nil {
			return errors.Join(result, err)
		}
		r, err := m.device.SetSceneMode(ctx, false)
		if err == nil && !r.Acknowledged {
			err = fmt.Errorf("退出场景未获成功响应")
		}
		result = errors.Join(result, err)
	}
	if m.coolingDirty {
		for _, key := range m.keys {
			if !m.touched[key] {
				continue
			}
			if err := m.guard(generation); err != nil {
				return errors.Join(result, err)
			}
			r, err := m.device.SetCooling(ctx, m.slots[key])
			if err == nil && !r.Confirmed {
				err = fmt.Errorf("散热槽 %d 恢复未读回匹配", key.slot)
			}
			result = errors.Join(result, err)
		}
		if err := m.guard(generation); err != nil {
			return errors.Join(result, err)
		}
		result = errors.Join(result, m.device.SetCoolingSource(ctx, m.source))
		if err := m.guard(generation); err != nil {
			return errors.Join(result, err)
		}
		r, err := m.device.SetCooling(ctx, m.cooling)
		if err == nil && (!r.Confirmed || !r.Active) {
			err = fmt.Errorf("原生效散热配置未恢复")
		}
		result = errors.Join(result, err)
	}
	if m.rgbDirty {
		for _, key := range m.rgbKeys {
			if !m.touchedRGB[key] {
				continue
			}
			if err := m.guard(generation); err != nil {
				return errors.Join(result, err)
			}
			r, e := m.device.SetRGB(ctx, m.rgbSlots[key])
			if e == nil && !r.Confirmed {
				e = fmt.Errorf("RGB 槽恢复未读回匹配")
			}
			result = errors.Join(result, e)
		}
		if err := m.guard(generation); err != nil {
			return errors.Join(result, err)
		}
		r, err := m.device.SetRGB(ctx, m.rgb)
		if err == nil && !r.Confirmed {
			err = fmt.Errorf("原灯光未读回恢复")
		}
		result = errors.Join(result, err)
	}
	var lighting *brb02.RGBConfig
	var cooling *brb02.CoolingConfig
	if m.rgbDirty {
		lighting = &m.rgb
	}
	if m.coolingDirty {
		cooling = &m.cooling
	}
	result = errors.Join(result, m.verify(ctx, lighting, cooling, generation))
	for _, key := range m.keys {
		if m.touched[key] {
			if err := m.guard(generation); err != nil {
				return errors.Join(result, err)
			}
			frame, e := m.device.GetCoolingPreset(ctx, key.curve, key.slot)
			if e == nil {
				saved, parseErr := frame.CoolingConfig()
				e = parseErr
				if e == nil && !equalCooling(saved, m.slots[key]) {
					e = fmt.Errorf("恢复后散热配置槽不同")
				}
			}
			result = errors.Join(result, e)
		}
	}
	for _, key := range m.rgbKeys {
		if m.touchedRGB[key] {
			if err := m.guard(generation); err != nil {
				return errors.Join(result, err)
			}
			saved, e := m.device.GetRGBPreset(ctx, key.effect, key.palette)
			if e == nil && saved != m.rgbSlots[key] {
				e = fmt.Errorf("恢复后 RGB 配置槽不同")
			}
			result = errors.Join(result, e)
		}
	}
	result = errors.Join(result, m.guard(generation))
	if result == nil {
		m.rgbDirty = false
		m.coolingDirty = false
		m.sceneDirty = false
		m.touched = map[slotKey]bool{}
		m.touchedRGB = map[rgbKey]bool{}
		m.active = -1
		m.generation = generation
	}
	return result
}
