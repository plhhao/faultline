package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"faultline/examples/grpc/unary"
	"faultline/internal/control"
	"faultline/internal/control/admin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestExtensionBinary(t *testing.T) { extensionDelivery(t, false) }
func TestExtensionDocker(t *testing.T) {
	if os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
		t.Skip("set FAULTLINE_DOCKER_TEST=1 with a running Docker daemon")
	}
	extensionDelivery(t, true)
}

func extensionDelivery(t *testing.T, container bool) {
	t.Helper()
	command := func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	must := func(name string, args ...string) []byte {
		t.Helper()
		data, err := command(name, args...)
		if err != nil {
			t.Fatalf("%s %v: %v %s", name, args, err, data)
		}
		return data
	}
	id := identity(t, false)
	upstream := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{id.cert}, ClientCAs: id.roots, ClientAuth: tls.RequireAndVerifyClientCert})))
	unary.Register(upstream, unary.Echo{})
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	go upstream.Serve(listener)
	t.Cleanup(upstream.Stop)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	origin := "https://127.0.0.1:" + port
	if container {
		origin = "https://host.docker.internal:" + port
	}
	dir := t.TempDir()
	// Only generated test keys are world-readable so the non-root container can mount them.
	for name, source := range map[string]string{"cert.pem": id.certFile, "key.pem": id.keyFile} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "faultline.yaml")
	addr, socket := address(t), adminSocket(t)
	listen := addr
	if container {
		listen = "0.0.0.0:8080"
	}
	yamlFor := func(action, direction string, enabled bool) string {
		n := 3
		if action == "throttle" {
			n = 16384
		}
		rules := strings.Replace(bodyRule(action, direction, n), "match: {path: /chosen}", "match: {service: faultline.demo.Echo, method: Call}", 1)
		if !enabled {
			rules = strings.Replace(rules, "probability: 1", "probability: 0", 1)
		}
		localID := id
		localID.certFile = "cert.pem"
		localID.keyFile = "key.pem"
		return fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n  - id: test\n    protocol: grpc\n    listen: %s\n    upstream: %s\n%s%s", listen, origin, tlsFields(2, 2, localID), rules)
	}
	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(root+".tmp", []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(root+".tmp", root); err != nil {
			t.Fatal(err)
		}
	}
	write(yamlFor("truncate", "request", false))
	var runAdmin func(string, ...string) ([]byte, error)
	var stop func()
	var logs func() []byte
	configPath := root
	if container {
		tag := fmt.Sprintf("faultline-phase6:%d", time.Now().UnixNano())
		must("docker", "build", "-f", "../../deploy/docker/Dockerfile", "-t", tag, "../..")
		t.Cleanup(func() { command("docker", "image", "rm", tag) })
		configPath = "/config/faultline.yaml"
		mount := dir + ":/config:ro"
		must("docker", "run", "--rm", "-v", mount, tag, "validate", "--config", configPath)
		cid := strings.TrimSpace(string(must("docker", "run", "-d", "--add-host", "host.docker.internal:host-gateway", "-p", "127.0.0.1::8080", "-v", mount, tag, "serve", "--config", configPath)))
		t.Cleanup(func() { command("docker", "rm", "-f", cid) })
		addr = strings.TrimSpace(string(must("docker", "port", cid, "8080/tcp")))
		runAdmin = func(operation string, args ...string) ([]byte, error) {
			return command("docker", append([]string{"exec", cid, "/faultline", operation}, args...)...)
		}
		stop = func() {
			must("docker", "stop", "--time", "5", cid)
			if got := strings.TrimSpace(string(must("docker", "inspect", "--format", "{{.State.ExitCode}}", cid))); got != "0" {
				t.Fatalf("container exit %s", got)
			}
		}
		logs = func() []byte { return must("docker", "logs", cid) }
	} else {
		binary := filepath.Join(t.TempDir(), "faultline")
		must("go", "build", "-trimpath", "-o", binary, "../../cmd/faultline")
		must(binary, "validate", "--config", root)
		var stdout, stderr bytes.Buffer
		process := exec.Command(binary, "serve", "--config", root, "--admin-socket", socket)
		process.Stdout, process.Stderr = &stdout, &stderr
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		var once sync.Once
		stop = func() {
			once.Do(func() {
				process.Process.Signal(os.Interrupt)
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("serve: %v %s", err, &stderr)
					}
				case <-time.After(8 * time.Second):
					process.Process.Kill()
					<-done
					t.Error("shutdown exceeded bound")
				}
			})
		}
		t.Cleanup(stop)
		logs = func() []byte { return append(append([]byte{}, stdout.Bytes()...), stderr.Bytes()...) }
		runAdmin = func(operation string, args ...string) ([]byte, error) {
			return command(binary, append([]string{operation, "--admin-socket", socket}, args...)...)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := runAdmin("status")
		if err == nil {
			var s admin.Status
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if !s.Ready || s.Info.Enabled {
				t.Fatalf("startup %+v", s)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup timeout: %v %s", err, data)
		}
		time.Sleep(30 * time.Millisecond)
	}
	conn := rpcClient(t, addr, 2, id)
	invoke := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return conn.Invoke(ctx, unary.Method, wrapperspb.Bytes(bytes.Repeat([]byte("x"), 4096)), new(wrapperspb.BytesValue))
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		err := invoke()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("application readiness: %v\n%s", err, logs())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if data, err := runAdmin("enable"); err != nil {
		t.Fatalf("enable: %v %s", err, data)
	}
	for _, action := range []string{"truncate", "throttle"} {
		for _, direction := range []string{"request", "response"} {
			write(yamlFor(action, direction, true))
			deadline := time.Now().Add(5 * time.Second)
			for {
				data, err := runAdmin("reload", "--config", configPath)
				if err != nil {
					t.Fatalf("reload: %v %s", err, data)
				}
				var result control.Result
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if result.Changed {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mounted reload did not converge")
				}
				time.Sleep(30 * time.Millisecond)
			}
			before := time.Now()
			err := invoke()
			elapsed := time.Since(before)
			if action == "truncate" && err == nil {
				t.Fatalf("%s truncation succeeded", direction)
			}
			if action == "throttle" && (err != nil || elapsed < 230*time.Millisecond || elapsed > 1500*time.Millisecond) {
				t.Fatalf("%s throttle: %s %v", direction, elapsed, err)
			}
			t.Logf("container=%t action=%s direction=%s elapsed=%s err=%v", container, action, direction, elapsed, err)
		}
	}
	if data, err := runAdmin("disable"); err != nil {
		t.Fatalf("disable: %v %s", err, data)
	}
	if err := invoke(); err != nil {
		t.Fatal(err)
	}
	stop()
	data := logs()
	if bytes.Contains(data, []byte("PRIVATE KEY")) {
		t.Fatal("private key exposed in diagnostics")
	}
	for _, action := range []string{"truncate", "throttle"} {
		if !bytes.Contains(data, []byte(`"action":"`+action+`"`)) {
			t.Fatalf("missing %s recorder evidence", action)
		}
	}
}
