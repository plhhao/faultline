package tcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

type endpoint struct {
	id, upstream string
	listener     net.Listener
}

type Server struct {
	service   *control.Service
	records   *recorder.Recorder
	runtime   config.Runtime
	endpoints []endpoint
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closing   bool
	once      sync.Once
	wg        sync.WaitGroup
	errors    chan error
	slots     chan struct{}
}

func Start(service *control.Service, records *recorder.Recorder) (*Server, error) {
	c := service.Acquire().Config()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{service: service, records: records, runtime: c.Runtime, ctx: ctx, cancel: cancel, errors: make(chan error, len(c.Proxies)), slots: make(chan struct{}, c.Runtime.MaxInflightRequests)}
	for _, p := range c.Proxies {
		if p.Protocol != "tcp" {
			continue
		}
		u, _ := url.Parse(p.Upstream)
		ln, err := net.Listen("tcp", p.Listen)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("proxy %s: bind listener: %w", p.ID, err)
		}
		s.endpoints = append(s.endpoints, endpoint{id: p.ID, upstream: u.Host, listener: ln})
	}
	for i := range s.endpoints {
		e := &s.endpoints[i]
		s.wg.Go(func() { s.accept(e) })
	}
	return s, nil
}

func (s *Server) Errors() <-chan error { return s.errors }

func (s *Server) Listeners() []control.ListenerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]control.ListenerStatus, 0, len(s.endpoints))
	for _, e := range s.endpoints {
		result = append(result, control.ListenerStatus{ProxyID: e.id, Address: e.listener.Addr().String(), Ready: !s.closing})
	}
	return result
}

func (s *Server) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.mu.Unlock()
		s.cancel()
		for _, e := range s.endpoints {
			e.listener.Close()
		}
		s.wg.Wait()
	})
}

func (s *Server) accept(e *endpoint) {
	for {
		client, err := e.listener.Accept()
		if err != nil {
			if s.ctx.Err() == nil {
				s.errors <- fmt.Errorf("proxy %s: listener: %w", e.id, err)
			}
			return
		}
		select {
		case s.slots <- struct{}{}:
			snapshot := s.service.Acquire()
			s.wg.Go(func() { defer func() { <-s.slots }(); s.serve(e, client, snapshot) })
		default:
			client.Close()
		}
	}
}

func (s *Server) serve(e *endpoint, client net.Conn, snapshot control.Snapshot) {
	defer client.Close()
	ctx, cancel := context.WithTimeout(s.ctx, s.runtime.RequestTimeout)
	defer cancel()
	stopClient := context.AfterFunc(ctx, func() { client.Close() })
	defer stopClient()
	decision, _ := snapshot.Decide(e.id, engine.Metadata{})
	event := recorder.Event{Info: snapshot.Info(), Type: "flow_started", FlowID: rand.Text(), ProxyID: e.id, Protocol: "tcp", StartedAt: time.Now().UTC()}
	s.records.Record(event)
	event.Type = "decision"
	event.RuleID, event.EligibleSequence, event.Selected = decision.RuleID, decision.EligibleSequence, decision.Selected
	event.Phase, event.Action = decision.Fault.Phase, decision.Fault.Action
	event.Probability, event.Nth, event.Every = decision.Selector.Probability, decision.Selector.Nth, decision.Selector.Every
	switch {
	case event.Probability != nil:
		event.Selector = "probability"
	case event.Nth != nil:
		event.Selector = "nth"
	case event.Every != nil:
		event.Selector = "every"
	}
	s.records.Record(event)
	var clientBytes, upstreamBytes atomic.Int64
	var applied atomic.Bool
	outcome := "forwarded"
	defer func() {
		event.Reached, event.Applied = applied.Load(), applied.Load()
		event.Type = "flow_finished"
		event.FinishedAt = time.Now().UTC()
		event.Outcome = outcome
		event.NotReached = event.Selected && !event.Reached
		event.ClientBytes = clientBytes.Load()
		event.UpstreamBytes = upstreamBytes.Load()
		s.records.Record(event)
	}()
	var prefetched []byte
	if decision.Selected && decision.Fault.Action == "delay_connect" {
		var waitErr error
		prefetched, waitErr = waitBeforeDial(ctx, client, *decision.Fault.Duration)
		if waitErr != nil {
			outcome = "client_closed"
			return
		}
		applied.Store(true)
		s.records.Record(appliedEvent(event))
	}
	dialer := net.Dialer{Timeout: s.runtime.RequestTimeout}
	upstream, err := dialer.DialContext(ctx, "tcp", e.upstream)
	if err != nil {
		outcome = "dial_error"
		return
	}
	defer upstream.Close()
	stopUpstream := context.AfterFunc(ctx, func() { upstream.Close() })
	defer stopUpstream()
	if !decision.Selected || decision.Fault.Action == "delay_connect" {
		forward(ctx, client, upstream, prefetched, nil, &clientBytes, &upstreamBytes)
		return
	}
	controller := newFault(ctx, decision.Fault, func() { client.Close(); upstream.Close() }, func() {
		applied.Store(true)
		s.records.Record(appliedEvent(event))
	})
	defer controller.stop()
	controller.start()
	forward(ctx, client, upstream, prefetched, controller, &clientBytes, &upstreamBytes)
	if controller.active.Load() {
		outcome = decision.Fault.Action
	}
}

func waitBeforeDial(ctx context.Context, client net.Conn, delay time.Duration) ([]byte, error) {
	deadline := time.Now().Add(delay)
	_ = client.SetReadDeadline(deadline)
	defer client.SetReadDeadline(time.Time{})
	buffer := make([]byte, 0, 16<<10)
	for len(buffer) < cap(buffer) {
		old := len(buffer)
		buffer = buffer[:old+min(4096, cap(buffer)-old)]
		n, err := client.Read(buffer[old:])
		buffer = buffer[:old+n]
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() && ctx.Err() == nil {
				return buffer, nil
			}
			return nil, err
		}
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return buffer, nil
	}
}

func appliedEvent(e recorder.Event) recorder.Event {
	e.Type = "fault_applied"
	e.Reached = true
	e.Applied = true
	return e
}

type faultState struct {
	ctx       context.Context
	f         config.Fault
	active    atomic.Bool
	mu        sync.Mutex
	finished  bool
	once      sync.Once
	closeBoth func()
	onApply   func()
	timer     *time.Timer
	holdTimer *time.Timer
}

func newFault(ctx context.Context, f config.Fault, closeBoth, onApply func()) *faultState {
	return &faultState{ctx: ctx, f: f, closeBoth: closeBoth, onApply: onApply}
}

func (f *faultState) start() {
	switch {
	case f.f.AfterDuration != nil:
		f.timer = time.AfterFunc(*f.f.AfterDuration, f.activate)
	case f.f.AfterBytes == nil || *f.f.AfterBytes == 0:
		f.activate()
	}
}

func (f *faultState) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = true
	if f.timer != nil {
		f.timer.Stop()
	}
	if f.holdTimer != nil {
		f.holdTimer.Stop()
	}
}

func (f *faultState) activate() {
	f.once.Do(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.finished {
			return
		}
		f.active.Store(true)
		f.onApply()
		if f.f.Action == "close_connection" {
			f.closeBoth()
		}
		if f.f.Action == "hold" {
			f.holdTimer = time.AfterFunc(*f.f.MaxDuration, f.closeBoth)
		}
	})
}

func (f *faultState) reachedBytes(direction string, count int64) {
	if f.f.AfterBytes != nil && f.f.TriggerDirection == direction && count >= int64(*f.f.AfterBytes) {
		f.activate()
	}
}

func (f *faultState) blocked(direction string) bool {
	return f.active.Load() && f.f.Action == "hold" && f.f.Direction == direction
}

func (f *faultState) pace(direction string, n int) bool {
	if !f.active.Load() || f.f.Action != "throttle" || f.f.Direction != direction {
		return true
	}
	d := time.Duration(int64(n) * int64(time.Second) / int64(*f.f.BytesPerSecond))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-f.ctx.Done():
		return false
	}
}

func forward(ctx context.Context, client, upstream net.Conn, prefetched []byte, f *faultState, clientBytes, upstreamBytes *atomic.Int64) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pump(ctx, io.MultiReader(bytes.NewReader(prefetched), client), upstream, "client_to_upstream", f, clientBytes)
	}()
	go func() { defer wg.Done(); pump(ctx, upstream, client, "upstream_to_client", f, upstreamBytes) }()
	wg.Wait()
}

func pump(ctx context.Context, src io.Reader, dst net.Conn, direction string, f *faultState, count *atomic.Int64) {
	defer func() {
		if tcp, ok := dst.(*net.TCPConn); ok {
			tcp.CloseWrite()
		}
	}()
	buf := make([]byte, 4096)
	for {
		if f != nil && f.blocked(direction) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(*f.f.MaxDuration):
				return
			}
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if f != nil && f.blocked(direction) {
				continue
			}
			payload := buf[:n]
			for len(payload) > 0 {
				chunk := payload
				if f != nil && f.f.AfterBytes != nil && f.f.TriggerDirection == direction && !f.active.Load() {
					remaining := int64(*f.f.AfterBytes) - count.Load()
					if remaining > 0 && remaining < int64(len(chunk)) {
						chunk = chunk[:remaining]
					}
				}
				if len(chunk) > 1024 && f != nil && f.f.Action == "throttle" {
					chunk = chunk[:1024]
				}
				if f != nil && !f.pace(direction, len(chunk)) {
					return
				}
				written, err := dst.Write(chunk)
				if written > 0 {
					amount := count.Add(int64(written))
					if f != nil {
						f.reachedBytes(direction, amount)
					}
					payload = payload[written:]
				}
				if err != nil || written == 0 {
					return
				}
				if f != nil && f.blocked(direction) {
					break
				}
			}
		}
		if readErr != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}
