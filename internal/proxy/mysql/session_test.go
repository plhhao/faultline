package mysql

import (
	"bytes"
	"context"
	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/recorder"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func testDocument(t *testing.T, probability int) *config.Document {
	t.Helper()
	d, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n- id: db\n  protocol: mysql\n  listen: localhost:13306\n  upstream: mysql://localhost:3306\n  rules:\n  - id: commit\n    match: {}\n    select: {probability: %d}\n    fault: {action: close_connection, phase: after_commit}\n", probability)), "/tmp/mysql.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestCycleSnapshotAndCommitError(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			doc := testDocument(t, 0)
			service, _ := control.New(doc)
			service.SetEnabled(true)
			var events bytes.Buffer
			records := recorder.New(&events, 100)
			s := &Server{service: service, records: records, runtime: doc.Config().Runtime}
			client, front := net.Pipe()
			up, backend := net.Pipe()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.exchange(ctx, cancel, &endpoint{proxy: doc.Config().Proxies[0]}, front, up, capDeprecateEOF)
			}()
			defer func() { cancel(); client.Close(); backend.Close(); <-done; records.Close(context.Background()) }()
			for i, q := range []string{"BEGIN", "COMMIT", "BEGIN", "COMMIT"} {
				backendDone := make(chan error, 1)
				go func() {
					p, err := readPacket(backend)
					if err != nil {
						backendDone <- err
						return
					}
					if string(p.body[1:]) != q {
						backendDone <- errProtocol
						return
					}
					status := byte(3)
					if q == "COMMIT" {
						status = 2
					}
					response := []byte{0, 0, 0, status, 0, 0, 0}
					if i == 1 {
						if failure {
							response = []byte{0xff, 0x15, 4, '#', 'H', 'Y', '0', '0', '0', 'e'}
						}
						_, err = service.Apply(testDocument(t, 1))
						if err != nil {
							backendDone <- err
							return
						}
					}
					backendDone <- writePacket(backend, packet{1, response})
				}()
				if err := writePacket(client, packet{0, append([]byte{3}, []byte(q)...)}); err != nil {
					t.Fatal(err)
				}
				p, err := readPacket(client)
				if i == 3 {
					if err == nil {
						t.Fatal("new snapshot did not inject")
					}
				} else if err != nil {
					t.Fatal(err)
				} else if i == 1 && failure && p.body[0] != 0xff {
					t.Fatal("commit ERR not forwarded")
				}
				if err := <-backendDone; err != nil {
					t.Fatal(err)
				}
			}
			if records.Counters().Applied != 1 {
				t.Fatal("stale snapshot or failed commit selected", records.Counters())
			}
		})
	}
}
func TestMalformedCycleAndUnsupportedCommands(t *testing.T) {
	for _, body := range [][]byte{{0x18}, {0x17}, {0x19}, {0x7f}, {3, 'C', 'A', 'L', 'L', ' ', 'x'}} {
		t.Run(fmt.Sprintf("%x", body), func(t *testing.T) {
			doc := testDocument(t, 1)
			service, _ := control.New(doc)
			records := recorder.New(io.Discard, 10)
			defer records.Close(context.Background())
			s := &Server{service: service, records: records, runtime: doc.Config().Runtime}
			client, front := net.Pipe()
			up, backend := net.Pipe()
			defer client.Close()
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.exchange(ctx, cancel, &endpoint{proxy: doc.Config().Proxies[0]}, front, up, 0)
			}()
			if err := writePacket(client, packet{0, body}); err != nil {
				t.Fatal(err)
			}
			p, err := readPacket(client)
			if err != nil || p.body[0] != 0xff {
				t.Fatal("missing rejection", err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cleanup timeout")
			}
			if records.Counters().Active != 0 {
				t.Fatal("leaked cycle")
			}
		})
	}
}

func TestResultBoundaries(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprint(modern), func(t *testing.T) {
			c, client := net.Pipe()
			defer c.Close()
			defer client.Close()
			c.SetDeadline(time.Now().Add(time.Second))
			drained := make(chan struct{})
			go func() { io.Copy(io.Discard, client); close(drained) }()
			defer func() { c.Close(); <-drained }()
			packets := make(chan packet, 5)
			seq := byte(2)
			add := func(body []byte) { packets <- packet{seq, body}; seq++ }
			add([]byte{3, 'd', 'e', 'f'})
			if !modern {
				add([]byte{0xfe, 0, 0, 3, 0})
			}
			add([]byte{0, 0, 42, 0, 0, 0, 0})
			if modern {
				add([]byte{0xfe, 0, 0, 3, 0, 0, 0})
			} else {
				add([]byte{0xfe, 0, 0, 3, 0})
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			x := cycle{ctx: ctx, c: c, back: packets, seq: 2, modern: modern}
			status, ok, err := x.result(packet{1, []byte{1}})
			if err != nil || !ok || status != 3 {
				t.Fatal(status, ok, err)
			}
			packets <- packet{99, []byte{0}}
			if _, err = x.read(); err == nil {
				t.Fatal("wrong sequence accepted")
			}
			if err = x.definitions(4097); err == nil {
				t.Fatal("metadata bound ignored")
			}
		})
	}
}
