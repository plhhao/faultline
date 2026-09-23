package bullmq

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/recorder"
)

const testConfig = "api_version: faultline/v1alpha1\nruntime: {request_timeout: 500ms, max_inflight_requests: 20}\nproxies:\n- id: q\n  protocol: bullmq\n  listen: localhost:16379\n  upstream: redis://localhost:6379\n  rules:\n  - id: chosen\n    match: {queue: orders}\n    select: {%s}\n    fault: {phase: after_job_add, %s}\n"

type harness struct {
	client, backend net.Conn
	service         *control.Service
	records         *recorder.Recorder
	output          bytes.Buffer
	done            chan struct{}
	cancel          context.CancelFunc
}

func startSession(t *testing.T, selector, action string) *harness {
	t.Helper()
	doc, err := config.Parse([]byte(fmt.Sprintf(testConfig, selector, action)), "/tmp/bullmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return startDocument(t, doc)
}

func startDocument(t *testing.T, doc *config.Document) *harness {
	t.Helper()
	var err error
	h := &harness{done: make(chan struct{})}
	h.service, err = control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	h.service.SetEnabled(true)
	h.records = recorder.New(&h.output, 1024)
	c, client := net.Pipe()
	u, backend := net.Pipe()
	h.client = client
	h.backend = backend
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	srv := &Server{service: h.service, records: h.records, runtime: doc.Config().Runtime, pending: make(chan struct{}, 20)}
	s := &session{server: srv, endpoint: &endpoint{proxy: doc.Config().Proxies[0]}, ctx: ctx, client: c, upstream: u}
	go func() { defer close(h.done); s.exchange() }()
	t.Cleanup(func() { h.close(t) })
	return h
}

func (h *harness) close(t *testing.T) {
	t.Helper()
	h.cancel()
	h.client.Close()
	h.backend.Close()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("session did not stop")
	}
	if err := h.records.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := h.records.Counters(); c.Active != 0 || c.ActiveFaults != 0 {
		t.Fatalf("leaked counters: %+v", c)
	}
}

func (h *harness) terminal(t *testing.T) []recorder.Event {
	h.close(t)
	var events []recorder.Event
	decoder := json.NewDecoder(bytes.NewReader(h.output.Bytes()))
	for decoder.More() {
		var e recorder.Event
		if err := decoder.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if e.Type == "flow_finished" {
			events = append(events, e)
		}
	}
	if strings.Contains(h.output.String(), "secret-payload") || strings.Contains(h.output.String(), "opaque-") {
		t.Fatal("arguments leaked")
	}
	return events
}

func exchangeReply(t *testing.T, h *harness, args []string, reply string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := readFrame(bufio.NewReader(h.backend))
		if err == nil {
			err = writeAll(h.backend, []byte(reply))
		}
		done <- err
	}()
	if err := writeAll(h.client, encode(args...)); err != nil {
		return err
	}
	_, err := readFrame(bufio.NewReader(h.client))
	select {
	case backendErr := <-done:
		if backendErr != nil {
			t.Fatal(backendErr)
		}
	case <-time.After(time.Second):
		t.Fatal("backend stuck")
	}
	return err
}

func TestReplySemantics(t *testing.T) {
	for _, reply := range []string{"$3\r\njob\r\n", ":-5\r\n", "-NOSCRIPT missing\r\n", "-ERR fixture\r\n", "$-1\r\n", "+OK\r\n", "$0\r\n\r\n"} {
		t.Run(reply, func(t *testing.T) {
			h := startSession(t, "probability: 1", "action: close_connection")
			err := exchangeReply(t, h, addArgs("orders"), reply)
			success := strings.HasPrefix(reply, "$3")
			if (err != nil) != success {
				t.Fatalf("reply outcome: %v", err)
			}
			events := h.terminal(t)
			if len(events) != 1 || events[0].Reached != success || events[0].Applied != success || events[0].NotReached == success {
				t.Fatalf("events: %+v", events)
			}
		})
	}
}

func TestSelectorsCacheMissAndReload(t *testing.T) {
	h := startSession(t, "nth: 2", "action: close_connection")
	if err := exchangeReply(t, h, addArgs("orders"), "-NOSCRIPT missing\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := exchangeReply(t, h, addArgs("orders"), "$3\r\njob\r\n"); err == nil {
		t.Fatal("second attempt not selected")
	}
	events := h.terminal(t)
	if len(events) != 2 || events[0].EligibleSequence != 1 || events[1].EligibleSequence != 2 || !events[1].Applied {
		t.Fatalf("events: %+v", events)
	}
	for _, operation := range []string{"disable", "reload"} {
		t.Run(operation, func(t *testing.T) {
			h := startSession(t, "probability: 1", "action: close_connection")
			read := make(chan struct{})
			release := make(chan struct{})
			go func() {
				_, _ = readFrame(bufio.NewReader(h.backend))
				close(read)
				<-release
				_ = writeAll(h.backend, []byte("$3\r\njob\r\n"))
			}()
			if err := writeAll(h.client, encode(addArgs("orders")...)); err != nil {
				t.Fatal(err)
			}
			<-read
			if operation == "disable" {
				h.service.SetEnabled(false)
			} else {
				_, err := h.service.Reload([]byte(fmt.Sprintf(testConfig, "probability: 0", "action: close_connection")), "/tmp/bullmq.yaml")
				if err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if _, err := readFrame(bufio.NewReader(h.client)); err == nil {
				t.Fatal("pending decision changed")
			}
			e := h.terminal(t)
			if len(e) != 1 || !e[0].Applied || e[0].Revision != 1 {
				t.Fatalf("snapshot: %+v", e)
			}
		})
	}
}

func TestFIFOAndCancellation(t *testing.T) {
	h := startSession(t, "nth: 1", "action: delay, duration: 80ms")
	go func() {
		r := bufio.NewReader(h.backend)
		_, _ = readFrame(r)
		_, _ = readFrame(r)
		_ = writeAll(h.backend, []byte("$3\r\none\r\n$3\r\ntwo\r\n"))
	}()
	start := time.Now()
	if err := writeAll(h.client, append(encode(addArgs("orders")...), encode(addArgs("orders")...)...)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(h.client)
	first, err := readFrame(r)
	if err != nil || string(first.value) != "one" || time.Since(start) < 70*time.Millisecond {
		t.Fatalf("first reply/order: %v", err)
	}
	second, err := readFrame(r)
	if err != nil || string(second.value) != "two" {
		t.Fatalf("second reply/order: %v", err)
	}
	if e := h.terminal(t); len(e) != 2 || !e[0].Applied || e[1].Applied {
		t.Fatalf("events %+v", e)
	}
	for _, end := range []string{"client", "upstream", "shutdown"} {
		t.Run(end, func(t *testing.T) {
			h := startSession(t, "probability: 1", "action: hold_response, max_duration: 10s")
			go func() { _, _ = readFrame(bufio.NewReader(h.backend)); _ = writeAll(h.backend, []byte("$3\r\njob\r\n")) }()
			_ = writeAll(h.client, encode(addArgs("orders")...))
			until := time.Now().Add(time.Second)
			for h.records.Counters().Applied == 0 && time.Now().Before(until) {
				time.Sleep(time.Millisecond)
			}
			if h.records.Counters().Applied != 1 {
				t.Fatal("hold not reached")
			}
			switch end {
			case "client":
				h.client.Close()
			case "upstream":
				h.backend.Close()
			case "shutdown":
				h.cancel()
			}
			select {
			case <-h.done:
			case <-time.After(time.Second):
				t.Fatal("hold did not terminate")
			}
			h.terminal(t)
		})
	}
}

func TestRuleOrderAndRawCommands(t *testing.T) {
	// First-match skips disabled and non-matching rules; probability 0 still owns its queue.
	source := strings.Replace(testConfig, "  - id: chosen\n    match: {queue: orders}\n    select: {%s}\n    fault: {phase: after_job_add, %s}\n",
		"  - id: disabled\n    enabled: false\n    select: {probability: 1}\n    fault: {phase: after_job_add, action: close_connection}\n"+
			"  - id: never\n    enabled: true\n    match: {queue: quiet}\n    select: {probability: 0}\n    fault: {phase: after_job_add, action: close_connection}\n"+
			"  - id: second\n    enabled: true\n    select: {every: 2}\n    fault: {phase: after_job_add, action: close_connection}\n", 1)
	doc, err := config.Parse([]byte(source), "/tmp/bullmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	h := startDocument(t, doc)
	if err := exchangeReply(t, h, []string{"PING"}, "+PONG\r\n"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := exchangeReply(t, h, addArgs("quiet"), "$3\r\njob\r\n"); err != nil {
			t.Fatalf("probability 0 applied: %v", err)
		}
	}
	if err := exchangeReply(t, h, addArgs("orders"), "$3\r\njob\r\n"); err != nil {
		t.Fatal("every: 2 selected first eligible attempt")
	}
	if err := exchangeReply(t, h, addArgs("orders"), "$3\r\njob\r\n"); err == nil {
		t.Fatal("every: 2 did not select second eligible attempt")
	}
	events := h.terminal(t)
	if len(events) != 5 {
		t.Fatalf("raw command created a flow or attempts missing: %+v", events)
	}
	for i, e := range events {
		rule, selected := "never", false
		if i >= 3 {
			rule, selected = "second", i == 4
		}
		if e.RuleID != rule || e.Selected != selected || e.Queue == "" {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
}

func TestUpstreamReplyTimeout(t *testing.T) {
	h := startSession(t, "probability: 1", "action: close_connection")
	go func() { _, _ = readFrame(bufio.NewReader(h.backend)) }()
	start := time.Now()
	if err := writeAll(h.client, encode(addArgs("orders")...)); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrame(bufio.NewReader(h.client)); err == nil {
		t.Fatal("reply without upstream data")
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("timeout not bounded by request_timeout: %s", elapsed)
	}
	if e := h.terminal(t); len(e) != 1 || e[0].Reached || !e[0].NotReached {
		t.Fatalf("events: %+v", e)
	}
}
