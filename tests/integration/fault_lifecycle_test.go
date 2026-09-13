package integration_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
)

func TestTimedFaultCancellation(t *testing.T) {
	cert, certFile, keyFile, roots := certificate(t, false)
	for _, secure := range []bool{false, true} {
		for _, action := range []string{"delay", "hold_request", "hold_response"} {
			for _, reason := range []string{"client", "deadline", "shutdown"} {
				t.Run(fmt.Sprintf("tls=%t/%s/%s", secure, action, reason), func(t *testing.T) {
					var payments atomic.Int32
					upstreamCanceled := make(chan struct{}, 1)
					upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						payments.Add(1)
						w.WriteHeader(201)
						http.NewResponseController(w).Flush()
						<-r.Context().Done()
						upstreamCanceled <- struct{}{}
					}))
					if secure {
						upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
						upstream.StartTLS()
					} else {
						upstream.Start()
					}
					t.Cleanup(upstream.Close)
					phase, parameters := config.BeforeUpstreamRequest, ", max_duration: 10s"
					if action == "delay" {
						parameters = ", duration: 10s"
					} else if action == "hold_response" {
						phase = config.AfterUpstreamHeaders
					}
					extra := faultRule(action, phase, parameters)
					scheme := "http"
					if secure {
						scheme = "https"
						extra += fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n    upstream_tls: {ca_file: '%s'}\n", certFile, keyFile, certFile)
					}
					runtime := "runtime: {max_inflight_requests: 1, request_timeout: 5s}"
					if reason == "deadline" {
						runtime = "runtime: {max_inflight_requests: 1, request_timeout: 250ms}"
					}
					addr := address(t)
					reports, entered := make(chan httpproxy.Report, 2), make(chan struct{}, 1)
					service, server := start(t, document(t, addr, upstream.URL, extra, runtime), true, httpproxy.Options{Executor: notifyingExecutor{entered: entered}, Observe: func(r httpproxy.Report) { reports <- r }})
					c := client(t)
					c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
					if reason == "client" {
						c.Timeout = 250 * time.Millisecond
					}
					done := make(chan error, 1)
					go func() {
						resp, err := c.Get(scheme + "://" + addr)
						if resp != nil {
							resp.Body.Close()
						}
						done <- err
					}()
					receive(t, entered)
					if action == "hold_response" {
						// Upstream is canceled while the client is still held, not at its timeout.
						receive(t, upstreamCanceled)
						select {
						case err := <-done:
							t.Fatalf("client completed before hold: %v", err)
						default:
						}
					}
					if reason == "shutdown" {
						time.Sleep(20 * time.Millisecond)
						stopped := make(chan struct{}, 1)
						go func() { server.Close(); stopped <- struct{}{} }()
						receive(t, stopped)
					}
					err := receive(t, done)
					if err == nil {
						t.Fatal("received final response while held/delayed")
					}
					if reason == "client" {
						var timeout net.Error
						if !errors.As(err, &timeout) || !timeout.Timeout() {
							t.Fatalf("expected client timeout: %v", err)
						}
					}
					r := receive(t, reports)
					if !r.Decision.Selected || !r.Reached || !r.Applied || r.NotReached || r.Outcome != "canceled" || r.Err == nil {
						t.Fatalf("%+v", r)
					}
					if action == "hold_response" && payments.Load() != 1 || action != "hold_response" && payments.Load() != 0 {
						t.Fatalf("payments=%d", payments.Load())
					}
					if reason != "shutdown" {
						service.SetEnabled(false)
						c.Timeout = 3 * time.Second
						resp, err := c.Get(scheme + "://" + addr)
						if err != nil {
							t.Fatal(err)
						}
						resp.Body.Close()
						if resp.StatusCode != 201 {
							t.Fatalf("inflight slot not released: %d", resp.StatusCode)
						}
					}
				})
			}
		}
	}
}

func TestManyHeldFlowsShutdown(t *testing.T) {
	const count = 24
	addr := address(t)
	entered, reports := make(chan struct{}, count), make(chan httpproxy.Report, count)
	_, server := start(t, document(t, addr, "http://127.0.0.1:1", faultRule("hold_request", config.BeforeUpstreamRequest, ", max_duration: 1m"), ""), true, httpproxy.Options{Executor: notifyingExecutor{entered: entered}, Observe: func(r httpproxy.Report) { reports <- r }})
	done := make(chan error, count)
	c := client(t)
	for range count {
		go func() {
			resp, err := c.Get("http://" + addr)
			if resp != nil {
				resp.Body.Close()
			}
			done <- err
		}()
	}
	for range count {
		receive(t, entered)
	}
	stopped := make(chan struct{}, 1)
	go func() { server.Close(); stopped <- struct{}{} }()
	receive(t, stopped)
	for range count {
		if err := receive(t, done); err == nil {
			t.Fatal("held request received response")
		}
		if r := receive(t, reports); r.Outcome != "canceled" {
			t.Fatalf("%+v", r)
		}
	}
}

func TestDelayAfterHeadersCancellation(t *testing.T) {
	canceled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		http.NewResponseController(w).Flush()
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	t.Cleanup(upstream.Close)
	addr := address(t)
	reports := make(chan httpproxy.Report, 1)
	start(t, document(t, addr, upstream.URL, faultRule("delay", config.AfterUpstreamHeaders, ", duration: 10s"), ""), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
	c := client(t)
	c.Timeout = 200 * time.Millisecond
	resp, err := c.Get("http://" + addr)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		t.Fatal("delay returned early")
	}
	receive(t, canceled)
	r := receive(t, reports)
	if !r.Applied || r.Outcome != "canceled" || !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("%+v", r)
	}
}

func TestDelayUnreadUploadDeadline(t *testing.T) {
	addr := address(t)
	reports := make(chan httpproxy.Report, 1)
	start(t, document(t, addr, "http://127.0.0.1:1", faultRule("delay", config.BeforeUpstreamRequest, ", duration: 10s"), "runtime: {request_timeout: 100ms}"), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1000000\r\n\r\nx")
	r := receive(t, reports)
	if !r.Applied || r.Outcome != "canceled" || !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("%+v", r)
	}
	data := make([]byte, 1)
	if n, err := conn.Read(data); n != 0 || err == nil {
		t.Fatalf("unexpected final response: %q err=%v", data[:n], err)
	}
}
