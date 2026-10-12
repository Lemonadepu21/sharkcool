package brb02

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestOfficialHostEventPacketsAndNoReplyDelivery(t *testing.T) {
	report, err := AudioLevelReport(3)
	if err != nil || !bytes.Equal(report[1:6], []byte{0xa5, 5, 0x15, 3, 0xc2}) {
		t.Fatal(report, err)
	}
	if _, err := AudioLevelReport(4); err == nil {
		t.Fatal("accepted unsupported audio level")
	}
	report = InputActivityReport()
	if !bytes.Equal(report[1:5], []byte{0xa5, 4, 0x16, 0xbf}) {
		t.Fatal(report)
	}
	d := &fakeTransport{reads: make(chan []byte, 1)}
	c := NewWithConnector(nil, false)
	c.dev = d
	result, err := c.SendAudioLevel(context.Background(), 0)
	if err != nil || !result.Sent {
		t.Fatal(result, err)
	}
	result, err = c.SendInputActivity(context.Background())
	if err != nil || !result.Sent || len(d.writes) != 2 {
		t.Fatal(result, err)
	}
	d.short = true
	result, err = c.SendAudioLevel(context.Background(), 1)
	if !errors.Is(err, io.ErrShortWrite) || result.Sent {
		t.Fatal("short event write confirmed", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.SendInputActivity(ctx); !errors.Is(err, context.Canceled) || len(d.writes) != 3 {
		t.Fatal("canceled activity wrote", err)
	}
	if _, err := NewWithConnector(nil, true).SendAudioLevel(context.Background(), 0); err == nil {
		t.Fatal("demo claimed audio delivery")
	}
}

func TestRGBPresetSelectorPacking(t *testing.T) {
	report, err := RGBPresetReport(8, 2)
	if err != nil || !bytes.Equal(report[1:6], []byte{0xa5, 5, 0x14, 0x28, 0xe6}) {
		t.Fatal(report, err)
	}
	for _, selector := range [][2]byte{{0, 0}, {9, 0}, {1, 16}} {
		if _, err := RGBPresetReport(selector[0], selector[1]); err == nil {
			t.Fatal("accepted invalid selector", selector)
		}
	}
}
