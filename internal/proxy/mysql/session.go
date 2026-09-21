package mysql

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
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
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return
	}
	deadline(raw, s.runtime.RequestTimeout)
	u, err := (&net.Dialer{Timeout: s.runtime.RequestTimeout}).DialContext(ctx, "tcp", e.address)
	if err != nil {
		return
	}
	defer u.Close()
	stopUp := context.AfterFunc(ctx, func() { u.Close() })
	defer stopUp()
	deadline(u, s.runtime.RequestTimeout)
	c, up, flags, err := authenticate(ctx, e, raw, u)
	if err != nil {
		return
	}
	s.exchange(ctx, cancel, e, c, up, flags)
}
func pump(ctx context.Context, c net.Conn, out chan<- packet, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		p, err := readPacket(c)
		if err != nil {
			cancel()
			return
		}
		select {
		case out <- p:
		case <-ctx.Done():
			return
		}
	}
}
func receive(ctx context.Context, ch <-chan packet) (packet, error) {
	select {
	case <-ctx.Done():
		return packet{}, ctx.Err()
	case p := <-ch:
		return p, nil
	}
}

type cycle struct {
	ctx    context.Context
	c      net.Conn
	back   <-chan packet
	seq    byte
	modern bool
}

func (x *cycle) read() (packet, error) {
	p, err := receive(x.ctx, x.back)
	if err != nil {
		return p, err
	}
	if p.seq != x.seq {
		return p, errProtocol
	}
	x.seq++
	return p, nil
}
func (x *cycle) forward(p packet) error { return writePacket(x.c, p) }
func (x *cycle) definitions(n int) error {
	if n > 4096 {
		return errProtocol
	}
	for i := 0; i < n; i++ {
		p, err := x.read()
		if err != nil {
			return err
		}
		if p.body[0] == 0xff {
			return errProtocol
		}
		if err = x.forward(p); err != nil {
			return err
		}
	}
	if n > 0 && !x.modern {
		p, err := x.read()
		if err != nil {
			return err
		}
		if _, ok := eofStatus(p.body, false); !ok {
			return errProtocol
		}
		return x.forward(p)
	}
	return nil
}
func (x *cycle) result(first packet) (uint16, bool, error) {
	n, _, ok := lenenc(first.body)
	if !ok || n == 0 || n > 4096 {
		return 0, false, errProtocol
	}
	if err := x.forward(first); err != nil {
		return 0, false, err
	}
	if err := x.definitions(int(n)); err != nil {
		return 0, false, err
	}
	for {
		p, err := x.read()
		if err != nil {
			return 0, false, err
		}
		if p.body[0] == 0xff {
			return 0, false, x.forward(p)
		}
		if flags, ok := eofStatus(p.body, x.modern); ok {
			if flags&8 != 0 {
				return 0, false, errProtocol
			}
			return flags, true, x.forward(p)
		}
		if err = x.forward(p); err != nil {
			return 0, false, err
		}
	}
}
func (s *Server) exchange(ctx context.Context, cancel context.CancelFunc, e *endpoint, c, u net.Conn, flags uint32) {
	front, back := make(chan packet, 1), make(chan packet, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go pump(ctx, c, front, cancel, &wg)
	go pump(ctx, u, back, cancel, &wg)
	defer func() { cancel(); c.Close(); u.Close(); wg.Wait() }()
	statements := map[uint32]statement{}
	transaction := false
	for {
		p, err := receive(ctx, front)
		if err != nil {
			return
		}
		if p.seq != 0 {
			reject(c, 1)
			return
		}
		command := p.body[0]
		kind := ordinary
		switch command {
		case 1:
			return
		case 3, 0x16:
			kind = classify(p.body[1:])
			if kind == unsupported {
				reject(c, 1)
				return
			}
			if command == 0x16 && len(statements) >= 128 {
				reject(c, 1)
				return
			}
		case 0x17:
			if len(p.body) < 10 || p.body[5] != 0 || binary.LittleEndian.Uint32(p.body[6:]) != 1 {
				reject(c, 1)
				return
			}
			var ok bool
			kind, ok = statements[binary.LittleEndian.Uint32(p.body[1:])]
			if !ok {
				reject(c, 1)
				return
			}
		case 0x19, 0x1a:
			if len(p.body) != 5 {
				reject(c, 1)
				return
			}
		case 2, 0x0e, 0x1f:
		default:
			reject(c, 1)
			return
		}
		snap := s.service.Acquire()
		deadline(c, s.runtime.RequestTimeout)
		deadline(u, s.runtime.RequestTimeout)
		cycleCtx, stopCycle := context.WithTimeout(ctx, s.runtime.RequestTimeout)
		abort := context.AfterFunc(cycleCtx, cancel)
		event := recorder.Event{Info: snap.Info(), FlowID: rand.Text(), ProxyID: e.proxy.ID, Protocol: "mysql", StartedAt: time.Now().UTC(), Type: "flow_started"}
		s.records.Record(event)
		outcome := "forwarded"
		err = func() error {
			if err := writePacket(u, p); err != nil {
				return err
			}
			if command == 0x19 {
				delete(statements, binary.LittleEndian.Uint32(p.body[1:]))
				return nil
			}
			x := cycle{ctx: cycleCtx, c: c, back: back, seq: 1, modern: flags&capDeprecateEOF != 0}
			first, err := x.read()
			if err != nil {
				return err
			}
			if first.body[0] == 0xff {
				transaction = false
				outcome = "upstream_error"
				return x.forward(first)
			}
			if command == 0x16 {
				if len(first.body) != 12 || first.body[0] != 0 {
					return errProtocol
				}
				id := binary.LittleEndian.Uint32(first.body[1:])
				if _, exists := statements[id]; exists {
					return errProtocol
				}
				if err = x.forward(first); err != nil {
					return err
				}
				if err = x.definitions(int(binary.LittleEndian.Uint16(first.body[7:]))); err != nil {
					return err
				}
				if err = x.definitions(int(binary.LittleEndian.Uint16(first.body[5:]))); err != nil {
					return err
				}
				statements[id] = kind
				return nil
			}
			if first.body[0] == 0xfb {
				return errProtocol
			}
			if first.body[0] == 0 {
				status, ok := okStatus(first.body)
				if !ok || status&8 != 0 {
					return errProtocol
				}
				confirmed := kind == commit && transaction && status&1 == 0
				if status&1 == 0 || kind == rollback {
					transaction = false
				}
				if kind == begin {
					transaction = status&1 != 0
				}
				if command == 0x1f {
					transaction = false
					clear(statements)
				}
				if confirmed && !s.commit(cycleCtx, snap, e, &event) {
					outcome = "commit_ack_lost"
					return errProtocol
				}
				return x.forward(first)
			}
			status, ok, err := x.result(first)
			if !ok || status&1 == 0 {
				transaction = false
			}
			return err
		}()
		abort()
		stopCycle()
		if err != nil && outcome == "forwarded" {
			outcome = "connection_closed"
		}
		event.Type = "flow_finished"
		event.Outcome = outcome
		event.FinishedAt = time.Now().UTC()
		s.records.Record(event)
		if err != nil {
			return
		}
		deadline(c, s.runtime.RequestTimeout)
		deadline(u, s.runtime.RequestTimeout)
	}
}
func (s *Server) commit(ctx context.Context, snap control.Snapshot, e *endpoint, event *recorder.Event) bool {
	d, _ := snap.Decide(e.proxy.ID, engine.Metadata{})
	event.Type = "decision"
	event.RuleID = d.RuleID
	event.EligibleSequence = d.EligibleSequence
	event.Selected = d.Selected
	event.Phase = d.Fault.Phase
	event.Action = d.Fault.Action
	event.Probability = d.Selector.Probability
	event.Nth = d.Selector.Nth
	event.Every = d.Selector.Every
	switch {
	case event.Probability != nil:
		event.Selector = "probability"
	case event.Nth != nil:
		event.Selector = "nth"
	case event.Every != nil:
		event.Selector = "every"
	}
	event.CommitConfirmed = true
	s.records.Record(*event)
	if !d.Selected {
		return true
	}
	event.Type = "fault_applied"
	event.Reached = true
	event.Applied = true
	s.records.Record(*event)
	return apply(ctx, d.Fault)
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
