package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
)

func TestCancellationDeadlineAndShutdown(t *testing.T) {
	for _, mode := range []string{"client", "deadline", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entered, canceled := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				close(canceled)
			}))
			defer upstream.Close()
			addr := address(t)
			runtime := "runtime: {request_timeout: 5s}"
			if mode == "deadline" {
				runtime = "runtime: {request_timeout: 200ms}"
			}
			reports := make(chan httpproxy.Report, 1)
			_, proxy := start(t, document(t, addr, upstream.URL, rule, runtime), true, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr, nil)
			result := make(chan error, 1)
			c := client(t)
			go func() {
				resp, err := c.Do(req)
				if resp != nil {
					resp.Body.Close()
				}
				result <- err
			}()
			receive(t, entered)
			switch mode {
			case "client":
				cancel()
			case "shutdown":
				proxy.Close()
			}
			receive(t, canceled)
			receive(t, result)
			r := receive(t, reports)
			if !r.NotReached || r.Applied || r.Err == nil {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestInflightLimitReleasedAfterCancel(t *testing.T) {
	entered := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wait" {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	addr := address(t)
	reports := make(chan httpproxy.Report, 2)
	start(t, document(t, addr, upstream.URL, "", "runtime: {max_inflight_requests: 1}"), false, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/wait", nil)
	c := client(t)
	done := make(chan struct{})
	go func() {
		resp, _ := c.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		close(done)
	}()
	receive(t, entered)
	if status, _ := readResponse(t, c, "http://"+addr); status != 503 {
		t.Fatal(status)
	}
	cancel()
	receive(t, done)
	receive(t, reports)
	// Observation precedes returning from the handler; retry only overload responses.
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, body := readResponse(t, c, "http://"+addr)
		if status == 200 && body == "ok" {
			break
		}
		if status != 503 || time.Now().After(deadline) {
			t.Fatalf("slot not released: %d", status)
		}
	}
}

func TestExecutorCapabilitiesAndHookOrder(t *testing.T) {
	for _, mode := range []string{"respond", "close", "cancel_upstream"} {
		t.Run(mode, func(t *testing.T) {
			var attempts atomic.Int32
			canceled := make(chan struct{}, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				canceled <- struct{}{}
			}))
			defer upstream.Close()
			addr := address(t)
			phase := config.BeforeUpstreamRequest
			if mode == "cancel_upstream" {
				phase = config.AfterUpstreamHeaders
			}
			rules := strings.Replace(rule, config.AfterUpstreamHeaders, phase, 1)
			reports := make(chan httpproxy.Report, 1)
			start(t, document(t, addr, upstream.URL, rules, ""), true, httpproxy.Options{
				Executor: executorFunc(func(ctx context.Context, action config.Fault, f fault.Capabilities) (bool, error) {
					if mode == "cancel_upstream" {
						f.CancelUpstream()
						return true, f.Respond(202, "replaced")
					}
					if attempts.Load() != 0 {
						t.Error("before hook ran too late")
					}
					if mode == "close" {
						return true, f.CloseConnection()
					}
					return true, f.Respond(503, "injected")
				}), Observe: func(r httpproxy.Report) { reports <- r },
			})
			resp, err := client(t).Get("http://" + addr)
			if mode == "close" {
				if err == nil {
					resp.Body.Close()
					t.Fatal("expected connection closure")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				expectedStatus, expectedBody := 503, "injected"
				if mode == "cancel_upstream" {
					expectedStatus, expectedBody = 202, "replaced"
					receive(t, canceled)
				}
				if err != nil || resp.StatusCode != expectedStatus || string(body) != expectedBody {
					t.Fatalf("%d %q %v", resp.StatusCode, body, err)
				}
			}
			r := receive(t, reports)
			if !r.Applied || !r.Reached || r.NotReached {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestSlowUploadAndTruncatedResponseCleanup(t *testing.T) {
	for _, slow := range []bool{true, false} {
		t.Run(fmt.Sprint(slow), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if slow {
					io.Copy(io.Discard, r.Body)
					return
				}
				w.Header().Set("Content-Length", "100")
				w.Write([]byte("short"))
			}))
			defer upstream.Close()
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			start(t, document(t, addr, upstream.URL, "", "runtime: {request_timeout: 200ms}"), false, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
			if slow {
				conn, err := net.Dial("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 100\r\n\r\nx")
				_, err = http.ReadResponse(bufio.NewReader(conn), nil)
				if err == nil {
					t.Fatal("slow upload unexpectedly completed")
				}
			} else {
				resp, err := client(t).Get("http://" + addr)
				if err == nil {
					_, err = io.ReadAll(resp.Body)
					resp.Body.Close()
				}
				if err == nil {
					t.Fatal("truncated response silently completed")
				}
			}
			r := receive(t, reports)
			if r.Err == nil || r.Applied {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestMultipleListenersAndStartupRollback(t *testing.T) {
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) }))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) }))
	defer upstreamB.Close()
	addrA, addrB := address(t), address(t)
	data := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- {id: a, protocol: http1, listen: '%s', upstream: '%s'}\n- {id: b, protocol: http1, listen: '%s', upstream: '%s'}", addrA, upstreamA.URL, addrB, upstreamB.URL)
	doc, err := config.Parse([]byte(data), t.TempDir()+"/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, proxy := start(t, doc, false, httpproxy.Options{})
	c := client(t)
	if _, body := readResponse(t, c, "http://"+addrA); body != "A" {
		t.Fatal(body)
	}
	if _, body := readResponse(t, c, "http://"+addrB); body != "B" {
		t.Fatal(body)
	}
	proxy.Close()
	occupied, err := net.Listen("tcp", addrB)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := httpproxy.Start(s, httpproxy.Options{}); err == nil {
		p.Close()
		t.Fatal("expected bind failure")
	}
	available, err := net.Listen("tcp", addrA)
	if err != nil {
		t.Fatalf("listener leaked: %v", err)
	}
	available.Close()
}

func TestEarlyResponseStopsIncompleteUpload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NewResponseController(w).EnableFullDuplex()
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(413)
		w.Write([]byte("no"))
	}))
	defer upstream.Close()
	addr := address(t)
	reports := make(chan httpproxy.Report, 1)
	_, proxy := start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1000000\r\n\r\nx")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 413 || string(body) != "no" {
		t.Fatalf("response %v %q %v", resp.StatusCode, body, err)
	}
	receive(t, reports)
	done := make(chan struct{})
	go func() { proxy.Close(); close(done) }()
	receive(t, done)
}

func TestShutdownDuringHook(t *testing.T) {
	for _, phase := range []string{config.BeforeUpstreamRequest, config.AfterUpstreamHeaders} {
		t.Run(phase, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
			defer upstream.Close()
			entered := make(chan struct{})
			reports := make(chan httpproxy.Report, 1)
			addr := address(t)
			_, proxy := start(t, document(t, addr, upstream.URL, strings.Replace(rule, config.AfterUpstreamHeaders, phase, 1), ""), true, httpproxy.Options{
				Executor: executorFunc(func(ctx context.Context, _ config.Fault, _ fault.Capabilities) (bool, error) {
					close(entered)
					<-ctx.Done()
					return true, ctx.Err()
				}),
				Observe: func(r httpproxy.Report) { reports <- r },
			})
			c := client(t)
			done := make(chan struct{})
			go func() {
				resp, _ := c.Get("http://" + addr)
				if resp != nil {
					resp.Body.Close()
				}
				close(done)
			}()
			receive(t, entered)
			stopped := make(chan struct{})
			go func() { proxy.Close(); close(stopped) }()
			receive(t, stopped)
			receive(t, done)
			r := receive(t, reports)
			if !r.Applied || !r.Reached || r.Outcome != "canceled" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
