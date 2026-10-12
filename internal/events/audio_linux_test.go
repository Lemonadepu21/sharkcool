//go:build linux

package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func stereoFrame(left, right int16) []byte {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint16(p, uint16(left))
	binary.LittleEndian.PutUint16(p[2:], uint16(right))
	return p
}

func TestAudioMagnitudeBoundariesAndSignedPCM(t *testing.T) {
	for _, tc := range []struct {
		left, right int16
		level       byte
	}{{0, 0, 0}, {19, 19, 0}, {20, 20, 1}, {-249, -249, 1}, {-250, -250, 2}, {3999, 3999, 2}, {-4000, -4000, 3}, {-32768, -32768, 3}, {10000, -10000, 0}, {40, 0, 1}} {
		level, err := AudioLevelStereo16(stereoFrame(tc.left, tc.right))
		if err != nil || level != tc.level {
			t.Fatalf("%+v: %d %v", tc, level, err)
		}
	}
	for _, pcm := range [][]byte{nil, {0}, {0, 0}, {0, 0, 0, 0, 0}} {
		if _, err := AudioLevelStereo16(pcm); err == nil {
			t.Fatal("accepted partial stereo frame")
		}
	}
}

func TestAudioStreamEmitsOnlyCompleteBlocksAndStopsOnSinkFailure(t *testing.T) {
	pcm := append(make([]byte, 6400), bytes.Repeat(stereoFrame(8000, 8000), 1600)...)
	pcm = append(pcm, stereoFrame(200, 200)...)
	var levels []byte
	err := consumeAudio(context.Background(), bytes.NewReader(pcm), func(level byte) error {
		levels = append(levels, level)
		return nil
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) || !bytes.Equal(levels, []byte{0, 3}) {
		t.Fatal(levels, err)
	}
	want := errors.New("device disappeared")
	calls := 0
	err = consumeAudio(context.Background(), bytes.NewReader(pcm), func(byte) error { calls++; return want })
	if !errors.Is(err, want) || calls != 1 {
		t.Fatal("continued sending after failed sink", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumeAudio(ctx, bytes.NewReader(pcm), func(byte) error { t.Fatal("canceled capture emitted"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
