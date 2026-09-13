package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"faultline/internal/control"
)

const DefaultBuffer = 1024

type Event struct {
	control.Info
	Type             string    `json:"type"`
	Time             time.Time `json:"time"`
	FlowID           string    `json:"flow_id,omitempty"`
	ProxyID          string    `json:"proxy_id,omitempty"`
	Protocol         string    `json:"protocol,omitempty"`
	StartedAt        time.Time `json:"started_at,omitzero"`
	FinishedAt       time.Time `json:"finished_at,omitzero"`
	RuleID           string    `json:"rule_id,omitempty"`
	EligibleSequence uint64    `json:"eligible_sequence,omitempty"`
	Selector         string    `json:"selector,omitempty"`
	Probability      *float64  `json:"probability,omitempty"`
	Nth              *uint64   `json:"nth,omitempty"`
	Every            *uint64   `json:"every,omitempty"`
	Selected         bool      `json:"selected"`
	Reached          bool      `json:"reached"`
	Applied          bool      `json:"applied"`
	NotReached       bool      `json:"not_reached"`
	Phase            string    `json:"phase,omitempty"`
	Action           string    `json:"action,omitempty"`
	Outcome          string    `json:"outcome,omitempty"`
	ErrorKind        string    `json:"error_kind,omitempty"`
	UpstreamStatus   int       `json:"upstream_status,omitempty"`
	Operation        string    `json:"operation,omitempty"`
	Changed          bool      `json:"changed"`
	Counters         *Counters `json:"counters,omitempty"`
}

type Counters struct {
	Total        uint64 `json:"total"`
	Eligible     uint64 `json:"eligible"`
	Selected     uint64 `json:"selected"`
	Applied      uint64 `json:"applied"`
	Active       uint64 `json:"active"`
	ActiveFaults uint64 `json:"active_fault_flows"`
	Dropped      uint64 `json:"dropped_events"`
	WriteErrors  uint64 `json:"write_errors"`
	Written      uint64 `json:"written_events"`
	Pending      uint64 `json:"pending_events"`
}

type Recorder struct {
	mu     sync.Mutex
	counts Counters
	queue  chan Event
	done   chan struct{}
	abort  chan struct{}
	once   sync.Once
	closed bool
}

func New(output io.Writer, capacity int) *Recorder {
	r := &Recorder{queue: make(chan Event, capacity), done: make(chan struct{}), abort: make(chan struct{})}
	go r.write(output)
	return r
}

// Record updates run counters independently of whether the event fits in the queue.
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch e.Type {
	case "flow_started":
		r.counts.Total++
		r.counts.Active++
	case "decision":
		if e.RuleID != "" {
			r.counts.Eligible++
		}
		if e.Selected {
			r.counts.Selected++
		}
	case "fault_applied":
		r.counts.Applied++
		r.counts.ActiveFaults++
	case "flow_finished":
		r.counts.Active--
		if e.Applied {
			r.counts.ActiveFaults--
		}
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if !r.closed {
		select {
		case r.queue <- e:
			r.counts.Pending++
			return
		default:
		}
	}
	r.counts.Dropped++
}

func (r *Recorder) Counters() Counters {
	if r == nil {
		return Counters{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

func (r *Recorder) write(output io.Writer) {
	defer close(r.done)
	encoder := json.NewEncoder(output)
	for e := range r.queue {
		select {
		case <-r.abort:
			return
		default:
		}
		err := encoder.Encode(e)
		r.mu.Lock()
		r.counts.Pending--
		if err != nil {
			r.counts.Dropped++
			r.counts.WriteErrors++
		} else {
			r.counts.Written++
		}
		r.mu.Unlock()
	}
}

// Close bounds flushing, even when the sink blocks. A blocked io.Writer cannot be forcibly interrupted.
func (r *Recorder) Close(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		if r.Counters().Pending != 0 {
			return errors.New("recorder stopped with unflushed events")
		}
		return nil
	case <-ctx.Done():
		r.once.Do(func() { close(r.abort) })
		return ctx.Err()
	}
}
