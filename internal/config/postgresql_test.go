package config

import (
	"strings"
	"testing"
)

func TestPostgreSQLCapabilities(t *testing.T) {
	base := "api_version: faultline/v1alpha1\nproxies:\n- id: db\n  protocol: postgresql\n  listen: localhost:5433\n  upstream: postgresql://localhost\n  rules:\n  - id: commit\n    match: {}\n    select: {probability: 1}\n    fault: {action: delay, phase: after_commit, duration: 1s}\n"
	doc, err := Parse([]byte(base), "/tmp/pg.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config().Proxies[0].Upstream != "postgresql://localhost:5432" {
		t.Fatal("default port")
	}
	for _, tc := range [][2]string{{"after_commit", "after_upstream_headers"}, {"match: {}", "match: {method: GET}"}, {"match: {}", "match: {path: /commit}"}, {"action: delay", "action: respond"}, {"duration: 1s", "duration: 0s"}, {"postgresql://localhost", "http://localhost"}, {"probability: 1", "probability: 2"}, {"postgresql://localhost", "postgresql://localhost:"}} {
		if _, err := Parse([]byte(strings.Replace(base, tc[0], tc[1], 1)), "/tmp/pg.yaml"); err == nil {
			t.Fatalf("accepted %v", tc)
		}
	}
}
