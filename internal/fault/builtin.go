package fault

import (
	"context"
	"fmt"
	"time"

	"github.com/plhhao/faultline/internal/config"
)

// Builtin executes actions from validated configuration using the adapter's capabilities.
type Builtin struct{}

func (Builtin) Execute(ctx context.Context, action config.Fault, flow Capabilities) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch action.Action {
	case "delay":
		applied(flow)
		return true, wait(ctx, *action.Duration)
	case "respond":
		applied(flow)
		return true, flow.Respond(*action.Status, *action.Body)
	case "close_connection":
		flow.CancelUpstream()
		err := flow.CloseConnection()
		if err == nil {
			applied(flow)
		}
		return err == nil, err
	case "hold_request", "hold_response":
		flow.CancelUpstream()
		applied(flow)
		err := wait(ctx, *action.MaxDuration)
		var closeErr error
		if stream, ok := flow.(interface{ EndStream() error }); ok {
			closeErr = stream.EndStream()
		} else {
			closeErr = flow.CloseConnection()
		}
		if err != nil {
			return true, err
		}
		return true, closeErr
	default:
		return false, fmt.Errorf("unsupported fault action %q", action.Action)
	}
}

func applied(flow Capabilities) {
	if observer, ok := flow.(ApplicationObserver); ok {
		observer.FaultApplied()
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
