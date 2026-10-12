package brb02

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

func screenInfoPacket(data []byte) []byte {
	p := make([]byte, 11)
	binary.LittleEndian.PutUint32(p, 1791287487)
	binary.LittleEndian.PutUint32(p[4:], uint32(len(data)))
	binary.LittleEndian.PutUint16(p[8:], ScreenCRC16(data))
	f, _ := BuildPacket(0xc5, p)
	return f
}

func TestScreenMetadataAndCRC(t *testing.T) {
	p := []byte{0xbf, 0xe0, 0xc4, 0x6a, 0xd0, 0xda, 1, 0, 0x1b, 0xe8, 0}
	info, err := (Frame{Command: 0xc5, Payload: p}).ScreenImageInfo()
	if err != nil || info.Timestamp != 1791287487 || info.Size != 121552 || info.CRC16 != 0xe81b {
		t.Fatalf("%+v %v", info, err)
	}
	if ScreenCRC16([]byte("123456789")) != 0x31c3 || ScreenCRC16(nil) != 0 {
		t.Fatal("CRC16/XMODEM test vector mismatch")
	}
	for _, f := range []Frame{{Command: 0xc5, Payload: []byte{0}}, {Command: 0xc4, Payload: p}, {Command: 0xc5, Long: true, Payload: p}} {
		if _, err := f.ScreenImageInfo(); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	binary.LittleEndian.PutUint32(p[4:], MaxScreenImageSize+1)
	if _, err := (Frame{Command: 0xc5, Payload: p}).ScreenImageInfo(); err == nil {
		t.Fatal("unbounded allocation accepted")
	}
}

func TestLongFrameBothTransportsAndMixedDecoder(t *testing.T) {
	p, err := BuildLongPacket(0xc7, 2, true, []byte{0xa5, 0xa4, 0, 0xff})
	if err != nil || !bytes.Equal(p[:5], []byte{0xa4, 10, 2, 0x80, 0xc7}) {
		t.Fatalf("%x %v", p, err)
	}
	r, err := PacketReport(p)
	if err != nil || len(r) != 65 || r[0] != 0 {
		t.Fatalf("%x %v", r, err)
	}
	wire, err := reportPacket(r)
	if err != nil || !bytes.Equal(wire, p) {
		t.Fatalf("BLE wire: %x %v", wire, err)
	}
	f, err := ParseReport(r)
	if err != nil || !f.Long || !f.First || f.Remaining != 2 || !bytes.Equal(f.Payload, p[5:9]) {
		t.Fatalf("%+v %v", f, err)
	}
	short, _ := BuildPacket(6, []byte{0x64, 0x05, 0})
	var d Decoder
	if got := d.Feed(append([]byte{0xff, 0, 0xa4, 3}, p[:3]...)); len(got) != 0 {
		t.Fatal(got)
	}
	got := d.Feed(append(p[3:], short...))
	if len(got) != 2 || !got[0].Long || got[1].Long || got[1].Command != 6 {
		t.Fatalf("%+v", got)
	}
	p[len(p)-1]++
	if _, err := PacketReport(p); err == nil {
		t.Fatal("bad TX checksum accepted")
	}
	for _, count := range []uint16{0, 0x8000} {
		if _, err := BuildLongPacket(0xc7, count, true, nil); err == nil {
			t.Fatal("bad fragment count accepted")
		}
	}
}

type screenTransport struct {
	fakeTransport
	data                                                   []byte
	packets                                                [][]byte
	index                                                  int
	badOrder, badCRC, shortACK, refuse, silent, disconnect bool
}

func (d *screenTransport) Write(r []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, append([]byte{}, r...))
	if r[3] == 0xc5 {
		d.reads <- screenInfoPacket(d.data)
	}
	if r[3] == 0xc7 || r[3] == screenImageDataCommand {
		if r[2] == 4 && d.silent {
			return len(r), nil
		}
		if r[2] == 4 && d.disconnect {
			d.reads <- nil
			return len(r), nil
		}
		if d.shortACK && r[2] == 5 {
			return 1, nil
		}
		if r[2] == 4 {
			if d.refuse {
				p, _ := BuildPacket(0xc7, []byte{1})
				d.reads <- p
				return len(r), nil
			}
			count := (len(d.data) + LongChunkSize - 1) / LongChunkSize
			for i := 0; i < count; i++ {
				data := append([]byte{}, d.data[i*LongChunkSize:min((i+1)*LongChunkSize, len(d.data))]...)
				if d.badCRC && i == 0 {
					data[0] ^= 1
				}
				remaining := count - i
				if d.badOrder && i == 1 {
					remaining++
				}
				p, _ := BuildLongPacket(screenImageDataCommand, uint16(remaining), i == 0, data)
				p[len(p)-1]++ // RX sum may be wrong; the full image CRC must match.
				d.packets = append(d.packets, p)
			}
		}
		if d.index < len(d.packets) {
			d.reads <- d.packets[d.index]
			d.index++
		}
	}
	return len(r), nil
}

func TestScreenBackupTransaction(t *testing.T) {
	for _, variant := range []string{"normal", "one-fragment", "empty", "bad-order", "bad-crc", "short-ack", "refused"} {
		t.Run(variant, func(t *testing.T) {
			data := bytes.Repeat([]byte{0xa4, 0xa5, 0xff}, 51)
			if variant == "one-fragment" {
				data = []byte{1, 2}
			}
			if variant == "empty" {
				data = nil
			}
			d := &screenTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, data: data, badOrder: variant == "bad-order", badCRC: variant == "bad-crc", shortACK: variant == "short-ack", refuse: variant == "refused"}
			c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "ble://test", nil }, false)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { c.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			waitStatus(t, c, func(s Status) bool { return s.Connected })
			queryCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			image, err := c.GetScreenImage(queryCtx)
			success := variant == "normal" || variant == "one-fragment" || variant == "empty"
			if success && (err != nil || !bytes.Equal(image.Data, data)) {
				t.Fatalf("%+v %v", image.Info, err)
			}
			if !success && (err == nil || image.Data != nil) {
				t.Fatalf("corrupt/partial backup accepted: %v", err)
			}
			if variant == "short-ack" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("%v", err)
			}
			if success {
				d.mu.Lock()
				defer d.mu.Unlock()
				want := 1
				if len(data) > 0 {
					want += 1 + (len(data)+57)/58
				}
				if len(d.writes) != want {
					t.Fatalf("writes %d != %d", len(d.writes), want)
				}
				for _, r := range d.writes[min(2, len(d.writes)):] {
					if r[3] != screenImageDataCommand || r[4] != 0 || r[2] != 5 {
						t.Fatalf("unexpected ACK: %x", r)
					}
				}
			}
		})
	}
}

func TestScreenBackupCancellationAndDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		d := &screenTransport{fakeTransport: fakeTransport{reads: make(chan []byte, 8)}, data: []byte{1, 2}, silent: !disconnect, disconnect: disconnect}
		c := NewWithConnector(func(context.Context) (Transport, string, error) { return d, "ble://test", nil }, false)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		waitStatus(t, c, func(s Status) bool { return s.Connected })
		queryCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
		image, err := c.GetScreenImage(queryCtx)
		stop()
		if err == nil || image.Data != nil {
			t.Fatalf("partial image accepted: %v", err)
		}
		if !disconnect && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%v", err)
		}
		c.mu.Lock()
		pending := c.screenRead
		c.mu.Unlock()
		if pending != nil {
			t.Fatal("cancelled transfer retained")
		}
		// An unsolicited late frame must not elicit a host acknowledgement.
		c.handle(Frame{Long: true, First: true, Remaining: 1, Command: screenImageDataCommand, Payload: d.data})
		d.mu.Lock()
		writes := len(d.writes)
		d.mu.Unlock()
		if writes != 2 {
			t.Fatalf("late fragment acknowledged: %d writes", writes)
		}
		cancel()
		<-done
	}
}
