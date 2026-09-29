package tcp_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/tcp"
	"github.com/plhhao/faultline/internal/recorder"
)

func startEchoUpstream(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Go(func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			})
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
		connections.Wait()
	})
	return listener.Addr().String()
}

func startTCPServer(t *testing.T, upstream, runtime, rules string, enabled bool) (string, string, *control.Service, *recorder.Recorder, *proxy.Server) {
	t.Helper()
	address := freeAddress(t)
	yaml := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {%s}\nproxies:\n  - id: tcp\n    protocol: tcp\n    listen: %s\n    upstream: tcp://%s\n%s", runtime, address, upstream, rules)
	document, err := config.Parse([]byte(yaml), "tcp-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.New(document)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEnabled(enabled)
	records := recorder.New(io.Discard, 256)
	server, err := proxy.Start(service, records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := records.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return address, yaml, service, records, server
}

func echo(t *testing.T, connection net.Conn, payload string) error {
	t.Helper()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	if _, err := io.WriteString(connection, payload); err != nil {
		return err
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, response); err != nil {
		return err
	}
	if string(response) != payload {
		return fmt.Errorf("response %q", response)
	}
	return nil
}

func waitForFlows(t *testing.T, records *recorder.Recorder, total uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for records.Counters().Total < total && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if records.Counters().Total < total {
		t.Fatalf("flow total=%d, want at least %d", records.Counters().Total, total)
	}
}

func waitForIdle(t *testing.T, records *recorder.Recorder) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for records.Counters().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if records.Counters().Active != 0 {
		t.Fatalf("active flows=%d", records.Counters().Active)
	}
}

func TestTCPConnectionPinsDecisionAcrossReload(t *testing.T) {
	upstream := startEchoUpstream(t)
	rules := "    rules:\n      - id: cut\n        enabled: true\n        match: {}\n        select: {probability: 1}\n        fault:\n          phase: on_transfer\n          action: close_connection\n          after_bytes: 4\n          trigger_direction: client_to_upstream\n"
	address, initial, service, records, _ := startTCPServer(t, upstream, "max_inflight_requests: 4, request_timeout: 2s", rules, true)

	oldConnection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer oldConnection.Close()
	waitForFlows(t, records, 1)

	reloaded := strings.Replace(initial, "probability: 1", "probability: 0", 1)
	result, err := service.Reload([]byte(reloaded), "tcp-reload.yaml")
	if err != nil || !result.Changed {
		t.Fatalf("reload result=%+v err=%v", result, err)
	}
	if noop, err := service.Reload([]byte(reloaded), "tcp-reload.yaml"); err != nil || noop.Changed || noop.Info.Revision != result.Info.Revision {
		t.Fatalf("no-op result=%+v err=%v", noop, err)
	}

	_ = oldConnection.SetDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(oldConnection, "ping")
	buffer := make([]byte, 8)
	if _, err := oldConnection.Read(buffer); err == nil {
		if _, err = oldConnection.Read(buffer); err == nil {
			t.Fatal("connection accepted under the old revision did not apply its pinned fault")
		}
	}

	newConnection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := echo(t, newConnection, "one"); err != nil {
		t.Fatal(err)
	}
	if err := echo(t, newConnection, "two"); err != nil {
		t.Fatal(err)
	}
	newConnection.Close()
	counters, ok := service.Acquire().Counters("tcp", "cut")
	if !ok || counters.Eligible != 1 || counters.Selected != 0 {
		t.Fatalf("one reused connection should have one decision: %+v", counters)
	}

	service.SetEnabled(false)
	disabled, err := service.Reload([]byte(initial), "tcp-reload.yaml")
	if err != nil || !disabled.Changed {
		t.Fatalf("disabled reload result=%+v err=%v", disabled, err)
	}
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := echo(t, connection, "safe"); err != nil {
		t.Fatalf("disabled injection affected new connection: %v", err)
	}
}

func TestTCPLifecycleBoundsAndRecovery(t *testing.T) {
	t.Run("inflight timeout and shutdown", func(t *testing.T) {
		upstream := startEchoUpstream(t)
		address, _, _, records, server := startTCPServer(t, upstream, "max_inflight_requests: 1, request_timeout: 250ms", "", false)
		first, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		waitForFlows(t, records, 1)

		overloaded, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := echo(t, overloaded, "blocked"); err == nil {
			t.Fatal("connection above the inflight limit was forwarded")
		}
		overloaded.Close()

		_ = first.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := first.Read(make([]byte, 1)); err == nil {
			t.Fatal("idle connection survived request timeout")
		}

		recovered, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := echo(t, recovered, "ok"); err != nil {
			t.Fatalf("slot was not recovered: %v", err)
		}
		recovered.Close()
		waitForIdle(t, records)

		active, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		waitForFlows(t, records, 3)
		closed := make(chan struct{})
		go func() { server.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("shutdown did not release active TCP connection")
		}
		_ = active.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := active.Read(make([]byte, 1)); err == nil {
			t.Fatal("active connection remained open after shutdown")
		}
		active.Close()
	})

	t.Run("upstream reset and concurrency", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		var accepted int
		var mu sync.Mutex
		go func() {
			for {
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				mu.Lock()
				accepted++
				current := accepted
				mu.Unlock()
				if current == 1 {
					if tcp, ok := connection.(*net.TCPConn); ok {
						_ = tcp.SetLinger(0)
					}
					connection.Close()
					continue
				}
				go func() { defer connection.Close(); _, _ = io.Copy(connection, connection) }()
			}
		}()
		t.Cleanup(func() { listener.Close() })
		address, _, _, _, _ := startTCPServer(t, listener.Addr().String(), "max_inflight_requests: 16, request_timeout: 2s", "", false)

		reset, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := echo(t, reset, "reset"); err == nil {
			t.Fatal("upstream reset was hidden from the client")
		}
		reset.Close()

		var group sync.WaitGroup
		errors := make(chan error, 8)
		for range 8 {
			group.Go(func() {
				connection, err := net.DialTimeout("tcp", address, time.Second)
				if err == nil {
					err = echo(t, connection, "parallel")
					connection.Close()
				}
				errors <- err
			})
		}
		group.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}
