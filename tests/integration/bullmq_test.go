package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
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

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/bullmq"
	"github.com/plhhao/faultline/internal/recorder"
)

const redisImage = "redis:7.4.9-alpine@sha256:6ab0b6e7381779332f97b8ca76193e45b0756f38d4c0dcda72dbb3c32061ab99"

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type bullAttempt struct {
	OK    bool   `json:"ok"`
	MS    int    `json:"ms"`
	Error string `json:"error"`
}

type bullResult struct {
	First   bullAttempt  `json:"first"`
	Retry   *bullAttempt `json:"retry"`
	Waiting int          `json:"waiting"`
	Known   *int         `json:"known"`
}

func bullmqEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("FAULTLINE_BULLMQ_TEST") != "1" {
		t.Skip("set FAULTLINE_BULLMQ_TEST=1 for the BullMQ Docker fixture")
	}
	if _, err := os.Stat("../../examples/bullmq/node_modules/bullmq"); err != nil {
		t.Skip("run npm ci in examples/bullmq for the BullMQ fixture")
	}
}

// redisFixture starts a disposable Redis container; conf is written as redis.conf beside dir's files.
func redisFixture(t *testing.T, dir, conf string) string {
	t.Helper()
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "redis.conf"), []byte(conf+"save \"\"\nappendonly no\n"), 0644); err != nil {
		t.Fatal(err)
	}
	id := docker("run", "-d", "-p", "127.0.0.1::6379", "-v", dir+":/fixture:ro", redisImage, "redis-server", "/fixture/redis.conf")
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	until := time.Now().Add(30 * time.Second)
	for !strings.Contains(docker("logs", id), "Ready to accept connections") {
		if time.Now().After(until) {
			t.Fatal("Redis startup timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	host := strings.Fields(docker("port", id, "6379/tcp"))[0]
	var secure *tls.Config
	if strings.Contains(conf, "tls-port 6379") {
		data, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			t.Fatal("invalid fixture CA")
		}
		secure = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"}
	}
	waitRedisReady(t, host, secure, strings.Contains(conf, "user fixture on"), until)
	return host
}

// Probe the published port: a Redis log line does not establish Docker forwarding readiness.
func waitRedisReady(t *testing.T, host string, secure *tls.Config, auth bool, until time.Time) {
	t.Helper()
	probe := func() error {
		dialer := &net.Dialer{Timeout: time.Second}
		var conn net.Conn
		var err error
		if secure != nil {
			conn, err = tls.DialWithDialer(dialer, "tcp", host, secure)
		} else {
			conn, err = dialer.Dial("tcp", host)
		}
		if err != nil {
			return err
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		r := bufio.NewReader(conn)
		if auth {
			if _, err := conn.Write([]byte("*3\r\n$4\r\nAUTH\r\n$7\r\nfixture\r\n$21\r\nfixture-only-password\r\n")); err != nil {
				return err
			}
			if line, err := r.ReadString('\n'); err != nil || line != "+OK\r\n" {
				return fmt.Errorf("fixture authentication not ready")
			}
		}
		if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
			return err
		}
		if line, err := r.ReadString('\n'); err != nil || line != "+PONG\r\n" {
			return fmt.Errorf("fixture PING not ready")
		}
		return nil
	}
	for {
		if err := probe(); err == nil {
			return
		} else if time.Now().After(until) {
			t.Fatalf("published Redis port not ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func bullStart(t *testing.T, source string) (*control.Service, *recorder.Recorder, *lockedBuffer) {
	t.Helper()
	doc, err := config.Parse([]byte(source), filepath.Join(t.TempDir(), "bullmq.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEnabled(true)
	output := &lockedBuffer{}
	records := recorder.New(output, 1024)
	server, err := proxy.Start(service, records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		_ = records.Close(context.Background())
	})
	return service, records, output
}

func bullRun(t *testing.T, listen, redis string, env ...string) bullResult {
	t.Helper()
	_, proxyPort, _ := net.SplitHostPort(listen)
	_, redisPort, _ := net.SplitHostPort(redis)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "../../examples/bullmq/fixture.cjs")
	command.Env = append(os.Environ(), append(env, "PROXY_PORT="+proxyPort, "REDIS_PORT="+redisPort)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		t.Fatalf("fixture: %v %s", err, stderr.String())
	}
	var result bullResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("fixture output %q: %v", out, err)
	}
	return result
}

func bullConfig(listen, upstream, extra, rule string) string {
	rules := ""
	if rule != "" {
		rules = "  rules:\n  - id: chosen\n" + rule
	}
	return fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 3s, max_inflight_requests: 20}\nproxies:\n- id: jobs\n  protocol: bullmq\n  listen: %s\n  upstream: %s\n%s%s", listen, upstream, extra, rules)
}

func settle(t *testing.T, records *recorder.Recorder) recorder.Counters {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for records.Counters().Active != 0 && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	counts := records.Counters()
	if counts.Active != 0 || counts.ActiveFaults != 0 {
		t.Fatalf("leaked flows: %+v", counts)
	}
	return counts
}

func TestBullMQReal(t *testing.T) {
	bullmqEnabled(t)
	redis := redisFixture(t, t.TempDir(), "")
	rule := func(match, fault string) string {
		return fmt.Sprintf("    match: {%s}\n    select: {nth: 1}\n    fault: {phase: after_job_add, %s}\n", match, fault)
	}
	for _, c := range []struct {
		name, rule string
		env        []string
		check      func(bullResult) bool
		applied    uint64
	}{
		{"pass-through", "", nil, func(r bullResult) bool { return r.First.OK && r.Waiting == 1 }, 0},
		{"queue-mismatch", rule("queue: other", "action: close_connection"), nil, func(r bullResult) bool { return r.First.OK && r.Waiting == 1 }, 0},
		{"delay", rule("queue: orders", "action: delay, duration: 300ms"), nil, func(r bullResult) bool { return r.First.OK && r.First.MS >= 280 && r.Waiting == 1 }, 1},
		{"hold", rule("", "action: hold_response, max_duration: 300ms"), nil, func(r bullResult) bool { return !r.First.OK && r.First.MS >= 280 && r.Waiting == 1 }, 1},
		{"close-generated-retry", rule("queue: orders", "action: close_connection"), []string{"RETRY=1"}, func(r bullResult) bool {
			return !r.First.OK && r.Retry.OK && r.Waiting == 2
		}, 1},
		{"close-known-retry", rule("queue: orders", "action: close_connection"), []string{"RETRY=1", "JOB_ID=known-job"}, func(r bullResult) bool {
			return !r.First.OK && r.Retry.OK && r.Waiting == 1 && *r.Known == 1
		}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			listen := address(t)
			queue := fmt.Sprintf("orders-%d", time.Now().UnixNano())
			_, records, output := bullStart(t, bullConfig(listen, "redis://"+redis, "", strings.ReplaceAll(c.rule, "queue: orders", "queue: "+queue)))
			result := bullRun(t, listen, redis, append(c.env, "QUEUE="+queue)...)
			if !c.check(result) {
				t.Fatalf("result: %+v", result)
			}
			if counts := settle(t, records); counts.Applied != c.applied {
				t.Fatalf("counters: %+v", counts)
			}
			if events := output.String(); strings.Contains(events, "known-job") || strings.Contains(events, "fixture\":true") {
				t.Fatal("event leaked job identifier or payload")
			}
		})
	}
}

func TestBullMQTLSReal(t *testing.T) {
	bullmqEnabled(t)
	_, cert, key, _ := certificate(t, false)
	for _, file := range []string{cert, key} {
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(cert)
	redis := redisFixture(t, dir, "port 0\ntls-port 6379\ntls-cert-file /fixture/cert.pem\ntls-key-file /fixture/key.pem\ntls-ca-cert-file /fixture/cert.pem\ntls-auth-clients no\nuser default off\nuser fixture on >fixture-only-password ~* &* +@all\n")
	listen := address(t)
	extra := fmt.Sprintf("  tls: {cert_file: '%s', key_file: '%s'}\n  upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
	_, records, output := bullStart(t, bullConfig(listen, "rediss://"+redis, extra, "    match: {}\n    select: {nth: 1}\n    fault: {phase: after_job_add, action: close_connection}\n"))
	result := bullRun(t, listen, redis, "TLS_DIR="+dir, "REDIS_AUTH=1", "RETRY=1", "QUEUE=secure")
	if result.First.OK || !result.Retry.OK || result.Waiting != 2 {
		t.Fatalf("result: %+v", result)
	}
	if counts := settle(t, records); counts.Applied != 1 {
		t.Fatalf("counters: %+v", counts)
	}
	if strings.Contains(output.String(), "fixture-only-password") {
		t.Fatal("event leaked Redis credential")
	}
}

func TestBullMQUpstreamTLSVerification(t *testing.T) {
	bullmqEnabled(t)
	_, cert, key, _ := certificate(t, true)
	for _, file := range []string{cert, key} {
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(cert)
	redis := redisFixture(t, dir, "port 0\ntls-port 6379\ntls-cert-file /fixture/cert.pem\ntls-key-file /fixture/key.pem\ntls-ca-cert-file /fixture/cert.pem\ntls-auth-clients no\n")
	listen := address(t)
	extra := fmt.Sprintf("  tls: {cert_file: '%s', key_file: '%s'}\n  upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
	_, records, _ := bullStart(t, bullConfig(listen, "rediss://"+redis, extra, ""))
	// The certificate omits 127.0.0.1, so the upstream leg must fail rather than downgrade.
	result := bullRun(t, listen, redis, "TLS_DIR="+dir, "QUEUE=rejected", "TLS_SERVERNAME=localhost")
	if result.First.OK || result.Waiting != 0 {
		t.Fatalf("hostname mismatch was accepted: %+v", result)
	}
	if counts := settle(t, records); counts.Total != 0 {
		t.Fatalf("rejected connection produced flows: %+v", counts)
	}
}

func TestBullMQContainerRuntime(t *testing.T) {
	if os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
		t.Skip("set FAULTLINE_DOCKER_TEST=1 and FAULTLINE_BULLMQ_TEST=1")
	}
	bullmqEnabled(t)
	redis := redisFixture(t, t.TempDir(), "")
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	tag := fmt.Sprintf("faultline-bullmq-test:%d", time.Now().UnixNano())
	docker("build", "-f", "../../deploy/docker/Dockerfile", "-t", tag, "../..")
	t.Cleanup(func() { docker("image", "rm", tag) })
	_, port, _ := net.SplitHostPort(redis)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source := bullConfig("0.0.0.0:6379", "redis://host.docker.internal:"+port, "", "    select: {nth: 1}\n    fault: {phase: after_job_add, action: close_connection}\n")
	if err := os.WriteFile(filepath.Join(dir, "faultline.yaml"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	id := docker("run", "-d", "--add-host", "host.docker.internal:host-gateway", "-p", "127.0.0.1::6379", "-v", dir+":/config:ro", tag, "serve", "--config", "/config/faultline.yaml")
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	listen := strings.Fields(docker("port", id, "6379/tcp"))[0]
	until := time.Now().Add(10 * time.Second)
	for !strings.Contains(docker("logs", id), "Listeners ready") {
		if time.Now().After(until) {
			t.Fatal("container readiness timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitRedisReady(t, listen, nil, false, until)
	docker("exec", id, "/faultline", "enable")
	result := bullRun(t, listen, redis, "RETRY=1", "QUEUE=container")
	if result.First.OK || !result.Retry.OK || result.Waiting != 2 {
		t.Fatalf("container result: %+v", result)
	}
	docker("stop", "--time", "5", id)
	if code := docker("inspect", "--format", "{{.State.ExitCode}}", id); code != "0" {
		t.Fatalf("container exit code %s", code)
	}
}

func TestBullMQTransportPassThrough(t *testing.T) {
	bullmqEnabled(t)
	redis := redisFixture(t, t.TempDir(), "")
	listen := address(t)
	_, records, _ := bullStart(t, bullConfig(listen, "redis://"+redis, "", "    select: {probability: 1}\n    fault: {phase: after_job_add, action: close_connection}\n"))
	dial := func() (net.Conn, *bufio.Reader) {
		t.Helper()
		conn, err := net.DialTimeout("tcp", listen, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		return conn, bufio.NewReader(conn)
	}
	expect := func(r *bufio.Reader, want ...string) {
		t.Helper()
		for _, w := range want {
			line, err := r.ReadString('\n')
			if err != nil || line != w {
				t.Fatalf("reply %q, want %q: %v", line, w, err)
			}
		}
	}
	// Two independent connections; one blocks while the other stays responsive.
	blocking, blockingReplies := dial()
	other, otherReplies := dial()
	if _, err := blocking.Write([]byte("*3\r\n$5\r\nBLPOP\r\n$5\r\nempty\r\n$3\r\n0.5\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Write([]byte("*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n*2\r\n$3\r\nGET\r\n$1\r\nk\r\n*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatal(err)
	}
	expect(otherReplies, "+OK\r\n", "$1\r\n", "v\r\n", "+PONG\r\n")
	expect(blockingReplies, "*-1\r\n")
	for _, rejected := range []string{"*1\r\n$5\r\nMULTI\r\n", "*1\r\n$5\r\nHELLO\r\n", "garbage\r\n", "*1\r\n$99999999\r\n"} {
		conn, replies := dial()
		if _, err := conn.Write([]byte(rejected)); err != nil {
			t.Fatal(err)
		}
		if line, err := replies.ReadString('\n'); err == nil {
			t.Fatalf("%q was answered with %q", rejected, line)
		}
	}
	if counts := settle(t, records); counts.Total != 0 {
		t.Fatalf("raw Redis commands created semantic flows: %+v", counts)
	}
}
