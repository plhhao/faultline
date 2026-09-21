package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/rabbitmq"
	"github.com/plhhao/faultline/internal/recorder"
	amqp "github.com/rabbitmq/amqp091-go"
)

const rabbitMQImage = "rabbitmq:4.2.9-management@sha256:e002010fddc214d38f7c8acf058a2fb62350daa10316431cd2cf959f42761e34"

func rabbitFixture(t *testing.T) string {
	t.Helper()
	if os.Getenv("FAULTLINE_RABBITMQ_TEST") != "1" {
		t.Skip("set FAULTLINE_RABBITMQ_TEST=1 for RabbitMQ Docker fixture")
	}
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
	id := docker("run", "-d", "-p", "127.0.0.1::5672", rabbitMQImage)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	host := strings.Fields(docker("port", id, "5672/tcp"))[0]
	until := time.Now().Add(75 * time.Second)
	for {
		connection, err := amqp.Dial("amqp://guest:guest@" + host + "/")
		if err == nil {
			_ = connection.Close()
			return host
		}
		if time.Now().After(until) {
			t.Fatalf("RabbitMQ startup: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func rabbitTLSFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if os.Getenv("FAULTLINE_RABBITMQ_TEST") != "1" {
		t.Skip("set FAULTLINE_RABBITMQ_TEST=1 for RabbitMQ Docker fixture")
	}
	_, cert, key, _ := certificate(t, false)
	if err := os.Chmod(filepath.Dir(cert), 0755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{cert, key} {
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
	}
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
	command := "cp /fixture/cert.pem /tmp/cert.pem && cp /fixture/key.pem /tmp/key.pem && chmod 644 /tmp/key.pem && printf 'listeners.tcp = none\\nlisteners.ssl.default = 5671\\nssl_options.cacertfile = /tmp/cert.pem\\nssl_options.certfile = /tmp/cert.pem\\nssl_options.keyfile = /tmp/key.pem\\nssl_options.verify = verify_none\\nssl_options.fail_if_no_peer_cert = false\\n' >/tmp/rabbitmq.conf && RABBITMQ_CONFIG_FILE=/tmp/rabbitmq exec docker-entrypoint.sh rabbitmq-server"
	id := docker("run", "-d", "-p", "127.0.0.1::5671", "-v", filepath.Dir(cert)+":/fixture:ro", "--entrypoint", "sh", rabbitMQImage, "-c", command)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	host := strings.Fields(docker("port", id, "5671/tcp"))[0]
	until := time.Now().Add(75 * time.Second)
	for {
		connection, err := rabbitDial(host, cert)
		if err == nil {
			_ = connection.Close()
			return host, cert, key
		}
		if time.Now().After(until) {
			t.Fatalf("RabbitMQ TLS startup: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func rabbitDoc(t *testing.T, address, upstream, action string) *config.Document {
	return rabbitDocExtra(t, address, upstream, "", action)
}

func rabbitDocExtra(t *testing.T, address, upstream, extra, action string) *config.Document {
	t.Helper()
	rules := ""
	if action != "" {
		rules = fmt.Sprintf("  rules:\n  - id: confirm\n    match: {}\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, %s}\n", action)
	}
	doc, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 3s, max_inflight_requests: 20}\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: %s\n  upstream: %s\n%s%s", address, upstream, extra, rules)), "/tmp/rabbitmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func rabbitStart(t *testing.T, doc *config.Document) (*control.Service, *recorder.Recorder, *proxy.Server) {
	t.Helper()
	service, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	records := recorder.New(io.Discard, 1024)
	t.Cleanup(func() { _ = records.Close(context.Background()) })
	server, err := proxy.Start(service, records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return service, records, server
}

func rabbitChannel(t *testing.T, host string) (*amqp.Connection, *amqp.Channel) {
	t.Helper()
	connection, err := rabbitDial(host, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	return connection, channel
}

func rabbitTLSChannel(t *testing.T, host, cert string) (*amqp.Connection, *amqp.Channel) {
	t.Helper()
	connection, err := rabbitDial(host, cert)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	return connection, channel
}

func rabbitDial(host, cert string) (*amqp.Connection, error) {
	config := amqp.Config{Heartbeat: time.Second}
	url := "amqp://guest:guest@" + host + "/?connection_timeout=2000"
	if cert != "" {
		data, err := os.ReadFile(cert)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("fixture CA")
		}
		config.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		url = "amqps://guest:guest@" + host + "/?connection_timeout=2000"
	}
	return amqp.DialConfig(url, config)
}

func rabbitPublish(t *testing.T, channel *amqp.Channel, queue string) <-chan amqp.Confirmation {
	t.Helper()
	if err := channel.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirmed := channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	if err := channel.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{DeliveryMode: amqp.Persistent, Body: []byte("fixture")}); err != nil {
		t.Fatal(err)
	}
	return confirmed
}

func rabbitGet(t *testing.T, host, queue string) bool {
	t.Helper()
	_, channel := rabbitChannel(t, host)
	message, ok, err := channel.Get(queue, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok && string(message.Body) != "fixture" {
		t.Fatal("unexpected message")
	}
	return ok
}

func TestRabbitMQReal(t *testing.T) {
	host := rabbitFixture(t)
	for _, action := range []string{"", "action: delay, duration: 150ms", "action: hold_response, max_duration: 150ms", "action: close_connection"} {
		t.Run(action, func(t *testing.T) {
			address := address(t)
			service, records, _ := rabbitStart(t, rabbitDoc(t, address, "amqp://"+host, action))
			if action != "" {
				service.SetEnabled(true)
			}
			_, setup := rabbitChannel(t, host)
			queue, err := setup.QueueDeclare(fmt.Sprintf("evidence-%d", time.Now().UnixNano()), false, true, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			connection, channel := rabbitChannel(t, address)
			confirmed := rabbitPublish(t, channel, queue.Name)
			start := time.Now()
			select {
			case confirmation, ok := <-confirmed:
				if action == "" || strings.HasPrefix(action, "action: delay") {
					if !ok || !confirmation.Ack || strings.HasPrefix(action, "action: delay") && time.Since(start) < 130*time.Millisecond {
						t.Fatalf("confirmation: %+v open=%t elapsed=%s", confirmation, ok, time.Since(start))
					}
				} else if ok {
					t.Fatal("lost confirmation was delivered")
				}
			case <-time.After(time.Second):
				if action == "" || strings.HasPrefix(action, "action: delay") {
					t.Fatal("confirmation timeout")
				}
			}
			if action != "" && connection.IsClosed() == false && !strings.HasPrefix(action, "action: delay") {
				t.Fatal("fault did not close client connection")
			}
			if !rabbitGet(t, host, queue.Name) {
				t.Fatal("independent broker observation missing")
			}
			if action == "" {
				deliveries, err := channel.Consume(queue.Name, "", false, false, false, false, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := setup.PublishWithContext(context.Background(), "", queue.Name, false, false, amqp.Publishing{Body: []byte("fixture")}); err != nil {
					t.Fatal(err)
				}
				select {
				case delivery := <-deliveries:
					if string(delivery.Body) != "fixture" || delivery.Ack(false) != nil {
						t.Fatal("consumer delivery/ack was not forwarded")
					}
				case <-time.After(time.Second):
					t.Fatal("consumer delivery timeout")
				}
			}
			if action != "" && records.Counters().Applied != 1 {
				t.Fatalf("counters: %+v", records.Counters())
			}
		})
	}
}

func TestRabbitMQTLSReal(t *testing.T) {
	host, cert, key := rabbitTLSFixture(t)
	addr := address(t)
	extra := fmt.Sprintf("  tls: {cert_file: '%s', key_file: '%s'}\n  upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
	service, records, _ := rabbitStart(t, rabbitDocExtra(t, addr, "amqps://"+host, extra, "action: close_connection"))
	service.SetEnabled(true)
	_, setup := rabbitTLSChannel(t, host, cert)
	queue, err := setup.QueueDeclare(fmt.Sprintf("tls-evidence-%d", time.Now().UnixNano()), false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	connection, channel := rabbitTLSChannel(t, addr, cert)
	confirmed := rabbitPublish(t, channel, queue.Name)
	select {
	case _, ok := <-confirmed:
		if ok {
			t.Fatal("lost TLS confirmation was delivered")
		}
	case <-time.After(time.Second):
	}
	if !connection.IsClosed() || records.Counters().Applied != 1 {
		t.Fatalf("connection=%t counters=%+v", connection.IsClosed(), records.Counters())
	}
	_, direct := rabbitTLSChannel(t, host, cert)
	message, ok, err := direct.Get(queue.Name, true)
	if err != nil || !ok || string(message.Body) != "fixture" {
		t.Fatalf("independent TLS observation: ok=%t message=%+v err=%v", ok, message, err)
	}
}

func TestRabbitMQReturnDoesNotInject(t *testing.T) {
	host := rabbitFixture(t)
	addr := address(t)
	service, records, _ := rabbitStart(t, rabbitDoc(t, addr, "amqp://"+host, "action: close_connection"))
	service.SetEnabled(true)
	connection, channel := rabbitChannel(t, addr)
	if err := channel.Confirm(false); err != nil {
		t.Fatal(err)
	}
	returned := channel.NotifyReturn(make(chan amqp.Return, 1))
	confirmed := channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := channel.PublishWithContext(context.Background(), "", "faultline-no-route", true, false, amqp.Publishing{Body: []byte("fixture")}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-returned:
		if message.ReplyCode != 312 {
			t.Fatalf("return: %+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("return timeout")
	}
	select {
	case confirmation, ok := <-confirmed:
		if !ok || !confirmation.Ack {
			t.Fatalf("confirmation: %+v open=%t", confirmation, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation timeout")
	}
	if connection.IsClosed() || records.Counters().Applied != 0 {
		t.Fatalf("return triggered fault: connection=%t counters=%+v", connection.IsClosed(), records.Counters())
	}
}
