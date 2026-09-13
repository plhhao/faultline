package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type blockedSink struct {
	bytes.Buffer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedSink) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.Buffer.Write(p)
}

func TestFullQueuePreservesCounters(t *testing.T) {
	sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
	r := New(sink, 1)
	r.Record(Event{Type: "control"})
	<-sink.entered
	r.Record(Event{Type: "flow_started"})
	r.Record(Event{Type: "decision", RuleID: "test", Selected: true})
	r.Record(Event{Type: "fault_applied"})
	if c := r.Counters(); c.Total != 1 || c.Eligible != 1 || c.Selected != 1 || c.Applied != 1 || c.Active != 1 || c.ActiveFaults != 1 || c.Dropped != 2 {
		t.Fatalf("%+v", c)
	}
	r.Record(Event{Type: "flow_finished", Applied: true})
	if c := r.Counters(); c.Active != 0 || c.ActiveFaults != 0 || c.Dropped != 3 {
		t.Fatalf("%+v", c)
	}
	close(sink.release)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := r.Counters(); c.Pending != 0 || c.Written != 2 {
		t.Fatalf("%+v", c)
	}
}

func TestBlockedSinkBoundedShutdown(t *testing.T) {
	sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
	r := New(sink, 1)
	t.Cleanup(func() { close(sink.release); <-r.done })
	r.Record(Event{Type: "control"})
	<-sink.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	r.Record(Event{Type: "control"})
	if c := r.Counters(); c.Pending != 1 || c.Dropped != 1 {
		t.Fatalf("%+v", c)
	}
}

type brokenSink struct{}

func (brokenSink) Write([]byte) (int, error) { return 0, errors.New("secret sink path") }

func TestWriteFailureIsCounted(t *testing.T) {
	r := New(brokenSink{}, 2)
	r.Record(Event{Type: "control"})
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := r.Counters(); c.WriteErrors != 1 || c.Dropped != 1 || c.Written != 0 || c.Pending != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestConcurrentLifecycleAccounting(t *testing.T) {
	var output bytes.Buffer
	r := New(&output, 2000)
	var workers sync.WaitGroup
	for range 100 {
		workers.Go(func() {
			r.Record(Event{Type: "flow_started"})
			r.Record(Event{Type: "decision", RuleID: "rule", Selected: true})
			r.Record(Event{Type: "fault_applied"})
			r.Record(Event{Type: "flow_finished", Applied: true})
		})
	}
	workers.Wait()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := r.Counters(); c.Total != 100 || c.Eligible != 100 || c.Selected != 100 || c.Applied != 100 || c.Active != 0 || c.ActiveFaults != 0 || c.Dropped != 0 || c.Written != 400 {
		t.Fatalf("%+v", c)
	}
	decoder := json.NewDecoder(&output)
	for range 400 {
		var e Event
		if err := decoder.Decode(&e); err != nil || e.Time.IsZero() {
			t.Fatalf("event=%+v err=%v", e, err)
		}
	}
	if err := decoder.Decode(new(Event)); err != io.EOF {
		t.Fatal(err)
	}
}
