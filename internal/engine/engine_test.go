package engine_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/engine"
)

func newEngine(t *testing.T, rules string) *engine.Engine {
	t.Helper()
	data := `api_version: faultline/v1alpha1
seed: 42
proxies:
  - id: payment
    protocol: http1
    listen: :8080
    upstream: http://localhost
    rules:
` + rules
	doc, err := config.Parse([]byte(data), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func rule(id, selector, matcher string) string {
	return fmt.Sprintf(`      - id: %s
        match: {%s}
        select: {%s}
        fault: {phase: before_upstream_request, action: close_connection}
`, id, matcher, selector)
}

func decide(t *testing.T, e *engine.Engine, m engine.Metadata, enabled bool) engine.Decision {
	t.Helper()
	d, err := e.Decide("payment", m, enabled)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMatching(t *testing.T) {
	m := engine.Metadata{Method: "POST", Path: "/payments?debug=true", Headers: map[string][]string{"x-TEST": {"other", "exact"}}}
	tests := []struct {
		name, matcher string
		metadata      engine.Metadata
		want          bool
	}{
		{"empty", "", m, true},
		{"all", "method: POST, path: /payments, headers: {X-Test: exact}", m, true},
		{"method mismatch", "method: GET", m, false},
		{"method case", "method: post", m, false},
		{"path mismatch", "path: /payment", m, false},
		{"missing header", "headers: {X-Missing: exact}", m, false},
		{"value case", "headers: {X-Test: Exact}", m, false},
		{"AND", "method: GET, path: /payments", m, false},
		{"empty missing value", "headers: {X-Missing: ''}", m, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEngine(t, rule("r", "probability: 1", tt.matcher))
			d := decide(t, e, tt.metadata, true)
			if d.Selected != tt.want {
				t.Fatalf("decision: %+v", d)
			}
			counters, _ := e.Counters("payment", "r")
			if !tt.want && counters.Eligible != 0 {
				t.Fatal("nonmatch consumed sequence")
			}
		})
	}
}

func TestFirstRuleOwnsRequest(t *testing.T) {
	e := newEngine(t, rule("first", "probability: 0", "")+rule("second", "probability: 1", ""))
	d := decide(t, e, engine.Metadata{}, true)
	if d.RuleID != "first" || d.Selected {
		t.Fatalf("unexpected fallback: %+v", d)
	}
	c, _ := e.Counters("payment", "second")
	if c.Eligible != 0 {
		t.Fatal("second rule counted")
	}
	disabled := strings.Replace(rule("first", "probability: 1", ""), "id: first", "id: first\n        enabled: false", 1)
	e = newEngine(t, disabled+rule("second", "probability: 1", ""))
	if d := decide(t, e, engine.Metadata{}, true); d.RuleID != "second" || !d.Selected {
		t.Fatalf("disabled rule not skipped: %+v", d)
	}
}

func TestSequenceSelectorsAndDisabledTraffic(t *testing.T) {
	for _, selector := range []string{"nth: 3", "every: 10", "probability: 0", "probability: 1"} {
		t.Run(selector, func(t *testing.T) {
			e := newEngine(t, rule("r", selector, "method: POST"))
			m := engine.Metadata{Method: "POST"}
			for i := 1; i <= 30; i++ {
				if d := decide(t, e, m, false); d.EligibleSequence != 0 {
					t.Fatal("disabled consumed sequence")
				}
				decide(t, e, engine.Metadata{Method: "GET"}, true)
				d := decide(t, e, m, true)
				want := selector == "probability: 1" || selector == "nth: 3" && i == 3 || selector == "every: 10" && i%10 == 0
				if d.EligibleSequence != uint64(i) || d.Selected != want {
					t.Fatalf("attempt %d: %+v", i, d)
				}
			}
			c, _ := e.Counters("payment", "r")
			if c.Eligible != 30 {
				t.Fatalf("counters %+v", c)
			}
		})
	}
}

func TestProbabilityReplayAndDistribution(t *testing.T) {
	const samples = 10000
	const minimum = 1700
	const maximum = 2300
	a := newEngine(t, rule("r", "probability: 0.2", ""))
	b := newEngine(t, rule("r", "probability: 0.2", ""))
	selected := 0
	for range samples {
		da, db := decide(t, a, engine.Metadata{}, true), decide(t, b, engine.Metadata{}, true)
		if da.Selected != db.Selected || da.EligibleSequence != db.EligibleSequence {
			t.Fatal("same seed/input did not replay")
		}
		if da.Selected {
			selected++
		}
	}
	if selected < minimum || selected > maximum {
		t.Fatalf("selected %d/%d, expected [%d,%d]", selected, samples, minimum, maximum)
	}
}

func TestRuleRandomStreamsAreIndependent(t *testing.T) {
	rules := rule("a", "probability: 0.5", "method: POST") + rule("b", "probability: 0.5", "method: GET")
	a, b := newEngine(t, rules), newEngine(t, rules)
	for range 100 {
		decide(t, a, engine.Metadata{Method: "GET"}, true)
		x := decide(t, a, engine.Metadata{Method: "POST"}, true)
		y := decide(t, b, engine.Metadata{Method: "POST"}, true)
		if x.Selected != y.Selected {
			t.Fatal("traffic for another rule changed random stream")
		}
	}
}

func TestConcurrentSequenceAllocation(t *testing.T) {
	e := newEngine(t, rule("r", "every: 10", ""))
	const requests = 1000
	results := make(chan engine.Decision, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			d, err := e.Decide("payment", engine.Metadata{}, true)
			if err != nil {
				t.Error(err)
				return
			}
			e.Counters("payment", "r")
			results <- d
		})
	}
	wg.Wait()
	close(results)
	seen := map[uint64]bool{}
	for d := range results {
		if seen[d.EligibleSequence] || d.EligibleSequence == 0 || d.Selected != (d.EligibleSequence%10 == 0) {
			t.Fatalf("invalid sequence %+v", d)
		}
		seen[d.EligibleSequence] = true
	}
	c, _ := e.Counters("payment", "r")
	if len(seen) != requests || c.Eligible != requests || c.Selected != requests/10 {
		t.Fatalf("lost counts: %+v", c)
	}
}

func TestDecisionDoesNotExposeRuleAndInvalidInputs(t *testing.T) {
	rules := strings.Replace(rule("r", "probability: 1", ""), "action: close_connection", "action: delay, duration: 1s", 1)
	e := newEngine(t, rules)
	d := decide(t, e, engine.Metadata{}, true)
	*d.Fault.Duration = 0
	if *decide(t, e, engine.Metadata{}, true).Fault.Duration == 0 {
		t.Fatal("decision changed engine config")
	}
	if _, err := e.Decide("unknown", engine.Metadata{}, false); err == nil {
		t.Fatal("unknown proxy accepted")
	}
	if _, ok := e.Counters("payment", "unknown"); ok {
		t.Fatal("unknown rule exists")
	}
	for _, doc := range []*config.Document{nil, {}} {
		if _, err := engine.New(doc); err == nil {
			t.Fatal("unvalidated document accepted")
		}
	}
}

func TestProxyCounterScopes(t *testing.T) {
	data := `api_version: faultline/v1alpha1
proxies:
  - id: a
    protocol: http1
    listen: :8080
    upstream: http://localhost
    rules:
` + rule("shared", "nth: 1", "") + `  - id: b
    protocol: http1
    listen: :8081
    upstream: http://localhost
    rules:
` + rule("shared", "nth: 1", "")
	doc, err := config.Parse([]byte(data), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		d, err := e.Decide(id, engine.Metadata{}, true)
		if err != nil || !d.Selected || d.EligibleSequence != 1 {
			t.Fatalf("proxy %s: %+v %v", id, d, err)
		}
	}
}
