package brb02

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const LongChunkSize = ReportSize - 6
const MaxScreenImageSize = LongChunkSize * 0x7fff
const screenImageDataCommand = 0xc8 // EXE jump table 0x54579c: C8 -> 0x545776.

// ScreenImageInfo follows EXE 0x56bd77..0x56bdc4: timestamp from
// QDateTime.toTime_t, file size, CRC16 and a reserved byte, all little endian.
// Image bytes use the separate A4 envelope. This is not a screen layout query.
type ScreenImageInfo struct {
	Timestamp uint32 `json:"timestamp"`
	Size      uint32 `json:"size"`
	CRC16     uint16 `json:"crc16"`
	Reserved  byte   `json:"reserved"`
}

func (f Frame) ScreenImageInfo() (ScreenImageInfo, error) {
	if f.Long || f.Command != 0xc5 || len(f.Payload) != 11 {
		return ScreenImageInfo{}, fmt.Errorf("无效的屏保信息响应: %x", f.Payload)
	}
	p := f.Payload
	info := ScreenImageInfo{binary.LittleEndian.Uint32(p), binary.LittleEndian.Uint32(p[4:]), binary.LittleEndian.Uint16(p[8:]), p[10]}
	if info.Size > MaxScreenImageSize {
		return ScreenImageInfo{}, fmt.Errorf("屏保图片超出协议分片上限: %d", info.Size)
	}
	return info, nil
}

func (c *Controller) GetScreenImageInfo(ctx context.Context) (ScreenImageInfo, error) {
	f, err := c.Query(ctx, "screen-info")
	if err != nil {
		return ScreenImageInfo{}, err
	}
	return f.ScreenImageInfo()
}

// ScreenCRC16 is the official polynomial 0x1021, initial value zero,
// no reflection or final XOR (EXE 0x544040..0x544157).
func ScreenCRC16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// BuildLongPacket creates a single official A4 fragment (EXE 0x544470).
// Remaining counts this fragment as well; the first fragment sets bit 15.
func BuildLongPacket(command byte, remaining uint16, first bool, payload []byte) ([]byte, error) {
	if remaining == 0 || remaining > 0x7fff || len(payload) > LongChunkSize {
		return nil, fmt.Errorf("无效的长帧分片数量或长度")
	}
	p := make([]byte, len(payload)+6)
	p[0], p[1], p[4] = 0xa4, byte(len(p)), command
	if first {
		remaining |= 0x8000
	}
	binary.LittleEndian.PutUint16(p[2:4], remaining)
	copy(p[5:], payload)
	p[len(p)-1] = checksum(p[:len(p)-1])
	return p, nil
}

type ScreenImage struct {
	Info ScreenImageInfo `json:"info"`
	Data []byte          `json:"-"`
}

type screenReadResult struct {
	data []byte
	err  error
}

type screenReadTransfer struct {
	ctx  context.Context
	info ScreenImageInfo
	next uint16
	data []byte
	done chan screenReadResult
}

func (t *screenReadTransfer) accept(f Frame) (bool, error) {
	if err := t.ctx.Err(); err != nil {
		return false, err
	}
	if f.First != (len(t.data) == 0) || f.Remaining != t.next {
		return false, fmt.Errorf("屏保分片顺序错误: first=%t remaining=%d，预期 %d", f.First, f.Remaining, t.next)
	}
	want := min(LongChunkSize, int(t.info.Size)-len(t.data))
	if len(f.Payload) != want || want == 0 {
		return false, fmt.Errorf("屏保分片长度错误: %d，预期 %d", len(f.Payload), want)
	}
	t.data = append(t.data, f.Payload...)
	t.next--
	if t.next != 0 {
		return false, nil
	}
	if got := ScreenCRC16(t.data); got != t.info.CRC16 {
		return false, fmt.Errorf("屏保图片 CRC16 不匹配: %04x，预期 %04x", got, t.info.CRC16)
	}
	return true, nil
}

// Called only by the controller's reader, while c.mu is held. Only an active
// C7 read can acknowledge C8 data fragments. No unsolicited long transfers.
func (c *Controller) handleScreenFragmentLocked(f Frame) {
	t := c.screenRead
	if t == nil || f.Command != screenImageDataCommand {
		return
	}
	complete, err := t.accept(f)
	if err == nil {
		// Official 0x541b5b sends A5 05 ORIGINAL_COMMAND 00 SUM per fragment.
		packet, _ := BuildPacket(screenImageDataCommand, []byte{0})
		report, _ := PacketReport(packet)
		n, writeErr := c.writeReportLocked(report)
		err = writeErr
		if err == nil && n != len(report) {
			err = io.ErrShortWrite
		}
	}
	if complete || err != nil {
		c.screenRead = nil
		t.done <- screenReadResult{data: t.data, err: err}
	}
}

// GetScreenImage reads and verifies a backup without writing image metadata or
// image bytes. It serializes C5 + C7 + fragment ACKs on one connection, bounds
// allocation by the recovered protocol limit, and rejects gaps/duplicates.
func (c *Controller) GetScreenImage(ctx context.Context) (ScreenImage, error) {
	if err := c.commandMu.Lock(ctx); err != nil {
		return ScreenImage{}, err
	}
	defer c.commandMu.Unlock()
	return c.getScreenImageLocked(ctx)
}

func (c *Controller) getScreenImageLocked(ctx context.Context) (ScreenImage, error) {
	c.mu.Lock()
	generation := c.lost
	c.mu.Unlock()
	query, _ := QueryReport("screen-info")
	f, err := c.exchangeLocked(ctx, query)
	if err != nil {
		return ScreenImage{}, err
	}
	info, err := f.ScreenImageInfo()
	if err != nil {
		return ScreenImage{}, err
	}
	if info.Size == 0 {
		if info.CRC16 != ScreenCRC16(nil) {
			return ScreenImage{}, fmt.Errorf("空屏保图片的 CRC16 无效")
		}
		return ScreenImage{Info: info}, nil
	}
	t := &screenReadTransfer{ctx: ctx, info: info, next: uint16((info.Size + LongChunkSize - 1) / LongChunkSize), data: make([]byte, 0, int(info.Size)), done: make(chan screenReadResult, 1)}
	packet, _ := BuildPacket(0xc7, nil)
	report, _ := PacketReport(packet)
	c.mu.Lock()
	dev, lost := c.dev, c.lost
	if dev == nil || lost != generation || c.demo || ctx.Err() != nil {
		c.mu.Unlock()
		return ScreenImage{}, fmt.Errorf("读取屏保需要有效连接与未取消的 context")
	}
	c.screenRead = t
	n, err := c.writeReportLocked(report)
	if err == nil && n != len(report) {
		err = io.ErrShortWrite
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.screenRead == t {
			c.screenRead = nil
		}
		c.mu.Unlock()
	}()
	if err != nil {
		return ScreenImage{}, err
	}
	select {
	case result := <-t.done:
		if result.err != nil {
			return ScreenImage{}, result.err
		}
		c.mu.Lock()
		same := c.lost == lost && c.dev != nil && c.linkError == nil
		c.mu.Unlock()
		if !same {
			return ScreenImage{}, fmt.Errorf("屏保读取期间设备已断开")
		}
		return ScreenImage{Info: info, Data: result.data}, nil
	case <-lost:
		return ScreenImage{}, fmt.Errorf("屏保读取期间设备已断开")
	case <-ctx.Done():
		c.mu.Lock()
		received := len(t.data)
		c.mu.Unlock()
		return ScreenImage{}, fmt.Errorf("屏保读取未完成（%d/%d 字节）: %w", received, info.Size, ctx.Err())
	}
}

// SetScreenImage serializes metadata, acknowledged C6 fragments, and a complete
// C7/C8 readback on one connection. Callers can persist GetScreenImage first.
// Progress runs outside c.mu; never retry a timed-out fragment using old ACKs.
func (c *Controller) SetScreenImage(ctx context.Context, image ScreenImage, progress func(int, int)) (ScreenImageInfo, error) {
	if len(image.Data) != ScreenImageBytes || image.Info.Size != uint32(len(image.Data)) || image.Info.CRC16 != ScreenCRC16(image.Data) || image.Info.Reserved != 0 {
		return ScreenImageInfo{}, fmt.Errorf("屏保需要有效的 428 × 142 RGB565 图片与校验信息")
	}
	// Own a stable copy throughout the transfer.
	image.Data = bytes.Clone(image.Data)
	if err := c.commandMu.Lock(ctx); err != nil {
		return ScreenImageInfo{}, err
	}
	defer c.commandMu.Unlock()
	c.mu.Lock()
	generation := c.lost
	c.mu.Unlock()
	checkConnection := func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.demo || c.dev == nil || c.lost != generation || c.linkError != nil {
			return fmt.Errorf("屏保上传期间连接已断开或更换")
		}
		return ctx.Err()
	}
	if err := checkConnection(); err != nil {
		return ScreenImageInfo{}, err
	}
	payload := make([]byte, 11)
	binary.LittleEndian.PutUint32(payload, image.Info.Timestamp)
	binary.LittleEndian.PutUint32(payload[4:], image.Info.Size)
	binary.LittleEndian.PutUint16(payload[8:], image.Info.CRC16)
	packet, _ := BuildPacket(0xc4, payload)
	report, _ := PacketReport(packet)
	ack := func(report []byte, command byte) error {
		if err := checkConnection(); err != nil {
			return err
		}
		deadline, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		f, err := c.exchangeCommandLocked(deadline, report, command)
		if err != nil {
			return err
		}
		if len(f.Payload) != 1 || f.Payload[0] != 0 {
			return fmt.Errorf("设备拒绝屏保传输（命令 %02X）", command)
		}
		return nil
	}
	if err := ack(report, 0xc4); err != nil {
		return ScreenImageInfo{}, fmt.Errorf("屏保准备失败: %w", err)
	}
	count := (len(image.Data) + LongChunkSize - 1) / LongChunkSize
	for i := 0; i < count; i++ {
		end := min((i+1)*LongChunkSize, len(image.Data))
		packet, _ := BuildLongPacket(0xc6, uint16(count-i), i == 0, image.Data[i*LongChunkSize:end])
		report, _ := PacketReport(packet)
		if err := ack(report, 0xc6); err != nil {
			return ScreenImageInfo{}, fmt.Errorf("屏保上传未完成（%d/%d 字节）: %w", i*LongChunkSize, len(image.Data), err)
		}
		if progress != nil {
			progress(end, len(image.Data))
		}
	}
	if err := checkConnection(); err != nil {
		return ScreenImageInfo{}, err
	}
	// Metadata alone was sent before the pixels: it cannot prove an upload.
	readback, err := c.getScreenImageLocked(ctx)
	if err != nil {
		return ScreenImageInfo{}, fmt.Errorf("屏保已发送，但完整图片读回失败: %w", err)
	}
	if readback.Info != image.Info || !bytes.Equal(readback.Data, image.Data) {
		return ScreenImageInfo{}, fmt.Errorf("屏保图片读回不匹配")
	}
	if err := checkConnection(); err != nil {
		return ScreenImageInfo{}, err
	}
	return readback.Info, nil
}
