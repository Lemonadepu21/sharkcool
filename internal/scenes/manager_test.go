package scenes

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"sharkcool-linux/brb02"
)

type fakeDevice struct {
	status      brb02.Status
	rgb         brb02.RGBConfig
	cooling     brb02.CoolingConfig
	rgbs        map[rgbKey]brb02.RGBConfig
	slots       map[slotKey]brb02.CoolingConfig
	writes      []string
	failRGB     bool
	inactive    bool
	failExit    bool
	resetOnFlag bool
}

func fixture() (*fakeDevice, Config) {
	d := &fakeDevice{status: brb02.Status{Connected: true, Device: "ble:test", ConnectionGeneration: 1}, rgb: brb02.RGBConfig{Effect: 8, Interval: 500, Brightness: 80, ColorCount: 10, Color: [3]byte{0, 255, 72}}, cooling: brb02.CoolingConfig{Slot: 1, RPM: 1200}, rgbs: map[rgbKey]brb02.RGBConfig{}, slots: map[slotKey]brb02.CoolingConfig{}}
	d.rgbs[rgbKey{8, 0}] = d.rgb
	d.rgbs[rgbKey{4, 0}] = brb02.RGBConfig{Effect: 4, Brightness: 60, ColorCount: 1, Color: [3]byte{255, 0, 0}}
	d.slots[slotKey{false, 1}] = d.cooling
	d.slots[slotKey{false, 2}] = brb02.CoolingConfig{Slot: 2, RPM: 2134}
	return d, Config{Version: 1, Profiles: []Profile{{Name: "game", Programs: []string{"game"}, Lighting: &brb02.RGBPatch{Effect: "static", Color: "#204060"}, Cooling: &brb02.CoolingConfig{Slot: 2, RPM: 1800}}}}
}
func (d *fakeDevice) Snapshot() brb02.Status                          { return d.status }
func (d *fakeDevice) GetRGB(context.Context) (brb02.RGBConfig, error) { return d.rgb, nil }
func (d *fakeDevice) GetRGBPreset(_ context.Context, e, p byte) (brb02.RGBConfig, error) {
	v, ok := d.rgbs[rgbKey{e, p}]
	if !ok {
		return v, errors.New("missing RGB")
	}
	return v, nil
}
func (d *fakeDevice) GetCooling(context.Context) (brb02.CoolingConfig, error) {
	return cloneCooling(d.cooling), nil
}
func (d *fakeDevice) GetCoolingSource(context.Context) (brb02.CoolingSource, error) {
	return d.cooling.Source, nil
}
func (d *fakeDevice) GetCoolingPreset(_ context.Context, curve bool, slot byte) (brb02.Frame, error) {
	v, ok := d.slots[slotKey{curve, slot}]
	if !ok {
		return brb02.Frame{}, errors.New("missing cooling")
	}
	r, err := v.Report()
	if err != nil {
		return brb02.Frame{}, err
	}
	return brb02.Frame{Command: 0x26, Payload: append([]byte(nil), r[4:int(r[2])]...)}, nil
}
func (d *fakeDevice) SetRGB(_ context.Context, v brb02.RGBConfig) (brb02.RGBResult, error) {
	d.writes = append(d.writes, "rgb")
	d.rgb = v
	d.rgbs[rgbKey{v.Effect, v.Palette}] = v
	if d.failRGB {
		d.failRGB = false
		return brb02.RGBResult{}, errors.New("RGB failed after write")
	}
	return brb02.RGBResult{Confirmed: true}, nil
}
func (d *fakeDevice) SetCooling(_ context.Context, v brb02.CoolingConfig) (brb02.CoolingResult, error) {
	d.writes = append(d.writes, "cooling")
	d.cooling = cloneCooling(v)
	d.slots[keyFor(v)] = cloneCooling(v)
	return brb02.CoolingResult{Confirmed: true, Active: !d.inactive}, nil
}
func (d *fakeDevice) SetCoolingSource(_ context.Context, v brb02.CoolingSource) error {
	d.writes = append(d.writes, "source")
	d.cooling.Source = v
	return nil
}
func (d *fakeDevice) SetSceneMode(_ context.Context, v bool) (brb02.SceneModeResult, error) {
	if d.resetOnFlag {
		d.cooling = d.slots[slotKey{false, 1}]
	}
	if v {
		d.writes = append(d.writes, "on")
	} else {
		d.writes = append(d.writes, "off")
		if d.failExit {
			return brb02.SceneModeResult{}, errors.New("exit failed")
		}
	}
	return brb02.SceneModeResult{Acknowledged: true}, nil
}

func TestApplyAndRestoreAllSlots(t *testing.T) {
	d, c := fixture()
	rgb, cooling := d.rgb, d.cooling
	rgbSlots := map[rgbKey]brb02.RGBConfig{}
	slots := map[slotKey]brb02.CoolingConfig{}
	for k, v := range d.rgbs {
		rgbSlots[k] = v
	}
	for k, v := range d.slots {
		slots[k] = v
	}
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), -1); err != nil || len(d.writes) != 0 {
		t.Fatal(err, d.writes)
	}
	r, err := m.Select(context.Background(), 0)
	if err != nil || !r.SceneAcknowledged || !r.CoolingConfirmed || !r.LightingConfirmed {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(d.writes, []string{"on", "rgb", "source", "cooling"}) {
		t.Fatal(d.writes)
	}
	n := len(d.writes)
	if _, err = m.Select(context.Background(), 0); err != nil || len(d.writes) != n {
		t.Fatal(err, d.writes)
	}
	if _, err = m.Select(context.Background(), -1); err != nil {
		t.Fatal(err)
	}
	if m.Dirty() || d.rgb != rgb || !equalCooling(d.cooling, cooling) || !reflect.DeepEqual(d.rgbs, rgbSlots) || !reflect.DeepEqual(d.slots, slots) {
		t.Fatal("baseline/presets not restored", d)
	}
}
func TestPartialFailureStillRestores(t *testing.T) {
	d, c := fixture()
	original := d.rgb
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	d.failRGB = true
	if _, err = m.Select(context.Background(), 0); err == nil || !m.Dirty() {
		t.Fatal("partial write lost", err)
	}
	if _, err = m.Select(context.Background(), -1); err != nil || m.Dirty() || d.rgb != original {
		t.Fatal("default skipped cleanup", err)
	}
}
func TestReconnectReappliesAndDifferentDeviceRejected(t *testing.T) {
	d, c := fixture()
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	n := len(d.writes)
	d.status.ConnectionGeneration++
	if r, err := m.Select(context.Background(), 0); err != nil || !r.Changed || len(d.writes) <= n {
		t.Fatal(r, err)
	}
	d.status.Device = "ble:other"
	n = len(d.writes)
	if err = m.Restore(context.Background()); err == nil || len(d.writes) != n {
		t.Fatal("wrote to different device", err)
	}
}
func TestSavedButInactiveRejectedAndRestoreRetry(t *testing.T) {
	d, c := fixture()
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	d.inactive = true
	if _, err = m.Select(context.Background(), 0); err == nil || !m.Dirty() {
		t.Fatal("inactive accepted", err)
	}
	d.inactive = false
	if err = m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	d.failExit = true
	if err = m.Restore(context.Background()); err == nil || !m.Dirty() {
		t.Fatal("failed cleanup cleared", err)
	}
	d.failExit = false
	if err = m.Restore(context.Background()); err != nil || m.Dirty() {
		t.Fatal(err)
	}
}
func TestInconsistentBackupMakesNoWrites(t *testing.T) {
	for _, domain := range []string{"rgb", "cooling"} {
		t.Run(domain, func(t *testing.T) {
			d, c := fixture()
			if domain == "rgb" {
				d.rgb.Brightness--
			} else {
				d.cooling.RPM++
			}
			if _, err := NewManager(context.Background(), d, c); err == nil || len(d.writes) != 0 {
				t.Fatal(err, d.writes)
			}
		})
	}
}

func TestFlagResetAndPeriodicVerification(t *testing.T) {
	d, c := fixture()
	d.resetOnFlag = true
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), 0); err != nil {
		t.Fatal("flag reset overwrote settings", err)
	}
	if err = m.VerifyActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.cooling.RPM = 1200
	if err = m.VerifyActive(context.Background()); err == nil {
		t.Fatal("missed device-side fallback")
	}
	if err = m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProfilesUseSessionBaseline(t *testing.T) {
	d, c := fixture()
	brightness := 50
	c.Profiles = append(c.Profiles, Profile{Name: "second", Programs: []string{"second"}, Lighting: &brb02.RGBPatch{Brightness: &brightness}})
	m, err := NewManager(context.Background(), d, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Select(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if d.rgb.Effect != 8 || d.rgb.ColorCount != 10 || d.rgb.Color != ([3]byte{0, 255, 72}) || d.rgb.Brightness != 50 || d.cooling.RPM != 1200 {
		t.Fatal("inherited previous profile", d.rgb, d.cooling)
	}
	if err = m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
}
