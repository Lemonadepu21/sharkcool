package sensors

import (
	"context"
	"fmt"
	"testing"

	"sharkcool-linux/brb02"
)

func TestPublisherEnableDisableReconnectAndSnapshot(t *testing.T) {
	writes := 0
	fail := false
	p := &Publisher{wake: make(chan struct{}, 1)}
	p.read = func(context.Context) brb02.SystemInfo { return brb02.SystemInfo{brb02.CPUTemperature: 35} }
	p.send = func(context.Context, brb02.SystemInfo) (brb02.Delivery, error) {
		writes++
		if fail {
			return brb02.Delivery{}, fmt.Errorf("disconnected")
		}
		return brb02.Delivery{Sent: true}, nil
	}
	p.sample(context.Background())
	if writes != 0 || p.Snapshot().Values[brb02.CPUTemperature] != 35 {
		t.Fatal("sent while disabled")
	}
	p.SetEnabled(true)
	fail = true
	p.sample(context.Background())
	if p.Snapshot().Error == "" || p.Snapshot().LastSent != 0 {
		t.Fatal("failed write reported as sent")
	}
	fail = false
	p.sample(context.Background())
	if p.Snapshot().Error != "" || p.Snapshot().LastSent == 0 {
		t.Fatal("did not resume after reconnect")
	}
	s := p.Snapshot()
	s.Values[brb02.CPUTemperature] = 99
	if p.Snapshot().Values[brb02.CPUTemperature] != 35 {
		t.Fatal("mutable snapshot")
	}
	p.SetEnabled(false)
	p.sample(context.Background())
	if writes != 2 {
		t.Fatal("sent after disable")
	}
	p.SetEnabled(true)
	p.read = func(context.Context) brb02.SystemInfo {
		p.SetEnabled(false)
		return brb02.SystemInfo{brb02.CPUTemperature: 36}
	}
	p.sample(context.Background())
	if writes != 2 {
		t.Fatal("enabled state changed during collection but sample sent")
	}
}
