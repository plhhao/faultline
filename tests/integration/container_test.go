package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/examples/http/paymentdemo"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/control/admin"
)

// This opt-in test builds the delivery Dockerfile and removes its own image/containers.
func TestContainerRuntime(t *testing.T) {
	if os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
		t.Skip("set FAULTLINE_DOCKER_TEST=1 with a running Docker daemon")
	}
	command := func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	docker := func(args ...string) []byte {
		t.Helper()
		data, err := command("docker", args...)
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, data)
		}
		return data
	}
	decode := func(data []byte, target any) {
		t.Helper()
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatalf("invalid admin JSON: %v\n%s", err, data)
		}
	}
	docker("info", "--format", "{{.Architecture}}")
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "proxies"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path+".tmp", []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	_, certFile, keyFile, roots := certificate(t, false)
	for target, source := range map[string]string{"cert.pem": certFile, "key.pem": keyFile} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(configDir, "proxies", target), string(data))
	}
	root := filepath.Join(configDir, "faultline.yaml")
	fragment := "proxies:\n- id: test\n  protocol: http1\n  listen: 0.0.0.0:8080\n  upstream: http://127.0.0.1:9000\n  tls: {cert_file: cert.pem, key_file: key.pem}\n  rules:\n  - id: test\n    select: {probability: 0}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n"
	write(root, "api_version: faultline/v1alpha1\ninclude: [proxies/test.yaml]\n")
	write(filepath.Join(configDir, "proxies", "test.yaml"), fragment)
	tag := fmt.Sprintf("faultline-delivery-test:%d", time.Now().UnixNano())
	docker("build", "-f", "../../deploy/docker/Dockerfile", "-t", tag, "../..")
	t.Cleanup(func() { command("docker", "image", "rm", tag) })
	if user := strings.TrimSpace(string(docker("inspect", "--format", "{{.Config.User}}", tag))); user != "65532:65532" {
		t.Fatalf("expected non-root image, got %s", user)
	}
	mount := configDir + ":/config:ro"
	docker("run", "--rm", "-v", mount, tag, "validate", "--config", "/config/faultline.yaml")
	write(filepath.Join(configDir, "single.yaml"), "api_version: faultline/v1alpha1\n"+strings.Replace(fragment, "cert_file: cert.pem, key_file: key.pem", "cert_file: proxies/cert.pem, key_file: proxies/key.pem", 1))
	docker("run", "--rm", "-v", mount, tag, "validate", "--config", "/config/single.yaml")
	write(filepath.Join(configDir, "bad-cert.yaml"), "api_version: faultline/v1alpha1\n"+strings.ReplaceAll(fragment, "cert_file: cert.pem", "cert_file: absent.pem"))
	if data, err := command("docker", "run", "--rm", "-v", mount, tag, "serve", "--config", "/config/bad-cert.yaml"); err == nil || !strings.Contains(string(data), "tls") {
		t.Fatalf("invalid certificate startup: %v %s", err, data)
	}
	id := strings.TrimSpace(string(docker("run", "-d", "-p", "127.0.0.1::8080", "-v", mount, tag, "serve", "--config", "/config/faultline.yaml")))
	t.Cleanup(func() { command("docker", "rm", "-f", id) })
	var status admin.Status
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := command("docker", "exec", id, "/faultline", "status")
		if err == nil {
			if err := json.Unmarshal(data, &status); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("container startup: %v\n%s", err, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !status.Ready || status.Info.Enabled {
		t.Fatalf("%+v", status)
	}
	docker("exec", id, "/faultline", "enable")
	write(filepath.Join(configDir, "proxies", "test.yaml"), strings.Replace(fragment, "probability: 0", "probability: 1", 1))
	var result control.Result
	decode(docker("exec", id, "/faultline", "reload", "--config", "/config/faultline.yaml"), &result)
	if !result.Changed || result.Info.Revision != 2 || !result.Info.Enabled {
		t.Fatalf("%+v", result)
	}
	port := strings.TrimSpace(string(docker("port", id, "8080/tcp")))
	c := client(t)
	c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
	if status, _ := readResponse(t, c, "https://"+port); status != 503 {
		t.Fatalf("container TLS fault: %d", status)
	}
	decode(docker("exec", id, "/faultline", "reload", "--config", "/config/faultline.yaml"), &result)
	if result.Changed {
		t.Fatal("equivalent reload changed revision")
	}
	write(filepath.Join(configDir, "single.yaml"), "api_version: faultline/v1alpha1\n"+strings.Replace(strings.Replace(fragment, "probability: 0", "probability: 1", 1), "cert_file: cert.pem, key_file: key.pem", "cert_file: proxies/cert.pem, key_file: proxies/key.pem", 1))
	decode(docker("exec", id, "/faultline", "reload", "--config", "/config/single.yaml"), &result)
	if result.Changed || result.Info.Revision != 2 {
		t.Fatalf("single-file equivalent reload: %+v", result)
	}
	write(filepath.Join(configDir, "missing.yaml"), "api_version: faultline/v1alpha1\ninclude: [proxies/absent.yaml]\n")
	if data, err := command("docker", "exec", id, "/faultline", "reload", "--config", "/config/missing.yaml"); err == nil || !strings.Contains(string(data), "/config/missing.yaml: include[0]") {
		t.Fatalf("missing fragment source: %v %s", err, data)
	}
	write(root, "api_version: faultline/v1alpha1\ninclude: [proxies/test.yaml, proxies/duplicate.yaml]\n")
	write(filepath.Join(configDir, "proxies", "duplicate.yaml"), fragment)
	// Wait for the updated fixture to become fully visible through the VM bind mount.
	deadline = time.Now().Add(5 * time.Second)
	for {
		data, err := command("docker", "exec", id, "/faultline", "validate", "--config", "/config/faultline.yaml")
		if err != nil && strings.Contains(string(data), "duplicate proxy ID") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mounted config did not converge: %v %s", err, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if data, err := command("docker", "exec", id, "/faultline", "reload", "--config", "/config/faultline.yaml"); err == nil || !strings.Contains(string(data), "/config/proxies/") || !strings.Contains(string(data), "duplicate proxy ID") {
		t.Fatalf("invalid fragment: %v %s", err, data)
	}
	decode(docker("exec", id, "/faultline", "status"), &status)
	if status.Info.Revision != 2 || !status.Info.Enabled {
		t.Fatalf("%+v", status)
	}
	docker("exec", id, "/faultline", "disable")
	decode(docker("exec", id, "/faultline", "status"), &status)
	if status.Info.Enabled {
		t.Fatal("disable failed")
	}
	docker("stop", "--time", "5", id)
	if code := strings.TrimSpace(string(docker("inspect", "--format", "{{.State.ExitCode}}", id))); code != "0" {
		t.Fatalf("container did not stop cleanly: exit code %s", code)
	}
	runDockerPaymentDemo(t, tag, docker)
}

func runDockerPaymentDemo(t *testing.T, tag string, docker func(...string) []byte) {
	t.Helper()
	upstream := httptest.NewUnstartedServer(paymentdemo.NewService())
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	upstream.Listener.Close()
	upstream.Listener = listener
	upstream.Start()
	t.Cleanup(upstream.Close)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	configDir := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := paymentConfig(t, "0.0.0.0:8080", "http://host.docker.internal:"+port)
	if err := os.WriteFile(filepath.Join(configDir, "faultline.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(docker("run", "-d", "--add-host", "host.docker.internal:host-gateway", "-p", "127.0.0.1::8080", "-v", configDir+":/config:ro", tag, "serve", "--config", "/config/faultline.yaml")))
	t.Cleanup(func() { docker("rm", "-f", id) })
	proxyURL := "http://" + strings.TrimSpace(string(docker("port", id, "8080/tcp")))
	c := client(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := c.Get(proxyURL + "/healthz")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("container application readiness: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	docker("exec", id, "/faultline", "enable")
	for _, idempotent := range []bool{false, true} {
		result, err := paymentdemo.Run(context.Background(), proxyURL, "http://127.0.0.1:"+port, idempotent, 200*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Docker payment demo: %+v", result)
	}
	docker("exec", id, "/faultline", "disable")
	docker("stop", "--time", "10", id)
	// Docker logs includes stderr readiness; only event lines belong to the JSON stream.
	var events strings.Builder
	for line := range strings.SplitSeq(string(docker("logs", id)), "\n") {
		if strings.HasPrefix(line, "{") {
			events.WriteString(line + "\n")
		}
	}
	assertPaymentEvents(t, []byte(events.String()), 4)
	if code := strings.TrimSpace(string(docker("inspect", "--format", "{{.State.ExitCode}}", id))); code != "0" {
		t.Fatalf("payment container exit code %s", code)
	}
}
