package fault

import (
	"context"
	"errors"

	"faultline/internal/config"
)

var ErrUnavailable = errors.New("fault executor is not implemented")

// Capabilities belong to one flow and must not be retained after Execute returns.
type Capabilities interface {
	CancelUpstream()
	Respond(status int, body string) error
	CloseConnection() error
}

// Executor reports whether the action took effect, independently of its final error.
// Implementations must honor ctx cancellation and may be called concurrently.
type Executor interface {
	Execute(ctx context.Context, action config.Fault, flow Capabilities) (applied bool, err error)
}

// ApplicationObserver receives the first effect before a blocking action completes.
type ApplicationObserver interface {
	FaultApplied()
}
