package brb02

import (
	"context"
	"fmt"
)

// SceneModeReport reproduces EXE 0x56ed97..0x56eda4 and 0x540224..0x54024d.
// The parameter is (~index)>>31: 1 for an active scene, 0 for default mode.
// It is a boolean, not a scene index. RGB/cooling are sent separately first.
func SceneModeReport(enabled bool) ([]byte, error) {
	value := byte(0)
	if enabled {
		value = 1
	}
	packet, _ := BuildPacket(0x08, []byte{value})
	return PacketReport(packet)
}

type SceneModeResult struct {
	Enabled      bool   `json:"enabled"`
	Acknowledged bool   `json:"acknowledged"`
	Message      string `json:"message"`
}

// SetSceneMode changes the device's scene flag. No query for this flag has
// been recovered; success establishes acceptance, not state read-back.
func (c *Controller) SetSceneMode(ctx context.Context, enabled bool) (SceneModeResult, error) {
	report, _ := SceneModeReport(enabled)
	f, err := c.exchange(ctx, report)
	if err != nil {
		return SceneModeResult{}, err
	}
	if len(f.Payload) != 1 || f.Payload[0] != 0 {
		return SceneModeResult{}, fmt.Errorf("场景模式未返回成功响应: %x", f.Payload)
	}
	return SceneModeResult{enabled, true, "设备已接受场景模式；该标志没有读回接口"}, nil
}
