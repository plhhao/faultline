package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
	"faultline/internal/recorder"
)

func TestResourceRecovery(t *testing.T) {
	for _, action := range []string{"delay", "hold_request", "hold_response"} {
		t.Run(action, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			baseline := runtime.NumGoroutine()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
			t.Cleanup(upstream.Close)
			phase, parameter := config.BeforeUpstreamRequest, ", max_duration: 1m"
			if action == "delay" {
				parameter = ", duration: 1m"
			} else if action == "hold_response" {
				phase = config.AfterUpstreamHeaders
			}
			const count = 32
			addr := address(t)
			records := recorder.New(io.Discard, 256)
			t.Cleanup(func() { records.Close(context.Background()) })
			_, proxy := start(t, document(t, addr, upstream.URL, faultRule(action, phase, parameter), "runtime: {max_inflight_requests: 32, request_timeout: 400ms}"), true, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
			c := client(t)
			c.Transport.(*http.Transport).DisableKeepAlives = true
			for wave := range 3 {
				done := make(chan error, count)
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
					if err := receive(t, done); err == nil {
						t.Fatal("long fault exceeded its flow deadline or returned a response")
					}
				}
				counts := awaitCounts(t, records, func(c recorder.Counters) bool { return c.Active == 0 })
				if counts.Total != uint64((wave+1)*count) || counts.Applied != counts.Total || counts.ActiveFaults != 0 {
					t.Fatalf("%+v", counts)
				}
			}
			proxy.Close()
			upstream.Close()
			c.CloseIdleConnections()
			records.Close(context.Background())
			deadline := time.Now().Add(3 * time.Second)
			for {
				runtime.GC()
				runtime.ReadMemStats(&after)
				goroutines := runtime.NumGoroutine()
				// Account for net/http cleanup and runtime/test caches, without requiring RSS to shrink.
				if goroutines <= baseline+8 && after.HeapAlloc <= before.HeapAlloc+8<<20 {
					t.Logf("96 flows: goroutines %d -> %d; live heap %d -> %d bytes", baseline, goroutines, before.HeapAlloc, after.HeapAlloc)
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("resources did not recover: goroutines %d -> %d; heap %d -> %d", baseline, goroutines, before.HeapAlloc, after.HeapAlloc)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestSlowHeadersAndIdleConnectionsExpire(t *testing.T) {
	addr := address(t)
	service, _ := start(t, document(t, addr, "http://127.0.0.1:1", faultRule("respond", config.BeforeUpstreamRequest, ", status: 204"), "runtime: {request_timeout: 100ms}"), true, httpproxy.Options{Executor: fault.Builtin{}})
	for _, request := range []string{"GET / HTTP/1.1\r\nHost: test\r\nX-Slow: ", ""} {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(conn, request)
		_, err = io.ReadAll(conn)
		conn.Close()
		if err != nil {
			t.Fatalf("server did not close incomplete headers before client deadline: %v", err)
		}
	}
	counts, _ := service.Acquire().Counters("test", "inject")
	if counts.Eligible != 0 {
		t.Fatalf("incomplete headers consumed selector: %+v", counts)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
	data, err := io.ReadAll(conn)
	if err != nil || len(data) == 0 {
		t.Fatalf("idle keep-alive did not expire: %q %v", data, err)
	}
}

func TestGracefulShutdownDrainAndDeadline(t *testing.T) {
	for _, finish := range []bool{true, false} {
		t.Run(fmt.Sprint(finish), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
					io.WriteString(w, "completed")
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(upstream.Close)
			addr := address(t)
			_, proxy := start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{})
			done := make(chan error, 1)
			c := client(t)
			go func() {
				resp, err := c.Get("http://" + addr)
				if resp != nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				done <- err
			}()
			receive(t, entered)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			stopped := make(chan struct{})
			go func() { proxy.Shutdown(ctx); close(stopped) }()
			for proxy.Listeners()[0].Ready {
				if ctx.Err() != nil {
					t.Fatal("shutdown did not stop admission")
				}
				time.Sleep(time.Millisecond)
			}
			if finish {
				close(release)
			}
			receive(t, stopped)
			if err := receive(t, done); (err == nil) != finish {
				t.Fatalf("finish=%t: %v", finish, err)
			}
		})
	}
}

func TestDelay500Milliseconds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, upstream.URL, faultRule("delay", config.BeforeUpstreamRequest, ", duration: 500ms"), ""), true, httpproxy.Options{Executor: fault.Builtin{}})
	started := time.Now()
	status, _ := readResponse(t, client(t), "http://"+addr)
	if elapsed := time.Since(started); status != 204 || elapsed < 500*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("status=%d elapsed=%s", status, elapsed)
	}
}
