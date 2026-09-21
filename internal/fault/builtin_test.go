package fault

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/plhhao/faultline/internal/config"
)

type capabilities struct {
	calls []string
	err   error
}

func (f *capabilities) CancelUpstream() { f.calls = append(f.calls, "cancel") }
func (f *capabilities) Respond(int, string) error {
	f.calls = append(f.calls, "respond")
	return f.err
}
func (f *capabilities) CloseConnection() error {
	f.calls = append(f.calls, "close")
	return f.err
}

func TestTimedActions(t *testing.T) {
	for _, action := range []string{"delay", "hold_request", "hold_response"} {
		for _, canceled := range []bool{false, true} {
			t.Run(action+"/"+map[bool]string{false: "complete", true: "cancel"}[canceled], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					duration := time.Hour
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if canceled {
						time.AfterFunc(time.Second, cancel)
					}
					f := &capabilities{}
					began := time.Now()
					applied, err := (Builtin{}).Execute(ctx, config.Fault{Action: action, Duration: &duration, MaxDuration: &duration}, f)
					wantDuration := duration
					var wantErr error
					if canceled {
						wantDuration, wantErr = time.Second, context.Canceled
					}
					if !applied || !errors.Is(err, wantErr) || time.Since(began) != wantDuration {
						t.Fatalf("applied=%t err=%v elapsed=%v", applied, err, time.Since(began))
					}
					var wantCalls []string
					if action != "delay" {
						wantCalls = []string{"cancel", "close"}
					}
					if !reflect.DeepEqual(f.calls, wantCalls) {
						t.Fatal(f.calls)
					}
				})
			})
		}
	}
}

func TestAlreadyCanceledDoesNotApply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, action := range []string{"delay", "respond", "close_connection", "hold_request", "hold_response"} {
		f := &capabilities{}
		applied, err := (Builtin{}).Execute(ctx, config.Fault{Action: action}, f)
		if applied || !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
			t.Fatalf("%s: applied=%t err=%v calls=%v", action, applied, err, f.calls)
		}
	}
}

func TestCapabilityErrors(t *testing.T) {
	failure := errors.New("I/O failed")
	status, body := 503, "unavailable"
	for _, action := range []string{"respond", "close_connection"} {
		f := &capabilities{err: failure}
		applied, err := (Builtin{}).Execute(context.Background(), config.Fault{Action: action, Status: &status, Body: &body}, f)
		if !errors.Is(err, failure) || applied != (action == "respond") {
			t.Fatalf("%s: applied=%t err=%v", action, applied, err)
		}
	}
}
