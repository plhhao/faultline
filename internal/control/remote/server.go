package remote

import (
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/control/admin"
	"faultline/internal/recorder"
)

//go:embed ui/*
var assets embed.FS

const sessionCookie = "__Host-faultline"

type session struct {
	Name, Role, Hash, CSRF string
	Expires                time.Time
}
type Server struct {
	mu          sync.Mutex
	store       *Store
	service     *control.Service
	records     *recorder.Recorder
	listeners   func() []control.ListenerStatus
	sessions    map[string]session
	actor       string
	loginWindow time.Time
	loginCount  int
	server      *http.Server
	errors      chan error
	done        chan struct{}
}

type Options struct{ Address, Certificate, Key string }

func New(store *Store, document *config.Document) (*Server, error) {
	users, err := readUsers(store.dir)
	if err != nil || len(users) == 0 {
		return nil, errors.New("create at least one account with faultline user before managed startup")
	}
	s := &Server{store: store, sessions: map[string]session{}, errors: make(chan error, 1), done: make(chan struct{})}
	s.service, err = control.NewManaged(document, store.state.Revision, func(doc *config.Document, result control.Result) error { return store.apply(s.actor, doc, result) })
	return s, err
}
func (s *Server) Service() *control.Service { return s.service }
func (s *Server) Errors() <-chan error      { return s.errors }
func (s *Server) Start(opts Options, records *recorder.Recorder, listeners func() []control.ListenerStatus) error {
	cert, err := tls.LoadX509KeyPair(opts.Certificate, opts.Key)
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", opts.Address)
	if err != nil {
		return err
	}
	s.records, s.listeners = records, listeners
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	go func() {
		defer close(s.done)
		err := s.server.Serve(tls.NewListener(l, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.fail(err)
		}
	}()
	return nil
}
func (s *Server) Close() {
	if s.server != nil {
		s.server.Close()
		<-s.done
	}
}
func (s *Server) fail(err error) {
	select {
	case s.errors <- err:
	default:
	}
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return errors.New("invalid JSON request or unknown field")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func (s *Server) audit(w http.ResponseWriter, actor, op, outcome string) bool {
	if err := s.store.audit(actor, op, outcome, s.service.Acquire().Info().Revision); err != nil {
		if s.store.poisoned {
			s.fail(err)
		}
		problem(w, 503, "audit storage unavailable; no operation performed")
		return false
	}
	return true
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	if r.TLS == nil {
		problem(w, 400, "HTTPS required")
		return
	}
	if r.Method != "GET" && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "https://"+r.Host {
		problem(w, 403, "cross-origin request rejected")
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/app.js" || r.URL.Path == "/diff.js" || r.URL.Path == "/style.css" || r.URL.Path == "/favicon.svg" {
		if r.Method != "GET" {
			problem(w, 405, "GET required")
			return
		}
		sub, _ := fs.Sub(assets, "ui")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.poisoned {
		problem(w, 503, "storage commit uncertain; restart required")
		return
	}
	now := time.Now()
	for key, sess := range s.sessions {
		if !now.Before(sess.Expires) {
			delete(s.sessions, key)
		}
	}
	if r.URL.Path == "/api/login" && r.Method == "POST" {
		s.login(w, r, now)
		return
	}
	cookie, err := r.Cookie(sessionCookie)
	var sess session
	if err == nil {
		sess = s.sessions[cookie.Value]
	}
	users, usersErr := readUsers(s.store.dir)
	u, exists := users[sess.Name]
	if sess.Name == "" || !exists || usersErr != nil || u.Hash != sess.Hash || u.Role != sess.Role {
		if err == nil {
			delete(s.sessions, cookie.Value)
		}
		if r.Method != "GET" && !s.audit(w, "anonymous", operation(r.URL.Path), "unauthorized") {
			return
		}
		problem(w, 401, "login required")
		return
	}
	if r.Method == "POST" && r.Header.Get("X-CSRF-Token") != sess.CSRF {
		if s.audit(w, sess.Name, operation(r.URL.Path), "csrf_rejected") {
			problem(w, 403, "CSRF token required")
		}
		return
	}
	if r.URL.Path == "/api/session" && r.Method == "GET" {
		reply(w, 200, map[string]string{"name": sess.Name, "role": sess.Role, "csrf": sess.CSRF})
		return
	}
	if r.URL.Path == "/api/logout" && r.Method == "POST" {
		if !s.audit(w, sess.Name, "logout", "ok") {
			return
		}
		delete(s.sessions, cookie.Value)
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/api/config":
			reply(w, 200, s.configView())
			return
		case "/api/status":
			reply(w, 200, struct {
				admin.Status
				Outcomes map[string]uint64 `json:"outcomes"`
			}{s.status(), s.records.Outcomes()})
			return
		case "/api/audit":
			reply(w, 200, s.store.state.Audit)
			return
		case "/api/capabilities":
			reply(w, 200, capabilities())
			return
		}
	}
	op := operation(r.URL.Path)
	if r.Method != "POST" || op == "unknown" {
		problem(w, 404, "unknown API operation")
		return
	}
	if sess.Role != "editor" {
		if s.audit(w, sess.Name, op, "forbidden") {
			problem(w, 403, "editor role required")
		}
		return
	}
	if op == "enable" || op == "disable" {
		if !s.audit(w, sess.Name, op, "ok") {
			return
		}
		result := s.service.SetEnabled(op == "enable")
		s.records.Record(recorder.Event{Info: result.Info, Type: "control", Operation: op, Changed: result.Changed, Outcome: "ok"})
		reply(w, 200, result)
		return
	}
	if op != "apply" && op != "validate" {
		problem(w, 404, "unknown API operation")
		return
	}
	var draft Draft
	if err := decode(w, r, &draft); err != nil {
		if s.audit(w, sess.Name, op, "invalid_request") {
			problem(w, 400, err.Error())
		}
		return
	}
	if draft.BaseRevision != s.service.Acquire().Info().Revision {
		if s.audit(w, sess.Name, op, "conflict") {
			reply(w, 409, map[string]any{"error": "Config changed. Compare your draft with the latest active revision.", "active": s.configView()})
		}
		return
	}
	doc, err := s.document(draft)
	if err != nil {
		if s.audit(w, sess.Name, op, "invalid_config") {
			problem(w, 422, err.Error())
		}
		return
	}
	if op == "validate" {
		if s.audit(w, sess.Name, op, "ok") {
			reply(w, 200, map[string]any{"valid": true, "base_revision": draft.BaseRevision})
		}
		return
	}
	s.actor = sess.Name
	result, err := s.service.ApplyRevision(doc, draft.BaseRevision)
	s.actor = ""
	if err != nil {
		s.records.Record(recorder.Event{Info: s.service.Acquire().Info(), Type: "control", Operation: "apply", Outcome: "rejected", ErrorKind: "managed_apply_failed"})
		if s.store.poisoned {
			s.fail(err)
		} else if !s.audit(w, sess.Name, "apply", "failed") {
			return
		}
		if errors.Is(err, control.ErrConflict) {
			problem(w, 409, "config revision conflict")
		} else {
			problem(w, 503, "apply failed; refresh active revision before retrying")
		}
		return
	}
	s.records.Record(recorder.Event{Info: result.Info, Type: "control", Operation: "apply", Changed: result.Changed, Outcome: "ok"})
	reply(w, 200, result)
}
func operation(path string) string {
	switch path {
	case "/api/login":
		return "login"
	case "/api/logout":
		return "logout"
	case "/api/apply":
		return "apply"
	case "/api/validate":
		return "validate"
	case "/api/enable":
		return "enable"
	case "/api/disable":
		return "disable"
	}
	return "unknown"
}
func (s *Server) login(w http.ResponseWriter, r *http.Request, now time.Time) {
	if now.Sub(s.loginWindow) >= time.Minute {
		s.loginWindow, s.loginCount = now, 0
	}
	if s.loginCount >= 20 {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "login rate limit; retry in one minute")
		return
	}
	s.loginCount++
	var credentials struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &credentials); err != nil || len(credentials.Password) > 1024 {
		problem(w, 400, "invalid login request")
		return
	}
	users, err := readUsers(s.store.dir)
	u := users[credentials.Name]
	valid := verifyPassword(u, credentials.Password)
	if err != nil || !valid {
		if s.audit(w, "anonymous", "login", "rejected") {
			problem(w, 401, "invalid username or password")
		}
		return
	}
	if len(s.sessions) >= 256 {
		problem(w, 503, "session limit reached")
		return
	}
	if !s.audit(w, credentials.Name, "login", "ok") {
		return
	}
	token := rand.Text()
	sess := session{credentials.Name, u.Role, u.Hash, rand.Text(), now.Add(8 * time.Hour)}
	if old, err := r.Cookie(sessionCookie); err == nil {
		delete(s.sessions, old.Value)
	}
	s.sessions[token] = sess
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 60 * 60})
	reply(w, 200, map[string]string{"name": sess.Name, "role": sess.Role, "csrf": sess.CSRF})
}

type ProxyDraft struct {
	ID    string        `json:"id"`
	Rules []config.Rule `json:"rules"`
}
type Draft struct {
	BaseRevision uint64       `json:"base_revision"`
	Proxies      []ProxyDraft `json:"proxies"`
}

func (s *Server) document(d Draft) (*config.Document, error) {
	c := s.service.Acquire().Config()
	if len(d.Proxies) != len(c.Proxies) {
		return nil, errors.New("proxies: all existing proxies are required")
	}
	seen := map[string]bool{}
	for _, edit := range d.Proxies {
		if seen[edit.ID] {
			return nil, errors.New("proxies: duplicate proxy")
		}
		seen[edit.ID] = true
		found := false
		for i := range c.Proxies {
			if c.Proxies[i].ID == edit.ID {
				c.Proxies[i].Rules = edit.Rules
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("proxies: unknown proxy")
		}
	}
	data, err := config.Encode(c)
	if err != nil {
		return nil, err
	}
	return config.Parse(data, filepath.Join(s.store.dir, "active.yaml"))
}
func (s *Server) configView() any {
	snapshot := s.service.Acquire()
	type proxyView struct {
		ID       string        `json:"id"`
		Protocol string        `json:"protocol"`
		Listen   string        `json:"listen"`
		Upstream string        `json:"upstream"`
		Rules    []config.Rule `json:"rules"`
	}
	proxies := []proxyView{}
	for _, p := range snapshot.Config().Proxies {
		proxies = append(proxies, proxyView{p.ID, p.Protocol, p.Listen, p.Upstream, p.Rules})
	}
	return map[string]any{"revision": snapshot.Info().Revision, "proxies": proxies, "last_apply": s.store.state.LastApply}
}
func (s *Server) status() admin.Status {
	snapshot := s.service.Acquire()
	status := admin.Status{Info: snapshot.Info(), Listeners: s.listeners(), Counters: s.records.Counters(), Rules: []admin.RuleCounters{}}
	status.Ready = len(status.Listeners) > 0
	for _, l := range status.Listeners {
		status.Ready = status.Ready && l.Ready
	}
	for _, p := range snapshot.Config().Proxies {
		for _, r := range p.Rules {
			c, _ := snapshot.Counters(p.ID, r.ID)
			status.Rules = append(status.Rules, admin.RuleCounters{ProxyID: p.ID, RuleID: r.ID, Eligible: c.Eligible, Selected: c.Selected})
		}
	}
	return status
}

func capabilities() any {
	return map[string]any{"actions": map[string][]string{"http1": config.Actions("http1"), "http2": config.Actions("http2"), "grpc": config.Actions("grpc"), "postgresql": config.Actions("postgresql"), "mysql": config.Actions("mysql")}, "phases": []string{config.BeforeUpstreamRequest, config.AfterUpstreamHeaders}, "protocol_phases": map[string][]string{"postgresql": {config.AfterCommit}, "mysql": {config.AfterCommit}}, "selectors": []string{"Probability", "Nth", "Every"}}
}
