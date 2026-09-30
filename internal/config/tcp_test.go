package config_test

import (
	"strings"
	"testing"

	"github.com/plhhao/faultline/internal/config"
)

const tcpYAML = `api_version: faultline/v1alpha1
proxies:
  - id: s3
    protocol: tcp
    listen: 127.0.0.1:9443
    upstream: tcp://s3.example.test:443
    rules:
      - id: cut
        enabled: true
        match: {}
        select: {probability: 1}
        fault:
          phase: on_transfer
          action: close_connection
          after_bytes: 1024
          trigger_direction: client_to_upstream
`

func TestTCPContract(t *testing.T) {
	d, err := config.Parse([]byte(tcpYAML), "tcp.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f := d.Config().Proxies[0].Rules[0].Fault
	if f.AfterBytes == nil || *f.AfterBytes != 1024 || f.TriggerDirection != "client_to_upstream" {
		t.Fatalf("unexpected fault: %+v", f)
	}
	bad := map[string]string{
		"matcher":                   strings.Replace(tcpYAML, "match: {}", "match: {method: PUT}", 1),
		"TLS termination":           strings.Replace(tcpYAML, "    rules:", "    tls: {cert_file: cert.pem, key_file: key.pem}\n    rules:", 1),
		"HTTP phase":                strings.Replace(tcpYAML, "phase: on_transfer", "phase: after_upstream_headers", 1),
		"missing trigger direction": strings.Replace(tcpYAML, "          trigger_direction: client_to_upstream\n", "", 1),
		"negative threshold":        strings.Replace(tcpYAML, "after_bytes: 1024", "after_bytes: -1", 1),
		"unknown direction":         strings.Replace(tcpYAML, "client_to_upstream", "request", 1),
		"self route":                strings.Replace(tcpYAML, "tcp://s3.example.test:443", "tcp://127.0.0.1:9443", 1),
		"empty query":               strings.Replace(tcpYAML, "tcp://s3.example.test:443", "tcp://s3.example.test:443?", 1),
	}
	for name, yaml := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse([]byte(yaml), "tcp.yaml"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
