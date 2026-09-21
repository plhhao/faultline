package rabbitmq

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

var protocolHeader = []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}

type endpoint struct {
	proxy                  config.Proxy
	listener               net.Listener
	clientTLS, upstreamTLS *tls.Config
	address                string
}

type Server struct {
	service    *control.Service
	records    *recorder.Recorder
	runtime    config.Runtime
	endpoints  []*endpoint
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	once       sync.Once
	errors     chan error
	slots      chan struct{}
	handshakes chan struct{}
	mu         sync.Mutex
	closing    bool
}

func Start(service *control.Service, records *recorder.Recorder) (*Server, error) {
	c := service.Acquire().Config()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{service: service, records: records, runtime: c.Runtime, ctx: ctx, cancel: cancel, errors: make(chan error, len(c.Proxies)), slots: make(chan struct{}, c.Runtime.MaxInflightRequests), handshakes: make(chan struct{}, c.Runtime.MaxInflightRequests+16)}
	for _, p := range c.Proxies {
		if p.Protocol != "rabbitmq" {
			continue
		}
		e, err := prepare(p)
		if err != nil {
			s.Close()
			return nil, err
		}
		e.listener, err = net.Listen("tcp", p.Listen)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.endpoints = append(s.endpoints, e)
	}
	for _, e := range s.endpoints {
		s.wg.Go(func() { s.accept(e) })
	}
	return s, nil
}

func prepare(p config.Proxy) (*endpoint, error) {
	u, _ := url.Parse(p.Upstream)
	e := &endpoint{proxy: p, address: u.Host}
	if p.TLS != nil {
		cert, err := tls.LoadX509KeyPair(p.TLS.CertFile, p.TLS.KeyFile)
		if err != nil {
			return nil, errors.New("rabbitmq: cannot load listener TLS")
		}
		e.clientTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		if p.TLS.ClientCAFile != "" {
			pool := x509.NewCertPool()
			data, err := os.ReadFile(p.TLS.ClientCAFile)
			if err != nil || !pool.AppendCertsFromPEM(data) {
				return nil, errors.New("rabbitmq: cannot load listener client CA")
			}
			e.clientTLS.ClientCAs = pool
			e.clientTLS.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	if u.Scheme == "amqps" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, errors.New("rabbitmq: cannot load system CA pool")
		}
		e.upstreamTLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: roots}
		if p.UpstreamTLS != nil && p.UpstreamTLS.CAFile != "" {
			data, err := os.ReadFile(p.UpstreamTLS.CAFile)
			if err != nil || !roots.AppendCertsFromPEM(data) {
				return nil, errors.New("rabbitmq: cannot load upstream CA")
			}
		}
		if p.UpstreamTLS != nil && p.UpstreamTLS.CertFile != "" {
			cert, err := tls.LoadX509KeyPair(p.UpstreamTLS.CertFile, p.UpstreamTLS.KeyFile)
			if err != nil {
				return nil, errors.New("rabbitmq: cannot load upstream client certificate/key")
			}
			e.upstreamTLS.Certificates = []tls.Certificate{cert}
		}
	}
	return e, nil
}

func (s *Server) accept(e *endpoint) {
	for {
		c, err := e.listener.Accept()
		if err != nil {
			if s.ctx.Err() == nil {
				s.errors <- errors.New("rabbitmq: listener failed")
			}
			return
		}
		select {
		case s.handshakes <- struct{}{}:
		default:
			_ = c.Close()
			continue
		}
		s.wg.Go(func() { defer func() { <-s.handshakes }(); s.serve(e, c) })
	}
}

func (s *Server) serve(e *endpoint, raw net.Conn) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if err := raw.SetDeadline(time.Now().Add(s.runtime.RequestTimeout)); err != nil {
		return
	}
	client := raw
	if e.clientTLS != nil {
		secure := tls.Server(raw, e.clientTLS)
		if secure.HandshakeContext(ctx) != nil {
			return
		}
		client = secure
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return
	}
	upstream, err := connectUpstream(ctx, e, s.runtime.RequestTimeout)
	if err != nil {
		return
	}
	defer upstream.Close()
	stopUpstream := context.AfterFunc(ctx, func() { _ = upstream.Close() })
	defer stopUpstream()
	if err := upstream.SetDeadline(time.Now().Add(s.runtime.RequestTimeout)); err != nil {
		return
	}
	if err := handshake(client, upstream); err != nil {
		return
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return
	}
	if err := upstream.SetDeadline(time.Time{}); err != nil {
		return
	}
	newSession(s, e, ctx, client, upstream).exchange()
}

func connectUpstream(ctx context.Context, e *endpoint, timeout time.Duration) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", e.address)
	if err != nil {
		return nil, err
	}
	if e.upstreamTLS == nil {
		return raw, nil
	}
	secure := tls.Client(raw, e.upstreamTLS)
	if err := secure.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return secure, nil
}

func handshake(client, upstream net.Conn) error {
	header := make([]byte, len(protocolHeader))
	if _, err := io.ReadFull(client, header); err != nil || string(header) != string(protocolHeader) {
		return errProtocol
	}
	if err := writeAll(upstream, header); err != nil {
		return err
	}
	for _, step := range []struct {
		from, to    net.Conn
		class, name uint16
	}{
		{upstream, client, 10, 10},
		{client, upstream, 10, 11},
		{upstream, client, 10, 30},
		{client, upstream, 10, 31},
		{client, upstream, 10, 40},
		{upstream, client, 10, 41},
	} {
		f, err := readFrame(step.from)
		if err != nil || f.channel != 0 || !method(f, step.class, step.name) {
			return errProtocol
		}
		if err := writeFrame(step.to, f); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Errors() <-chan error { return s.errors }

func (s *Server) Listeners() []control.ListenerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	listeners := make([]control.ListenerStatus, 0, len(s.endpoints))
	for _, e := range s.endpoints {
		listeners = append(listeners, control.ListenerStatus{ProxyID: e.proxy.ID, Address: e.listener.Addr().String(), Ready: !s.closing})
	}
	return listeners
}

func (s *Server) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.mu.Unlock()
		s.cancel()
		for _, e := range s.endpoints {
			_ = e.listener.Close()
		}
		s.wg.Wait()
	})
}

type publish struct {
	sequence   uint64
	event      recorder.Event
	decision   engine.Decision
	exchange   string
	routingKey string
	returned   bool
}

type publishCandidate struct {
	exchange, routingKey string
	remaining            uint64
	header               bool
}

type channelState struct {
	confirming bool
	requested  bool
	next       uint64
	pending    map[uint64]*publish
	candidate  *publishCandidate
}

type session struct {
	server   *Server
	endpoint *endpoint
	ctx      context.Context
	client   net.Conn
	upstream net.Conn
	mu       sync.Mutex
	channels map[uint16]*channelState
}

func newSession(server *Server, endpoint *endpoint, ctx context.Context, client, upstream net.Conn) *session {
	return &session{server: server, endpoint: endpoint, ctx: ctx, client: client, upstream: upstream, channels: map[uint16]*channelState{}}
}

func (s *session) state(channel uint16) *channelState {
	state := s.channels[channel]
	if state == nil {
		state = &channelState{pending: map[uint64]*publish{}}
		s.channels[channel] = state
	}
	return state
}

func (s *session) clientFrame(f frame) error {
	if method(f, 20, 40) {
		s.closeChannel(f.channel)
		return writeFrame(s.upstream, f)
	}
	if f.kind != frameHeartbeat && f.kind != frameHeader && f.kind != frameBody && s.incomplete(f.channel) {
		return errProtocol
	}
	if method(f, 85, 10) {
		s.mu.Lock()
		s.state(f.channel).requested = true
		s.mu.Unlock()
	}
	if method(f, 60, 40) {
		exchange, routingKey, ok := publishMetadata(f)
		if !ok {
			return errProtocol
		}
		if err := s.startPublish(f.channel, exchange, routingKey); err != nil {
			return err
		}
	}
	if f.kind == frameHeader {
		if err := s.publishHeader(f); err != nil {
			return err
		}
	}
	if f.kind == frameBody {
		if err := s.publishBody(f); err != nil {
			return err
		}
	}
	return writeFrame(s.upstream, f)
}

func (s *session) incomplete(channel uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.channels[channel]
	return state != nil && state.candidate != nil
}

func (s *session) startPublish(channel uint16, exchange, routingKey string) error {
	s.mu.Lock()
	state := s.state(channel)
	defer s.mu.Unlock()
	if state.candidate != nil {
		return errProtocol
	}
	if state.confirming {
		state.candidate = &publishCandidate{exchange: exchange, routingKey: routingKey}
	}
	return nil
}

func (s *session) publishHeader(f frame) error {
	s.mu.Lock()
	state := s.state(f.channel)
	candidate := state.candidate
	if candidate == nil {
		s.mu.Unlock()
		return nil
	}
	size, ok := contentSize(f)
	if !ok || candidate.header {
		s.mu.Unlock()
		return errProtocol
	}
	candidate.header = true
	candidate.remaining = size
	if size != 0 {
		s.mu.Unlock()
		return nil
	}
	state.candidate = nil
	s.mu.Unlock()
	return s.addPublish(f.channel, candidate)
}

func (s *session) publishBody(f frame) error {
	s.mu.Lock()
	state := s.state(f.channel)
	candidate := state.candidate
	if candidate == nil {
		s.mu.Unlock()
		return nil
	}
	if !candidate.header || uint64(len(f.body)) > candidate.remaining {
		s.mu.Unlock()
		return errProtocol
	}
	candidate.remaining -= uint64(len(f.body))
	if candidate.remaining != 0 {
		s.mu.Unlock()
		return nil
	}
	state.candidate = nil
	s.mu.Unlock()
	return s.addPublish(f.channel, candidate)
}

func (s *session) addPublish(channel uint16, candidate *publishCandidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state(channel)
	if len(state.pending) >= s.server.runtime.MaxInflightRequests {
		return errProtocol
	}
	state.next++
	snap := s.server.service.Acquire()
	decision, err := snap.Decide(s.endpoint.proxy.ID, engine.Metadata{Exchange: candidate.exchange, RoutingKey: candidate.routingKey})
	if err != nil {
		return err
	}
	event := recorder.Event{Info: snap.Info(), FlowID: rand.Text(), ProxyID: s.endpoint.proxy.ID, Protocol: "rabbitmq", Operation: "publish", StartedAt: time.Now().UTC(), Type: "flow_started"}
	s.server.records.Record(event)
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
	s.server.records.Record(event)
	state.pending[state.next] = &publish{sequence: state.next, event: event, decision: decision, exchange: candidate.exchange, routingKey: candidate.routingKey}
	return nil
}

func (s *session) serverFrame(f frame) (bool, error) {
	if method(f, 20, 40) {
		s.closeChannel(f.channel)
		return true, writeFrame(s.client, f)
	}
	if method(f, 85, 11) {
		s.mu.Lock()
		state := s.state(f.channel)
		if !state.requested {
			s.mu.Unlock()
			return false, errProtocol
		}
		state.confirming = true
		s.mu.Unlock()
		return true, writeFrame(s.client, f)
	}
	if tag, multiple, ok := confirm(f); ok {
		return s.handleConfirm(f, tag, multiple, true)
	}
	if tag, multiple, ok := negativeConfirm(f); ok {
		return s.handleConfirm(f, tag, multiple, false)
	}
	if exchange, routingKey, ok := returnMetadata(f); ok {
		s.markReturn(f.channel, exchange, routingKey)
	}
	return true, writeFrame(s.client, f)
}

func (s *session) markReturn(channel uint16, exchange, routingKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state(channel)
	var matched *publish
	for _, p := range state.pending {
		if p.exchange == exchange && p.routingKey == routingKey && !p.returned && (matched == nil || p.sequence < matched.sequence) {
			matched = p
		}
	}
	if matched != nil {
		matched.returned = true
	}
}

func (s *session) closeChannel(channel uint16) {
	s.mu.Lock()
	state := s.channels[channel]
	delete(s.channels, channel)
	s.mu.Unlock()
	if state == nil {
		return
	}
	for _, p := range state.pending {
		s.finish(p, "channel_closed")
	}
}

func (s *session) take(channel uint16, tag uint64, multiple bool) []*publish {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state(channel)
	matched := []*publish{}
	for sequence, p := range state.pending {
		if sequence == tag || multiple && (tag == 0 || sequence <= tag) {
			matched = append(matched, p)
			delete(state.pending, sequence)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].sequence < matched[j].sequence })
	return matched
}

func (s *session) handleConfirm(f frame, tag uint64, multiple, accepted bool) (bool, error) {
	publishes := s.take(f.channel, tag, multiple)
	if len(publishes) == 0 {
		return true, writeFrame(s.client, f)
	}
	if !accepted {
		for _, p := range publishes {
			s.finish(p, "publisher_nack")
		}
		return true, writeFrame(s.client, f)
	}
	var chosen *publish
	for _, p := range publishes {
		if p.returned {
			continue
		}
		if p.decision.Selected {
			p.event.Reached = true
		}
		if p.decision.Selected && chosen == nil {
			chosen = p
		}
	}
	if chosen == nil {
		if err := writeFrame(s.client, f); err != nil {
			return false, err
		}
		for _, p := range publishes {
			outcome := "publish_confirmed"
			if p.returned {
				outcome = "publish_returned"
			}
			s.finish(p, outcome)
		}
		return true, nil
	}
	chosen.event.Type = "fault_applied"
	chosen.event.Applied = true
	s.server.records.Record(chosen.event)
	if !s.apply(chosen.decision.Fault) {
		for _, p := range publishes {
			s.finish(p, "publish_confirm_lost")
		}
		return false, nil
	}
	if err := writeFrame(s.client, f); err != nil {
		return false, err
	}
	for _, p := range publishes {
		outcome := "publish_confirmed"
		if p.returned {
			outcome = "publish_returned"
		} else if p == chosen {
			outcome = "publish_confirm_delayed"
		}
		s.finish(p, outcome)
	}
	return true, nil
}

func (s *session) apply(f config.Fault) bool {
	if f.Action == "close_connection" {
		return false
	}
	duration := f.Duration
	if f.Action == "hold_response" {
		duration = f.MaxDuration
	}
	ctx, cancel := context.WithTimeout(s.ctx, s.server.runtime.RequestTimeout)
	defer cancel()
	timer := time.NewTimer(*duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return f.Action == "delay"
	}
}

func (s *session) finish(p *publish, outcome string) {
	p.event.Type = "flow_finished"
	p.event.Outcome = outcome
	p.event.FinishedAt = time.Now().UTC()
	p.event.NotReached = p.event.Selected && !p.event.Reached
	s.server.records.Record(p.event)
}

func (s *session) finishPending(outcome string) {
	s.mu.Lock()
	pending := []*publish{}
	for _, state := range s.channels {
		for _, p := range state.pending {
			pending = append(pending, p)
		}
		state.pending = map[uint64]*publish{}
	}
	s.mu.Unlock()
	for _, p := range pending {
		s.finish(p, outcome)
	}
}

func (s *session) exchange() {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	defer s.finishPending("connection_closed")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			f, err := readFrame(s.client)
			if err != nil {
				errs <- err
				return
			}
			if err := s.clientFrame(f); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			f, err := readFrame(s.upstream)
			if err != nil {
				errs <- err
				return
			}
			keep, err := s.serverFrame(f)
			if err != nil || !keep {
				errs <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
	case <-errs:
	}
	_ = s.client.Close()
	_ = s.upstream.Close()
	wg.Wait()
}
