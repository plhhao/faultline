package postgresql

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/recorder"
)

func TestFramingAndStartupBounds(t *testing.T) {
	for _, n := range []uint32{0, 3, maxFrame + 1, 0xffffffff} {
		if _, err := readPacket(bytes.NewReader(binary.BigEndian.AppendUint32(nil, n)), maxFrame); err == nil {
			t.Fatal(n)
		}
	}
	var b bytes.Buffer
	want := frame{'C', []byte("COMMIT\x00")}
	if err := writeFrame(&b, want); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&b)
	if err != nil || got.kind != want.kind || !bytes.Equal(got.body, want.body) {
		t.Fatal(got, err)
	}
	good := append(binary.BigEndian.AppendUint32(nil, 196608), []byte("user\x00tester\x00database\x00db\x00\x00")...)
	if !validStartup(good) {
		t.Fatal("valid startup")
	}
	for _, bad := range [][]byte{nil, {0, 3, 0, 1, 0}, append(binary.BigEndian.AppendUint32(nil, 196608), []byte("replication\x00true\x00\x00")...), good[:len(good)-1]} {
		if validStartup(bad) {
			t.Fatal("invalid startup accepted")
		}
	}
}

func TestCommitBoundaryAndSnapshot(t *testing.T) {
	for _, tag := range []string{"COMMIT", "ROLLBACK"} {
		t.Run(tag, func(t *testing.T) {
			doc, err := config.Parse([]byte("api_version: faultline/v1alpha1\nproxies:\n- id: db\n  protocol: postgresql\n  listen: 127.0.0.1:5433\n  upstream: postgresql://127.0.0.1:5432\n  rules:\n  - id: commit\n    select: {probability: 1}\n    fault: {action: close_connection, phase: after_commit}\n"), "/tmp/test.yaml")
			if err != nil {
				t.Fatal(err)
			}
			service, _ := control.New(doc)
			service.SetEnabled(true)
			var output bytes.Buffer
			records := recorder.New(&output, 100)
			s := &Server{service: service, records: records, runtime: config.Runtime{RequestTimeout: time.Second}}
			client, pc := net.Pipe()
			backend, pu := net.Pipe()
			defer client.Close()
			defer backend.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); s.exchange(ctx, cancel, &endpoint{proxy: config.Proxy{ID: "db"}}, pc, pu) }()
			exchange := func(sql, result string, status byte) {
				t.Helper()
				if err := writeFrame(client, frame{'Q', []byte(sql + "\x00")}); err != nil {
					t.Fatal(err)
				}
				if _, err := readFrame(backend); err != nil {
					t.Fatal(err)
				}
				if err := writeFrame(backend, frame{'C', []byte(result + "\x00")}); err != nil {
					t.Fatal(err)
				}
				if _, err := readFrame(client); err != nil {
					t.Fatal(err)
				}
				if err := writeFrame(backend, frame{'Z', []byte{status}}); err != nil {
					t.Fatal(err)
				}
				if _, err := readFrame(client); err != nil {
					t.Fatal(err)
				}
			}
			exchange("begin /* sensitive-marker */", "BEGIN", 'T')
			if err := writeFrame(client, frame{'Q', []byte("commit\x00")}); err != nil {
				t.Fatal(err)
			}
			if _, err := readFrame(backend); err != nil {
				t.Fatal(err)
			}
			// This cycle has already acquired its enabled snapshot.
			service.SetEnabled(false)
			if err := writeFrame(backend, frame{'C', []byte(tag + "\x00")}); err != nil {
				t.Fatal(err)
			}
			f, err := readFrame(client)
			if tag == "COMMIT" {
				if err == nil {
					t.Fatalf("ack leaked: %+v", f)
				}
			} else {
				if err != nil || f.kind != 'C' {
					t.Fatal(err)
				}
			}
			cancel()
			client.Close()
			backend.Close()
			<-done
			records.Close(context.Background())
			if bytes.Contains(output.Bytes(), []byte("sensitive-marker")) {
				t.Fatal("SQL leaked")
			}
			if tag == "COMMIT" {
				if records.Counters().Applied != 1 || !bytes.Contains(output.Bytes(), []byte(`"commit_confirmed":true`)) {
					t.Fatal(output.String())
				}
			} else if records.Counters().Applied != 0 {
				t.Fatal("rollback injected")
			}
			if records.Counters().Active != 0 || records.Counters().ActiveFaults != 0 {
				t.Fatal(records.Counters())
			}
		})
	}
}

func TestWaitCancellation(t *testing.T) {
	d := time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if apply(ctx, config.Fault{Action: "hold_response", MaxDuration: &d}) {
		t.Fatal("hold resumed")
	}
}

func TestPumpCancellation(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	out := make(chan frame, 1)
	go pump(ctx, a, out, cancel, &wg)
	b.Close()
	wg.Wait()
	if ctx.Err() == nil {
		t.Fatal("EOF did not cancel")
	}
}

func TestRejectChannelBindingWithoutDowngrade(t *testing.T) {
	client, pc := net.Pipe()
	backend, pu := net.Pipe()
	defer client.Close()
	defer pc.Close()
	defer backend.Close()
	defer pu.Close()
	deadline(client, time.Second)
	deadline(pc, time.Second)
	deadline(backend, time.Second)
	deadline(pu, time.Second)
	s := &Server{}
	done := make(chan error, 1)
	go func() { _, err := s.authenticate(&endpoint{}, pc, pu, func() {}); done <- err }()
	mechanisms := append(binary.BigEndian.AppendUint32(nil, 10), []byte("SCRAM-SHA-256-PLUS\x00SCRAM-SHA-256\x00\x00")...)
	if err := writeFrame(backend, frame{'R', mechanisms}); err != nil {
		t.Fatal(err)
	}
	advertised, err := readFrame(client)
	if err != nil || !bytes.Equal(advertised.body, mechanisms) {
		t.Fatal("mechanisms changed", err)
	}
	if err := writeFrame(client, frame{'p', []byte("SCRAM-SHA-256-PLUS\x00")}); err != nil {
		t.Fatal(err)
	}
	rejected, err := readFrame(client)
	if err != nil || rejected.kind != 'E' {
		t.Fatal("PLUS not rejected", err)
	}
	if err := <-done; err == nil {
		t.Fatal("PLUS accepted")
	}
}
