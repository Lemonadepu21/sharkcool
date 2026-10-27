package brb02

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCoolingCapturedSlotsAndRoundTrip(t *testing.T) {
	for _, wire := range []string{"000001b004", "00010114b00428e8053c560850c40a", "00010414560828fa0b3ca00f50a00f"} {
		payload, _ := hex.DecodeString(wire)
		config, err := (Frame{Command: 0x26, Payload: payload}).CoolingConfig()
		if err != nil {
			t.Fatal(err)
		}
		report, err := config.Report()
		if err != nil || !bytes.Equal(report[4:4+len(payload)], payload) {
			t.Fatalf("capture changed: %x %v", report, err)
		}
	}
	report, err := CoolingPresetReport(true, 3)
	f, _ := ParseReport(report)
	if err != nil || f.Command != 0x26 || !bytes.Equal(f.Payload, []byte{1, 3}) {
		t.Fatal(f, err)
	}
	for _, config := range []CoolingConfig{
		{Source: 2, Slot: 1, RPM: 1200}, {Slot: 0, RPM: 1200}, {Slot: 5, RPM: 1200}, {Slot: 1, RPM: 1199}, {Slot: 1, RPM: 4001},
		{Slot: 1, Points: []CurvePoint{{20, 1200}}},
		{Slot: 1, RPM: 1200, Points: []CurvePoint{{20, 1200}, {40, 1800}, {60, 2600}, {80, 3600}}},
		{Slot: 1, Points: []CurvePoint{{20, 1200}, {20, 1800}, {60, 2600}, {80, 3600}}},
	} {
		if _, err := config.Report(); err == nil {
			t.Fatalf("accepted invalid config: %+v", config)
		}
	}
	for _, f := range []Frame{{Command: 0x24, Payload: []byte{0}}, {Command: 0x25, Payload: []byte{0, 1, 1}}, {Command: 0x26, Payload: []byte{0, 2, 1, 0, 0}}} {
		if _, err := f.CoolingConfig(); err == nil {
			t.Fatalf("malformed response accepted: %+v", f)
		}
	}
}

type coolingTransport struct {
	fakeTransport
	preset       []byte
	current      []byte
	apply        bool
	stale        bool
	transient    bool
	currentReads int
	dropCurrent  bool
	presetReads  int
	dropPreset   bool
}

// Stores matching bytes even when the command has not been accepted, so a
// readback alone cannot substitute for the firmware's command completion.
type coolingACKTransport struct {
	coolingTransport
	ackPayload []byte
	missingACK bool
}

func (d *coolingACKTransport) Write(report []byte) (int, error) {
	f, _ := ParseReport(report)
	if f.Command != 0x24 {
		return d.coolingTransport.Write(report)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, bytes.Clone(report))
	d.preset = bytes.Clone(f.Payload)
	d.current = bytes.Clone(f.Payload)
	if !d.missingACK {
		ack, _ := BuildPacket(0x24, d.ackPayload)
		d.reads <- ack
	}
	return len(report), nil
}

func TestCoolingWaitsForAcceptedCommandBeforeReadback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ack     []byte
		missing bool
	}{
		{"refused", []byte{1}, false},
		{"empty", nil, false},
		{"extra-bytes", []byte{0, 0}, false},
		{"missing", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &coolingACKTransport{coolingTransport: coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}}, ackPayload: tc.ack, missingACK: tc.missing}
			c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected })
			commandCtx, stop := context.WithTimeout(ctx, 50*time.Millisecond)
			defer stop()
			result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 3, RPM: 2974})
			if err == nil || result.Confirmed || result.Active {
				t.Fatalf("unaccepted command reported as applied: %+v %v", result, err)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.writes) != 1 {
				t.Fatalf("sent %d commands before a successful C24 ACK", len(d.writes))
			}
		})
	}
}

func (d *coolingTransport) Write(report []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, report...))
	f, _ := ParseReport(report)
	var payload []byte
	switch f.Command {
	case 0x24:
		if !d.stale {
			d.preset = append([]byte{}, f.Payload...)
		}
		if d.apply {
			d.current = append([]byte{}, f.Payload...)
		}
		payload = []byte{0}
	case 0x26:
		payload = d.preset
		d.presetReads++
		if d.dropPreset && d.presetReads == 1 {
			return len(report), nil
		}
	case 0x25:
		payload = d.current
		if d.transient && d.currentReads == 0 {
			payload = d.preset
		}
		d.currentReads++
		if d.dropCurrent && d.currentReads == 1 {
			return len(report), nil
		}
	case 0x22:
		d.current[0] = f.Payload[0]
		payload = []byte{0}
	case 0x23:
		payload = []byte{d.current[0]}
	}
	packet, _ := BuildPacket(f.Command, payload)
	d.reads <- packet
	return len(report), nil
}

func TestCoolingSaveIsSeparateFromActivationAndSource(t *testing.T) {
	cfg := CoolingConfig{Slot: 1, Points: []CurvePoint{{20, 1200}, {40, 1800}, {60, 2600}, {80, 3600}}}
	for _, apply := range []bool{false, true} {
		d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, current: []byte{0, 0, 1, 0xb0, 4}, apply: apply}
		c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		waitStatus(t, c, func(s Status) bool { return s.Connected })
		commandCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		result, err := c.SetCooling(commandCtx, cfg)
		if err != nil || !result.Confirmed || result.Active != apply {
			t.Fatalf("saved/active conflated: %+v %v", result, err)
		}
		if err := c.SetCoolingSource(commandCtx, SourceGPU); err != nil {
			t.Fatal(err)
		}
		source, err := c.GetCoolingSource(commandCtx)
		if err != nil || source != SourceGPU {
			t.Fatal(source, err)
		}
		stop()
		cancel()
		<-done
	}
}

func TestCurveTransientReadbackDoesNotConfirmActivation(t *testing.T) {
	d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, current: []byte{0, 0, 1, 0xb0, 4}, transient: true}
	c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	commandCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 1, Points: []CurvePoint{{20, 1800}, {40, 1800}, {60, 1800}, {80, 1800}}})
	if err != nil || !result.Confirmed || result.Active || !strings.Contains(result.Message, "回退") {
		t.Fatalf("transient curve was confirmed: %+v %v", result, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.currentReads != 2 {
		t.Fatal("did not observe current configuration twice", d.currentReads)
	}
}

func TestCurveObservationCancellationKeepsSavedResult(t *testing.T) {
	d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, current: []byte{0, 0, 1, 0xb0, 4}, apply: true}
	c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	commandCtx, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 1, Points: []CurvePoint{{20, 1800}, {40, 1800}, {60, 1800}, {80, 1800}}})
	if !errors.Is(err, context.DeadlineExceeded) || !result.Confirmed || result.Active {
		t.Fatalf("canceled observation lost save or confirmed activity: %+v %v", result, err)
	}
}

func TestCoolingStalePresetCannotConfirmSave(t *testing.T) {
	d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, preset: []byte{0, 0, 1, 0xb0, 4}, current: []byte{0, 0, 1, 0xb0, 4}, stale: true}
	c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	commandCtx, stop := context.WithTimeout(ctx, 25*time.Millisecond)
	defer stop()
	result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 1, RPM: 1800})
	if err == nil || result.Confirmed {
		t.Fatalf("stale slot confirmed: %+v %v", result, err)
	}
}

func TestCoolingLostReadRetriesWithoutRewritingConfiguration(t *testing.T) {
	d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, current: []byte{0, 0, 1, 0xb0, 4}, apply: true, dropCurrent: true}
	c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	commandCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 3, RPM: 2912})
	if err != nil || !result.Confirmed || !result.Active {
		t.Fatalf("lost read wasn't recovered: %+v %v", result, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	writes := 0
	for _, report := range d.writes {
		frame, _ := ParseReport(report)
		if frame.Command == 0x24 {
			writes++
		}
	}
	if writes != 1 || d.currentReads != 2 {
		t.Fatalf("writes=%d reads=%d", writes, d.currentReads)
	}
}

func TestCoolingLostPresetReadRetriesWithoutRewritingConfiguration(t *testing.T) {
	d := &coolingTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, current: []byte{0, 0, 1, 0xb0, 4}, apply: true, dropPreset: true}
	c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "test", nil }, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitStatus(t, c, func(s Status) bool { return s.Connected })
	commandCtx, stop := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer stop()
	result, err := c.SetCooling(commandCtx, CoolingConfig{Slot: 3, RPM: 2974})
	if err != nil || !result.Confirmed || !result.Active {
		t.Fatalf("lost preset read wasn't recovered: %+v %v", result, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	writes := 0
	for _, report := range d.writes {
		if report[3] == 0x24 {
			writes++
		}
	}
	if writes != 1 || d.presetReads != 2 {
		t.Fatalf("writes=%d preset reads=%d", writes, d.presetReads)
	}
}
