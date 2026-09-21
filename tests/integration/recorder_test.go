package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control/admin"
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
	"github.com/plhhao/faultline/internal/recorder"
)

func adminSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "faultline-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "admin.sock")
}

func adminCall(t *testing.T, socket, operation, filename string) []byte {
	t.Helper()
	data, err := admin.Call(context.Background(), socket, operation, filename, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func adminStatus(t *testing.T, socket string) admin.Status {
	t.Helper()
	var status admin.Status
	if err := json.Unmarshal(adminCall(t, socket, "status", ""), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func awaitCounts(t *testing.T, records *recorder.Recorder, condition func(recorder.Counters) bool) recorder.Counters {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c := records.Counters()
		if condition(c) {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("counter condition timed out: %+v", c)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDisableAndReloadPreserveActiveFault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	t.Cleanup(upstream.Close)
	var output bytes.Buffer
	records := recorder.New(&output, 256)
	t.Cleanup(func() { records.Close(context.Background()) })
	addr := address(t)
	reports := make(chan httpproxy.Report, 3)
	service, server := start(t, document(t, addr, upstream.URL, faultRule("hold_response", config.AfterUpstreamHeaders, ", max_duration: 750ms"), ""), true, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records, Observe: func(r httpproxy.Report) { reports <- r }})
	socket := adminSocket(t)
	management, err := admin.Start(socket, service, records, server.Listeners)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(management.Close)
	c := client(t)
	done := make(chan error, 1)
	go func() {
		resp, err := c.Get("http://" + addr)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	awaitCounts(t, records, func(c recorder.Counters) bool { return c.ActiveFaults == 1 })
	adminCall(t, socket, "disable", "")
	if s := adminStatus(t, socket); s.Info.Enabled || s.Counters.ActiveFaults != 1 || s.Counters.Applied != 1 {
		t.Fatalf("%+v", s)
	}
	if status, body := readResponse(t, c, "http://"+addr); status != 200 || body != "ok" {
		t.Fatalf("%d %q", status, body)
	}
	r := receive(t, reports)
	if r.Info.Enabled || r.Applied {
		t.Fatalf("%+v", r)
	}
	root := filepath.Join(t.TempDir(), "reload.yaml")
	yaml := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: %s\n  upstream: %s\n  rules:\n  - id: inject\n    select: {probability: 1}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n", addr, upstream.URL)
	if err := os.WriteFile(root, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	adminCall(t, socket, "reload", root)
	if s := adminStatus(t, socket); s.Info.Revision != 2 || s.Info.Enabled || s.Counters.ActiveFaults != 1 {
		t.Fatalf("%+v", s)
	}
	adminCall(t, socket, "enable", "")
	if status, _ := readResponse(t, c, "http://"+addr); status != 503 {
		t.Fatal(status)
	}
	receive(t, reports)
	if err := receive(t, done); err == nil {
		t.Fatal("held flow received response")
	}
	r = receive(t, reports)
	if r.Info.Revision != 1 || !r.Info.Enabled || !r.Applied || r.Err != nil {
		t.Fatalf("%+v", r)
	}
	counts := awaitCounts(t, records, func(c recorder.Counters) bool { return c.Active == 0 })
	if counts.Total != 3 || counts.Eligible != 2 || counts.Selected != 2 || counts.Applied != 2 || counts.ActiveFaults != 0 {
		t.Fatalf("%+v", counts)
	}
}

type gatedOutput struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *gatedOutput) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return len(p), nil
}

func TestSlowRecorderDoesNotBlockProxyOrAdmin(t *testing.T) {
	sink := &gatedOutput{entered: make(chan struct{}), release: make(chan struct{})}
	records := recorder.New(sink, 1)
	t.Cleanup(func() { close(sink.release); records.Close(context.Background()) })
	addr := address(t)
	service, server := start(t, document(t, addr, "http://127.0.0.1:1", faultRule("respond", config.BeforeUpstreamRequest, ", status: 503"), ""), true, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
	socket := adminSocket(t)
	management, err := admin.Start(socket, service, records, server.Listeners)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(management.Close)
	records.Record(recorder.Event{Type: "control"})
	receive(t, sink.entered)
	c := client(t)
	for range 10 {
		if status, _ := readResponse(t, c, "http://"+addr); status != 503 {
			t.Fatal(status)
		}
	}
	awaitCounts(t, records, func(c recorder.Counters) bool { return c.Active == 0 })
	s := adminStatus(t, socket)
	if s.Counters.Total != 10 || s.Counters.Selected != 10 || s.Counters.Applied != 10 || s.Counters.Dropped == 0 || s.Counters.Pending > 2 {
		t.Fatalf("%+v", s)
	}
	adminCall(t, socket, "disable", "")
	if adminStatus(t, socket).Info.Enabled {
		t.Fatal("admin was blocked")
	}
}

func TestEventOutcomesAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, rule, runtime, outcome, errorKind string
		enabled                                 bool
	}{
		{"not_reached", faultRule("hold_response", config.AfterUpstreamHeaders, ", max_duration: 1s"), "", "upstream_error", "upstream_error", true},
		{"deadline", faultRule("hold_request", config.BeforeUpstreamRequest, ", max_duration: 10s"), "runtime: {request_timeout: 50ms}", "proxy_timeout", "proxy_timeout", true},
		{"respond", faultRule("respond", config.BeforeUpstreamRequest, ", status: 503, body: response-secret"), "", "fault_applied", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			records := recorder.New(&output, 32)
			t.Cleanup(func() { records.Close(context.Background()) })
			addr := address(t)
			_, server := start(t, document(t, addr, "http://127.0.0.1:1", tc.rule, tc.runtime), tc.enabled, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
			req, _ := http.NewRequest("POST", "http://"+addr+"/path-secret?token=query-secret", strings.NewReader("request-secret"))
			req.Header.Set("Authorization", "Bearer authorization-secret")
			resp, _ := client(t).Do(req)
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			awaitCounts(t, records, func(c recorder.Counters) bool { return c.Active == 0 })
			server.Close()
			if err := records.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			for _, secret := range []string{"response-secret", "request-secret", "authorization-secret", "query-secret", "path-secret"} {
				if strings.Contains(text, secret) {
					t.Fatalf("leaked %q", secret)
				}
			}
			decoder := json.NewDecoder(strings.NewReader(text))
			var last recorder.Event
			for {
				var e recorder.Event
				if err := decoder.Decode(&e); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				last = e
			}
			if last.Type != "flow_finished" || last.Outcome != tc.outcome || last.ErrorKind != tc.errorKind || last.Selector != "probability" || last.Probability == nil {
				t.Fatalf("%+v", last)
			}
			if tc.name == "not_reached" && (!last.NotReached || last.Applied || !last.Selected) {
				t.Fatalf("%+v", last)
			}
		})
	}
}

func TestOverloadIsNotSelected(t *testing.T) {
	var output bytes.Buffer
	records := recorder.New(&output, 64)
	t.Cleanup(func() { records.Close(context.Background()) })
	addr := address(t)
	_, server := start(t, document(t, addr, "http://127.0.0.1:1", faultRule("hold_request", config.BeforeUpstreamRequest, ", max_duration: 10s"), "runtime: {max_inflight_requests: 1}"), true, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
	c := client(t)
	done := make(chan error, 1)
	go func() {
		resp, err := c.Get("http://" + addr)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	awaitCounts(t, records, func(c recorder.Counters) bool { return c.ActiveFaults == 1 })
	if status, _ := readResponse(t, c, "http://"+addr); status != 503 {
		t.Fatal(status)
	}
	server.Close()
	receive(t, done)
	records.Close(context.Background())
	counts := records.Counters()
	if counts.Total != 2 || counts.Eligible != 1 || counts.Selected != 1 || counts.Applied != 1 || counts.Active != 0 {
		t.Fatalf("%+v", counts)
	}
	decoder := json.NewDecoder(&output)
	found := false
	for {
		var e recorder.Event
		if err := decoder.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if e.Outcome == "proxy_overload" {
			found = true
			if e.Selected || e.Applied || e.RuleID != "" {
				t.Fatalf("%+v", e)
			}
		}
	}
	if !found {
		t.Fatal("missing overload event")
	}
}
