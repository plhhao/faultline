package config_test

import (
	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolAndBodyValidation(t *testing.T) {
	base := "api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: grpc\n  listen: 127.0.0.1:8000\n  upstream: http://localhost:9000\n  rules:\n  - id: cut\n    select: {probability: 1}\n    match: {service: package.Service, method: Method}\n    fault: {action: truncate, direction: request, phase: before_upstream_request, bytes: 0}\n"
	d, err := config.Parse([]byte(base), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Config().Proxies[0].UpstreamProtocol != "http2" {
		t.Fatal("grpc must default to HTTP/2")
	}
	*d.Config().Proxies[0].Rules[0].Fault.Bytes = 123
	if *d.Config().Proxies[0].Rules[0].Fault.Bytes != 0 {
		t.Fatal("fault alias escaped")
	}
	for _, pair := range [][2]string{
		{"protocol: grpc", "protocol: grpc\n  upstream_protocol: http1"},
		{"protocol: grpc", "protocol: tcp"},
		{"direction: request", "direction: both"},
		{"direction: request", "direction: response"},
		{"bytes: 0", "bytes: -1"},
		{"bytes: 0", "bytes: 0, duration: 1s"},
		{"bytes: 0", "bytes: 0, bytes_per_second: 1"},
		{"action: truncate, direction: request, phase: before_upstream_request, bytes: 0", "action: close_connection, phase: before_upstream_request"},
		{"action: truncate, direction: request, phase: before_upstream_request, bytes: 0", "action: respond, phase: before_upstream_request, status: 503"},
		{"action: truncate, direction: request, phase: before_upstream_request, bytes: 0", "action: throttle, direction: request, phase: before_upstream_request, bytes_per_second: 0"},
		{"action: truncate, direction: request, phase: before_upstream_request, bytes: 0", "action: delay, phase: before_upstream_request, duration: 1s, direction: request"},
	} {
		if _, err := config.Parse([]byte(strings.Replace(base, pair[0], pair[1], 1)), "test.yaml"); err == nil {
			t.Fatalf("accepted %s", pair[1])
		}
	}
}

func TestMTLSIncludesAndRestart(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "parts")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	writeCertificate(t, sub)
	root := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(sub, "proxy.yaml")
	base := "proxies:\n- id: test\n  protocol: http2\n  listen: 127.0.0.1:8000\n  upstream: https://localhost:9000\n  tls: {cert_file: cert.pem, key_file: key.pem, client_ca_file: cert.pem}\n  upstream_tls: {cert_file: cert.pem, key_file: key.pem}\n"
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "api_version: faultline/v1alpha1\ninclude: [parts/proxy.yaml]\n")
	write(fragment, base)
	doc, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Config().Proxies[0]
	if p.TLS.ClientCAFile != filepath.Join(sub, "cert.pem") || p.UpstreamTLS.CertFile != p.TLS.CertFile || p.UpstreamTLS.KeyFile != p.TLS.KeyFile {
		t.Fatalf("%+v", p)
	}
	for _, pair := range [][2]string{
		{"client_ca_file: cert.pem", "client_ca_file: key.pem"},
		{"upstream_tls: {cert_file: cert.pem, key_file: key.pem}", "upstream_tls: {cert_file: cert.pem}"},
		{"upstream_tls: {cert_file: cert.pem, key_file: key.pem}", "upstream_tls: {key_file: key.pem}"},
		{"https://localhost:9000", "http://localhost:9000"},
		{"tls: {cert_file: cert.pem, key_file: key.pem, client_ca_file: cert.pem}", "tls: {client_ca_file: cert.pem}"},
	} {
		write(fragment, strings.Replace(base, pair[0], pair[1], 1))
		if _, err := config.Load(root); err == nil {
			t.Fatalf("accepted %s", pair[1])
		}
	}
	write(fragment, base)
	svc, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetEnabled(true)
	before := svc.Acquire().Info()
	writeCertificate(t, sub)
	changed, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(changed); err == nil {
		t.Fatal("certificate content reloaded")
	}
	if svc.Acquire().Info() != before {
		t.Fatal("failed reload changed state")
	}
	p.TLS.ClientCAFile = "changed"
	p.UpstreamTLS.KeyFile = "changed"
	if doc.Config().Proxies[0].TLS.ClientCAFile == "changed" || doc.Config().Proxies[0].UpstreamTLS.KeyFile == "changed" {
		t.Fatal("TLS alias escaped")
	}
}
