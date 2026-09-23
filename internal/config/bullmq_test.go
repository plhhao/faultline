package config

import (
	"strings"
	"testing"
)

func TestBullMQValidation(t *testing.T) {
	base := "api_version: faultline/v1alpha1\nproxies:\n- id: jobs\n  protocol: bullmq\n  listen: localhost:16379\n  upstream: redis://localhost\n  rules:\n  - id: added\n    match: {queue: orders}\n    select: {nth: 1}\n    fault: {action: delay, phase: after_job_add, duration: 1s}\n"
	doc, err := Parse([]byte(base), "/tmp/bullmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Config().Proxies[0]
	if p.Upstream != "redis://localhost:6379" || p.Rules[0].Match.Queue != "orders" {
		t.Fatalf("unexpected normalized proxy: %+v", p)
	}
	if _, err := Parse([]byte(strings.Replace(base, "match: {queue: orders}", "match: {}", 1)), "/tmp/bullmq.yaml"); err != nil {
		t.Fatalf("blank queue matcher: %v", err)
	}
	for _, c := range []struct{ old, replacement string }{
		{"phase: after_job_add", "phase: after_upstream_headers"},
		{"phase: after_job_add", "phase: after_publish_confirm"},
		{"match: {queue: orders}", "match: {method: POST}"},
		{"match: {queue: orders}", "match: {exchange: events}"},
		{"match: {queue: orders}", "match: {queue: 'bad:queue'}"},
		{"match: {queue: orders}", "match: {queue: " + strings.Repeat("q", 129) + "}"},
		{"action: delay, phase: after_job_add, duration: 1s", "action: respond, phase: after_job_add, status: 503"},
		{"action: delay, phase: after_job_add, duration: 1s", "action: truncate, phase: after_job_add, direction: response, bytes: 0"},
		{"redis://localhost", "redis://user:secret@localhost"},
		{"redis://localhost", "redis://localhost/1"},
		{"redis://localhost", "amqp://localhost"},
		// Mixed legs: TLS upstream without a TLS listener.
		{"redis://localhost", "rediss://localhost"},
	} {
		if _, err := Parse([]byte(strings.Replace(base, c.old, c.replacement, 1)), "/tmp/bullmq.yaml"); err == nil {
			t.Fatalf("accepted %q", c.replacement)
		}
	}
	mtls := strings.Replace(base, "upstream: redis://localhost", "upstream: rediss://localhost\n  tls: {cert_file: missing.pem, key_file: missing.pem, client_ca_file: missing.pem}", 1)
	if _, err := Parse([]byte(mtls), "/tmp/bullmq.yaml"); err == nil || !strings.Contains(err.Error(), "mTLS") {
		t.Fatalf("BullMQ mTLS validation: %v", err)
	}
	queueElsewhere := strings.NewReplacer("protocol: bullmq", "protocol: rabbitmq", "redis://localhost", "amqp://localhost", "after_job_add", "after_publish_confirm").Replace(base)
	if _, err := Parse([]byte(queueElsewhere), "/tmp/bullmq.yaml"); err == nil {
		t.Fatal("queue matcher accepted outside bullmq")
	}
}
