package config

import (
	"strings"
	"testing"
)

func TestMySQLCapabilities(t *testing.T) {
	base := "api_version: faultline/v1alpha1\nproxies:\n- id: db\n  protocol: mysql\n  listen: localhost:13306\n  upstream: mysql://localhost\n  rules:\n  - id: commit\n    match: {}\n    select: {probability: 1}\n    fault: {action: delay, phase: after_commit, duration: 1s}\n"
	doc, err := Parse([]byte(base), "/tmp/mysql.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config().Proxies[0].Upstream != "mysql://localhost:3306" {
		t.Fatal("default port")
	}
	for _, tc := range [][2]string{{"after_commit", "after_upstream_headers"}, {"match: {}", "match: {method: GET}"}, {"match: {}", "match: {path: /commit}"}, {"action: delay", "action: respond"}, {"duration: 1s", "duration: 0s"}, {"mysql://localhost", "http://localhost"}, {"mysql://localhost", "mysqls://localhost"}, {"probability: 1", "probability: 2"}, {"mysql://localhost", "mysql://localhost:"}} {
		if _, err := Parse([]byte(strings.Replace(base, tc[0], tc[1], 1)), "/tmp/mysql.yaml"); err == nil {
			t.Fatalf("accepted %v", tc)
		}
	}
}
