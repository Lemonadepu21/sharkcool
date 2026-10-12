package brb02

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestRGBOfficialCaptureAndPatch(t *testing.T) {
	// Captured on firmware 3.0.3 before any lighting changes.
	f := Frame{Command: 0x13, Payload: []byte{8, 0xf4, 1, 80, 10, 0, 255, 72}}
	c, err := f.RGBConfig()
	if err != nil || c.Interval != 500 || c.Color != [3]byte{0, 255, 72} {
		t.Fatalf("%+v %v", c, err)
	}
	r, err := c.Report()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r[4:12], f.Payload) {
		t.Fatalf("cannot restore captured settings: %x", r)
	}
	brightness, speed := 35, 100
	updated, err := (RGBPatch{Effect: "breathing", Color: "#204060", Brightness: &brightness, Speed: &speed}).Apply(c)
	if err != nil || updated.Interval != 1000 || updated.Brightness != 35 || updated.ColorCount != 1 || updated.Color != [3]byte{32, 64, 96} {
		t.Fatalf("%+v %v", updated, err)
	}
	colorOnly, err := (RGBPatch{Color: "112233"}).Apply(c)
	if err != nil || colorOnly.Effect != c.Effect || colorOnly.Palette != c.Palette || colorOnly.Interval != c.Interval || colorOnly.Brightness != c.Brightness {
		t.Fatalf("unrelated fields changed: %+v %v", colorOnly, err)
	}
	for _, p := range []RGBPatch{{}, {Effect: "audio"}, {Effect: "reactive"}, {Color: "abc"}, {Color: "zz1122"}, {Brightness: intPointer(101)}, {Speed: intPointer(-1)}} {
		if p.Validate() == nil {
			t.Fatalf("invalid RGB patch accepted: %+v", p)
		}
	}
	if _, err := (RGBPatch{Effect: "static", Speed: intPointer(50)}).Apply(c); err == nil {
		t.Fatal("static speed accepted")
	}
	if _, err := (Frame{Command: 0x13, Payload: []byte{0}}).RGBConfig(); err == nil {
		t.Fatal("ACK treated as RGB config")
	}
}

func intPointer(i int) *int { return &i }

func TestRGBColorModeRoundTrip(t *testing.T) {
	original := RGBConfig{Effect: 8, Palette: 2, Interval: 500, Brightness: 80, ColorCount: 10, Color: [3]byte{0, 255, 72}}
	single, err := (RGBPatch{ColorMode: "single"}).Apply(original)
	if err != nil || single.ColorCount != 1 || single.Color != original.Color {
		t.Fatalf("single: %+v %v", single, err)
	}
	multi, err := (RGBPatch{ColorMode: "multi"}).Apply(single)
	if err != nil || multi != original {
		t.Fatalf("multi did not preserve other fields: %+v %v", multi, err)
	}
	for _, p := range []RGBPatch{{ColorMode: "invalid"}, {ColorMode: "multi", Color: "#112233"}} {
		if p.Validate() == nil {
			t.Fatal("ambiguous color mode accepted")
		}
	}
}

type rgbTransport struct {
	fakeTransport
	config []byte
	apply  bool
}

func (d *rgbTransport) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, b...))
	if b[3] == 0x12 {
		if d.apply {
			d.config = append([]byte{}, b[4:12]...)
		}
		ack, _ := BuildPacket(0x12, []byte{0})
		d.reads <- ack
	}
	if b[3] == 0x13 {
		packet, _ := BuildPacket(0x13, d.config)
		d.reads <- packet
	}
	return len(b), nil
}

func TestRGBReadbackRequiresMatchingConfiguration(t *testing.T) {
	for _, apply := range []bool{true, false} {
		t.Run(map[bool]string{true: "applied", false: "ack-only"}[apply], func(t *testing.T) {
			d := &rgbTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, config: []byte{8, 0xf4, 1, 80, 10, 0, 255, 72}, apply: apply}
			// A caller-supplied backend may be an uncomparable interface value.
			backend := struct {
				Transport
				Marker []byte
			}{d, []byte{1}}
			c := NewWithConnector(func(context.Context) (Transport, string, error) { return backend, "ble://test", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected })
			deadline, stop := context.WithTimeout(ctx, 50*time.Millisecond)
			defer stop()
			res, err := c.UpdateRGB(deadline, RGBPatch{Color: "112233"})
			if apply && (err != nil || !res.Confirmed) {
				t.Fatalf("%+v %v", res, err)
			}
			if !apply && (res.Confirmed || !errors.Is(err, context.DeadlineExceeded)) {
				t.Fatalf("ACK accepted without matching config: %+v %v", res, err)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.writes) != 3 || d.writes[0][3] != 0x13 || d.writes[1][3] != 0x12 || d.writes[2][3] != 0x13 {
				t.Fatal("configuration transaction incomplete")
			}
		})
	}
}
