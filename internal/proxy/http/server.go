package httpproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"

	"faultline/internal/control"
	"faultline/internal/fault"
)

// Observe runs synchronously as a flow finishes; it must be concurrency-safe and bounded.
type Options struct {
	Executor fault.Executor
	Observe  func(Report)
}

type endpoint struct {
	id        string
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
}

type Server struct {
	endpoints []endpoint
	cancel    context.CancelFunc
	once      sync.Once
	mu        sync.Mutex
	closing   bool
	wg        sync.WaitGroup
	errors    chan error
}

type connectionKey struct{}

// Start prepares every listener before any Serve loop starts. Success means bound,
// not that upstream dependencies or the application are ready.
func Start(service *control.Service, options Options) (*Server, error) {
	c := service.Acquire().Config()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cancel: cancel, errors: make(chan error, len(c.Proxies))}
	failed := true
	defer func() {
		if failed {
			s.Close()
		}
	}()
	slots := make(chan struct{}, c.Runtime.MaxInflightRequests)
	for _, p := range c.Proxies {
		transport, listenerTLS, err := transportFor(p)
		if err != nil {
			return nil, fmt.Errorf("proxy %s: %w", p.ID, err)
		}
		listener, err := net.Listen("tcp", p.Listen)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, fmt.Errorf("proxy %s: bind listener: %w", p.ID, err)
		}
		if listenerTLS != nil {
			listener = tls.NewListener(listener, listenerTLS)
		}
		target, _ := url.Parse(p.Upstream)
		handler := &handler{service: service, proxyID: p.ID, target: target, transport: transport, runtime: c.Runtime, slots: slots, options: options}
		server := &http.Server{
			Handler: s.track(handler), Protocols: http1(),
			ReadHeaderTimeout: c.Runtime.RequestTimeout, ReadTimeout: c.Runtime.RequestTimeout,
			WriteTimeout: c.Runtime.RequestTimeout, IdleTimeout: c.Runtime.RequestTimeout,
			BaseContext: func(net.Listener) context.Context { return ctx },
			ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
				return context.WithValue(ctx, connectionKey{}, conn)
			},
			ErrorLog: log.New(io.Discard, "", 0),
		}
		s.endpoints = append(s.endpoints, endpoint{p.ID, listener, server, transport})
	}
	for _, e := range s.endpoints {
		s.wg.Go(func() {
			err := e.server.Serve(e.listener)
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				s.errors <- fmt.Errorf("proxy %s: %w", e.id, err)
			}
		})
	}
	failed = false
	return s, nil
}

func (s *Server) Errors() <-chan error { return s.errors }

func (s *Server) track(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return
		}
		s.wg.Add(1)
		s.mu.Unlock()
		defer s.wg.Done()
		next.ServeHTTP(w, r)
	})
}

// Close cancels active flows and closes connections instead of draining requests.
func (s *Server) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.mu.Unlock()
		s.cancel()
		for _, e := range s.endpoints {
			e.server.Close()
			e.listener.Close()
			e.transport.CloseIdleConnections()
		}
		s.wg.Wait()
	})
}
