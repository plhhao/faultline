package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/control/admin"
	"github.com/plhhao/faultline/internal/recorder"
)

func TestCLIChildProcess(t *testing.T) {
	if os.Getenv("FAULTLINE_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"faultline"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func testAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func runtimeCommand(t *testing.T, socket, operation string, extra ...string) []byte {
	t.Helper()
	args := append([]string{operation, "--admin-socket", socket}, extra...)
	var output bytes.Buffer
	if err := run(context.Background(), args, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func runtimeStatus(t *testing.T, socket string) admin.Status {
	t.Helper()
	var status admin.Status
	if err := json.Unmarshal(runtimeCommand(t, socket, "status"), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func startProcess(t *testing.T, root, socket string, sink io.Writer) (*bytes.Buffer, func()) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestCLIChildProcess$", "--", "serve", "--config", root, "--admin-socket", socket)
	cmd.Env = append(os.Environ(), "FAULTLINE_TEST_PROCESS=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	if sink != nil {
		cmd.Stdout = sink
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	ready, done := make(chan string, 1), make(chan struct{})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "Listeners ready") {
				ready <- scanner.Text()
			}
		}
	}()
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	stop := func() {
		select {
		case <-done:
		default:
			cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				cmd.Process.Kill()
				<-done
				t.Error("serve did not stop within deadline")
			}
		}
		if waitErr != nil {
			t.Errorf("serve exit: %v", waitErr)
		}
	}
	t.Cleanup(stop)
	select {
	case <-ready:
	case <-done:
		t.Fatalf("serve startup: %v", waitErr)
	case <-time.After(6 * time.Second):
		t.Fatal("serve startup timed out")
	}
	return &output, stop
}

func TestProcessRuntimeReloadAndEvents(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	dir, err := os.MkdirTemp("/tmp", "faultline-process-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root, fragment, socket := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "proxy.yaml"), filepath.Join(dir, "admin.sock")
	addr := testAddress(t)
	write := func(selector string) {
		t.Helper()
		data := fmt.Sprintf("proxies:\n- id: test\n  protocol: http1\n  listen: %s\n  upstream: %s\n  rules:\n  - id: first\n    select: {%s}\n    fault: {phase: before_upstream_request, action: respond, status: 503, body: configured-secret}\n", addr, upstream.URL, selector)
		if err := os.WriteFile(fragment, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(root, []byte("api_version: faultline/v1alpha1\ninclude: [proxy.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write("nth: 1")
	output, stop := startProcess(t, root, socket, nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	get := func(path string) int {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
		req.Header.Set("Authorization", "Bearer request-secret")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := get("/?token=query-secret"); status != 200 {
		t.Fatal(status)
	}
	initial := runtimeStatus(t, socket)
	if initial.Info.Enabled || !initial.Ready || initial.Rules[0].Eligible != 0 {
		t.Fatalf("%+v", initial)
	}
	runtimeCommand(t, socket, "enable")
	if status := get("/"); status != 503 {
		t.Fatal(status)
	}
	runtimeCommand(t, socket, "disable")
	runtimeCommand(t, socket, "disable")
	runtimeCommand(t, socket, "enable")
	if status := get("/"); status != 200 {
		t.Fatal(status)
	}
	if s := runtimeStatus(t, socket); s.Info.ControlSequence != 3 || s.Rules[0].Eligible != 2 {
		t.Fatalf("%+v", s)
	}
	finished := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://" + addr + "/slow")
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				err = fmt.Errorf("old request: %d", resp.StatusCode)
			}
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not reached")
	}
	write("probability: 1")
	var applied control.Result
	json.Unmarshal(runtimeCommand(t, socket, "reload", "--config", root), &applied)
	if !applied.Changed || applied.Info.Revision != 2 || !applied.Info.Enabled {
		t.Fatalf("%+v", applied)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var reused bool
	req, _ := http.NewRequest("GET", "http://"+addr, nil)
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !reused || resp.StatusCode != 503 {
		t.Fatalf("reused=%t status=%d", reused, resp.StatusCode)
	}
	json.Unmarshal(runtimeCommand(t, socket, "reload", "--config", root), &applied)
	if applied.Changed || applied.Info.Revision != 2 {
		t.Fatalf("%+v", applied)
	}
	status := runtimeStatus(t, socket)
	if status.Rules[0].Eligible != 1 || status.Counters.Total != 5 || status.Counters.Selected != 2 || status.Counters.Applied != 2 {
		t.Fatalf("%+v", status)
	}
	stop()
	text := output.String()
	for _, secret := range []string{"configured-secret", "request-secret", "query-secret", "Authorization"} {
		if strings.Contains(text, secret) {
			t.Fatalf("events leaked %q", secret)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	finishedCount, oldRevision := 0, 0
	for {
		var e recorder.Event
		if err := decoder.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if e.RunID != initial.Info.RunID || e.Time.IsZero() {
			t.Fatalf("%+v", e)
		}
		if e.Type == "flow_finished" {
			finishedCount++
			if e.FlowID == "" || e.Protocol != "http1" || e.ProxyID != "test" || e.StartedAt.IsZero() || e.FinishedAt.Before(e.StartedAt) {
				t.Fatalf("%+v", e)
			}
			if e.Revision == 1 {
				oldRevision++
			}
		}
	}
	if finishedCount != 5 || oldRevision != 4 {
		t.Fatalf("finished=%d old_revision=%d", finishedCount, oldRevision)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket not removed: %v", err)
	}
	_, stopAgain := startProcess(t, root, socket, nil)
	if s := runtimeStatus(t, socket); s.Info.Enabled || s.Info.RunID == initial.Info.RunID || s.Info.Revision != 1 {
		t.Fatalf("%+v", s)
	}
	stopAgain()
}

func TestClosedStdoutKeepsAdminAvailable(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "faultline-pipe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root, socket := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "admin.sock")
	data := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: %s\n  upstream: http://127.0.0.1:1\n", testAddress(t))
	if err := os.WriteFile(root, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	defer writer.Close()
	_, stop := startProcess(t, root, socket, writer)
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := runtimeStatus(t, socket)
		if status.Counters.WriteErrors > 0 {
			if status.Counters.Dropped == 0 {
				t.Fatalf("%+v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sink failure was not counted")
		}
		time.Sleep(time.Millisecond)
	}
	runtimeCommand(t, socket, "enable")
	if !runtimeStatus(t, socket).Info.Enabled {
		t.Fatal("admin unavailable after broken pipe")
	}
}
