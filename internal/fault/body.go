package fault

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"faultline/internal/config"
)

var ErrTruncated = errors.New("body truncated by fault")

// Body applies faults with bounded reads. Close closes the source and joins active
// reads; callers cancel the context to interrupt pacing.
type Body struct {
	source    io.ReadCloser
	ctx       context.Context
	action    config.Fault
	remaining int
	onApplied func()
	mu        sync.Mutex
}

func WrapBody(ctx context.Context, source io.ReadCloser, action config.Fault, onApplied func()) *Body {
	b := &Body{source: source, ctx: ctx, action: action, onApplied: onApplied}
	if action.Bytes != nil {
		b.remaining = *action.Bytes
	}
	return b
}

func (b *Body) Close() error {
	err := b.source.Close()
	b.mu.Lock()
	b.mu.Unlock()
	return err
}

func (b *Body) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.action.Action == "truncate" {
		if b.remaining == 0 {
			var probe [1]byte
			n, err := b.source.Read(probe[:])
			if n > 0 {
				b.onApplied()
				return 0, ErrTruncated
			}
			return 0, err
		}
		n, err := b.source.Read(p[:min(len(p), b.remaining)])
		b.remaining -= n
		return n, err
	}
	rate := *b.action.BytesPerSecond
	chunk := min(16<<10, max(1, rate/10))
	n, err := b.source.Read(p[:min(len(p), chunk)])
	if n > 0 {
		b.onApplied()
		duration := max(time.Nanosecond, time.Duration(float64(n)/float64(rate)*float64(time.Second)))
		if waitErr := wait(b.ctx, duration); waitErr != nil {
			return 0, waitErr
		}
	}
	return n, err
}
