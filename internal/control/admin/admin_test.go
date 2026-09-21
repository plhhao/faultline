package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

func privateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "faultline-admin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func adminFixture(t *testing.T) (string, *control.Service) {
	t.Helper()
	doc, err := config.Parse([]byte("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: 127.0.0.1:8080\n  upstream: http://127.0.0.1:9000\n  rules:\n  - id: first\n    select: {nth: 1}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n"), "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	records := recorder.New(io.Discard, 64)
	t.Cleanup(func() { records.Close(context.Background()) })
	socket := filepath.Join(privateDir(t), "admin.sock")
	s, err := Start(socket, service, records, func() []control.ListenerStatus {
		return []control.ListenerStatus{{ProxyID: "test", Address: "127.0.0.1:8080", Ready: true}}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return socket, service
}

func call(t *testing.T, socket, command, filename string) []byte {
	t.Helper()
	data, err := Call(context.Background(), socket, command, filename, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstanceIsolationAndNoOp(t *testing.T) {
	one, service := adminFixture(t)
	two, _ := adminFixture(t)
	var initial Status
	if err := json.Unmarshal(call(t, one, "status", ""), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Info.Enabled || !initial.Ready || initial.Rules[0].Eligible != 0 {
		t.Fatalf("%+v", initial)
	}
	for range 2 {
		call(t, one, "enable", "")
	}
	d, _ := service.Acquire().Decide("test", engine.Metadata{})
	if !d.Selected || d.EligibleSequence != 1 {
		t.Fatalf("%+v", d)
	}
	call(t, one, "disable", "")
	call(t, one, "enable", "")
	d, _ = service.Acquire().Decide("test", engine.Metadata{})
	if d.Selected || d.EligibleSequence != 2 {
		t.Fatalf("%+v", d)
	}
	var status Status
	json.Unmarshal(call(t, one, "status", ""), &status)
	if status.Info.ControlSequence != 3 || status.Info.Revision != 1 || status.Rules[0].Eligible != 2 {
		t.Fatalf("%+v", status)
	}
	json.Unmarshal(call(t, two, "status", ""), &status)
	if status.Info.Enabled || status.Info.RunID == initial.Info.RunID {
		t.Fatalf("%+v", status)
	}
	if info, err := os.Stat(one); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
	if _, err := Start(one, service, nil, nil); err == nil {
		t.Fatal("overwrote live instance")
	}
	call(t, one, "status", "")
}

func TestReloadIncludesAndAtomicRejection(t *testing.T) {
	socket, service := adminFixture(t)
	dir := t.TempDir()
	root := filepath.Join(dir, "faultline.yaml")
	fragment := "proxies:\n- id: test\n  protocol: http1\n  listen: 127.0.0.1:8080\n  upstream: http://127.0.0.1:9000\n  rules:\n  - id: first\n    select: {probability: 1}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n"
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "api_version: faultline/v1alpha1\ninclude: [proxy.yaml]\n")
	write(filepath.Join(dir, "proxy.yaml"), fragment)
	call(t, socket, "enable", "")
	call(t, socket, "reload", root)
	d, _ := service.Acquire().Decide("test", engine.Metadata{})
	if !d.Selected || d.EligibleSequence != 1 {
		t.Fatalf("%+v", d)
	}
	before := service.Acquire().Info()
	call(t, socket, "reload", root)
	if service.Acquire().Info() != before {
		t.Fatal("no-op changed info")
	}
	write(root, "api_version: faultline/v1alpha1\n"+fragment)
	call(t, socket, "reload", root)
	if service.Acquire().Info() != before {
		t.Fatal("repartitioning changed revision")
	}
	for _, invalid := range []string{
		"api_version: faultline/v1alpha1\ninclude: [missing/*.yaml]\n",
		"api_version: faultline/v1alpha1\ninclude: [proxy.yaml, duplicate.yaml]\n",
		"api_version: faultline/v1alpha1\n" + strings.Replace(fragment, "9000", "9001", 1),
		"api_version: faultline/v1alpha1\nruntime: {request_timeout: 1s}\n" + fragment,
		"unknown: true\n",
	} {
		write(filepath.Join(dir, "duplicate.yaml"), fragment)
		write(root, invalid)
		if _, err := Call(context.Background(), socket, "reload", root, time.Second); err == nil {
			t.Fatal("accepted invalid reload")
		}
		if service.Acquire().Info() != before {
			t.Fatal("rejected reload changed snapshot")
		}
	}
	call(t, socket, "disable", "")
	write(root, "api_version: faultline/v1alpha1\n"+strings.Replace(fragment, "probability: 1", "probability: 0", 1))
	call(t, socket, "reload", root)
	snapshot := service.Acquire()
	counts, _ := snapshot.Counters("test", "first")
	if snapshot.Info().Enabled || snapshot.Info().Revision != before.Revision+1 || counts.Eligible != 0 {
		t.Fatal(snapshot.Info(), counts)
	}
}

func TestAdminTimeoutAndPermissions(t *testing.T) {
	dir := privateDir(t)
	socket := filepath.Join(dir, "slow.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	if _, err := Call(context.Background(), socket, "status", "", 30*time.Millisecond); err == nil {
		t.Fatal("missing timeout")
	}
	if conn := <-accepted; conn != nil {
		conn.Close()
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(filepath.Join(dir, "open.sock"), nil, nil, nil); err == nil {
		t.Fatal("accepted public socket directory")
	}
}

func TestMalformedAdminRequests(t *testing.T) {
	socket, _ := adminFixture(t)
	for _, request := range []string{
		"POST /reload HTTP/1.1\r\nHost: local\r\nContent-Length: 19\r\nConnection: close\r\n\r\n{\"config\":\"local\"}",
		"GET /status HTTP/1.1\r\nHost: local\r\nOrigin: http://untrusted\r\nConnection: close\r\n\r\n",
		"GET /enable HTTP/1.1\r\nHost: local\r\nConnection: close\r\n\r\n",
	} {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		fmt.Fprint(conn, request)
		data, _ := io.ReadAll(conn)
		conn.Close()
		if strings.Contains(string(data), "200 OK") || len(data) == 0 {
			t.Fatalf("%q", data)
		}
	}
}

func TestConcurrentReloadToggleAndRequests(t *testing.T) {
	socket, service := adminFixture(t)
	dir := t.TempDir()
	var files []string
	for _, probability := range []int{0, 1} {
		filename := filepath.Join(dir, fmt.Sprintf("config-%d.yaml", probability))
		data := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: 127.0.0.1:8080\n  upstream: http://127.0.0.1:9000\n  rules:\n  - id: first\n    select: {probability: %d}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n", probability)
		if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, filename)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 3)
	workers.Go(func() {
		for i := range 30 {
			if _, err := Call(context.Background(), socket, "reload", files[i%2], time.Second); err != nil {
				failures <- err
				return
			}
		}
	})
	workers.Go(func() {
		for i := range 30 {
			operation := "enable"
			if i%2 == 0 {
				operation = "disable"
			}
			if _, err := Call(context.Background(), socket, operation, "", time.Second); err != nil {
				failures <- err
				return
			}
		}
	})
	workers.Go(func() {
		for range 1000 {
			snapshot := service.Acquire()
			decision, err := snapshot.Decide("test", engine.Metadata{})
			if err != nil {
				failures <- err
				return
			}
			if !snapshot.Info().Enabled && decision.Selected {
				failures <- fmt.Errorf("disabled snapshot selected a fault")
				return
			}
		}
	})
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	call(t, socket, "disable", "")
	var status Status
	json.Unmarshal(call(t, socket, "status", ""), &status)
	if status.Info.Enabled || status.Info.Revision < 2 {
		t.Fatalf("%+v", status)
	}
}
