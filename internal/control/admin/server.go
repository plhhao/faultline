package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"faultline/internal/control"
	"faultline/internal/recorder"
)

type RuleCounters struct {
	ProxyID  string `json:"proxy_id"`
	RuleID   string `json:"rule_id"`
	Eligible uint64 `json:"eligible"`
	Selected uint64 `json:"selected"`
}

type Status struct {
	Info      control.Info             `json:"info"`
	Ready     bool                     `json:"ready"`
	Listeners []control.ListenerStatus `json:"listeners"`
	Counters  recorder.Counters        `json:"run_counters"`
	Rules     []RuleCounters           `json:"revision_rule_counters"`
}

type Server struct {
	service   *control.Service
	recorder  *recorder.Recorder
	listeners func() []control.ListenerStatus
	server    *http.Server
	listener  *net.UnixListener
	errors    chan error
	done      chan struct{}
	readOnly  bool
	mu        sync.Mutex
}

func DefaultSocket() string {
	return filepath.Join("/tmp", fmt.Sprintf("faultline-%d", os.Getuid()), "admin.sock")
}

func Start(socket string, service *control.Service, records *recorder.Recorder, listeners func() []control.ListenerStatus, readOnly ...bool) (*Server, error) {
	socket, err := filepath.Abs(socket)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("admin directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("admin socket requires a private directory (0700)")
	}
	// Never unlink an existing socket: it may belong to another live instance.
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("bind admin socket: %w", err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		l.Close()
		return nil, err
	}
	s := &Server{service: service, recorder: records, listeners: listeners, listener: l, errors: make(chan error, 1), done: make(chan struct{})}
	s.readOnly = len(readOnly) > 0 && readOnly[0]
	s.server = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	go func() {
		defer close(s.done)
		if err := s.server.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			s.errors <- err
		}
	}()
	return s, nil
}

func (s *Server) Errors() <-chan error { return s.errors }

func (s *Server) Close() {
	s.server.Close()
	s.listener.Close()
	<-s.done
}

func (s *Server) status() Status {
	snapshot := s.service.Acquire()
	status := Status{Info: snapshot.Info(), Listeners: s.listeners(), Counters: s.recorder.Counters(), Rules: []RuleCounters{}}
	status.Ready = len(status.Listeners) > 0
	for _, listener := range status.Listeners {
		status.Ready = status.Ready && listener.Ready
	}
	for _, proxy := range snapshot.Config().Proxies {
		for _, rule := range proxy.Rules {
			counters, _ := snapshot.Counters(proxy.ID, rule.ID)
			status.Rules = append(status.Rules, RuleCounters{proxy.ID, rule.ID, counters.Eligible, counters.Selected})
		}
	}
	return status
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Origin") != "" {
		writeError(w, http.StatusForbidden, errors.New("browser requests are unsupported"))
		return
	}
	if r.URL.Path == "/status" && r.Method == http.MethodGet {
		s.mu.Lock()
		status := s.status()
		s.recorder.Record(recorder.Event{Info: status.Info, Type: "control", Operation: "status", Outcome: "ok"})
		s.mu.Unlock()
		json.NewEncoder(w).Encode(status)
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != "/enable" && r.URL.Path != "/disable" && r.URL.Path != "/reload") {
		writeError(w, http.StatusNotFound, errors.New("unknown admin operation"))
		return
	}
	if s.readOnly {
		writeError(w, http.StatusForbidden, errors.New("managed mode: use authenticated HTTPS API for mutations"))
		return
	}
	var request struct {
		Config string `json:"config"`
	}
	if r.URL.Path == "/reload" {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || !filepath.IsAbs(request.Config) {
			writeError(w, http.StatusBadRequest, errors.New("reload requires an absolute config path"))
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeError(w, http.StatusBadRequest, errors.New("expected one JSON object"))
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	var result control.Result
	var err error
	operation := r.URL.Path[1:]
	if operation == "reload" {
		result, err = s.service.ReloadFile(request.Config)
	} else {
		result = s.service.SetEnabled(operation == "enable")
	}
	event := recorder.Event{Info: result.Info, Type: "control", Operation: operation, Changed: result.Changed, Outcome: "ok"}
	if err != nil {
		event.Info, event.Outcome, event.ErrorKind = s.service.Acquire().Info(), "rejected", "invalid_config"
	}
	s.recorder.Record(event)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{err.Error()})
}
