package config

import (
	"strings"
	"testing"
)

func TestRabbitMQValidation(t *testing.T) {
	base := "api_version: faultline/v1alpha1\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: localhost:15672\n  upstream: amqp://localhost\n  rules:\n  - id: confirm\n    match: {exchange: events, routing_key: payment.created}\n    select: {probability: 1}\n    fault: {action: delay, phase: after_publish_confirm, duration: 1s}\n"
	doc, err := Parse([]byte(base), "/tmp/rabbitmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Config().Proxies[0]
	if p.Upstream != "amqp://localhost:5672" || p.Rules[0].Match.Exchange != "events" || p.Rules[0].Match.RoutingKey != "payment.created" {
		t.Fatalf("unexpected normalized proxy: %+v", p)
	}
	secure := strings.Replace(base, "amqp://localhost", "amqps://localhost", 1)
	doc, err = Parse([]byte(secure), "/tmp/rabbitmq.yaml")
	if err != nil || doc.Config().Proxies[0].Upstream != "amqps://localhost:5671" {
		t.Fatalf("secure normalization: %v %+v", err, doc)
	}
	for _, replacement := range []string{
		"phase: after_upstream_headers",
		"match: {method: POST}",
		"match: {headers: {X-Test: value}}",
		"action: respond, phase: after_publish_confirm, status: 503",
		"amqp://localhost:",
		"http://localhost",
		"exchange: 'bad\\nvalue'",
	} {
		candidate := strings.Replace(base, "phase: after_publish_confirm", replacement, 1)
		if replacement == "match: {method: POST}" || replacement == "match: {headers: {X-Test: value}}" || replacement == "exchange: 'bad\\nvalue'" {
			candidate = strings.Replace(base, "match: {exchange: events, routing_key: payment.created}", replacement, 1)
		}
		if replacement == "amqp://localhost:" || replacement == "http://localhost" {
			candidate = strings.Replace(base, "amqp://localhost", replacement, 1)
		}
		if _, err := Parse([]byte(candidate), "/tmp/rabbitmq.yaml"); err == nil {
			t.Fatalf("accepted %q", replacement)
		}
	}
	mtls := strings.Replace(base, "upstream: amqp://localhost", "upstream: amqps://localhost\n  tls: {cert_file: missing.pem, key_file: missing.pem, client_ca_file: missing.pem}", 1)
	if _, err := Parse([]byte(mtls), "/tmp/rabbitmq.yaml"); err == nil || !strings.Contains(err.Error(), "mTLS") {
		t.Fatalf("RabbitMQ mTLS validation: %v", err)
	}
}
