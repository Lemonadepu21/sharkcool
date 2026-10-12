package brb02

import (
	"context"
	"sync"
)

// Waiting for a long image transfer must not outlive a caller's deadline.
// The zero value is ready to use, including for embedded SDK controllers.
type commandLock struct {
	once   sync.Once
	permit chan struct{}
}

func (m *commandLock) Lock(ctx context.Context) error {
	m.once.Do(func() { m.permit = make(chan struct{}, 1); m.permit <- struct{}{} })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.permit:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	}
}

func (m *commandLock) Unlock() { m.permit <- struct{}{} }
