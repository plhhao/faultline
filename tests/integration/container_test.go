package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"faultline/internal/control"
	"faultline/internal/control/admin"
)

// This opt-in test builds an ephemeral scratch image; release packaging belongs to phase 5.
func TestContainerRuntime(t *testing.T) {
	if os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
		t.Skip("set FAULTLINE_DOCKER_TEST=1 with a running Docker daemon")
	}
	command := func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	arch := strings.TrimSpace(string(docker("info", "--format", "{{.Architecture}}")))
	switch arch {
	case "aarch64", "arm64":
		arch = "arm64"
	case "x86_64", "amd64":
		arch = "amd64"
	default:
		t.Fatalf("unsupported Docker architecture: %s", arch)
	}
	dir := t.TempDir()
	buildDir := filepath.Join(dir, "image")
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(buildDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "proxies"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path+".tmp", []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(buildDir, "Dockerfile"), "FROM scratch\nCOPY faultline /faultline\nENTRYPOINT [\"/faultline\"]\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(buildDir, "faultline"), "./cmd/faultline")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("Linux build: %v\n%s", err, data)
	}
	_, certFile, keyFile, _ := certificate(t, false)
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
	tag := fmt.Sprintf("faultline-phase4-test:%d", time.Now().UnixNano())
	docker("build", "-t", tag, buildDir)
	t.Cleanup(func() { command("docker", "image", "rm", tag) })
	mount := configDir + ":/config:ro"
	docker("run", "--rm", "-v", mount, tag, "validate", "--config", "/config/faultline.yaml")
	id := strings.TrimSpace(string(docker("run", "-d", "-v", mount, tag, "serve", "--config", "/config/faultline.yaml")))
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
	decode(docker("exec", id, "/faultline", "reload", "--config", "/config/faultline.yaml"), &result)
	if result.Changed {
		t.Fatal("equivalent reload changed revision")
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
}
