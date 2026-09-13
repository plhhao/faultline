package fault

import (
	"context"
	"fmt"
	"time"

	"faultline/internal/config"
)

// Builtin executes actions from validated configuration using the adapter's capabilities.
type Builtin struct{}

func (Builtin) Execute(ctx context.Context, action config.Fault, flow Capabilities) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch action.Action {
	case "delay":
		return true, wait(ctx, *action.Duration)
	case "respond":
		return true, flow.Respond(*action.Status, *action.Body)
	case "close_connection":
		flow.CancelUpstream()
		err := flow.CloseConnection()
		return err == nil, err
	case "hold_request", "hold_response":
		flow.CancelUpstream()
		err := wait(ctx, *action.MaxDuration)
		closeErr := flow.CloseConnection()
		if err != nil {
			return true, err
		}
		return true, closeErr
	default:
		return false, fmt.Errorf("unsupported fault action %q", action.Action)
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
