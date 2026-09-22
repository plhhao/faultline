package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/rabbitmq"
	"github.com/plhhao/faultline/internal/recorder"
	amqp "github.com/rabbitmq/amqp091-go"
)

func rabbitAwaitConfirm(t *testing.T, confirmations <-chan amqp.Confirmation) {
	t.Helper()
	select {
	case confirmation, ok := <-confirmations:
		if !ok || !confirmation.Ack {
			t.Fatalf("confirmation: %+v open=%t", confirmation, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmation timeout")
	}
}

func TestRabbitMQSelectorsReload(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, records, _ := rabbitStart(t, rabbitDoc(t, addr, "amqp://"+host, ""))
	service.SetEnabled(true)
	_, channel := rabbitChannel(t, addr)
	queue, err := channel.QueueDeclare("", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirmations := channel.NotifyPublish(make(chan amqp.Confirmation, 8))
	for _, tc := range []struct {
		selector string
		want     uint64
	}{{"probability: 0", 0}, {"probability: 1", 4}, {"nth: 2", 1}, {"every: 2", 2}} {
		t.Run(tc.selector, func(t *testing.T) {
			data := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 3s, max_inflight_requests: 20}\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: %s\n  upstream: amqp://%s\n  rules:\n  - id: chosen\n    match: {routing_key: '%s'}\n    select: {%s}\n    fault: {phase: after_publish_confirm, action: delay, duration: 10ms}\n", addr, host, queue.Name, tc.selector)
			data = strings.Replace(data, "  rules:\n", "  rules:\n  - id: disabled\n    enabled: false\n    match: {}\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, action: close_connection}\n  - id: wrong-exchange\n    match: {exchange: unmatched}\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, action: close_connection}\n", 1)
			data += "  - id: shadowed\n    match: {}\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, action: close_connection}\n"
			if _, err := service.Reload([]byte(data), "/tmp/rabbitmq.yaml"); err != nil {
				t.Fatal(err)
			}
			before := records.Counters().Applied
			for range 4 {
				if err := channel.PublishWithContext(context.Background(), "", queue.Name, false, false, amqp.Publishing{Body: []byte("fixture")}); err != nil {
					t.Fatal(err)
				}
				rabbitAwaitConfirm(t, confirmations)
			}
			if got := records.Counters().Applied - before; got != tc.want {
				t.Fatalf("applied=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestRabbitMQTLSRejections(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		t.Run(fmt.Sprintf("upstream-wrong-host-%t", wrongHost), func(t *testing.T) {
			cert, certFile, _, _ := certificate(t, wrongHost)
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				done <- conn.(*tls.Conn).Handshake()
			}()
			addr := address(t)
			extra := ""
			if wrongHost {
				extra = fmt.Sprintf("  upstream_tls: {ca_file: '%s'}\n", certFile)
			}
			_, _, _ = rabbitStart(t, rabbitDocExtra(t, addr, "amqps://"+listener.Addr().String(), extra, ""))
			conn, err := rabbitDial(addr, "")
			if conn != nil {
				conn.Close()
			}
			if err == nil {
				t.Fatal("invalid upstream TLS accepted")
			}
			if err := <-done; err == nil {
				t.Fatal("upstream handshake unexpectedly succeeded")
			}
		})
	}
	_, cert, key, _ := certificate(t, false)
	addr := address(t)
	_, _, _ = rabbitStart(t, rabbitDocExtra(t, addr, "amqp://127.0.0.1:1", fmt.Sprintf("  tls: {cert_file: '%s', key_file: '%s'}\n", cert, key), ""))
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", addr, &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12})
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("untrusted listener certificate accepted")
	}
}

func TestRabbitMQPersistentChannelsAndRetry(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, records, server := rabbitStart(t, rabbitDoc(t, addr, "amqp://"+host, "action: close_connection"))
	_, direct := rabbitChannel(t, host)
	queue, err := direct.QueueDeclare("", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	connection, first := rabbitChannel(t, addr)
	second, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	// Survive several negotiated heartbeat intervals before using both channels.
	time.Sleep(3 * time.Second)
	for _, channel := range []*amqp.Channel{first, second} {
		rabbitAwaitConfirm(t, rabbitPublish(t, channel, queue.Name))
	}
	for range 2 {
		delivery, ok, err := second.Get(queue.Name, false)
		if err != nil || !ok {
			t.Fatalf("get: %t %v", ok, err)
		}
		if err := delivery.Nack(false, true); err != nil {
			t.Fatal(err)
		}
		delivery, ok, err = second.Get(queue.Name, false)
		if err != nil || !ok {
			t.Fatalf("redelivery: %t %v", ok, err)
		}
		if err := delivery.Ack(false); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	first, err = connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	service.SetEnabled(true)
	lost := rabbitPublish(t, first, queue.Name)
	select {
	case _, ok := <-lost:
		if ok {
			t.Fatal("unexpected confirm")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection fault timeout")
	}
	service.SetEnabled(false)
	_, retry := rabbitChannel(t, addr)
	rabbitAwaitConfirm(t, rabbitPublish(t, retry, queue.Name))
	// Independent observation proves both the unconfirmed original and retry exist.
	for range 2 {
		if !rabbitGet(t, host, queue.Name) {
			t.Fatal("original or retry missing")
		}
	}
	if rabbitGet(t, host, queue.Name) {
		t.Fatal("proxy duplicated a publish")
	}
	server.Close()
	if counts := records.Counters(); counts.Active != 0 || counts.ActiveFaults != 0 || counts.Applied != 1 {
		t.Fatalf("shutdown counters: %+v", counts)
	}
}

func TestRabbitMQConcurrentSnapshot(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, records, _ := rabbitStart(t, rabbitDoc(t, addr, "amqp://"+host, "action: delay, duration: 500ms"))
	service.SetEnabled(true)
	connection, first := rabbitChannel(t, addr)
	second, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	queue, err := first.QueueDeclare("", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Confirm(false); err != nil {
		t.Fatal(err)
	}
	secondConfirm := second.NotifyPublish(make(chan amqp.Confirmation, 1))
	start := time.Now()
	firstConfirm := rabbitPublish(t, first, queue.Name)
	deadline := time.Now().Add(time.Second)
	for records.Counters().Applied == 0 {
		if time.Now().After(deadline) {
			t.Fatal("delay not started")
		}
		time.Sleep(time.Millisecond)
	}
	service.SetEnabled(false)
	data := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 3s, max_inflight_requests: 20}\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: %s\n  upstream: amqp://%s\n", addr, host)
	if _, err := service.Reload([]byte(data), "/tmp/rabbitmq.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := second.PublishWithContext(context.Background(), "", queue.Name, false, false, amqp.Publishing{Body: []byte("fixture")}); err != nil {
		t.Fatal(err)
	}
	rabbitAwaitConfirm(t, firstConfirm)
	if time.Since(start) < 450*time.Millisecond {
		t.Fatal("reload changed existing delay")
	}
	rabbitAwaitConfirm(t, secondConfirm)
	if records.Counters().Applied != 1 {
		t.Fatal("new publish used old rule")
	}
}

func TestRabbitMQPublisherNack(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, records, _ := rabbitStart(t, rabbitDoc(t, addr, "amqp://"+host, "action: close_connection"))
	connection, channel := rabbitChannel(t, addr)
	queue, err := channel.QueueDeclare("", false, true, false, false, amqp.Table{"x-max-length": int32(1), "x-overflow": "reject-publish"})
	if err != nil {
		t.Fatal(err)
	}
	rabbitAwaitConfirm(t, rabbitPublish(t, channel, queue.Name))
	service.SetEnabled(true)
	confirmed := rabbitPublish(t, channel, queue.Name)
	select {
	case confirmation, ok := <-confirmed:
		if !ok || confirmation.Ack {
			t.Fatalf("expected broker nack: %+v open=%t", confirmation, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broker nack timeout")
	}
	if connection.IsClosed() || records.Counters().Applied != 0 {
		t.Fatal("publisher nack injected a confirm fault")
	}
	if !rabbitGet(t, host, queue.Name) || rabbitGet(t, host, queue.Name) {
		t.Fatal("queue overflow observation mismatch")
	}
}

func TestRabbitMQRecorderSecrecy(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, err := control.New(rabbitDoc(t, addr, "amqp://"+host, ""))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	records := recorder.New(&output, 128)
	defer records.Close(context.Background())
	server, err := proxy.Start(service, records)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bad, err := amqp.Dial("amqp://guest:private-password-marker@" + addr + "/?connection_timeout=2000")
	if bad != nil {
		bad.Close()
	}
	if err == nil {
		t.Fatal("bad credentials accepted")
	}
	_, ch := rabbitChannel(t, addr)
	queue, err := ch.QueueDeclare("", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := ch.PublishWithContext(context.Background(), "", queue.Name, false, false, amqp.Publishing{Body: []byte("private-body-marker"), Headers: amqp.Table{"private-header": "private-header-marker"}}); err != nil {
		t.Fatal(err)
	}
	rabbitAwaitConfirm(t, confirms)
	server.Close()
	if err := records.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if records.Counters().Total != 1 || records.Counters().Active != 0 {
		t.Fatalf("counters: %+v", records.Counters())
	}
	for _, secret := range []string{"private-password-marker", "private-body-marker", "private-header-marker", "guest"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("recorder exposed sensitive traffic")
		}
	}
}

func TestRabbitMQContainerRuntime(t *testing.T) {
	if os.Getenv("FAULTLINE_DOCKER_TEST") != "1" {
		t.Skip("set FAULTLINE_DOCKER_TEST=1 and FAULTLINE_RABBITMQ_TEST=1")
	}
	host := rabbitFixture(t)
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
	tag := fmt.Sprintf("faultline-rabbitmq-test:%d", time.Now().UnixNano())
	docker("build", "-f", "../../deploy/docker/Dockerfile", "-t", tag, "../..")
	t.Cleanup(func() { docker("image", "rm", tag) })
	_, port, err := net.SplitHostPort(host)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: 0.0.0.0:5672\n  upstream: amqp://host.docker.internal:%s\n  rules:\n  - id: close\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, action: close_connection}\n", port)
	if err := os.WriteFile(filepath.Join(dir, "faultline.yaml"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	id := docker("run", "-d", "--add-host", "host.docker.internal:host-gateway", "-p", "127.0.0.1::5672", "-v", dir+":/config:ro", tag, "serve", "--config", "/config/faultline.yaml")
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	addr := strings.Fields(docker("port", id, "5672/tcp"))[0]
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := rabbitDial(addr, "")
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("container readiness: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, direct := rabbitChannel(t, host)
	queue, err := direct.QueueDeclare("", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	docker("exec", id, "/faultline", "enable")
	_, channel := rabbitChannel(t, addr)
	confirms := rabbitPublish(t, channel, queue.Name)
	select {
	case _, ok := <-confirms:
		if ok {
			t.Fatal("container delivered lost confirm")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("container fault timeout")
	}
	if !rabbitGet(t, host, queue.Name) {
		t.Fatal("container publish missing at broker")
	}
	docker("stop", "--time", "5", id)
	if code := docker("inspect", "--format", "{{.State.ExitCode}}", id); code != "0" {
		t.Fatalf("container exit=%s", code)
	}
}
