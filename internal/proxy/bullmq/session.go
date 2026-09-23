package bullmq

import (
	"bufio"
	"context"
	"crypto/rand"
	"net"
	"sync"
	"time"

	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

type attempt struct {
	event    recorder.Event
	decision engine.Decision
	deadline time.Time
}

type session struct {
	server           *Server
	endpoint         *endpoint
	ctx              context.Context
	client, upstream net.Conn
}

func (s *session) begin(queue string) *attempt {
	p := &attempt{deadline: time.Now().Add(s.server.runtime.RequestTimeout)}
	if queue == "" {
		return p
	}
	snap := s.server.service.Acquire()
	p.decision, _ = snap.Decide(s.endpoint.proxy.ID, engine.Metadata{Queue: queue})
	p.event = recorder.Event{Info: snap.Info(), FlowID: rand.Text(), ProxyID: s.endpoint.proxy.ID, Protocol: "bullmq", Operation: "job_add", Queue: queue, StartedAt: time.Now().UTC(), Type: "flow_started"}
	s.server.records.Record(p.event)
	e := &p.event
	d := p.decision
	e.Type = "decision"
	e.RuleID = d.RuleID
	e.EligibleSequence = d.EligibleSequence
	e.Selected = d.Selected
	e.Phase = d.Fault.Phase
	e.Action = d.Fault.Action
	e.Probability = d.Selector.Probability
	e.Nth = d.Selector.Nth
	e.Every = d.Selector.Every
	switch {
	case e.Probability != nil:
		e.Selector = "probability"
	case e.Nth != nil:
		e.Selector = "nth"
	case e.Every != nil:
		e.Selector = "every"
	}
	s.server.records.Record(*e)
	return p
}

func (s *session) finish(p *attempt, outcome string) {
	<-s.server.pending
	if p.event.FlowID == "" {
		return
	}
	p.event.Type = "flow_finished"
	p.event.Outcome = outcome
	p.event.FinishedAt = time.Now().UTC()
	p.event.NotReached = p.event.Selected && !p.event.Reached
	s.server.records.Record(p.event)
}

func (s *session) response(r *bufio.Reader, p *attempt) (err error) {
	outcome := "connection_closed"
	defer func() { s.finish(p, outcome) }()
	_ = s.upstream.SetReadDeadline(p.deadline)
	f, err := readFrame(r)
	if err != nil {
		return err
	}
	if p.event.FlowID != "" {
		outcome = "job_add_not_confirmed"
		if f.kind == '$' && len(f.value) > 0 {
			outcome = "job_add_confirmed"
			if p.decision.Selected {
				p.event.Reached = true
				p.event.Applied = true
				p.event.Type = "fault_applied"
				s.server.records.Record(p.event)
				outcome = "job_add_reply_lost"
				if !s.apply(p) {
					return context.Canceled
				}
				outcome = "job_add_reply_delayed"
			}
		}
	}
	_ = s.client.SetWriteDeadline(p.deadline)
	if err = writeAll(s.client, f.wire); err != nil {
		outcome = "reply_write_failed"
	}
	return err
}

func (s *session) apply(p *attempt) bool {
	f := p.decision.Fault
	if f.Action == "close_connection" {
		return false
	}
	d := f.Duration
	if f.Action == "hold_response" {
		d = f.MaxDuration
	}
	ctx, cancel := context.WithDeadline(s.ctx, p.deadline)
	defer cancel()
	timer := time.NewTimer(*d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return f.Action == "delay"
	}
}

func (s *session) exchange() {
	ctx, cancel := context.WithCancel(s.ctx)
	s.ctx = ctx
	defer cancel()
	pending := make(chan *attempt, min(maxPending-1, s.server.runtime.MaxInflightRequests))
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(pending)
		r := bufio.NewReader(s.client)
		for {
			_ = s.client.SetReadDeadline(time.Now().Add(s.server.runtime.RequestTimeout))
			f, err := readFrame(r)
			if err != nil {
				errs <- err
				return
			}
			args, err := command(f)
			if err != nil {
				errs <- err
				return
			}
			if len(pending) == cap(pending) {
				errs <- errProtocol
				return
			}
			select {
			case s.server.pending <- struct{}{}:
			default:
				errs <- errProtocol
				return
			}
			p := s.begin(queueName(args))
			// Publish correlation before the write: Redis may respond immediately.
			pending <- p
			_ = s.upstream.SetWriteDeadline(p.deadline)
			if err := writeAll(s.upstream, f.wire); err != nil {
				errs <- err
				return
			}
		}
	})
	wg.Go(func() {
		r := bufio.NewReader(s.upstream)
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-pending:
				if !ok {
					return
				}
				if err := s.response(r, p); err != nil {
					errs <- err
					return
				}
			}
		}
	})
	select {
	case <-ctx.Done():
	case <-errs:
	}
	cancel()
	_ = s.client.Close()
	_ = s.upstream.Close()
	wg.Wait()
	for p := range pending {
		s.finish(p, "connection_closed")
	}
}
