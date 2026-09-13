package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"serve", "--help"}, {"validate", "--config", "../../examples/http/faultline.yaml"}, {"validate", "--config", "../../examples/http/multi-file/faultline.yaml"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatal("no output")
		}
	}
	for _, args := range [][]string{{"status"}, {"serve"}, {"validate", "--start-enabled"}, {"validate", "--config", "missing.yaml"}, {"serve", "--config", "x", "extra"}} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

type readyWriter chan string

func (w readyWriter) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }

func TestServeIncludesAndStartEnabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer upstream.Close()
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := l.Addr().String()
			l.Close()
			dir := t.TempDir()
			root := filepath.Join(dir, "faultline.yaml")
			if err := os.WriteFile(root, []byte("api_version: faultline/v1alpha1\ninclude: [proxy.yaml]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			fragment := fmt.Sprintf("proxies:\n- id: test\n  protocol: http1\n  listen: '%s'\n  upstream: '%s'\n  rules:\n  - id: chosen\n    select: {probability: 1}\n    fault: {phase: before_upstream_request, action: close_connection}\n", addr, upstream.URL)
			if err := os.WriteFile(filepath.Join(dir, "proxy.yaml"), []byte(fragment), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			args := []string{"serve", "--config", root}
			if enabled {
				args = append(args, "--start-enabled")
			}
			ready, done := make(readyWriter, 1), make(chan error, 1)
			go func() { done <- run(ctx, args, ready, io.Discard) }()
			select {
			case msg := <-ready:
				if !strings.Contains(msg, fmt.Sprintf("injection_enabled=%t", enabled)) {
					t.Fatal(msg)
				}
			case err := <-done:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("serve did not start")
			}
			c := &http.Client{Timeout: 3 * time.Second}
			resp, err := c.Get("http://" + addr)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			c.CloseIdleConnections()
			want := 200
			if enabled {
				want = 501
			}
			if resp.StatusCode != want {
				t.Fatal(resp.StatusCode)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("serve did not stop")
			}
		})
	}
}

func TestInvalidIncludeDoesNotServe(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "faultline.yaml")
	if err := os.WriteFile(filename, []byte("api_version: faultline/v1alpha1\ninclude: [missing.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"serve", "--config", filename}, &output, io.Discard); err == nil || output.Len() != 0 {
		t.Fatalf("err=%v output=%q", err, output.String())
	}
}
