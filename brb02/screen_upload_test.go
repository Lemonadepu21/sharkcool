package brb02

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestScreenPixelOrderAndCenterCrop(t *testing.T) {
	for _, sample := range []struct {
		color color.RGBA
		pixel uint16
	}{
		{color.RGBA{255, 0, 0, 255}, 0xf800}, {color.RGBA{0, 255, 0, 255}, 0x07e0},
		{color.RGBA{0, 0, 255, 255}, 0x001f}, {color.RGBA{255, 255, 255, 255}, 0xffff},
	} {
		data, err := EncodeScreenImage(&image.Uniform{C: sample.color})
		if err != nil || len(data) != ScreenImageBytes || binary.BigEndian.Uint16(data) != sample.pixel {
			t.Fatalf("%v %v", sample, err)
		}
		decoded, err := DecodeScreenImage(data)
		if err != nil || decoded.RGBAAt(0, 0) != sample.color {
			t.Fatalf("decoded %v %v", decoded.RGBAAt(0, 0), err)
		}
	}
	source := image.NewRGBA(image.Rect(10, 20, 438, 446))
	for y := 162; y < 304; y++ {
		for x := 10; x < 438; x++ {
			source.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	data, err := EncodeScreenImage(source)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(data); i += 2 {
		if binary.BigEndian.Uint16(data[i:]) != 0xf800 {
			t.Fatal("center crop changed aspect ratio or bounds")
		}
	}
}

type screenUploadTransport struct {
	screenTransport
	received                                        []byte
	next                                            uint16
	refuseC4, refuseC6, wrongReadback, disconnectC6 bool
}

func (d *screenUploadTransport) Write(report []byte) (int, error) {
	f, err := ParseReport(report)
	if err != nil {
		return 0, err
	}
	if f.Command != 0xc4 && f.Command != 0xc6 {
		return d.screenTransport.Write(report)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, bytes.Clone(report))
	if f.Command == 0xc6 && d.disconnectC6 {
		d.reads <- nil
		return len(report), nil
	}
	status := byte(0)
	if f.Command == 0xc4 && d.refuseC4 || f.Command == 0xc6 && d.refuseC6 {
		status = 1
	}
	if f.Long && status == 0 {
		if f.First {
			d.received = nil
			d.next = f.Remaining
		}
		if d.next != f.Remaining {
			status = 1
		} else {
			d.received = append(d.received, f.Payload...)
			d.next--
			if d.next == 0 {
				d.data = bytes.Clone(d.received)
				if d.wrongReadback {
					d.data[0] ^= 1
				}
			}
		}
	}
	ack, _ := BuildPacket(f.Command, []byte{status})
	d.reads <- ack
	return len(report), nil
}

func TestScreenUploadRequiresPixelsReadback(t *testing.T) {
	for _, variant := range []string{"success", "refused-metadata", "refused-fragment", "wrong-pixels", "silent-readback", "cancelled", "disconnected"} {
		t.Run(variant, func(t *testing.T) {
			data := bytes.Repeat([]byte{0xa4, 0xa5}, ScreenImageBytes/2)
			d := &screenUploadTransport{screenTransport: screenTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, data: bytes.Clone(data), silent: variant == "silent-readback"}, refuseC4: variant == "refused-metadata", refuseC6: variant == "refused-fragment", wrongReadback: variant == "wrong-pixels", disconnectC6: variant == "disconnected"}
			c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "/dev/test", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected })
			deadline, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			if variant == "cancelled" {
				stop()
			}
			info := ScreenImageInfo{Timestamp: 1791287487, Size: uint32(len(data)), CRC16: ScreenCRC16(data)}
			progress := 0
			_, err := c.SetScreenImage(deadline, ScreenImage{info, data}, func(n, total int) {
				if n <= progress || total != len(data) {
					t.Error("non-monotonic progress")
				}
				progress = n
			})
			if variant == "success" && (err != nil || progress != len(data)) {
				t.Fatalf("%v %d", err, progress)
			}
			if variant != "success" && err == nil {
				t.Fatal("upload falsely confirmed")
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if variant == "cancelled" && len(d.writes) != 0 {
				t.Fatal("cancelled operation wrote to device")
			}
			if variant == "refused-metadata" && len(d.writes) != 1 {
				t.Fatal("pixels sent after metadata refusal")
			}
		})
	}
}
