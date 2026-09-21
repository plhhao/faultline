package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
)

const validYAML = `api_version: faultline/v1alpha1
seed: 42
proxies:
  - id: payment
    protocol: http1
    listen: :8080
    upstream: http://EXAMPLE.test:80/
    rules:
      - id: failure
        match:
          method: POST
          path: /payments
          headers: {X-Test: exact}
        select: {probability: 0.2}
        fault:
          phase: before_upstream_request
          action: close_connection
`

func parse(t *testing.T, data string) *config.Document {
	t.Helper()
	d, err := config.Parse([]byte(data), filepath.Join(t.TempDir(), "faultline.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDefaultsNormalizationAndCopies(t *testing.T) {
	d := parse(t, validYAML)
	c := d.Config()
	if c.Runtime != config.DefaultRuntime() || c.Seed != 42 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	p := c.Proxies[0]
	if p.Listen != "127.0.0.1:8080" || p.Upstream != "http://example.test" || !p.Rules[0].Enabled {
		t.Fatalf("unexpected normalized proxy: %+v", p)
	}
	if p.Rules[0].Match.Headers["x-test"] != "exact" {
		t.Fatal("header not normalized")
	}
	c.Proxies[0].Rules[0].Match.Headers["x-test"] = "changed"
	*c.Proxies[0].Rules[0].Select.Probability = 1
	c.Proxies[0].ID = "changed"
	again := d.Config()
	if again.Proxies[0].ID != "payment" || again.Proxies[0].Rules[0].Match.Headers["x-test"] != "exact" || *again.Proxies[0].Rules[0].Select.Probability != .2 {
		t.Fatal("caller mutated document")
	}
}

func TestStrictValidation(t *testing.T) {
	tests := []struct{ name, old, replacement, field string }{
		{"version", "faultline/v1alpha1", "faultline/v2", "api_version"},
		{"unknown nested", "probability: 0.2", "probabilty: 0.2", "proxies[0].rules[0].select.probabilty"},
		{"unknown root", "seed: 42", "seed: 42\nsecret: top-secret", "secret"},
		{"duplicate key", "seed: 42", "seed: 42\nseed: 43", "seed"},
		{"wrong seed type", "seed: 42", "seed: secret-value", "seed"},
		{"negative seed", "seed: 42", "seed: -1", "seed"},
		{"selector missing", "select: {probability: 0.2}", "select: {}", "proxies[0].rules[0].select"},
		{"selector multiple", "probability: 0.2", "probability: 0.2, nth: 1", "proxies[0].rules[0].select"},
		{"probability high", "probability: 0.2", "probability: 1.1", "proxies[0].rules[0].select.probability"},
		{"probability low", "probability: 0.2", "probability: -0.1", "proxies[0].rules[0].select.probability"},
		{"probability nan", "probability: 0.2", "probability: .nan", "proxies[0].rules[0].select.probability"},
		{"probability inf", "probability: 0.2", "probability: .inf", "proxies[0].rules[0].select.probability"},
		{"nth zero", "probability: 0.2", "nth: 0", "proxies[0].rules[0].select.nth"},
		{"every zero", "probability: 0.2", "every: 0", "proxies[0].rules[0].select.every"},
		{"nth fractional", "probability: 0.2", "nth: 1.5", "proxies[0].rules[0].select.nth"},
		{"nth overflow", "probability: 0.2", "nth: 18446744073709551616", "proxies[0].rules[0].select.nth"},
		{"null", "seed: 42", "seed: null", "seed"},
		{"nonempty ID", "id: payment", "id: ''", "proxies[0].id"},
		{"protocol", "protocol: http1", "protocol: tcp", "proxies[0].protocol"},
		{"listener", "listen: :8080", "listen: localhost", "proxies[0].listen"},
		{"listener port", "listen: :8080", "listen: :0", "proxies[0].listen"},
		{"listener host", "listen: :8080", "listen: 'bad host:80'", "proxies[0].listen"},
		{"upstream path", "http://EXAMPLE.test:80/", "http://example.test/path", "proxies[0].upstream"},
		{"upstream port", "http://EXAMPLE.test:80/", "http://example.test:65536", "proxies[0].upstream"},
		{"upstream empty port", "http://EXAMPLE.test:80/", "http://example.test:", "proxies[0].upstream"},
		{"upstream credentials", "http://EXAMPLE.test:80/", "http://user:secret-value@example.test", "proxies[0].upstream"},
		{"upstream query", "http://EXAMPLE.test:80/", "http://example.test?secret-value", "proxies[0].upstream"},
		{"query matcher", "path: /payments", "path: /payments?x=1", "proxies[0].rules[0].match.path"},
		{"duplicate header", "X-Test: exact", "X-Test: exact, x-test: other", "proxies[0].rules[0].match.headers"},
		{"header value type", "X-Test: exact", "X-Test: 123", "proxies[0].rules[0].match.headers.X-Test"},
		{"method", "method: POST", "method: 'BAD METHOD'", "proxies[0].rules[0].match.method"},
		{"unsupported action", "action: close_connection", "action: reset", "proxies[0].rules[0].fault.action"},
		{"unsupported phase", "phase: before_upstream_request", "phase: middle_of_body", "proxies[0].rules[0].fault.phase"},
		{"delay duration required", "action: close_connection", "action: delay", "proxies[0].rules[0].fault.duration"},
		{"duration zero", "action: close_connection", "action: delay\n          duration: 0s", "proxies[0].rules[0].fault.duration"},
		{"duration negative", "action: close_connection", "action: delay\n          duration: -1s", "proxies[0].rules[0].fault.duration"},
		{"duration type", "action: close_connection", "action: delay\n          duration: 100", "proxies[0].rules[0].fault.duration"},
		{"duration syntax", "action: close_connection", "action: delay\n          duration: secret-value", "proxies[0].rules[0].fault.duration"},
		{"extra duration", "action: close_connection", "action: close_connection\n          duration: 1s", "proxies[0].rules[0].fault.duration"},
		{"extra body", "action: close_connection", "action: close_connection\n          body: ''", "proxies[0].rules[0].fault.body"},
		{"respond low", "action: close_connection", "action: respond\n          status: 199", "proxies[0].rules[0].fault.status"},
		{"respond high", "action: close_connection", "action: respond\n          status: 600", "proxies[0].rules[0].fault.status"},
		{"respond missing", "action: close_connection", "action: respond", "proxies[0].rules[0].fault.status"},
		{"hold phase", "action: close_connection", "action: hold_response\n          max_duration: 1s", "proxies[0].rules[0].fault.phase"},
		{"hold duration", "action: close_connection", "action: hold_request\n          max_duration: 0s", "proxies[0].rules[0].fault.max_duration"},
		{"runtime zero", "seed: 42", "runtime: {max_inflight_requests: 0}", "runtime.max_inflight_requests"},
		{"runtime timeout", "seed: 42", "runtime: {request_timeout: -1s}", "runtime.request_timeout"},
		{"bool coercion", "id: failure", "id: failure\n        enabled: yes", "proxies[0].rules[0].enabled"},
		{"alias", "seed: 42", "seed: &value 42\nruntime: *value", "runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := strings.Replace(validYAML, tt.old, tt.replacement, 1)
			if tt.name == "upstream empty port" {
				data = strings.Replace(data, "upstream: http://example.test:", "upstream: 'http://example.test:'", 1)
			}
			_, err := config.Parse([]byte(data), "test.yaml")
			var field *config.FieldError
			if !errors.As(err, &field) || field.Path != tt.field {
				t.Fatalf("want field %s, got %v", tt.field, err)
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "top-secret") {
				t.Fatal("error leaked value")
			}
		})
	}
}

func TestMalformedDocumentsAndIDs(t *testing.T) {
	for name, data := range map[string]string{
		"empty": "", "empty mapping": "{}", "multiple": validYAML + "---\n{}", "syntax": "proxies: [", "sequence": "[]", "no proxies": "api_version: faultline/v1alpha1\nproxies: []",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse([]byte(data), "test.yaml"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	duplicateRule := validYAML + `      - id: failure
        select: {nth: 1}
        fault: {phase: before_upstream_request, action: close_connection}
`
	duplicateProxy := validYAML + `  - id: payment
    protocol: http1
    listen: :9090
    upstream: http://example.test
`
	for _, data := range []string{duplicateRule, duplicateProxy, strings.Replace(duplicateProxy, "id: payment\n    protocol: http1\n    listen: :9090", "id: other\n    protocol: http1\n    listen: :8080", 1)} {
		if _, err := config.Parse([]byte(data), "test.yaml"); err == nil {
			t.Fatal("duplicate accepted")
		}
	}
}

func TestEffectiveEqualityAndRuleOrder(t *testing.T) {
	a := parse(t, validYAML)
	equivalent := strings.ReplaceAll(validYAML, "X-Test", "x-test")
	equivalent = strings.Replace(equivalent, "seed: 42", "seed: 42\nruntime: {max_inflight_requests: 1000, request_timeout: 30000ms}", 1)
	equivalent = strings.Replace(equivalent, "id: failure", "id: failure\n        enabled: true", 1)
	equivalent = strings.Replace(equivalent, "http://EXAMPLE.test:80/", "http://example.test", 1)
	if !a.Equal(parse(t, equivalent)) {
		t.Fatal("equivalent config changed fingerprint")
	}
	changed := parse(t, strings.Replace(validYAML, "probability: 0.2", "probability: 0.3", 1))
	if a.Equal(changed) || !a.RestartCompatible(changed) {
		t.Fatal("rule change classification")
	}
	if a.RestartCompatible(parse(t, strings.Replace(validYAML, "listen: :8080", "listen: :8081", 1))) {
		t.Fatal("listener change allowed")
	}
	one := `api_version: faultline/v1alpha1
proxies:
  - id: a
    protocol: http1
    listen: :8080
    upstream: http://localhost
    rules:
      - id: first
        select: {nth: 1}
        fault: {phase: before_upstream_request, action: close_connection}
      - id: second
        select: {nth: 1}
        fault: {phase: before_upstream_request, action: close_connection}
`
	reordered := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(one, "first", "temp"), "second", "first"), "temp", "second")
	if parse(t, one).Equal(parse(t, reordered)) {
		t.Fatal("rule order lost")
	}
	if parse(t, one).Config().Proxies[0].Rules[0].ID != "first" {
		t.Fatal("rule order not preserved")
	}
}

func TestSupportedActionsAndDurations(t *testing.T) {
	for _, fault := range []string{
		"phase: before_upstream_request, action: close_connection", "phase: after_upstream_headers, action: close_connection",
		"phase: before_upstream_request, action: delay, duration: 500ms", "phase: after_upstream_headers, action: delay, duration: 1s",
		"phase: before_upstream_request, action: respond, status: 200", "phase: before_upstream_request, action: respond, status: 599, body: failure",
		"phase: before_upstream_request, action: hold_request, max_duration: 1s", "phase: after_upstream_headers, action: hold_response, max_duration: 1s",
	} {
		data := strings.Replace(validYAML, "fault:\n          phase: before_upstream_request\n          action: close_connection", "fault: {"+fault+"}", 1)
		parse(t, data)
	}
	d := parse(t, strings.Replace(validYAML, "action: close_connection", "action: delay\n          duration: 500ms", 1))
	*d.Config().Proxies[0].Rules[0].Fault.Duration = time.Hour
	if *d.Config().Proxies[0].Rules[0].Fault.Duration != 500*time.Millisecond {
		t.Fatal("fault mutation escaped")
	}
}

func TestLoadExampleAndReadFailure(t *testing.T) {
	if _, err := config.Load("../../examples/http/faultline.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := config.Parse([]byte(validYAML), ""); err == nil {
		t.Fatal("empty filename accepted")
	}
	name := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(name, []byte(validYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(name); err != nil {
		t.Fatal(err)
	}
}

func TestEquivalentEmptyValuesAndProxyOrdering(t *testing.T) {
	zero := strings.Replace(validYAML, "probability: 0.2", "probability: 0", 1)
	negativeZero := strings.Replace(validYAML, "probability: 0.2", "probability: -0.0", 1)
	if !parse(t, zero).Equal(parse(t, negativeZero)) {
		t.Fatal("signed zero caused a revision change")
	}
	respond := strings.Replace(validYAML, "action: close_connection", "action: respond\n          status: 503", 1)
	explicit := strings.Replace(respond, "status: 503", "status: 503\n          body: ''", 1)
	if !parse(t, respond).Equal(parse(t, explicit)) {
		t.Fatal("empty response body caused a revision change")
	}
	header := "api_version: faultline/v1alpha1\nproxies:\n"
	a := "  - {id: a, protocol: http1, listen: ':8080', upstream: 'http://localhost'}\n"
	b := "  - {id: b, protocol: http1, listen: ':8081', upstream: 'http://localhost', rules: []}\n"
	if !parse(t, header+a+b).Equal(parse(t, header+b+a)) {
		t.Fatal("proxy ordering caused a revision change")
	}
}
