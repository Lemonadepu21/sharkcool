//go:build linux

package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
	"time"
)

// AudioLevelStereo16 reduces a 100 ms stereo PCM block to four levels.
// Linux uses RMS amplitude; the official Windows producer also performs
// frequency analysis. Only the on-wire level range and thresholds are shared.
func AudioLevelStereo16(pcm []byte) (byte, error) {
	if len(pcm) == 0 || len(pcm)%4 != 0 {
		return 0, fmt.Errorf("需要完整的 s16le 双声道采样")
	}
	var energy float64
	for i := 0; i < len(pcm); i += 4 {
		left := int16(binary.LittleEndian.Uint16(pcm[i:]))
		right := int16(binary.LittleEndian.Uint16(pcm[i+2:]))
		mono := (float64(left) + float64(right)) / 2
		energy += mono * mono
	}
	rms := math.Sqrt(energy / float64(len(pcm)/4))
	switch {
	case rms < 20:
		return 0, nil
	case rms < 250:
		return 1, nil
	case rms < 4000:
		return 2, nil
	default:
		return 3, nil
	}
}

// CaptureAudio captures the default playback monitor, never a microphone.
// No recording is saved; each PCM block is discarded after emitting a level.
// PulseAudio or PipeWire's PulseAudio compatibility server supplies parec.
func CaptureAudio(ctx context.Context, emit func(byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "parec", "--device=@DEFAULT_MONITOR@", "--format=s16le", "--channels=2", "--rate=16000", "--raw", "--latency-msec=100")
	cmd.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &limitedLog{buffer: &stderr}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("音频采集需要 parec 和 PulseAudio/PipeWire 播放监测接口: %w", err)
	}
	streamErr := consumeAudio(childCtx, out, emit)
	cancel()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if streamErr != nil && streamErr != io.EOF && streamErr != io.ErrUnexpectedEOF {
		return streamErr
	}
	return fmt.Errorf("播放音频监测流已结束: %v %s", waitErr, strings.TrimSpace(stderr.String()))
}

func consumeAudio(ctx context.Context, reader io.Reader, emit func(byte) error) error {
	pcm := make([]byte, 1600*4) // 16 kHz, stereo s16le, 100 ms.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.ReadFull(reader, pcm); err != nil {
			return err
		}
		level, _ := AudioLevelStereo16(pcm)
		if err := emit(level); err != nil {
			return err
		}
	}
}

type limitedLog struct{ buffer *bytes.Buffer }

func (l *limitedLog) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 4096 - l.buffer.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = l.buffer.Write(p)
	}
	return n, nil
}
