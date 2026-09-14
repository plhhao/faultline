package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"faultline/examples/http/paymentdemo"
	"faultline/internal/control/admin"
	"faultline/internal/recorder"
)

func paymentConfig(t *testing.T, listen, upstream string) string {
	t.Helper()
	data, err := os.ReadFile("../../examples/http/paymentdemo/faultline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("127.0.0.1:8080", listen, "http://127.0.0.1:9000", upstream).Replace(string(data))
}

func TestPaymentDemoBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "faultline")
	driver := filepath.Join(t.TempDir(), "paymentdemo")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binary, "./cmd/faultline")
	build.Dir = "../.."
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	build = exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", driver, "./examples/http/paymentdemo/cmd")
	build.Dir = "../.."
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("driver build: %v %s", err, data)
	}
	for _, idempotent := range []bool{false, true} {
		t.Run(fmt.Sprint(idempotent), func(t *testing.T) {
			upstream := httptest.NewServer(paymentdemo.NewService())
			t.Cleanup(upstream.Close)
			addr, socket := address(t), adminSocket(t)
			root := filepath.Join(t.TempDir(), "faultline.yaml")
			if err := os.WriteFile(root, []byte(paymentConfig(t, addr, upstream.URL)), 0600); err != nil {
				t.Fatal(err)
			}
			validate := exec.CommandContext(ctx, binary, "validate", "--config", root)
			if data, err := validate.CombinedOutput(); err != nil {
				t.Fatalf("validate: %v %s", err, data)
			}
			var events, diagnostics bytes.Buffer
			process := exec.Command(binary, "serve", "--config", root, "--admin-socket", socket)
			process.Stdout, process.Stderr = &events, &diagnostics
			if err := process.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- process.Wait() }()
			var once sync.Once
			stop := func() {
				once.Do(func() {
					process.Process.Signal(os.Interrupt)
					select {
					case err := <-done:
						if err != nil {
							t.Errorf("serve: %v %s", err, &diagnostics)
						}
					case <-time.After(8 * time.Second):
						process.Process.Kill()
						<-done
						t.Error("binary shutdown exceeded bound")
					}
				})
			}
			t.Cleanup(stop)
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := admin.Call(ctx, socket, "status", "", time.Second); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("binary did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if status, _ := readResponse(t, client(t), "http://"+addr+"/healthz"); status != 200 || adminStatus(t, socket).Info.Enabled {
				t.Fatal("application readiness must precede injection")
			}
			command := func(operation string, extra ...string) []byte {
				t.Helper()
				args := append([]string{operation, "--admin-socket", socket}, extra...)
				data, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v %s", operation, err, data)
				}
				return data
			}
			command("status")
			command("enable")
			args := []string{"run", "--proxy", "http://" + addr, "--upstream", upstream.URL}
			if idempotent {
				args = append(args, "--idempotent")
			}
			data, err := exec.CommandContext(ctx, driver, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("driver: %v %s", err, data)
			}
			var result paymentdemo.Result
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			t.Logf("%+v", result)
			command("reload", "--config", root)
			command("disable")
			stop()
			assertPaymentEvents(t, events.Bytes(), 2)
		})
	}
}

func assertPaymentEvents(t *testing.T, data []byte, want int) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	finished := 0
	for {
		var event recorder.Event
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if event.Type == "flow_finished" && event.RuleID == "lost-response" {
			finished++
			if !event.Selected || !event.Reached || !event.Applied || event.NotReached || event.UpstreamStatus != 201 || event.FlowID == "" || event.Revision != 1 {
				t.Fatalf("lost-response evidence: %+v", event)
			}
		}
	}
	if finished != want {
		t.Fatalf("expected %d lost-response flows, got %d", want, finished)
	}
}
