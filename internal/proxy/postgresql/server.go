package postgresql

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
	slots      chan struct{}
	handshakes chan struct{}
	mu         sync.Mutex
	closing    bool
	cancels    map[string]context.CancelFunc
}

func Start(service *control.Service, records *recorder.Recorder) (*Server, error) {
	c := service.Acquire().Config()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{service: service, records: records, runtime: c.Runtime, ctx: ctx, cancel: cancel, errors: make(chan error, len(c.Proxies)), slots: make(chan struct{}, c.Runtime.MaxInflightRequests), handshakes: make(chan struct{}, c.Runtime.MaxInflightRequests+16), cancels: map[string]context.CancelFunc{}}
	for _, p := range c.Proxies {
		if p.Protocol != "postgresql" {
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
			return nil, errors.New("postgresql: cannot load listener TLS")
		}
		e.clientTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	if u.Scheme == "postgresqls" {
		e.upstreamTLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
		if p.UpstreamTLS != nil && p.UpstreamTLS.CAFile != "" {
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			b, err := os.ReadFile(p.UpstreamTLS.CAFile)
			if err != nil || !roots.AppendCertsFromPEM(b) {
				return nil, errors.New("postgresql: cannot load upstream CA")
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
				s.errors <- errors.New("postgresql: listener failed")
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
func (s *Server) register(key string, cancel context.CancelFunc) func() {
	s.mu.Lock()
	s.cancels[key] = cancel
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.cancels, key); s.mu.Unlock() }
}
func (s *Server) cancelSession(key string) {
	s.mu.Lock()
	cancel := s.cancels[key]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func deadline(c net.Conn, d time.Duration) { _ = c.SetDeadline(time.Now().Add(d)) }
