package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	proxy "github.com/plhhao/faultline/internal/proxy/mysql"
	"github.com/plhhao/faultline/internal/recorder"
	driver "github.com/go-sql-driver/mysql"
)

const mysqlImage = "mysql:8.4.8@sha256:2952e3be7807f06fc18de50b3ea1a632d5c70d63482ff7d7376fe3aa8999babf"

func myFixture(t *testing.T, wrongHost ...bool) (string, string, string, func(...string) string) {
	t.Helper()
	if os.Getenv("FAULTLINE_MYSQL_TEST") != "1" {
		t.Skip("set FAULTLINE_MYSQL_TEST=1 for MySQL Docker fixture")
	}
	_, cert, key, _ := certificate(t, len(wrongHost) > 0 && wrongHost[0])
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
	id := docker("run", "--rm", "-d", "-e", "MYSQL_ROOT_PASSWORD=fixture-secret", "-e", "MYSQL_DATABASE=fixture", "-e", "MYSQL_USER=fixture", "-e", "MYSQL_PASSWORD=fixture-secret", "-p", "127.0.0.1::3306", "-v", filepath.Dir(cert)+":/fixture:ro", "--entrypoint", "sh", mysqlImage, "-c", "cp /fixture/cert.pem /tmp/server-cert.pem && chmod 644 /tmp/server-cert.pem && cp /fixture/key.pem /tmp/server-key.pem && chown mysql:mysql /tmp/server-key.pem && chmod 600 /tmp/server-key.pem && exec docker-entrypoint.sh mysqld --ssl-cert=/tmp/server-cert.pem --ssl-key=/tmp/server-key.pem")
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(docker("logs", id))
		}
		docker("rm", "-f", id)
	})
	host := docker("port", id, "3306/tcp")
	db := myOpen(t, host, "")
	until := time.Now().Add(75 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := db.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("startup: %v %s", err, docker("logs", id))
		}
		time.Sleep(200 * time.Millisecond)
	}
	root := func(args ...string) string {
		return docker(append([]string{"exec", id, "mysql", "-uroot", "-pfixture-secret", "-e"}, args...)...)
	}
	return host, cert, key, root
}
func myOpen(t *testing.T, host, cert string) *sql.DB {
	t.Helper()
	cfg := driver.NewConfig()
	cfg.User = "fixture"
	cfg.Passwd = "fixture-secret"
	cfg.Net = "tcp"
	cfg.Addr = host
	cfg.DBName = "fixture"
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 3 * time.Second
	cfg.WriteTimeout = 3 * time.Second
	if cert != "" {
		b, err := os.ReadFile(cert)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AppendCertsFromPEM(b)
		cfg.TLS = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func myDoc(t *testing.T, addr, host, extra, action, selector string) *config.Document {
	t.Helper()
	rules := ""
	if action != "" {
		rules = fmt.Sprintf("    rules:\n    - id: commit\n      match: {}\n      select: {%s}\n      fault: {phase: after_commit, %s}\n", selector, action)
	}
	b := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 4s, max_inflight_requests: 20}\nproxies:\n  - id: db\n    protocol: mysql\n    listen: %s\n    upstream: %s\n%s%s", addr, host, extra, rules)
	doc, err := config.Parse([]byte(b), "/tmp/my-test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func myStart(t *testing.T, doc *config.Document) (*control.Service, *recorder.Recorder, *proxy.Server) {
	t.Helper()
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	r := recorder.New(io.Discard, 1024)
	t.Cleanup(func() { r.Close(context.Background()) })
	p, err := proxy.Start(s, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return s, r, p
}
func myExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		t.Fatal(err)
	}
}
func myCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func myCommit(t *testing.T, db *sql.DB, insert bool) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if insert {
		if _, err := tx.Exec("INSERT INTO evidence(op) VALUES (?)", "operation"); err != nil {
			t.Fatal(err)
		}
	}
	return tx.Commit()
}

func TestMySQLReal(t *testing.T) {
	host, cert, key, root := myFixture(t)
	direct := myOpen(t, host, "")
	myExec(t, direct, "CREATE TABLE evidence (op VARCHAR(100)) ENGINE=InnoDB")
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline-TLS=%t", secure), func(t *testing.T) {
			addr := address(t)
			scheme, extra, trust := "mysql://", "", ""
			if secure {
				if err := myOpen(t, host, cert).Ping(); err != nil {
					t.Fatal("direct TLS:", err)
				}
				scheme = "mysqls://"
				trust = cert
				extra = fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n    upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
			}
			_, _, _ = myStart(t, myDoc(t, addr, scheme+host, extra, "", ""))
			for _, cold := range []bool{true, false} {
				if cold {
					root("FLUSH PRIVILEGES")
				}
				db := myOpen(t, addr, trust)
				if err := db.Ping(); err != nil {
					t.Fatal(err)
				}
				var n int
				if err := db.QueryRow("SELECT ?", 42).Scan(&n); err != nil || n != 42 {
					t.Fatal(n, err)
				}
				if err := myCommit(t, db, true); err != nil {
					t.Fatal(err)
				}
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec("INSERT INTO evidence VALUES (?)", "rollback"); err != nil {
					t.Fatal(err)
				}
				if err = tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				if myCount(t, direct, "evidence") != 1 {
					t.Fatal("transaction data")
				}
				myExec(t, direct, "DELETE FROM evidence")
				db.Close()
			}
		})
	}
	for _, secure := range []bool{false, true} {
		for _, action := range []string{"delay, duration: 150ms", "hold_response, max_duration: 150ms", "close_connection"} {
			t.Run(fmt.Sprintf("fault/%t/%s", secure, action), func(t *testing.T) {
				addr := address(t)
				scheme, extra, trust := "mysql://", "", ""
				if secure {
					scheme = "mysqls://"
					trust = cert
					extra = fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n    upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
				}
				s, r, _ := myStart(t, myDoc(t, addr, scheme+host, extra, "action: "+action, "probability: 1"))
				s.SetEnabled(true)
				db := myOpen(t, addr, trust)
				start := time.Now()
				err := myCommit(t, db, true)
				elapsed := time.Since(start)
				if strings.HasPrefix(action, "delay") {
					if err != nil || elapsed < 140*time.Millisecond {
						t.Fatal(err, elapsed)
					}
				} else if err == nil {
					t.Fatal("ACK delivered")
				}
				if myCount(t, direct, "evidence") != 1 || r.Counters().Applied != 1 {
					t.Fatal("missing commit proof", r.Counters())
				}
				myExec(t, direct, "DELETE FROM evidence")
				s.SetEnabled(false)
				if err := db.Ping(); err != nil {
					t.Fatal("pool did not reconnect", err)
				}
			})
		}
	}
	t.Run("not-commit", func(t *testing.T) {
		addr := address(t)
		s, r, _ := myStart(t, myDoc(t, addr, "mysql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		db := myOpen(t, addr, "")
		for _, q := range []string{"SELECT 'COMMIT'", "/* COMMIT */ SELECT 1", "COMMIT", "INSERT INTO evidence VALUES ('autocommit')", "BEGIN", "ROLLBACK", "BEGIN", "CREATE TABLE implicit (id INT)", "COMMIT", "BEGIN", "COMMIT /* unrecognized syntax */"} {
			myExec(t, db, q)
		}
		myExec(t, db, "BEGIN")
		if _, err := db.Exec("INSERT INTO nonexistent VALUES (1)"); err == nil {
			t.Fatal("expected error")
		}
		myExec(t, db, "COMMIT")
		if r.Counters().Applied != 0 {
			t.Fatal("false commit", r.Counters())
		}
		myExec(t, direct, "DELETE FROM evidence")
	})
	t.Run("selectors-reload-disable", func(t *testing.T) {
		addr := address(t)
		doc := myDoc(t, addr, "mysql://"+host, "", "action: close_connection", "probability: 0")
		s, r, _ := myStart(t, doc)
		s.SetEnabled(true)
		db := myOpen(t, addr, "")
		if err := myCommit(t, db, false); err != nil {
			t.Fatal(err)
		}
		result, err := s.Apply(doc)
		if err != nil || result.Changed {
			t.Fatal("no-op reload")
		}
		for _, selector := range []string{"nth: 2", "every: 2"} {
			next := myDoc(t, addr, "mysql://"+host, "", "action: close_connection", selector)
			if _, err = s.Apply(next); err != nil {
				t.Fatal(err)
			}
			if err = myCommit(t, db, false); err != nil {
				t.Fatal(err)
			}
			s.SetEnabled(false)
			if err = myCommit(t, db, false); err != nil {
				t.Fatal(err)
			}
			s.SetEnabled(true)
			if err = myCommit(t, db, false); err == nil {
				t.Fatal("selector ignored")
			}
		}
		if r.Counters().Applied != 2 {
			t.Fatal(r.Counters())
		}
	})
	for _, end := range []string{"client-timeout", "shutdown", "runtime-timeout", "disable"} {
		t.Run(end, func(t *testing.T) {
			addr := address(t)
			doc := myDoc(t, addr, "mysql://"+host, "", "action: hold_response, max_duration: 600ms", "probability: 1")
			if end == "runtime-timeout" {
				cfg := doc.Config()
				cfg.Runtime.RequestTimeout = 200 * time.Millisecond
				b, _ := config.Encode(cfg)
				var err error
				doc, err = config.Parse(b, "/tmp/my.yaml")
				if err != nil {
					t.Fatal(err)
				}
			}
			s, r, p := myStart(t, doc)
			s.SetEnabled(true)
			db := myOpen(t, addr, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			myExec(t, db, "BEGIN")
			done := make(chan error, 1)
			started := time.Now()
			go func() { _, err := db.ExecContext(ctx, "COMMIT"); done <- err }()
			until := time.Now().Add(time.Second)
			for r.Counters().Applied == 0 {
				if time.Now().After(until) {
					t.Fatal("hold not applied")
				}
				time.Sleep(time.Millisecond)
			}
			switch end {
			case "client-timeout":
				cancel()
			case "shutdown":
				p.Close()
			case "disable":
				s.SetEnabled(false)
				select {
				case <-done:
					t.Fatal("disable released hold")
				case <-time.After(50 * time.Millisecond):
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("hold succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("unbounded hold")
			}
			if end == "client-timeout" && time.Since(started) > 400*time.Millisecond {
				t.Fatal("client cancellation ignored")
			}
			p.Close()
			if r.Counters().Active != 0 || r.Counters().ActiveFaults != 0 {
				t.Fatal("leak", r.Counters())
			}
		})
	}
	t.Run("tls-untrusted", func(t *testing.T) {
		addr := address(t)
		extra := fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n", cert, key)
		_, _, _ = myStart(t, myDoc(t, addr, "mysqls://"+host, extra, "", ""))
		db := myOpen(t, addr, cert)
		if err := db.Ping(); err == nil {
			t.Fatal("untrusted upstream accepted")
		}
	})
	t.Run("prepared-commit", func(t *testing.T) {
		addr := address(t)
		s, r, _ := myStart(t, myDoc(t, addr, "mysql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		db := myOpen(t, addr, "")
		stmt, err := db.Prepare("COMMIT")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		myExec(t, db, "BEGIN")
		if _, err = stmt.Exec(); err == nil {
			t.Fatal("ACK delivered")
		}
		if r.Counters().Applied != 1 {
			t.Fatal(r.Counters())
		}
	})
	t.Run("rule-order-and-disabled", func(t *testing.T) {
		addr := address(t)
		doc := myDoc(t, addr, "mysql://"+host, "", "action: close_connection", "probability: 0")
		cfg := doc.Config()
		second := cfg.Proxies[0].Rules[0]
		second.ID = "second"
		one := float64(1)
		second.Select.Probability = &one
		cfg.Proxies[0].Rules = append(cfg.Proxies[0].Rules, second)
		encode := func() *config.Document {
			b, err := config.Encode(cfg)
			if err != nil {
				t.Fatal(err)
			}
			d, err := config.Parse(b, "/tmp/mysql.yaml")
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
		service, records, _ := myStart(t, encode())
		service.SetEnabled(true)
		db := myOpen(t, addr, "")
		if err := myCommit(t, db, false); err != nil {
			t.Fatal("first matching rule must win", err)
		}
		cfg.Proxies[0].Rules[0].Enabled = false
		if _, err := service.Apply(encode()); err != nil {
			t.Fatal(err)
		}
		if err := myCommit(t, db, false); err == nil || records.Counters().Applied != 1 {
			t.Fatal("disabled first rule did not fall through")
		}
	})
	t.Run("session-and-statement-bounds", func(t *testing.T) {
		addr := address(t)
		doc := myDoc(t, addr, "mysql://"+host, "", "", "")
		cfg := doc.Config()
		cfg.Runtime.MaxInflightRequests = 1
		b, _ := config.Encode(cfg)
		doc, err := config.Parse(b, "/tmp/mysql.yaml")
		if err != nil {
			t.Fatal(err)
		}
		_, r, p := myStart(t, doc)
		db := myOpen(t, addr, "")
		if err := db.Ping(); err != nil {
			t.Fatal(err)
		}
		other := myOpen(t, addr, "")
		if err := other.Ping(); err == nil {
			t.Fatal("session limit ignored")
		}
		statements := []*sql.Stmt{}
		defer func() {
			for _, stmt := range statements {
				stmt.Close()
			}
		}()
		for i := 0; i < 128; i++ {
			stmt, err := db.Prepare("SELECT ?")
			if err != nil {
				t.Fatal(i, err)
			}
			statements = append(statements, stmt)
		}
		if stmt, err := db.Prepare("SELECT ?"); err == nil {
			stmt.Close()
			t.Fatal("prepared limit ignored")
		}
		p.Close()
		if r.Counters().Active != 0 {
			t.Fatal("leaked active flow")
		}
	})

	t.Run("binary-retry-demo", func(t *testing.T) {
		dir := t.TempDir()
		binary := filepath.Join(dir, "faultline")
		build := exec.Command("go", "build", "-o", binary, "./cmd/faultline")
		build.Dir = "../.."
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
		addr := address(t)
		doc := myDoc(t, addr, "mysql://"+host, "", "action: close_connection", "probability: 1")
		encoded, err := config.Encode(doc.Config())
		if err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(filename, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		socketDir, err := os.MkdirTemp("/tmp", "fl-my-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(socketDir) })
		socket := filepath.Join(socketDir, "admin.sock")
		var logs bytes.Buffer
		cmd := exec.Command(binary, "serve", "--config", filename, "--admin-socket", socket)
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			if t.Failed() {
				t.Log(logs.String())
			}
			if bytes.Contains(logs.Bytes(), []byte("fixture-secret")) {
				t.Error("credentials leaked")
			}
		})
		until := time.Now().Add(5 * time.Second)
		for {
			if err := exec.Command(binary, "status", "--admin-socket", socket).Run(); err == nil {
				break
			}
			if time.Now().After(until) {
				t.Fatal("binary not ready")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if out, err := exec.Command(binary, "enable", "--admin-socket", socket).CombinedOutput(); err != nil {
			t.Fatalf("enable: %v %s", err, out)
		}
		for _, args := range [][]string{{"validate", "--config", filename}, {"disable", "--admin-socket", socket}, {"reload", "--admin-socket", socket, "--config", filename}, {"enable", "--admin-socket", socket}} {
			if out, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: %v %s", args[0], err, out)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		demo := exec.CommandContext(ctx, "go", "run", "./examples/mysql")
		demo.Dir = "../.."
		demo.Env = append(os.Environ(), "FAULTLINE_MYSQL_DIRECT=fixture:fixture-secret@tcp("+host+")/fixture?timeout=2s&readTimeout=3s&writeTimeout=3s", "FAULTLINE_MYSQL_PROXY=fixture:fixture-secret@tcp("+addr+")/fixture?timeout=2s&readTimeout=3s&writeTimeout=3s")
		out, err := demo.CombinedOutput()
		if err != nil {
			t.Fatalf("demo: %v %s", err, out)
		}
		if strings.Count(string(out), "client_commit_success=false") != 4 || !strings.Contains(string(out), "deduplicate=false independently_verified_rows=2") || !strings.Contains(string(out), "deduplicate=true independently_verified_rows=1") {
			t.Fatal(string(out))
		}
	})

}

func TestMySQLWrongHostname(t *testing.T) {
	host, cert, key, _ := myFixture(t, true)
	addr := address(t)
	extra := fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n    upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
	_, _, _ = myStart(t, myDoc(t, addr, "mysqls://"+host, extra, "", ""))
	// Trust this CA and use localhost for the front leg; only backend hostname is wrong.
	_, port, _ := net.SplitHostPort(addr)
	db := myOpen(t, "localhost:"+port, cert)
	if err := db.Ping(); err == nil {
		t.Fatal("upstream hostname mismatch accepted")
	}
}
