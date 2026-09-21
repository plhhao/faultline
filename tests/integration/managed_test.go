package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/control/admin"
	"github.com/plhhao/faultline/internal/control/remote"
)

func TestManagedDelivery(t *testing.T) {
	for _, mode := range []string{"binary", "docker"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "docker" && os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
				t.Skip("set FAULTLINE_DOCKER_TEST=1")
			}
			dir, err := os.MkdirTemp("/tmp", "faultline-managed-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			dataDir := filepath.Join(dir, "data")
			if err := remote.SetUser(dataDir, "tester", "editor", "managed-test-password", false); err != nil {
				t.Fatal(err)
			}
			_, certFile, keyFile, roots := certificate(t, false)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "upstream-ok") }))
			defer upstream.Close()
			proxyAddr, apiAddr := address(t), address(t)
			root := filepath.Join(dir, "config.yaml")
			upstreamURL := upstream.URL
			rootArg, dataArg, certArg, keyArg := root, dataDir, certFile, keyFile
			listenProxy, listenAPI := proxyAddr, apiAddr
			if mode == "docker" {
				listenProxy, listenAPI = "0.0.0.0:8080", "0.0.0.0:8443"
				upstreamURL = strings.Replace(upstreamURL, "127.0.0.1", "host.docker.internal", 1)
				rootArg, dataArg, certArg, keyArg = "/fixture/config.yaml", "/fixture/data", "/fixture/cert.pem", "/fixture/key.pem"
				for target, source := range map[string]string{"cert.pem": certFile, "key.pem": keyFile} {
					b, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, target), b, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			yaml := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: %s\n  upstream: %s\n  rules:\n  - id: fail\n    select: {probability: 0}\n    fault: {action: respond, phase: before_upstream_request, status: 503}\n", listenProxy, upstreamURL)
			if err := os.WriteFile(root, []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			command := func(name string, args ...string) []byte {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
				defer cancel()
				out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s", name, args, err, out)
				}
				return out
			}
			binary := filepath.Join(dir, "faultline")
			tag := fmt.Sprintf("faultline-managed-test:%d", time.Now().UnixNano())
			if mode == "binary" {
				command("go", "build", "-o", binary, "../../cmd/faultline")
			} else {
				command("docker", "build", "-f", "../../deploy/docker/Dockerfile", "-t", tag, "../..")
				t.Cleanup(func() { exec.Command("docker", "image", "rm", tag).Run() })
			}
			socket := filepath.Join(dir, "admin.sock")
			baseArgs := []string{"serve", "--config", rootArg, "--data-dir", dataArg, "--api-listen", listenAPI, "--api-cert", certArg, "--api-key", keyArg}
			if mode == "binary" {
				baseArgs = append(baseArgs, "--admin-socket", socket)
			}
			var stop func()
			var container string
			start := func() {
				t.Helper()
				if mode == "docker" {
					args := []string{"run", "-d", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--tmpfs", "/tmp:mode=1777", "--add-host", "host.docker.internal:host-gateway", "-v", dir + ":/fixture", "-p", proxyAddr + ":8080", "-p", apiAddr + ":8443", tag}
					container = strings.TrimSpace(string(command("docker", append(args, baseArgs...)...)))
					id := container
					stop = func() {
						command("docker", "stop", "--time", "5", id)
						if strings.TrimSpace(string(command("docker", "inspect", "--format", "{{.State.ExitCode}}", id))) != "0" {
							t.Error("unclean container exit")
						}
						command("docker", "rm", id)
					}
				} else {
					cmd := exec.Command(binary, baseArgs...)
					cmd.Stdout = io.Discard
					var diagnostics bytes.Buffer
					cmd.Stderr = &diagnostics
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { done <- cmd.Wait() }()
					stop = func() {
						cmd.Process.Signal(syscall.SIGTERM)
						select {
						case err := <-done:
							if err != nil {
								t.Errorf("process: %v %s", err, diagnostics.String())
							}
						case <-time.After(8 * time.Second):
							cmd.Process.Kill()
							<-done
							t.Error("shutdown timeout")
						}
					}
				}
			}
			start()
			defer func() {
				if stop != nil {
					stop()
				}
			}()
			jar, _ := cookiejar.New(nil)
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Jar: jar, Timeout: 3 * time.Second}
			base := "https://" + apiAddr
			wait := func() {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					resp, err := client.Get(base + "/")
					if err == nil {
						resp.Body.Close()
						if resp.StatusCode == 200 {
							return
						}
					}
					if time.Now().After(deadline) {
						if mode == "docker" {
							t.Log(string(command("docker", "logs", container)))
						}
						t.Fatal("managed HTTPS startup timed out")
					}
					time.Sleep(30 * time.Millisecond)
				}
			}
			wait()
			csrf := ""
			api := func(method, path string, body any, want int, target any) {
				t.Helper()
				var payload []byte
				if body != nil {
					payload, _ = json.Marshal(body)
				}
				req, _ := http.NewRequest(method, base+"/api/"+path, bytes.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-CSRF-Token", csrf)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != want {
					t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
				}
				if target != nil {
					if err := json.Unmarshal(data, target); err != nil {
						t.Fatal(err)
					}
				}
			}
			login := func() {
				var result map[string]string
				api("POST", "login", map[string]string{"name": "tester", "password": "managed-test-password"}, 200, &result)
				csrf = result["csrf"]
			}
			login()
			var view struct {
				Revision uint64              `json:"revision"`
				Proxies  []remote.ProxyDraft `json:"proxies"`
			}
			api("GET", "config", nil, 200, &view)
			if view.Revision != 1 {
				t.Fatal("bootstrap revision")
			}
			p := 1.0
			view.Proxies[0].Rules[0].Select.Probability = &p
			draft := remote.Draft{BaseRevision: view.Revision, Proxies: view.Proxies}
			api("POST", "validate", draft, 200, nil)
			var result control.Result
			api("POST", "apply", draft, 200, &result)
			if result.Info.Revision != 2 || !result.Changed {
				t.Fatal("apply result")
			}
			api("POST", "apply", draft, 409, nil)
			api("POST", "enable", map[string]any{}, 200, nil)
			resp, err := http.Get("http://" + proxyAddr)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 503 {
				t.Fatalf("fault traffic status %d", resp.StatusCode)
			}
			if mode == "binary" {
				if _, err := admin.Call(context.Background(), socket, "enable", "", time.Second); err == nil {
					t.Fatal("local admin bypassed auth")
				}
			} else {
				if err := exec.Command("docker", "exec", container, "/faultline", "enable").Run(); err == nil {
					t.Fatal("container admin bypassed auth")
				}
			}
			api("POST", "disable", map[string]any{}, 200, nil)
			// Force an actual process crash after an acknowledged commit.
			if mode == "docker" {
				command("docker", "kill", container)
				command("docker", "rm", container)
			} else {
				// Graceful binary restart is covered here; the durable write boundary is covered by store tests.
				stop()
			}
			stop = nil
			if err := os.WriteFile(root, []byte("invalid bootstrap should be ignored"), 0600); err != nil {
				t.Fatal(err)
			}
			start()
			wait()
			api("GET", "session", nil, 401, nil)
			login()
			var status admin.Status
			api("GET", "status", nil, 200, &status)
			if status.Info.Revision != 2 || status.Info.Enabled {
				t.Fatalf("recovery %+v", status.Info)
			}
			api("POST", "enable", map[string]any{}, 200, nil)
			resp, err = http.Get("http://" + proxyAddr)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 503 {
				t.Fatal("recovered rules missing")
			}
			t.Logf("%s: HTTPS login, validate/apply/conflict, fault traffic, local auth boundary and durable restart passed", mode)
		})
	}
}

func TestManagedOfflineConfigure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root.yaml")
	text := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: test\n  protocol: http1\n  listen: %s\n  upstream: http://127.0.0.1:9000\n", address(t))
	if err := os.WriteFile(root, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	s, _, err := remote.Open(dir, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Configure(dir, root); err == nil {
		t.Fatal("live configure allowed")
	}
	s.Close()
	text = strings.Replace(text, ":9000", ":9001", 1)
	os.WriteFile(root, []byte(text), 0600)
	if err := remote.Configure(dir, root); err != nil {
		t.Fatal(err)
	}
	s, doc, err := remote.Open(dir, "absent")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if doc.Config().Proxies[0].Upstream != "http://127.0.0.1:9001" {
		t.Fatal("offline infrastructure not saved")
	}
	data, err := config.Encode(doc.Config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse(data, "roundtrip.yaml"); err != nil {
		t.Fatal(err)
	}
}
