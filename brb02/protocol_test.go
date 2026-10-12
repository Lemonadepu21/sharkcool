package brb02

import (
	"bytes"
	"testing"
)

func TestCapturedReports(t *testing.T) {
	for mode, hex := range map[int][]byte{2: {0, 0xa5, 9, 0x24, 0, 0, 1, 0x60, 0x0b, 0x3e}, 3: {0, 0xa5, 9, 0x24, 0, 0, 1, 0xce, 0x0d, 0xae}} {
		r, err := ModeReport(mode)
		if err != nil || len(r) != 65 || !bytes.Equal(r[:len(hex)], hex) {
			t.Fatalf("mode %d: %x %v", mode, r, err)
		}
		if !bytes.Equal(r[len(hex):], make([]byte, 65-len(hex))) {
			t.Fatal("nonzero padding")
		}
	}
	if _, err := ModeReport(0); err == nil {
		t.Fatal("unverified mode accepted")
	}
}

func TestHeartbeatFormatsAndMalformedInput(t *testing.T) {
	// Actual capture A5 07 06 06 09 01 C2, length includes header and checksum.
	input := []byte{0xa5, 7, 6, 6, 9, 1, 0xc2, 0, 0}
	for _, r := range [][]byte{input, append([]byte{0}, input...)} {
		f, err := ParseReport(r)
		if err != nil {
			t.Fatal(err)
		}
		rpm, field, ok := f.Heartbeat()
		if !ok || rpm != 2310 || field != 1 {
			t.Fatalf("%d %x %v", rpm, field, ok)
		}
	}
	for _, r := range [][]byte{nil, {0xa5}, {0xa5, 7, 6, 1}, {0xa5, 7, 62}, make([]byte, 66), {1, 0xa5, 7, 0}} {
		if _, err := ParseReport(r); err == nil {
			t.Fatalf("malformed report accepted: %x", r)
		}
	}
	ack := make([]byte, 64)
	copy(ack, []byte{0xa5, 5, 0x24, 0, 0xce})
	f, err := ParseReport(ack)
	if err != nil || !f.ConfigACK() {
		t.Fatal("checksum-valid ACK not recognized")
	}
	f.Payload[0] = 1
	if f.ConfigACK() {
		t.Fatal("negative response recognized as success")
	}
}

func FuzzParseReport(f *testing.F) {
	f.Add([]byte{0xa5, 7, 6, 6, 9, 1, 0xc2, 0, 0})
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, b []byte) {
		r, err := ParseReport(b)
		if err == nil {
			r.Heartbeat()
			r.ConfigACK()
		}
	})
}

func TestDecoderFragmentsMultipleFramesAndChecksum(t *testing.T) {
	heartbeat := []byte{0xa5, 7, 6, 6, 9, 1, 0xc2}
	ack := []byte{0xa5, 5, 0x24, 0, 0xce}
	var decoder Decoder
	if got := decoder.Feed(heartbeat[:3]); len(got) != 0 {
		t.Fatal("partial frame emitted")
	}
	tail := append(append([]byte{}, heartbeat[3:]...), ack...)
	tail = append(tail, make([]byte, 60)...)
	frames := decoder.Feed(tail)
	if len(frames) != 2 || !frames[1].ConfigACK() {
		t.Fatalf("%+v", frames)
	}
	bad := []byte{0xa5, 5, 0x24, 0, 0xe1}
	if f, err := ParseReport(bad); err != nil || f.ChecksumValid {
		t.Fatal("receive checksum must be reported independently")
	}
	frames = decoder.Feed(append(bad, heartbeat...))
	if len(frames) != 2 || frames[0].ChecksumValid || frames[1].Command != 6 {
		t.Fatalf("resynchronization failed: %+v", frames)
	}
}

func TestOfficialQueryAndSwitchPackets(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want []byte
	}{
		{"firmware", []byte{0xa5, 4, 1, 0xaa}},
		{"fan-switch", []byte{0xa5, 4, 0x21, 0xca}},
		{"lcd-switch", []byte{0xa5, 4, 0xc1, 0x6a}},
		{"rgb-switch", []byte{0xa5, 4, 0x11, 0xba}},
		{"screen-info", []byte{0xa5, 4, 0xc5, 0x6e}},
	} {
		report, err := QueryReport(tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := reportPacket(report)
		if err != nil || !bytes.Equal(packet, tc.want) {
			t.Fatalf("%s %x %v", tc.kind, packet, err)
		}
	}
	report, _ := SwitchReport("rgb", false)
	packet, _ := reportPacket(report)
	if !bytes.Equal(packet, []byte{0xa5, 5, 0x10, 0, 0xba}) {
		t.Fatalf("%x", packet)
	}
	if _, err := QueryReport("boot"); err == nil {
		t.Fatal("unknown query accepted")
	}
}

func TestPacketReportRejectsReportID(t *testing.T) {
	if _, err := PacketReport([]byte{0, 0xa5, 4, 1, 0xaa}); err == nil {
		t.Fatal("report accepted as packet")
	}
}

func TestActualFirmwareReplyAndStrictTransmitChecksum(t *testing.T) {
	packet := []byte{0xa5, 9, 1, 0x33, 0x2e, 0x30, 0x2e, 0x33, 0x6f}
	f, err := ParseReport(packet)
	if err != nil || f.ChecksumValid || string(f.Payload) != "3.0.3" {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err = PacketReport(packet); err == nil {
		t.Fatal("bad receive sum allowed for transmit")
	}
}
