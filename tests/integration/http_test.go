package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
)

func address(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func document(t *testing.T, listen, upstream, extra, runtime string) *config.Document {
	t.Helper()
	doc, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\n%s\nproxies:\n  - id: test\n    protocol: http1\n    listen: %s\n    upstream: %s\n%s", runtime, listen, upstream, extra)), t.TempDir()+"/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func start(t *testing.T, doc *config.Document, enabled bool, options httpproxy.Options) (*control.Service, *httpproxy.Server) {
	t.Helper()
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.SetEnabled(enabled)
	p, err := httpproxy.Start(s, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return s, p
}

func client(t *testing.T) *http.Client {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func readResponse(t *testing.T, c *http.Client, url string) (int, string) {
	t.Helper()
	r, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r.StatusCode, string(b)
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
		var zero T
		return zero
	}
}

const rule = `    rules:
      - id: chosen
        select: {probability: 1}
        fault: {action: delay, phase: after_upstream_headers, duration: 1ms}
`

func TestForwardingAndDisabledSelection(t *testing.T) {
	seen := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- fmt.Sprintf("%s %s %s %s %s", r.Method, r.RequestURI, r.Proto, r.Header.Get("X-Remove"), string(b))
		// Write the wire fixture directly so net/http does not rewrite Connection.
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 201 Created\r\nConnection: X-Remove\r\nX-Remove: secret\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\nTrailer: X-Checksum\r\nTransfer-Encoding: chunked\r\n\r\n8\r\nresponse\r\n0\r\nX-Checksum: done\r\n\r\n")
	}))
	defer upstream.Close()
	addr := address(t)
	s, _ := start(t, document(t, addr, upstream.URL, rule, ""), false, httpproxy.Options{})
	req, _ := http.NewRequest("POST", "http://"+addr+"/a%2Fb?x=1;x=2&z=%2F", strings.NewReader("payload"))
	req.Header.Set("Connection", "X-Remove")
	req.Header.Set("X-Remove", "secret")
	resp, err := client(t).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 || string(body) != "response" || resp.Header.Get("X-Remove") != "" || len(resp.Header.Values("Set-Cookie")) != 2 || resp.Trailer.Get("X-Checksum") != "done" {
		t.Fatalf("unexpected response: %+v %s", resp, body)
	}
	if got := receive(t, seen); got != "POST /a%2Fb?x=1;x=2&z=%2F HTTP/1.1  payload" {
		t.Fatal(got)
	}
	counts, _ := s.Acquire().Counters("test", "chosen")
	if counts.Eligible != 0 {
		t.Fatal(counts)
	}
}

func TestStreamingBothDirections(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := make([]byte, 4)
		if _, err := io.ReadFull(r.Body, prefix); err != nil {
			return
		}
		close(first)
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil || n != 8<<20 {
			t.Errorf("upload: %d %v", n, err)
		}
		w.Write([]byte("head"))
		w.(http.Flusher).Flush()
		<-release
		w.Write([]byte("tail"))
	}))
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{})
	reader, writer := io.Pipe()
	done := make(chan *http.Response, 1)
	errors := make(chan error, 1)
	req, _ := http.NewRequest("POST", "http://"+addr, reader)
	c := client(t)
	go func() {
		resp, err := c.Do(req)
		if err != nil {
			errors <- err
			return
		}
		done <- resp
	}()
	go func() {
		defer writer.Close()
		writer.Write([]byte("head"))
		<-first
		chunk := make([]byte, 32<<10)
		for range 256 {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
		}
	}()
	var resp *http.Response
	select {
	case resp = <-done:
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy buffered stream")
	}
	defer resp.Body.Close()
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(resp.Body, prefix); err != nil || string(prefix) != "head" {
		t.Fatalf("prefix: %q %v", prefix, err)
	}
	close(release)
	tail, err := io.ReadAll(resp.Body)
	if err != nil || string(tail) != "tail" {
		t.Fatalf("tail: %q %v", tail, err)
	}
}

func TestNoUpstreamRetryAndClientKeepAlive(t *testing.T) {
	var attempts atomic.Int32
	var connections atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) > 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Write([]byte("ok"))
	}))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{})
	c := client(t)
	readResponse(t, c, "http://"+addr)
	var reused bool
	req, _ := http.NewRequest("GET", "http://"+addr, nil)
	req.Header.Set("Idempotency-Key", "test")
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 502 || attempts.Load() != 2 || connections.Load() != 2 || !reused {
		t.Fatalf("status=%d attempts=%d connections=%d reused=%t", resp.StatusCode, attempts.Load(), connections.Load(), reused)
	}
}

type executorFunc func(context.Context, config.Fault, fault.Capabilities) (bool, error)

func (f executorFunc) Execute(ctx context.Context, action config.Fault, c fault.Capabilities) (bool, error) {
	return f(ctx, action, c)
}

func TestFinalHeadersAndSnapshot(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(103)
		entered <- struct{}{}
		<-release
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	addr := address(t)
	reports := make(chan httpproxy.Report, 2)
	var calls atomic.Int32
	s, _ := start(t, document(t, addr, upstream.URL, rule, ""), true, httpproxy.Options{
		Executor: executorFunc(func(context.Context, config.Fault, fault.Capabilities) (bool, error) { calls.Add(1); return true, nil }),
		Observe:  func(r httpproxy.Report) { reports <- r },
	})
	c := client(t)
	result := make(chan error, 1)
	info := make(chan struct{}, 1)
	req, _ := http.NewRequest("GET", "http://"+addr, nil)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{Got1xxResponse: func(code int, _ textproto.MIMEHeader) error {
		if code == 103 {
			info <- struct{}{}
		}
		return nil
	}}))
	go func() {
		resp, err := c.Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		result <- err
	}()
	receive(t, entered)
	receive(t, info)
	if calls.Load() != 0 {
		t.Fatal("hook ran for informational headers")
	}
	newDoc := document(t, addr, upstream.URL, strings.Replace(rule, "probability: 1", "probability: 0", 1), "")
	if _, err := s.Apply(newDoc); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := receive(t, result); err != nil {
		t.Fatal(err)
	}
	first := receive(t, reports)
	if first.Info.Revision != 1 || !first.Applied || !first.Decision.Selected || calls.Load() != 1 {
		t.Fatalf("%+v", first)
	}
	var reused bool
	next, _ := http.NewRequest("GET", "http://"+addr, nil)
	next = next.WithContext(httptrace.WithClientTrace(next.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
	resp, err := c.Do(next)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !reused {
		t.Fatal("expected next request on existing client connection")
	}
	second := receive(t, reports)
	if second.Info.Revision != 2 || second.Decision.Selected || first.FlowID == second.FlowID {
		t.Fatalf("%+v", second)
	}
}

func TestNotReachedAndUnavailableExecutor(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if broken {
					c, _, _ := w.(http.Hijacker).Hijack()
					c.Close()
					return
				}
				w.Write([]byte("ok"))
			}))
			defer upstream.Close()
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			start(t, document(t, addr, upstream.URL, rule, ""), true, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
			status, _ := readResponse(t, client(t), "http://"+addr)
			r := receive(t, reports)
			if !r.Decision.Selected || r.Applied || r.NotReached != broken {
				t.Fatalf("%+v", r)
			}
			if broken && status != 502 || !broken && status != 501 {
				t.Fatal(status)
			}
		})
	}
}

func TestUnsupportedRequests(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1) }))
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{})
	for _, raw := range []string{
		"CONNECT example.com:443 HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n",
		"GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"GET / HTTP/1.0\r\n\r\n",
	} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(conn, raw)
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		conn.Close()
		if err != nil || resp.StatusCode != 501 {
			t.Fatalf("%v %v", resp, err)
		}
	}
	if attempts.Load() != 0 {
		t.Fatal("unsupported traffic reached upstream")
	}
}

func TestRequestTrailers(t *testing.T) {
	trailer := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		trailer <- r.Trailer.Get("X-Checksum")
		w.WriteHeader(204)
	}))
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, upstream.URL, "", ""), false, httpproxy.Options{})
	req, _ := http.NewRequest("POST", "http://"+addr, strings.NewReader("payload"))
	req.ContentLength = -1
	req.Trailer = http.Header{"X-Checksum": {"done"}}
	resp, err := client(t).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := receive(t, trailer); got != "done" {
		t.Fatalf("trailer lost: %q", got)
	}
}
