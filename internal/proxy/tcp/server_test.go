package tcp_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/tcp"
	"github.com/plhhao/faultline/internal/recorder"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	return address
}

func setup(t *testing.T, upstream string, fault string, enabled bool) (string, *recorder.Recorder, *bytes.Buffer) {
	t.Helper()
	address := freeAddress(t)
	rules := ""
	if fault != "" {
		rules = fmt.Sprintf("    rules:\n      - id: inject\n        enabled: true\n        match: {}\n        select: {probability: 1}\n        fault:\n%s\n", fault)
	}
	yaml := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {max_inflight_requests: 8, request_timeout: 5s}\nproxies:\n  - id: s3\n    protocol: tcp\n    listen: %s\n    upstream: tcp://%s\n%s", address, upstream, rules)
	doc, err := config.Parse([]byte(yaml), "tcp.yaml")
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEnabled(enabled)
	var output bytes.Buffer
	records := recorder.New(&output, 128)
	s, err := proxy.Start(service, records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := records.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return address, records, &output
}

func secureRequest(t *testing.T, address string, cert *x509.Certificate) (string, error) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	name := ""
	if len(cert.DNSNames) > 0 {
		name = cert.DNSNames[0]
	} else if len(cert.IPAddresses) > 0 {
		name = cert.IPAddresses[0].String()
	}
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS12})
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err = io.WriteString(c, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n"); err != nil {
		return "", err
	}
	b, err := io.ReadAll(c)
	return string(b), err
}

func TestTLSPassthroughAndCertificate(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("through-proxy")) }))
	var sni atomic.Value
	upstream.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { sni.Store(hello.ServerName); return nil, nil }}
	upstream.StartTLS()
	defer upstream.Close()
	address, _, _ := setup(t, strings.TrimPrefix(upstream.URL, "https://"), "", false)
	response, err := secureRequest(t, address, upstream.Certificate())
	if err != nil || !strings.Contains(response, "through-proxy") {
		t.Fatalf("response=%q err=%v", response, err)
	}
	if got := sni.Load(); got != upstream.Certificate().DNSNames[0] {
		t.Fatalf("upstream saw SNI %v", got)
	}
	name := upstream.Certificate().DNSNames[0]
	_, err = tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{RootCAs: x509.NewCertPool(), ServerName: name, MinVersion: tls.VersionTLS12})
	if err == nil {
		t.Fatal("wrong certificate was accepted")
	}
}

func TestTCPFaults(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fixture")) }))
	defer upstream.Close()
	base := strings.TrimPrefix(upstream.URL, "https://")
	cases := []struct {
		name, fault string
		minimum     time.Duration
		wantError   bool
	}{
		{"disconnect", "          phase: on_transfer\n          action: close_connection", 0, true},
		{"cut after bytes", "          phase: on_transfer\n          action: close_connection\n          after_bytes: 8\n          trigger_direction: client_to_upstream", 0, true},
		{"hold response", "          phase: on_transfer\n          action: hold\n          direction: upstream_to_client\n          max_duration: 150ms", 100 * time.Millisecond, true},
		{"hold upload", "          phase: on_transfer\n          action: hold\n          direction: client_to_upstream\n          max_duration: 150ms", 100 * time.Millisecond, true},
		{"slow upload", "          phase: on_transfer\n          action: throttle\n          direction: client_to_upstream\n          bytes_per_second: 20000", 50 * time.Millisecond, false},
		{"slow response", "          phase: on_transfer\n          action: throttle\n          direction: upstream_to_client\n          bytes_per_second: 20000", 50 * time.Millisecond, false},
		{"delay connect", "          phase: on_connect\n          action: delay_connect\n          duration: 150ms", 100 * time.Millisecond, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address, records, _ := setup(t, base, tc.fault, true)
			start := time.Now()
			response, err := secureRequest(t, address, upstream.Certificate())
			elapsed := time.Since(start)
			if elapsed < tc.minimum {
				t.Fatalf("elapsed %v < %v", elapsed, tc.minimum)
			}
			if tc.wantError && err == nil && strings.Contains(response, "fixture") {
				t.Fatalf("fault did not interrupt: %q", response)
			}
			if !tc.wantError && (err != nil || !strings.Contains(response, "fixture")) {
				t.Fatalf("response=%q err=%v", response, err)
			}
			deadline := time.Now().Add(time.Second)
			for records.Counters().Applied == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if records.Counters().Applied != 1 {
				t.Fatalf("fault applied count=%d", records.Counters().Applied)
			}
		})
	}
}

func TestTCPByteTriggerNotReached(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fixture")) }))
	defer upstream.Close()
	fault := "          phase: on_transfer\n          action: close_connection\n          after_bytes: 1000000\n          trigger_direction: client_to_upstream"
	address, records, output := setup(t, strings.TrimPrefix(upstream.URL, "https://"), fault, true)
	response, err := secureRequest(t, address, upstream.Certificate())
	if err != nil || !strings.Contains(response, "fixture") {
		t.Fatalf("response=%q err=%v", response, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		counts := records.Counters()
		if counts.Active == 0 && counts.Pending == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if records.Counters().Applied != 0 || !strings.Contains(output.String(), `"not_reached":true`) {
		t.Fatalf("unexpected counters/events: %+v %s", records.Counters(), output.String())
	}
}
