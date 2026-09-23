package bullmq

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/recorder"
)

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
	handshakes chan struct{}
	pending    chan struct{}
	mu         sync.Mutex
	closing    bool
}

func Start(service *control.Service, records *recorder.Recorder) (*Server, error) {
	c := service.Acquire().Config()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{service: service, records: records, runtime: c.Runtime, ctx: ctx, cancel: cancel, errors: make(chan error, len(c.Proxies)), handshakes: make(chan struct{}, c.Runtime.MaxInflightRequests), pending: make(chan struct{}, c.Runtime.MaxInflightRequests)}
	for _, p := range c.Proxies {
		if p.Protocol != "bullmq" {
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
			return nil, errors.New("bullmq: cannot load listener TLS")
		}
		e.clientTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	if u.Scheme == "rediss" {
		e.upstreamTLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
		if p.UpstreamTLS != nil && p.UpstreamTLS.CAFile != "" {
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			b, err := os.ReadFile(p.UpstreamTLS.CAFile)
			if err != nil || !roots.AppendCertsFromPEM(b) {
				return nil, errors.New("bullmq: cannot load upstream CA")
			}
			e.upstreamTLS.RootCAs = roots
		}
	}
	return e, nil
}
func (s *Server) accept(e *endpoint) {
	for {
		c, err := e.listener.Accept()
		if err != nil {
			if s.ctx.Err() == nil {
				s.errors <- errors.New("bullmq: listener failed")
			}
			return
		}
		select {
		case s.handshakes <- struct{}{}:
		default:
			c.Close()
			continue
		}
		s.wg.Go(func() { defer func() { <-s.handshakes }(); s.serve(e, c) })
	}
}
func (s *Server) Errors() <-chan error { return s.errors }
func (s *Server) Listeners() []control.ListenerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []control.ListenerStatus{}
	for _, e := range s.endpoints {
		result = append(result, control.ListenerStatus{ProxyID: e.proxy.ID, Address: e.listener.Addr().String(), Ready: !s.closing})
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
func (s *Server) serve(e *endpoint, raw net.Conn) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	_ = raw.SetDeadline(time.Now().Add(s.runtime.RequestTimeout))
	client := raw
	if e.clientTLS != nil {
		secure := tls.Server(raw, e.clientTLS)
		if secure.HandshakeContext(ctx) != nil {
			return
		}
		client = secure
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, s.runtime.RequestTimeout)
	defer dialCancel()
	upstream, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", e.address)
	if err != nil {
		return
	}
	defer upstream.Close()
	rawUpstream := upstream
	stopUpstream := context.AfterFunc(ctx, func() { _ = rawUpstream.Close() })
	defer stopUpstream()
	if e.upstreamTLS != nil {
		secure := tls.Client(upstream, e.upstreamTLS)
		if secure.HandshakeContext(dialCtx) != nil {
			return
		}
		upstream = secure
	}
	_ = client.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})
	(&session{server: s, endpoint: e, ctx: ctx, client: client, upstream: upstream}).exchange()
}
