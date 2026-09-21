package integration_test

import (
	"bufio"
	"context"
	"crypto/tls"
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
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
)

func faultRule(action, phase, parameters string) string {
	return fmt.Sprintf("    rules:\n    - id: inject\n      select: {probability: 1}\n      fault: {action: %s, phase: %s%s}\n", action, phase, parameters)
}

func TestFaultTLSMatrix(t *testing.T) {
	cert, certFile, keyFile, roots := certificate(t, false)
	cases := []struct {
		action, phase, parameters string
		disconnect                bool
		wait                      time.Duration
	}{
		{"delay", config.BeforeUpstreamRequest, ", duration: 500ms", false, 500 * time.Millisecond},
		{"delay", config.AfterUpstreamHeaders, ", duration: 500ms", false, 500 * time.Millisecond},
		{"respond", config.BeforeUpstreamRequest, ", status: 503, body: injected", false, 0},
		{"close_connection", config.BeforeUpstreamRequest, "", true, 0},
		{"close_connection", config.AfterUpstreamHeaders, "", true, 0},
		{"hold_request", config.BeforeUpstreamRequest, ", max_duration: 150ms", true, 150 * time.Millisecond},
		{"hold_response", config.AfterUpstreamHeaders, ", max_duration: 150ms", true, 150 * time.Millisecond},
	}
	for _, inbound := range []bool{false, true} {
		for _, outbound := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("in=%t/out=%t/%s/%s", inbound, outbound, tc.action, tc.phase), func(t *testing.T) {
					var attempts atomic.Int32
					arrived, canceled := make(chan time.Time, 1), make(chan time.Time, 1)
					upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						attempts.Add(1)
						arrived <- time.Now()
						w.WriteHeader(201)
						if tc.phase == config.AfterUpstreamHeaders && tc.disconnect {
							http.NewResponseController(w).Flush()
							<-r.Context().Done()
							canceled <- time.Now()
							return
						}
						io.WriteString(w, "upstream")
					}))
					if outbound {
						upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
						upstream.StartTLS()
					} else {
						upstream.Start()
					}
					t.Cleanup(upstream.Close)
					addr, scheme, extra := address(t), "http", faultRule(tc.action, tc.phase, tc.parameters)
					if inbound {
						extra += fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n", certFile, keyFile)
						scheme = "https"
					}
					if outbound {
						extra += fmt.Sprintf("    upstream_tls: {ca_file: '%s'}\n", certFile)
					}
					reports := make(chan httpproxy.Report, 1)
					start(t, document(t, addr, upstream.URL, extra, ""), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
					c := client(t)
					c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
					began := time.Now()
					resp, err := c.Get(scheme + "://" + addr)
					elapsed := time.Since(began)
					if tc.disconnect {
						if err == nil {
							resp.Body.Close()
							t.Fatal("received final response for disconnect/hold")
						}
						if e, ok := err.(net.Error); ok && e.Timeout() {
							t.Fatalf("client timed out instead of observing close: %v", err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						body, readErr := io.ReadAll(resp.Body)
						resp.Body.Close()
						status, wantBody := 201, "upstream"
						if tc.action == "respond" {
							status, wantBody = 503, "injected"
						}
						if readErr != nil || resp.StatusCode != status || string(body) != wantBody {
							t.Fatalf("status=%d body=%q err=%v", resp.StatusCode, body, readErr)
						}
					}
					if elapsed < tc.wait-25*time.Millisecond || elapsed > tc.wait+2*time.Second {
						t.Fatalf("elapsed=%v expected=%v (-25ms/+2s)", elapsed, tc.wait)
					}
					wantAttempts := int32(0)
					if tc.action == "delay" || tc.phase == config.AfterUpstreamHeaders {
						wantAttempts = 1
						arrival := receive(t, arrived)
						if tc.action == "delay" && tc.phase == config.BeforeUpstreamRequest && arrival.Sub(began) < 475*time.Millisecond {
							t.Fatal("request reached upstream before delay elapsed")
						}
						if tc.action == "delay" && tc.phase == config.AfterUpstreamHeaders && time.Since(arrival) < 475*time.Millisecond {
							t.Fatal("response was not delayed after upstream arrival")
						}
					}
					if tc.phase == config.AfterUpstreamHeaders && tc.disconnect {
						receive(t, canceled)
					}
					r := receive(t, reports)
					if attempts.Load() != wantAttempts || !r.Decision.Selected || !r.Reached || !r.Applied || r.NotReached || r.Err != nil {
						t.Fatalf("attempts=%d report=%+v", attempts.Load(), r)
					}
				})
			}
		}
	}
}

func TestRespondFramingAndKeepAlive(t *testing.T) {
	for _, status := range []int{200, 204, 205, 304, 503} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				addr := address(t)
				reports := make(chan httpproxy.Report, 2)
				start(t, document(t, addr, "http://127.0.0.1:1", faultRule("respond", config.BeforeUpstreamRequest, fmt.Sprintf(", status: %d, body: injected", status)), ""), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
				conn, err := net.Dial("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(conn)
				for range 2 {
					fmt.Fprintf(conn, "%s / HTTP/1.1\r\nHost: test\r\n\r\n", method)
					resp, err := http.ReadResponse(reader, &http.Request{Method: method})
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					want := "injected"
					if method == http.MethodHead || status == 204 || status == 205 || status == 304 {
						want = ""
					}
					if err != nil || resp.StatusCode != status || string(body) != want {
						t.Fatalf("status=%d body=%q err=%v", resp.StatusCode, body, err)
					}
					if r := receive(t, reports); !r.Applied || r.Err != nil {
						t.Fatalf("%+v", r)
					}
				}
			})
		}
	}
}

func TestRespondDuringIncompleteUpload(t *testing.T) {
	addr := address(t)
	start(t, document(t, addr, "http://127.0.0.1:1", faultRule("respond", config.BeforeUpstreamRequest, ", status: 503, body: injected"), ""), true, httpproxy.Options{Executor: fault.Builtin{}})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1000000\r\n\r\nx")
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 503 || string(body) != "injected" {
		t.Fatalf("status=%d body=%q err=%v", resp.StatusCode, body, err)
	}
}

func TestHoldRequestUploadLifecycle(t *testing.T) {
	for _, reason := range []string{"client", "max_duration", "deadline", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			addr := address(t)
			reports, entered := make(chan httpproxy.Report, 1), make(chan struct{}, 1)
			parameters, runtime := ", max_duration: 10s", ""
			if reason == "max_duration" {
				parameters = ", max_duration: 100ms"
			} else if reason == "deadline" {
				runtime = "runtime: {request_timeout: 100ms}"
			}
			_, server := start(t, document(t, addr, "http://127.0.0.1:1", faultRule("hold_request", config.BeforeUpstreamRequest, parameters), runtime), true, httpproxy.Options{Executor: notifyingExecutor{entered: entered}, Observe: func(r httpproxy.Report) { reports <- r }})
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1000000\r\n\r\nx")
			receive(t, entered)
			if reason == "client" {
				conn.Close()
			} else if reason == "shutdown" {
				stopped := make(chan struct{}, 1)
				go func() { server.Close(); stopped <- struct{}{} }()
				receive(t, stopped)
			}
			r := receive(t, reports)
			want := "canceled"
			if reason == "max_duration" {
				want = "completed"
				if !r.Applied || r.Err != nil {
					t.Fatalf("%+v", r)
				}
			}
			if !r.Reached || r.Outcome != want {
				t.Fatalf("%+v", r)
			}
			if reason != "client" {
				data := make([]byte, 1)
				if n, err := conn.Read(data); n != 0 || err == nil {
					t.Fatalf("unexpected final response: %q err=%v", data[:n], err)
				}
			}
		})
	}
}

func TestCloseProbabilityAndListenerIsolation(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); io.WriteString(w, "ok") }))
	t.Cleanup(upstream.Close)
	for _, probability := range []string{"0", "1"} {
		t.Run(probability, func(t *testing.T) {
			addr, other := address(t), address(t)
			extra := strings.Replace(faultRule("close_connection", config.BeforeUpstreamRequest, ""), "probability: 1", "probability: "+probability, 1)
			extra += fmt.Sprintf("  - id: unaffected\n    protocol: http1\n    listen: %s\n    upstream: %s\n", other, upstream.URL)
			reports := make(chan httpproxy.Report, 20)
			start(t, document(t, addr, upstream.URL, extra, ""), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			before := attempts.Load()
			for range 5 {
				c := client(t)
				resp, err := c.Get("http://" + addr)
				if probability == "1" {
					if err == nil {
						resp.Body.Close()
						t.Fatal("connection was not closed")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				r := receive(t, reports)
				if r.Applied != (probability == "1") || r.Decision.Selected != (probability == "1") {
					t.Fatalf("%+v", r)
				}
				if status, body := readResponse(t, c, "http://"+other); status != 200 || body != "ok" {
					t.Fatalf("other listener: %d %q", status, body)
				}
				receive(t, reports)
			}
			want := int32(5)
			if probability == "0" {
				want = 10
			}
			if attempts.Load()-before != want {
				t.Fatal(attempts.Load() - before)
			}
		})
	}
}

func TestBuiltinNotReached(t *testing.T) {
	for _, action := range []string{"delay", "close_connection", "hold_response"} {
		t.Run(action, func(t *testing.T) {
			parameters := ""
			if action == "delay" {
				parameters = ", duration: 500ms"
			} else if action == "hold_response" {
				parameters = ", max_duration: 1s"
			}
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			start(t, document(t, addr, "http://127.0.0.1:1", faultRule(action, config.AfterUpstreamHeaders, parameters), ""), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			if status, _ := readResponse(t, client(t), "http://"+addr); status != 502 {
				t.Fatal(status)
			}
			r := receive(t, reports)
			if !r.Decision.Selected || !r.NotReached || r.Reached || r.Applied || r.Outcome != "upstream_error" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

type notifyingExecutor struct {
	entered chan struct{}
	inner   fault.Builtin
}

func (e notifyingExecutor) Execute(ctx context.Context, action config.Fault, flow fault.Capabilities) (bool, error) {
	e.entered <- struct{}{}
	return e.inner.Execute(ctx, action, flow)
}
