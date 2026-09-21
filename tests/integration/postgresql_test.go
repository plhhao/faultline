package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/proxy/postgresql"
	"faultline/internal/recorder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pgFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if os.Getenv("FAULTLINE_POSTGRES_TEST") != "1" {
		t.Skip("set FAULTLINE_POSTGRES_TEST=1 for PostgreSQL Docker fixture")
	}
	_, cert, key, _ := certificate(t, false)
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	id := docker("run", "--rm", "-d", "-e", "POSTGRES_PASSWORD=fixture-secret", "-e", "POSTGRES_HOST_AUTH_METHOD=scram-sha-256", "-p", "127.0.0.1::5432", "-v", filepath.Dir(cert)+":/fixture:ro", "--entrypoint", "sh", "postgres:17.11-bookworm", "-c", "cp /fixture/cert.pem /tmp/pg-cert.pem && chmod 644 /tmp/pg-cert.pem && cp /fixture/key.pem /tmp/pg-key.pem && chown postgres:postgres /tmp/pg-key.pem && chmod 600 /tmp/pg-key.pem && exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/pg-cert.pem -c ssl_key_file=/tmp/pg-key.pem")
	t.Cleanup(func() { docker("rm", "-f", id) })
	host := docker("port", id, "5432/tcp")
	until := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := pgx.Connect(ctx, "postgres://postgres:fixture-secret@"+host+"/postgres?sslmode=disable")
		cancel()
		if err == nil {
			c.Close(context.Background())
			break
		}
		if time.Now().After(until) {
			t.Fatalf("postgres startup: %v: %s", err, docker("logs", id))
		}
		time.Sleep(100 * time.Millisecond)
	}
	return host, cert, key
}
func pgConnect(t *testing.T, host, cert string, mode pgx.QueryExecMode) *pgx.Conn {
	t.Helper()
	cfg, err := pgx.ParseConfig("postgres://postgres:fixture-secret@" + host + "/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if cert != "" {
		cfg, err = pgx.ParseConfig("postgres://postgres:fixture-secret@" + host + "/postgres?sslmode=verify-full&sslrootcert=" + cert)
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg.DefaultQueryExecMode = mode
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}
func pgDoc(t *testing.T, addr, upstream, extra, action, selector string) *config.Document {
	t.Helper()
	rules := ""
	if action != "" {
		rules = fmt.Sprintf("    rules:\n      - id: commit\n        match: {}\n        select: {%s}\n        fault: {phase: after_commit, %s}\n", selector, action)
	}
	doc, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 5s, max_inflight_requests: 20}\nproxies:\n  - id: db\n    protocol: postgresql\n    listen: %s\n    upstream: %s\n%s%s", addr, upstream, extra, rules)), "/tmp/pg-test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func pgStart(t *testing.T, doc *config.Document) (*control.Service, *recorder.Recorder, *postgresql.Server) {
	t.Helper()
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	r := recorder.New(io.Discard, 1024)
	t.Cleanup(func() { r.Close(context.Background()) })
	p, err := postgresql.Start(s, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return s, r, p
}
func pgExec(t *testing.T, c *pgx.Conn, sql string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err := c.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func pgCount(t *testing.T, c *pgx.Conn, table string) int {
	t.Helper()
	var n int
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := c.QueryRow(ctx, "select count(*) from "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPostgreSQLReal(t *testing.T) {
	host, cert, key := pgFixture(t)
	direct := pgConnect(t, host, "", pgx.QueryExecModeCacheStatement)
	pgExec(t, direct, "create table evidence (id int primary key, op text unique deferrable initially deferred)")
	for _, inTLS := range []bool{false, true} {
		for _, outTLS := range []bool{false, true} {
			t.Run(fmt.Sprintf("baseline/TLS=%t-%t", inTLS, outTLS), func(t *testing.T) {
				addr := address(t)
				scheme, extra, clientCert := "postgresql://", "", ""
				if inTLS {
					extra += fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n", cert, key)
					clientCert = cert
				}
				if outTLS {
					scheme = "postgresqls://"
					extra += fmt.Sprintf("    upstream_tls: {ca_file: '%s'}\n", cert)
				}
				_, _, _ = pgStart(t, pgDoc(t, addr, scheme+host, extra, "", ""))
				for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeSimpleProtocol, pgx.QueryExecModeCacheStatement} {
					c := pgConnect(t, addr, clientCert, mode)
					pgExec(t, c, "begin")
					pgExec(t, c, "insert into evidence values ($1,$2)", 1, "baseline")
					pgExec(t, c, "rollback")
					if pgCount(t, direct, "evidence") != 0 {
						t.Fatal("rollback persisted")
					}
					pgExec(t, c, "begin")
					pgExec(t, c, "insert into evidence values ($1,$2)", 1, "baseline")
					pgExec(t, c, "commit")
					if pgCount(t, direct, "evidence") != 1 {
						t.Fatal("commit missing")
					}
					pgExec(t, direct, "delete from evidence")
				}
			})
		}
	}
	for _, secure := range []bool{false, true} {
		for _, action := range []string{"delay, duration: 150ms", "hold_response, max_duration: 150ms", "close_connection"} {
			t.Run(fmt.Sprintf("fault/%t/%s", secure, action), func(t *testing.T) {
				pgExec(t, direct, "delete from evidence")
				addr := address(t)
				scheme, extra, clientCert := "postgresql://", "", ""
				if secure {
					scheme = "postgresqls://"
					clientCert = cert
					extra = fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n    upstream_tls: {ca_file: '%s'}\n", cert, key, cert)
				}
				s, r, _ := pgStart(t, pgDoc(t, addr, scheme+host, extra, "action: "+action, "probability: 1"))
				s.SetEnabled(true)
				c := pgConnect(t, addr, clientCert, pgx.QueryExecModeCacheStatement)
				pgExec(t, c, "begin")
				pgExec(t, c, "insert into evidence values ($1,$2)", 2, "fault")
				start := time.Now()
				_, err := c.Exec(context.Background(), "commit")
				elapsed := time.Since(start)
				if strings.HasPrefix(action, "delay") {
					if err != nil || elapsed < 140*time.Millisecond {
						t.Fatalf("delay: %v %v", err, elapsed)
					}
				} else if err == nil {
					t.Fatal("expected lost acknowledgment")
				}
				if elapsed > 3*time.Second {
					t.Fatal("unbounded fault")
				}
				if pgCount(t, direct, "evidence") != 1 {
					t.Fatal("independent commit evidence missing")
				}
				if r.Counters().Applied != 1 {
					t.Fatalf("counters: %+v", r.Counters())
				}
			})
		}
	}
	t.Run("errors-not-commit", func(t *testing.T) {
		pgExec(t, direct, "delete from evidence")
		addr := address(t)
		s, r, _ := pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		c := pgConnect(t, addr, "", pgx.QueryExecModeCacheStatement)
		pgExec(t, c, "begin")
		if _, err := c.Exec(context.Background(), "select 1/0"); err == nil {
			t.Fatal("expected query error")
		}
		pgExec(t, c, "commit")
		pgExec(t, c, "begin")
		pgExec(t, c, "insert into evidence values (1,'duplicate'),(2,'duplicate')")
		if _, err := c.Exec(context.Background(), "commit"); err == nil {
			t.Fatal("expected deferred constraint error")
		}
		pgExec(t, c, "begin")
		pgExec(t, c, "rollback")
		if r.Counters().Applied != 0 || pgCount(t, direct, "evidence") != 0 {
			t.Fatal("failed commit misclassified")
		}
	})
	t.Run("reload-disable-probability", func(t *testing.T) {
		addr := address(t)
		doc := pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 0")
		s, r, _ := pgStart(t, doc)
		s.SetEnabled(true)
		c := pgConnect(t, addr, "", pgx.QueryExecModeSimpleProtocol)
		pgExec(t, c, "begin")
		pgExec(t, c, "commit")
		result, err := s.Apply(doc)
		if err != nil || result.Changed {
			t.Fatal("no-op reload")
		}
		next := pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "nth: 2")
		if _, err := s.Apply(next); err != nil {
			t.Fatal(err)
		}
		pgExec(t, c, "begin")
		pgExec(t, c, "commit")
		s.SetEnabled(false)
		pgExec(t, c, "begin")
		pgExec(t, c, "commit")
		s.SetEnabled(true)
		pgExec(t, c, "begin")
		if _, err := c.Exec(context.Background(), "commit"); err == nil {
			t.Fatal("nth ignored on existing session")
		}
		if r.Counters().Applied != 1 {
			t.Fatal("selector counters")
		}
	})
	t.Run("cancel-held-commit", func(t *testing.T) {
		addr := address(t)
		s, r, _ := pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "action: hold_response, max_duration: 4s", "probability: 1"))
		s.SetEnabled(true)
		c := pgConnect(t, addr, "", pgx.QueryExecModeSimpleProtocol)
		pgExec(t, c, "begin")
		done := make(chan error, 1)
		go func() { _, err := c.Exec(context.Background(), "commit"); done <- err }()
		until := time.Now().Add(2 * time.Second)
		for r.Counters().Applied == 0 {
			if time.Now().After(until) {
				t.Fatal("hold not reached")
			}
			time.Sleep(time.Millisecond)
		}
		s.SetEnabled(false)
		select {
		case <-done:
			t.Fatal("disable released hold")
		case <-time.After(50 * time.Millisecond):
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.PgConn().CancelRequest(ctx)
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancel succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not end hold")
		}
	})
	t.Run("tls-reject-untrusted", func(t *testing.T) {
		addr := address(t)
		_, _, _ = pgStart(t, pgDoc(t, addr, "postgresqls://"+host, "", "", ""))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, "postgres://postgres:fixture-secret@"+addr+"/postgres?sslmode=disable")
		if err == nil {
			c.Close(ctx)
			t.Fatal("untrusted TLS accepted")
		}
	})
	t.Run("retry-data-evidence", func(t *testing.T) {
		pgExec(t, direct, "create table retry_plain (op text); create table retry_unique (op text primary key)")
		addr := address(t)
		s, _, _ := pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		for _, unique := range []bool{false, true} {
			table, suffix := "retry_plain", ""
			if unique {
				table = "retry_unique"
				suffix = " on conflict do nothing"
			}
			for i := 0; i < 2; i++ {
				c := pgConnect(t, addr, "", pgx.QueryExecModeCacheStatement)
				pgExec(t, c, "begin")
				pgExec(t, c, "insert into "+table+" values ($1)"+suffix, "same-operation")
				if _, err := c.Exec(context.Background(), "commit"); err == nil {
					t.Fatal("ACK unexpectedly delivered")
				}
			}
			want := 2
			if unique {
				want = 1
			}
			if got := pgCount(t, direct, table); got != want {
				t.Fatalf("%s rows=%d want=%d", table, got, want)
			}
		}
	})
	for _, end := range []string{"client-timeout", "shutdown", "runtime-timeout"} {
		t.Run(end, func(t *testing.T) {
			addr := address(t)
			doc := pgDoc(t, addr, "postgresql://"+host, "", "action: hold_response, max_duration: 10s", "probability: 1")
			if end == "runtime-timeout" {
				cfg := doc.Config()
				cfg.Runtime.RequestTimeout = 400 * time.Millisecond
				b, _ := config.Encode(cfg)
				var err error
				doc, err = config.Parse(b, "/tmp/pg-test.yaml")
				if err != nil {
					t.Fatal(err)
				}
			}
			s, r, server := pgStart(t, doc)
			s.SetEnabled(true)
			c := pgConnect(t, addr, "", pgx.QueryExecModeSimpleProtocol)
			pgExec(t, c, "begin")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if end == "client-timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 150*time.Millisecond)
				defer stop()
			}
			done := make(chan error, 1)
			start := time.Now()
			go func() { _, err := c.Exec(ctx, "commit"); done <- err }()
			until := time.Now().Add(time.Second)
			for r.Counters().Applied == 0 {
				if time.Now().After(until) {
					t.Fatal("not applied")
				}
				time.Sleep(time.Millisecond)
			}
			if end == "shutdown" {
				server.Close()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("hold returned success")
				}
			case <-time.After(time.Second):
				t.Fatal("did not stop held session")
			}
			if time.Since(start) > time.Second {
				t.Fatal("deadline ignored")
			}
			server.Close()
			if r.Counters().Active != 0 || r.Counters().ActiveFaults != 0 {
				t.Fatal("leaked active flow", r.Counters())
			}
		})
	}
	t.Run("extended-commit", func(t *testing.T) {
		addr := address(t)
		s, r, _ := pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		c := pgConnect(t, addr, "", pgx.QueryExecModeCacheStatement)
		pgExec(t, c, "begin")
		result := c.PgConn().ExecParams(context.Background(), "COMMIT", nil, nil, nil, nil).Read()
		if result.Err == nil || r.Counters().Applied != 1 {
			t.Fatal("extended COMMIT not intercepted", result.Err)
		}
	})
	t.Run("unsupported-copy", func(t *testing.T) {
		addr := address(t)
		_, _, _ = pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "", ""))
		c := pgConnect(t, addr, "", pgx.QueryExecModeSimpleProtocol)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := c.Exec(ctx, "copy evidence to stdout"); err == nil {
			t.Fatal("COPY accepted")
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
		doc := pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 1")
		encoded, err := config.Encode(doc.Config())
		if err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(filename, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		socketDir, err := os.MkdirTemp("/tmp", "fl-pg-")
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		demo := exec.CommandContext(ctx, "go", "run", "./examples/postgresql")
		demo.Dir = "../.."
		demo.Env = append(os.Environ(), "FAULTLINE_PG_DIRECT=postgres://postgres:fixture-secret@"+host+"/postgres?sslmode=disable", "FAULTLINE_PG_PROXY=postgres://postgres:fixture-secret@"+addr+"/postgres?sslmode=disable")
		out, err := demo.CombinedOutput()
		if err != nil {
			t.Fatalf("demo: %v %s", err, out)
		}
		if strings.Count(string(out), "client_commit_success=false") != 4 || !strings.Contains(string(out), "deduplicate=false independently_verified_rows=2") || !strings.Contains(string(out), "deduplicate=true independently_verified_rows=1") {
			t.Fatal(string(out))
		}
	})

	t.Run("pool-recovery", func(t *testing.T) {
		addr := address(t)
		s, _, _ := pgStart(t, pgDoc(t, addr, "postgresql://"+host, "", "action: close_connection", "probability: 1"))
		s.SetEnabled(true)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, "postgres://postgres:fixture-secret@"+addr+"/postgres?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err == nil {
			t.Fatal("expected lost ACK")
		}
		s.SetEnabled(false)
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal("pool did not reconnect", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	})

}
