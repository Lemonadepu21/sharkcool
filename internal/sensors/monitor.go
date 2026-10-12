package sensors

import (
	"context"
	"sync"
	"time"
)

// Monitor belongs to the application; the device driver never starts host
// processes or reads host sensors. Each platform supplies its own reader.
type Monitor struct {
	mu    sync.Mutex
	temps map[string]int64
	read  func(context.Context) map[string]int64
}

func NewMonitor(read func(context.Context) map[string]int64) *Monitor {
	return &Monitor{read: read}
}

func (m *Monitor) Snapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := map[string]int64{}
	for k, v := range m.temps {
		values[k] = v
	}
	return values
}

func (m *Monitor) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		values := m.read(ctx)
		m.mu.Lock()
		m.temps = values
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
