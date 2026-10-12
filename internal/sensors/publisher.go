package sensors

import (
	"context"
	"sync"
	"time"

	"sharkcool-linux/brb02"
)

type PushStatus struct {
	Enabled  bool             `json:"enabled"`
	Values   brb02.SystemInfo `json:"values"`
	LastSent int64            `json:"lastSent"`
	Error    string           `json:"error"`
}

// Publisher is application policy, separate from the portable driver. It
// keeps collecting available readings, but sends only while explicitly enabled.
type Publisher struct {
	mu         sync.Mutex
	status     PushStatus
	generation uint64
	wake       chan struct{}
	read       func(context.Context) brb02.SystemInfo
	send       func(context.Context, brb02.SystemInfo) (brb02.Delivery, error)
}

func NewPublisher(c *brb02.Controller, read func(context.Context) brb02.SystemInfo) *Publisher {
	return &Publisher{read: read, send: c.SendSystemInfo, wake: make(chan struct{}, 1)}
}

func (p *Publisher) Snapshot() PushStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Values = brb02.SystemInfo{}
	for metric, value := range p.status.Values {
		s.Values[metric] = value
	}
	return s
}

func (p *Publisher) SetEnabled(enabled bool) {
	p.mu.Lock()
	p.status.Enabled = enabled
	p.status.Error = ""
	p.generation++
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Publisher) sample(ctx context.Context) {
	p.mu.Lock()
	generation := p.generation
	p.mu.Unlock()
	values := p.read(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.Values = values
	if !p.status.Enabled || p.generation != generation || ctx.Err() != nil {
		return
	}
	// Holding mu means SetEnabled(false) returns only after any in-flight write.
	sendCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.send(sendCtx, values)
	if err != nil {
		p.status.Error = err.Error()
		return
	}
	p.status.Error = ""
	p.status.LastSent = time.Now().UnixMilli()
}

func (p *Publisher) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		p.sample(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-p.wake:
		}
	}
}
