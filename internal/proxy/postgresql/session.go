package postgresql

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/engine"
	"faultline/internal/recorder"
)

func (s *Server) serve(e *endpoint, raw net.Conn) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	defer raw.Close()
	deadline(raw, s.runtime.RequestTimeout)
	client := raw
	b, err := readPacket(client, maxStartup)
	if err != nil {
		return
	}
	if len(b) == 4 && binary.BigEndian.Uint32(b) == sslRequest {
		if e.clientTLS == nil {
			if writeAll(client, []byte("N")) != nil {
				return
			}
		} else {
			if writeAll(client, []byte("S")) != nil {
				return
			}
			secure := tls.Server(client, e.clientTLS)
			if secure.HandshakeContext(ctx) != nil {
				return
			}
			client = secure
		}
		b, err = readPacket(client, maxStartup)
		if err != nil {
			return
		}
	}
	if len(b) == 12 && binary.BigEndian.Uint32(b[:4]) == cancelRequest {
		// Cancellation is scoped to sessions of this listener, never arbitrary backends.
		s.cancelSession(e.proxy.ID + string(b[4:]))
		return
	}
	if e.clientTLS != nil {
		if _, ok := client.(*tls.Conn); !ok {
			reject(client)
			return
		}
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		reject(client)
		return
	}
	if !validStartup(b) {
		reject(client)
		return
	}

	upstream, err := connectUpstream(ctx, e, s.runtime.RequestTimeout)
	if err != nil {
		return
	}
	defer upstream.Close()
	closeUpstream := context.AfterFunc(ctx, func() { upstream.Close() })
	defer closeUpstream()
	if writePacket(upstream, b) != nil {
		return
	}
	cleanup, err := s.authenticate(e, client, upstream, cancel)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return
	}
	s.exchange(ctx, cancel, e, client, upstream)
}
func validStartup(b []byte) bool {
	if len(b) < 5 || binary.BigEndian.Uint32(b[:4]) != 196608 || b[len(b)-1] != 0 {
		return false
	}
	fields := bytes.Split(b[4:len(b)-1], []byte{0})
	if len(fields)%2 != 1 || len(fields[len(fields)-1]) != 0 {
		return false
	}
	for i := 0; i < len(fields)-1; i += 2 {
		if len(fields[i]) == 0 || string(fields[i]) == "replication" {
			return false
		}
	}
	return true
}
func (s *Server) authenticate(e *endpoint, c, u net.Conn, cancel context.CancelFunc) (func(), error) {
	var cleanup func()
	stage := 0
	for {
		f, err := readFrame(u)
		if err != nil {
			return cleanup, err
		}
		switch f.kind {
		case 'R':
			if len(f.body) < 4 {
				reject(c)
				return cleanup, errProtocol
			}
			code := binary.BigEndian.Uint32(f.body)
			switch code {
			case 10:
				if stage != 0 || !bytes.Contains(f.body[4:], []byte("SCRAM-SHA-256\x00")) {
					reject(c)
					return cleanup, errProtocol
				}
				stage = 1
			case 11:
				if stage != 1 {
					reject(c)
					return cleanup, errProtocol
				}
				stage = 2
			case 12:
				if stage != 2 {
					reject(c)
					return cleanup, errProtocol
				}
				stage = 3
			case 0:
				if stage != 3 {
					reject(c)
					return cleanup, errProtocol
				}
				stage = 4
			default:
				reject(c)
				return cleanup, errProtocol
			}
			if writeFrame(c, f) != nil {
				return cleanup, io.ErrClosedPipe
			}
			if code == 10 || code == 11 {
				response, err := readFrame(c)
				if err != nil {
					return cleanup, err
				}
				if response.kind != 'p' {
					reject(c)
					return cleanup, errProtocol
				}
				if code == 10 {
					name, data, ok := bytes.Cut(response.body, []byte{0})
					if !ok || string(name) != "SCRAM-SHA-256" || len(data) < 4 || int(binary.BigEndian.Uint32(data)) != len(data)-4 || !bytes.HasPrefix(data[4:], []byte("n,,")) {
						reject(c)
						return cleanup, errProtocol
					}
				}
				if err := writeFrame(u, response); err != nil {
					return cleanup, err
				}
			}
			continue
		case 'K':
			if len(f.body) != 8 || cleanup != nil {
				return cleanup, errProtocol
			}
			// Replace backend keys to avoid exposing/reusing upstream cancellation credentials.
			key := make([]byte, 8)
			if _, err := rand.Read(key); err != nil {
				return cleanup, err
			}
			backendKey := append([]byte(nil), f.body...)
			f.body = key
			cleanup = s.register(e.proxy.ID+string(key), func() { cancel(); s.cancelBackend(e, backendKey) })
		case 'Z':
			if stage != 4 || len(f.body) != 1 || f.body[0] != 'I' {
				return cleanup, errProtocol
			}
			return cleanup, writeFrame(c, f)
		case 'E':
			_ = writeFrame(c, f)
			return cleanup, errProtocol
		case 'S', 'N':
		default:
			reject(c)
			return cleanup, errProtocol
		}
		if err := writeFrame(c, f); err != nil {
			return cleanup, err
		}
	}
}

func pump(ctx context.Context, c net.Conn, out chan<- frame, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		f, err := readFrame(c)
		if err != nil {
			cancel()
			return
		}
		select {
		case out <- f:
		case <-ctx.Done():
			return
		}
	}
}
func (s *Server) exchange(ctx context.Context, cancel context.CancelFunc, e *endpoint, c, u net.Conn) {
	front, back := make(chan frame, 1), make(chan frame, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go pump(ctx, c, front, cancel, &wg)
	go pump(ctx, u, back, cancel, &wg)
	defer func() { cancel(); c.Close(); u.Close(); wg.Wait() }()
	var snap control.Snapshot
	var event recorder.Event
	active, awaiting, transaction, executed, decided := false, false, false, false, false
	var cycleTimer *time.Timer
	defer func() {
		if cycleTimer != nil {
			cycleTimer.Stop()
		}
	}()
	outcome := "forwarded"
	finish := func(outcome string) {
		if !active {
			return
		}
		event.Type = "flow_finished"
		event.Outcome = outcome
		event.FinishedAt = time.Now().UTC()
		event.NotReached = event.Selected && !event.Reached
		s.records.Record(event)
		active = false
		snap = control.Snapshot{}
		if cycleTimer != nil {
			cycleTimer.Stop()
		}
	}
	defer func() { finish("connection_closed") }()
	deadline(c, s.runtime.RequestTimeout)
	deadline(u, s.runtime.RequestTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-front:
			if f.kind == 'X' {
				return
			}
			if !strings.ContainsRune("QPBDEHCS", rune(f.kind)) || awaiting {
				reject(c)
				return
			}
			if !active {
				snap = s.service.Acquire()
				active = true
				executed = false
				decided = false
				outcome = "forwarded"
				cycleTimer = time.AfterFunc(s.runtime.RequestTimeout, cancel)
				event = recorder.Event{Info: snap.Info(), FlowID: rand.Text(), ProxyID: e.proxy.ID, Protocol: "postgresql", StartedAt: time.Now().UTC(), Type: "flow_started"}
				s.records.Record(event)
				deadline(c, s.runtime.RequestTimeout)
				deadline(u, s.runtime.RequestTimeout)
			}
			if f.kind == 'E' {
				if executed {
					reject(c)
					return
				}
				executed = true
			}
			if f.kind == 'Q' || f.kind == 'S' {
				awaiting = true
			}
			if writeFrame(u, f) != nil {
				return
			}
		case f := <-back:
			if !strings.ContainsRune("123nTsDCIENSAZt", rune(f.kind)) {
				reject(c)
				return
			}
			if f.kind == 'E' {
				outcome = "upstream_error"
			}
			if f.kind == 'C' {
				switch string(f.body) {
				case "BEGIN\x00":
					transaction = true
				case "ROLLBACK\x00":
					transaction = false
				case "COMMIT\x00":
					if transaction && active && !decided {
						transaction = false
						decided = true
						decision, _ := snap.Decide(e.proxy.ID, engine.Metadata{})
						event.Type = "decision"
						event.RuleID = decision.RuleID
						event.EligibleSequence = decision.EligibleSequence
						event.Selected = decision.Selected
						event.Phase = decision.Fault.Phase
						event.Action = decision.Fault.Action
						event.Probability = decision.Selector.Probability
						event.Nth = decision.Selector.Nth
						event.Every = decision.Selector.Every
						switch {
						case event.Probability != nil:
							event.Selector = "probability"
						case event.Nth != nil:
							event.Selector = "nth"
						case event.Every != nil:
							event.Selector = "every"
						}
						event.CommitConfirmed = true
						s.records.Record(event)
						if decision.Selected {
							event.Type = "fault_applied"
							event.Reached = true
							event.Applied = true
							s.records.Record(event)
							if !apply(ctx, decision.Fault) {
								finish("commit_ack_lost")
								return
							}
						}
					}
				}
			}
			if f.kind == 'Z' {
				if len(f.body) != 1 || !strings.ContainsRune("ITE", rune(f.body[0])) {
					reject(c)
					return
				}
				transaction = f.body[0] != 'I'
				awaiting = false
				if writeFrame(c, f) != nil {
					return
				}
				finish(outcome)
				deadline(c, s.runtime.RequestTimeout)
				deadline(u, s.runtime.RequestTimeout)
				continue
			}
			if writeFrame(c, f) != nil {
				return
			}
		}
	}
}
func apply(ctx context.Context, f config.Fault) bool {
	if f.Action == "close_connection" {
		return false
	}
	duration := f.Duration
	if f.Action == "hold_response" {
		duration = f.MaxDuration
	}
	timer := time.NewTimer(*duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return f.Action == "delay"
	}
}

func connectUpstream(ctx context.Context, e *endpoint, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, "tcp", e.address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	deadline(raw, timeout)
	if e.upstreamTLS == nil {
		return raw, nil
	}
	fail := func() (net.Conn, error) { raw.Close(); return nil, errProtocol }
	if writePacket(raw, binary.BigEndian.AppendUint32(nil, sslRequest)) != nil {
		return fail()
	}
	var answer [1]byte
	if _, err := io.ReadFull(raw, answer[:]); err != nil || answer[0] != 'S' {
		return fail()
	}
	secure := tls.Client(raw, e.upstreamTLS)
	if secure.HandshakeContext(ctx) != nil {
		return fail()
	}
	return secure, nil
}
func (s *Server) cancelBackend(e *endpoint, key []byte) {
	ctx, cancel := context.WithTimeout(s.ctx, s.runtime.RequestTimeout)
	defer cancel()
	c, err := connectUpstream(ctx, e, s.runtime.RequestTimeout)
	if err != nil {
		return
	}
	defer c.Close()
	// Backend credentials only travel to the configured upstream, never to logs or clients.
	_ = writePacket(c, append(binary.BigEndian.AppendUint32(nil, cancelRequest), key...))
}
