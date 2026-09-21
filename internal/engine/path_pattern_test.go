package engine_test

import (
	"github.com/plhhao/faultline/internal/engine"
	"strings"
	"testing"
)

func TestPathPatternMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"/payment/:id", "/payment/123", true},
		{"/payment/:id", "/payment/history", true},
		{"/payment/:id", "/payment/123?x=1", true},
		{"/payment/:id", "/payment/", false},
		{"/payment/:id", "/payment/123/", false},
		{"/payment/:id", "/payment/123/items", false},
		{"/payment/:id", "/Payment/123", false},
		{"/payment/:id/items/:item_id", "/payment/1/items/2", true},
		{"/payment/:id/items/:item_id", "/payment/1/items/", false},
		{"/", "/", true}, {"/", "", false},
		{"/a//b", "/a/b", false}, {"/a//b", "/a//b", true},
		{"/payment/:id", "/payment/%2F", true},
	} {
		e := newEngine(t, rule("r", "probability: 1", "path_pattern: "+tc.pattern))
		if d := decide(t, e, engine.Metadata{Path: tc.path}, true); d.Selected != tc.want {
			t.Errorf("%s %s: %+v", tc.pattern, tc.path, d)
		}
	}
	e := newEngine(t, rule("r", "probability: 1", "path: /payment/:id"))
	if decide(t, e, engine.Metadata{Path: "/payment/123"}, true).Selected {
		t.Fatal("exact became pattern")
	}
	if !decide(t, e, engine.Metadata{Path: "/payment/:id"}, true).Selected {
		t.Fatal("literal colon lost")
	}
	e = newEngine(t, rule("r", "probability: 1", "path_pattern: /payment/:id, method: GET, headers: {X-Test: yes}"))
	for _, m := range []engine.Metadata{{Path: "/payment/1", Method: "POST", Headers: map[string][]string{"X-Test": {"yes"}}}, {Path: "/payment/1", Method: "GET"}} {
		if decide(t, e, m, true).Selected {
			t.Fatal("AND lost")
		}
	}
}

func TestPathPatternRuleOwnership(t *testing.T) {
	exact := rule("history", "probability: 0", "path: /payment/history")
	pattern := rule("detail", "probability: 1", "path_pattern: /payment/:id")
	m := engine.Metadata{Path: "/payment/history"}
	d := decide(t, newEngine(t, exact+pattern), m, true)
	if d.RuleID != "history" || d.Selected {
		t.Fatal(d)
	}
	d = decide(t, newEngine(t, pattern+exact), m, true)
	if d.RuleID != "detail" || !d.Selected {
		t.Fatal(d)
	}
	exact = strings.Replace(exact, "id: history", "id: history\n        enabled: false", 1)
	if d = decide(t, newEngine(t, exact+pattern), m, true); d.RuleID != "detail" || !d.Selected {
		t.Fatal(d)
	}
}
