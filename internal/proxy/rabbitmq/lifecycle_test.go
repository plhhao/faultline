package rabbitmq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

func TestUpstreamTLSHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := connectUpstream(ctx, &endpoint{address: listener.Addr().String(), upstreamTLS: &tls.Config{ServerName: "localhost", MinVersion: tls.VersionTLS12}}, 50*time.Millisecond)
	if conn != nil {
		conn.Close()
	}
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("handshake timeout: %v (%v)", err, time.Since(start))
	}
	<-done
}

func TestAMQPHandshakeTimeoutReleasesSession(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	brokerDone := make(chan struct{})
	go func() {
		defer close(brokerDone)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	client, peer := net.Pipe()
	defer peer.Close()
	s := &Server{ctx: context.Background(), runtime: config.Runtime{RequestTimeout: 50 * time.Millisecond}, slots: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() { s.serve(&endpoint{address: listener.Addr().String()}, client); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		client.Close()
		t.Fatal("AMQP handshake exceeded timeout")
	}
	select {
	case <-brokerDone:
	case <-time.After(time.Second):
		t.Fatal("upstream not closed")
	}
	if len(s.slots) != 0 {
		t.Fatal("handshake leaked session slot")
	}
}

func TestSessionShutdownAndBrokerClose(t *testing.T) {
	for _, cause := range []string{"shutdown", "broker-close"} {
		t.Run(cause, func(t *testing.T) {
			records := recorder.New(io.Discard, 16)
			defer records.Close(context.Background())
			client, peer := net.Pipe()
			upstream, broker := net.Pipe()
			defer peer.Close()
			defer broker.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			records.Record(recorder.Event{Type: "flow_started"})
			p := &publish{event: recorder.Event{Selected: true}}
			s := &session{ctx: ctx, client: client, upstream: upstream, server: &Server{records: records}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p}}}}
			done := make(chan struct{})
			go func() { s.exchange(); close(done) }()
			if cause == "shutdown" {
				cancel()
			} else {
				broker.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				cancel()
				t.Fatal("session did not close")
			}
			if counts := records.Counters(); counts.Active != 0 || counts.ActiveFaults != 0 || !p.event.NotReached {
				t.Fatalf("cleanup: %+v %+v", counts, p.event)
			}
		})
	}
}

func TestFrameAndPendingBounds(t *testing.T) {
	header := []byte{frameBody, 0, 1, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(header[3:], maxFrame+1)
	if _, err := readFrame(bytes.NewReader(header)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	s := &session{server: &Server{runtime: config.Runtime{MaxInflightRequests: 1}}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: {}}}}}
	if err := s.addPublish(1, &publishCandidate{}); err == nil {
		t.Fatal("pending limit ignored")
	}
	if len(s.channels[1].pending) != 1 {
		t.Fatal("pending state changed on rejection")
	}
}

func TestDelayedFramesPreserveConnectionOrder(t *testing.T) {
	records := recorder.New(io.Discard, 32)
	defer records.Close(context.Background())
	client, peer := net.Pipe()
	upstream, broker := net.Pipe()
	defer peer.Close()
	defer broker.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	duration := 50 * time.Millisecond
	p := &publish{decision: engine.Decision{Selected: true, Fault: config.Fault{Action: "delay", Duration: &duration}}}
	q := &publish{}
	for range 2 {
		records.Record(recorder.Event{Type: "flow_started"})
	}
	s := &session{ctx: ctx, client: client, upstream: upstream, server: &Server{records: records, runtime: config.DefaultRuntime()}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p}}, 2: {pending: map[uint64]*publish{1: q}}}}
	done := make(chan struct{})
	go func() { s.exchange(); close(done) }()
	defer func() { cancel(); <-done }()
	body := make([]byte, 13)
	binary.BigEndian.PutUint16(body, 60)
	binary.BigEndian.PutUint16(body[2:], 80)
	binary.BigEndian.PutUint64(body[4:], 1)
	frames := []frame{{kind: frameMethod, channel: 1, body: body}, {kind: frameHeartbeat}, {kind: frameMethod, channel: 2, body: body}}
	written := make(chan error, 1)
	go func() {
		for _, f := range frames {
			if err := writeFrame(broker, f); err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	start := time.Now()
	for i, want := range frames {
		got, err := readFrame(peer)
		if err != nil || got.kind != want.kind || got.channel != want.channel {
			t.Fatalf("frame %d: %+v %v", i, got, err)
		}
		if i == 0 && time.Since(start) < 40*time.Millisecond {
			t.Fatal("confirm not delayed")
		}
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestConfirmWriteFailureFinishesFlows(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "passthrough", true: "delay"}[selected], func(t *testing.T) {
			records := recorder.New(io.Discard, 16)
			defer records.Close(context.Background())
			client, peer := net.Pipe()
			defer client.Close()
			peer.Close()
			duration := time.Millisecond
			p := &publish{sequence: 1, decision: engine.Decision{Selected: selected, Fault: config.Fault{Action: "delay", Duration: &duration}}}
			records.Record(recorder.Event{Type: "flow_started"})
			s := &session{ctx: context.Background(), client: client, server: &Server{records: records, runtime: config.DefaultRuntime()}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p}}}}
			keep, err := s.handleConfirm(frame{kind: frameMethod, channel: 1}, 1, false, true)
			if keep || err == nil {
				t.Fatalf("keep=%v err=%v", keep, err)
			}
			s.finishPending("connection_closed")
			counts := records.Counters()
			if counts.Active != 0 || counts.ActiveFaults != 0 || p.event.Outcome != "publish_confirm_lost" {
				t.Fatalf("counts=%+v event=%+v", counts, p.event)
			}
		})
	}
}

func TestDisconnectCancelsConfirmWait(t *testing.T) {
	for _, action := range []string{"delay", "hold_response"} {
		t.Run(action, func(t *testing.T) {
			records := recorder.New(io.Discard, 16)
			defer records.Close(context.Background())
			client, peer := net.Pipe()
			upstream, broker := net.Pipe()
			defer peer.Close()
			defer broker.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			duration := 10 * time.Second
			p := &publish{sequence: 1, decision: engine.Decision{Selected: true, Fault: config.Fault{Action: action, Duration: &duration, MaxDuration: &duration}}}
			records.Record(recorder.Event{Type: "flow_started"})
			s := &session{ctx: ctx, client: client, upstream: upstream, server: &Server{records: records, runtime: config.DefaultRuntime()}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p}}}}
			done := make(chan struct{})
			go func() { s.exchange(); close(done) }()
			defer func() { cancel(); <-done }()
			body := make([]byte, 13)
			binary.BigEndian.PutUint16(body, 60)
			binary.BigEndian.PutUint16(body[2:], 80)
			binary.BigEndian.PutUint64(body[4:], 1)
			broker.SetWriteDeadline(time.Now().Add(time.Second))
			if err := writeFrame(broker, frame{kind: frameMethod, channel: 1, body: body}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for records.Counters().Applied == 0 {
				if time.Now().After(deadline) {
					t.Fatal("fault did not start")
				}
				time.Sleep(time.Millisecond)
			}
			peer.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("disconnect did not cancel fault wait")
			}
			if counts := records.Counters(); counts.Active != 0 || counts.ActiveFaults != 0 {
				t.Fatalf("leaked flows: %+v", counts)
			}
		})
	}
}

func TestNackAndChannelCorrelation(t *testing.T) {
	records := recorder.New(io.Discard, 32)
	defer records.Close(context.Background())
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	p := &publish{sequence: 1, event: recorder.Event{Selected: true}, decision: engine.Decision{Selected: true, Fault: config.Fault{Action: "close_connection"}}}
	other := &publish{sequence: 1}
	records.Record(recorder.Event{Type: "flow_started"})
	s := &session{client: client, server: &Server{records: records}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p}}, 2: {pending: map[uint64]*publish{1: other}}}}
	done := make(chan error, 1)
	go func() { _, err := readFrame(peer); done <- err }()
	if keep, err := s.handleConfirm(frame{kind: frameMethod, channel: 1}, 1, false, false); err != nil || !keep {
		t.Fatalf("nack: %v %v", keep, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !p.event.NotReached || p.event.Reached || p.event.Applied || p.event.Outcome != "publisher_nack" {
		t.Fatalf("nack event: %+v", p.event)
	}
	if len(s.channels[2].pending) != 1 || records.Counters().Active != 0 {
		t.Fatal("nack affected other channel or leaked flow")
	}
}

func TestPublishSnapshotBeforeConfirm(t *testing.T) {
	source := "api_version: faultline/v1alpha1\nproxies:\n- id: broker\n  protocol: rabbitmq\n  listen: localhost:15672\n  upstream: amqp://localhost\n  rules:\n  - id: close\n    select: {probability: 1}\n    fault: {phase: after_publish_confirm, action: close_connection}\n"
	doc, err := config.Parse([]byte(source), "/tmp/rabbitmq.yaml")
	if err != nil {
		t.Fatal(err)
	}
	service, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEnabled(true)
	records := recorder.New(io.Discard, 32)
	defer records.Close(context.Background())
	s := &session{server: &Server{service: service, records: records, runtime: config.DefaultRuntime()}, endpoint: &endpoint{proxy: doc.Config().Proxies[0]}, channels: map[uint16]*channelState{}}
	if err := s.addPublish(1, &publishCandidate{}); err != nil {
		t.Fatal(err)
	}
	p := s.channels[1].pending[1]
	service.SetEnabled(false)
	if keep, err := s.handleConfirm(frame{kind: frameMethod, channel: 1}, 1, false, true); keep || err != nil || !p.event.Applied {
		t.Fatalf("snapshot was changed: keep=%t err=%v event=%+v", keep, err, p.event)
	}
	if err := s.addPublish(1, &publishCandidate{}); err != nil {
		t.Fatal(err)
	}
	if s.channels[1].pending[2].decision.Selected {
		t.Fatal("new flow ignored disable")
	}
	s.finishPending("connection_closed")
}

func TestConfirmTagBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tag      uint64
		multiple bool
		count    int
	}{{"unknown", 99, false, 0}, {"all", 0, true, 2}, {"prefix", 1, true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: {sequence: 1}, 2: {sequence: 2}}}, 2: {pending: map[uint64]*publish{1: {sequence: 1}}}}}
			if got := len(s.take(1, tc.tag, tc.multiple)); got != tc.count {
				t.Fatalf("matched=%d want=%d", got, tc.count)
			}
			if len(s.channels[2].pending) != 1 {
				t.Fatal("confirm crossed channels")
			}
		})
	}
}
