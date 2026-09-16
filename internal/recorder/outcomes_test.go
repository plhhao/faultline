package recorder

import (
	"context"
	"io"
	"testing"
)

func TestOutcomesSurviveDroppedEventsAndReturnCopies(t *testing.T) {
	r := New(io.Discard, 1)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Record(Event{Type: "flow_started"})
	r.Record(Event{Type: "flow_finished", Outcome: "client_canceled"})
	outcomes := r.Outcomes()
	if outcomes["client_canceled"] != 1 || r.Counters().Dropped != 2 {
		t.Fatal("dropped outcome not counted")
	}
	outcomes["client_canceled"] = 99
	if r.Outcomes()["client_canceled"] != 1 {
		t.Fatal("mutable outcome counters")
	}
}
