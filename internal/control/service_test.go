package control_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/engine"
)

func document(t *testing.T, seed int, selector string) *config.Document {
	t.Helper()
	d, err := config.Parse([]byte(yamlConfig(seed, selector)), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func yamlConfig(seed int, selector string) string {
	return fmt.Sprintf(`api_version: faultline/v1alpha1
seed: %d
proxies:
  - id: payment
    protocol: http1
    listen: :8080
    upstream: http://localhost
    rules:
      - id: r
        select: {%s}
        fault: {phase: before_upstream_request, action: close_connection}
`, seed, selector)
}
func service(t *testing.T, doc *config.Document) *control.Service {
	t.Helper()
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func decide(t *testing.T, s control.Snapshot) engine.Decision {
	t.Helper()
	d, err := s.Decide("payment", engine.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestStartupAndToggleSemantics(t *testing.T) {
	s := service(t, document(t, 42, "nth: 1"))
	startup := s.Acquire()
	if startup.Info().Enabled || startup.Info().Revision != 1 || startup.Info().RunID == "" {
		t.Fatalf("startup: %+v", startup.Info())
	}
	for range 10 {
		if decide(t, startup).EligibleSequence != 0 {
			t.Fatal("startup consumed nth")
		}
	}
	if s.SetEnabled(false).Changed {
		t.Fatal("disabled no-op changed")
	}
	on := s.SetEnabled(true)
	if !on.Changed || !on.Info.Enabled || on.Info.ControlSequence != 1 {
		t.Fatalf("enable: %+v", on)
	}
	if s.SetEnabled(true).Changed {
		t.Fatal("enabled no-op changed")
	}
	captured := s.Acquire()
	if d := decide(t, captured); !d.Selected || d.EligibleSequence != 1 {
		t.Fatalf("first request after enable: %+v", d)
	}
	off := s.SetEnabled(false)
	if !off.Changed || off.Info.Revision != on.Info.Revision || off.Info.StateChangedAt.Before(on.Info.StateChangedAt) {
		t.Fatal("invalid disable transition")
	}
	if d := decide(t, s.Acquire()); d.EligibleSequence != 0 {
		t.Fatal("disabled allocated sequence")
	}
	if d := decide(t, captured); d.EligibleSequence != 2 {
		t.Fatal("old enabled snapshot changed")
	}
	s.SetEnabled(true)
	if d := decide(t, s.Acquire()); d.EligibleSequence != 3 {
		t.Fatal("enable reset sequence")
	}
	if startup.Info().Enabled || decide(t, startup).EligibleSequence != 0 {
		t.Fatal("startup snapshot changed")
	}
}

func TestApplyNoopChangedRevisionAndOldOwnership(t *testing.T) {
	initial := document(t, 42, "every: 2")
	s := service(t, initial)
	s.SetEnabled(true)
	old := s.Acquire()
	decide(t, old)
	result, err := s.Apply(document(t, 42, "every: 2"))
	if err != nil || result.Changed || result.Info != old.Info() {
		t.Fatalf("no-op: %+v %v", result, err)
	}
	if !decide(t, s.Acquire()).Selected {
		t.Fatal("no-op reset counters")
	}
	result, err = s.Apply(document(t, 43, "every: 2"))
	if err != nil || !result.Changed || !result.Info.Enabled || result.Info.Revision != 2 {
		t.Fatalf("apply: %+v %v", result, err)
	}
	fresh := s.Acquire()
	if d := decide(t, fresh); d.EligibleSequence != 1 || d.Selected {
		t.Fatalf("new counters: %+v", d)
	}
	if d := decide(t, old); d.EligibleSequence != 3 {
		t.Fatal("old revision counters lost")
	}
	if old.Info().Revision != 1 || old.Config().Seed != 42 || fresh.Config().Seed != 43 {
		t.Fatal("snapshot config changed")
	}
	c, _ := old.Counters("payment", "r")
	next, _ := fresh.Counters("payment", "r")
	if c.Eligible != 3 || next.Eligible != 1 {
		t.Fatal("counter scopes mixed")
	}
	changed := fresh.Config()
	changed.Proxies[0].Rules[0].ID = "mutated"
	if fresh.Config().Proxies[0].Rules[0].ID != "r" {
		t.Fatal("snapshot config exposed")
	}
	s.SetEnabled(false)
	if _, err := s.Apply(initial); err != nil {
		t.Fatal(err)
	}
	if s.Acquire().Info().Enabled {
		t.Fatal("reload enabled injection")
	}
	s.SetEnabled(true)
	if d := decide(t, s.Acquire()); d.EligibleSequence != 1 {
		t.Fatal("returning to old config reused old counters")
	}
	restarted := service(t, initial).Acquire()
	if restarted.Info().Enabled || restarted.Info().RunID == old.Info().RunID {
		t.Fatal("restart reused runtime state")
	}
}

func TestInvalidReloadAndRestartFieldsAreAtomic(t *testing.T) {
	s := service(t, document(t, 42, "nth: 1"))
	before := s.Acquire().Info()
	for _, replacement := range []struct{ old, new string }{
		{"nth: 1", "nth: 0"},
		{"listen: :8080", "listen: :8081"},
		{"upstream: http://localhost", "upstream: https://localhost"},
		{"seed: 42", "seed: 42\nruntime: {request_timeout: 1s}"},
		{"id: payment", "id: other"},
		{"protocol: http1", "protocol: grpc"},
	} {
		data := strings.Replace(yamlConfig(42, "nth: 1"), replacement.old, replacement.new, 1)
		if _, err := s.Reload([]byte(data), "test.yaml"); err == nil {
			t.Fatalf("accepted change %s", replacement.new)
		}
		if s.Acquire().Info() != before {
			t.Fatal("failed reload changed active state")
		}
	}
	for _, doc := range []*config.Document{nil, {}} {
		if _, err := s.Apply(doc); err == nil {
			t.Fatal("invalid apply accepted")
		}
		if _, err := control.New(doc); err == nil {
			t.Fatal("invalid startup accepted")
		}
	}
}

func TestSeedReplayAcrossRevisionAndRun(t *testing.T) {
	doc := document(t, 42, "probability: 0.4")
	s := service(t, doc)
	s.SetEnabled(true)
	expected := make([]bool, 100)
	for i := range expected {
		expected[i] = decide(t, s.Acquire()).Selected
	}
	if _, err := s.Apply(document(t, 43, "probability: 0.4")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(doc); err != nil {
		t.Fatal(err)
	}
	other := service(t, doc)
	other.SetEnabled(true)
	for _, svc := range []*control.Service{s, other} {
		for _, want := range expected {
			if decide(t, svc.Acquire()).Selected != want {
				t.Fatal("revision/run ID affected seeded decisions")
			}
		}
	}
}

func TestConcurrentAcquireApplyAndToggle(t *testing.T) {
	odd, even := document(t, 1, "every: 2"), document(t, 2, "every: 2")
	s := service(t, odd)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 200; i++ {
			d := even
			if i%2 == 1 {
				d = odd
			}
			if _, err := s.Apply(d); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		for i := 0; i < 400; i++ {
			s.SetEnabled(i%2 == 0)
		}
	})
	for range 8 {
		wg.Go(func() {
			for range 300 {
				snapshot := s.Acquire()
				info := snapshot.Info()
				if snapshot.Config().Seed != 2-info.Revision%2 {
					t.Error("mixed config/revision snapshot")
					return
				}
				d, err := snapshot.Decide("payment", engine.Metadata{})
				if err != nil {
					t.Error(err)
					return
				}
				if !info.Enabled && d.EligibleSequence != 0 {
					t.Error("mixed injection state")
					return
				}
				if d.Selected && d.EligibleSequence%2 != 0 {
					t.Error("inconsistent selection")
					return
				}
				snapshot.Counters("payment", "r")
			}
		})
	}
	wg.Wait()
	if s.Acquire().Info().Revision != 201 {
		t.Fatal("lost apply")
	}
}

func TestServiceOwnsDocumentValues(t *testing.T) {
	initial := document(t, 42, "nth: 1")
	s := service(t, initial)
	*initial = config.Document{}
	if s.Acquire().Config().Seed != 42 {
		t.Fatal("caller replaced startup config")
	}
	next := document(t, 43, "nth: 1")
	if _, err := s.Apply(next); err != nil {
		t.Fatal(err)
	}
	*next = config.Document{}
	if s.Acquire().Config().Seed != 43 {
		t.Fatal("caller replaced applied config")
	}
	if _, err := s.Apply(document(t, 43, "nth: 1")); err != nil {
		t.Fatal(err)
	}
}
