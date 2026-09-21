package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/recorder"
)

const fixture = `api_version: faultline/v1alpha1
proxies:
- id: test
  protocol: http1
  listen: 127.0.0.1:18080
  upstream: http://127.0.0.1:19090
  rules:
  - id: fail
    select: {probability: 0}
    fault: {action: respond, phase: before_upstream_request, status: 503}
`

func setup(t *testing.T) *Server {
	t.Helper()
	return setupConfig(t, fixture)
}

func setupConfig(t *testing.T, fixture string) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "bootstrap.yaml")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetUser(dir, "tester", "editor", "test-password-123", false); err != nil {
		t.Fatal(err)
	}
	store, doc, err := Open(dir, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	s, err := New(store, doc)
	if err != nil {
		t.Fatal(err)
	}
	s.records = recorder.New(io.Discard, 100)
	t.Cleanup(func() { s.records.Close(context.Background()) })
	s.listeners = func() []control.ListenerStatus { return []control.ListenerStatus{{ProxyID: "test", Ready: true}} }
	return s
}
func call(s *Server, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "https://localhost"+path, bytes.NewReader(data))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	w := call(s, "POST", "/api/login", map[string]string{"name": "tester", "password": "test-password-123"}, nil, "")
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body)
	}
	var data map[string]string
	json.Unmarshal(w.Body.Bytes(), &data)
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	return cookie, data["csrf"]
}
func draftFor(s *Server, probability float64) Draft {
	c := s.service.Acquire().Config()
	c.Proxies[0].Rules[0].Select.Probability = &probability
	return Draft{s.service.Acquire().Info().Revision, []ProxyDraft{{"test", c.Proxies[0].Rules}}}
}
func TestApplyConflictPersistenceAndNoOp(t *testing.T) {
	s := setup(t)
	cookie, csrf := login(t, s)
	initial := s.service.Acquire()
	d := draftFor(s, 1)
	d.Proxies[0].Rules[0].Match.PathPattern = "/payment/:id"
	if w := call(s, "POST", "/api/validate", d, cookie, csrf); w.Code != 200 {
		t.Fatalf("pattern validation: %d %s", w.Code, w.Body)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- call(s, "POST", "/api/apply", d, cookie, csrf).Code })
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("%v", counts)
	}
	if initial.Config().Proxies[0].Rules[0].Match.PathPattern != "" || initial.Info().Revision != 1 || *initial.Config().Proxies[0].Rules[0].Select.Probability != 0 {
		t.Fatal("old snapshot changed")
	}
	if s.service.Acquire().Info().Revision != 2 {
		t.Fatal("revision")
	}
	w := call(s, "POST", "/api/apply", draftFor(s, 1), cookie, csrf)
	if w.Code != 200 || s.service.Acquire().Info().Revision != 2 || s.store.state.LastApply.Changed {
		t.Fatal("no-op changed revision")
	}
	call(s, "POST", "/api/enable", map[string]any{}, cookie, csrf)
	dir := s.store.dir
	s.store.Close()
	store, doc, err := Open(dir, "missing-bootstrap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored, err := New(store, doc)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config().Proxies[0].Rules[0].Match.PathPattern != "/payment/:id" || restored.service.Acquire().Info().Revision != 2 || restored.service.Acquire().Info().Enabled || *doc.Config().Proxies[0].Rules[0].Select.Probability != 1 {
		t.Fatal("restart did not restore disabled committed config")
	}
	if len(store.state.Audit) < 5 {
		t.Fatal("audit not durable")
	}
	if _, err := s.service.Apply(doc); err == nil {
		t.Fatal("unguarded apply allowed")
	}
}
func TestValidationPermissionsAndSessionRevocation(t *testing.T) {
	s := setup(t)
	cookie, csrf := login(t, s)
	invalid := draftFor(s, 2)
	for _, op := range []string{"validate", "apply"} {
		w := call(s, "POST", "/api/"+op, invalid, cookie, csrf)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "probability") {
			t.Fatalf("%s %d %s", op, w.Code, w.Body)
		}
	}
	if s.service.Acquire().Info().Revision != 1 {
		t.Fatal("invalid changed runtime")
	}
	if w := call(s, "POST", "/api/apply", map[string]any{"base_revision": 1, "proxies": []any{map[string]any{"id": "test", "listen": "127.0.0.1:9"}}}, cookie, csrf); w.Code != 400 {
		t.Fatal("infrastructure field accepted")
	}
	if w := call(s, "POST", "/api/enable", map[string]any{}, cookie, ""); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	r := httptest.NewRequest("POST", "https://localhost/api/enable", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	users, _ := readUsers(s.store.dir)
	u := users["tester"]
	s.sessions[cookie.Value] = session{"tester", "viewer", u.Hash, csrf, time.Now().Add(time.Hour)}
	u.Role = "viewer"
	users["tester"] = u
	data, _ := json.Marshal(users)
	atomicFile(filepath.Join(s.store.dir, "users.json"), data)
	for _, op := range []string{"apply", "enable", "disable", "validate"} {
		if w := call(s, "POST", "/api/"+op, draftFor(s, 1), cookie, csrf); w.Code != 403 {
			t.Fatalf("viewer %s %d", op, w.Code)
		}
	}
	if w := call(s, "GET", "/api/config", nil, cookie, ""); w.Code != 200 {
		t.Fatal("viewer cannot read")
	}
	if err := SetUser(s.store.dir, "tester", "", "", true); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/api/status", nil, cookie, ""); w.Code != 401 {
		t.Fatal("deleted account session survived")
	}
}
func TestStorageFailureLeavesRuntimeAndAuditBounded(t *testing.T) {
	s := setup(t)
	cookie, csrf := login(t, s)
	original := s.store.dir
	s.store.dir = filepath.Join(original, "missing")
	if w := call(s, "POST", "/api/apply", draftFor(s, 1), cookie, csrf); w.Code != 503 {
		t.Fatalf("unreadable users not fail closed: %d", w.Code)
	}
	s.store.dir = original
	// Force a pre-rename write failure without changing account lookup.
	if err := os.Rename(filepath.Join(original, "state.json"), filepath.Join(original, "backup.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(original, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	cookie, csrf = loginWithoutAudit(t, s)
	if w := call(s, "POST", "/api/apply", draftFor(s, 1), cookie, csrf); w.Code != 503 {
		t.Fatalf("write failure %d %s", w.Code, w.Body)
	}
	if s.service.Acquire().Info().Revision != 1 {
		t.Fatal("failed persistence published")
	}
	if w := call(s, "POST", "/api/enable", map[string]any{}, cookie, csrf); w.Code != 503 || s.service.Acquire().Info().Enabled {
		t.Fatal("audit failure allowed toggle")
	}
	st := state{}
	for range auditLimit + 10 {
		st = appendAudit(st, "actor", "validate", "ok", 1)
	}
	if len(st.Audit) != auditLimit {
		t.Fatal("unbounded audit")
	}
}
func loginWithoutAudit(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	users, err := readUsers(s.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	s.sessions["test-session"] = session{"tester", users["tester"].Role, users["tester"].Hash, "csrf", time.Now().Add(time.Hour)}
	return &http.Cookie{Name: sessionCookie, Value: "test-session"}, "csrf"
}
func TestExpiryLogoutRateLimitAndAssets(t *testing.T) {
	s := setup(t)
	bad := call(s, "POST", "/api/login", map[string]string{"name": "tester", "password": "wrong-password"}, nil, "")
	if bad.Code != 401 || len(bad.Result().Cookies()) != 0 {
		t.Fatal("invalid password accepted")
	}

	cookie, csrf := login(t, s)
	for _, path := range []string{"/", "/app.js", "/diff.js", "/style.css", "/favicon.svg"} {
		w := call(s, "GET", path, nil, nil, "")
		if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("asset %s %d", path, w.Code)
		}
	}
	if w := call(s, "POST", "/api/logout", map[string]any{}, cookie, csrf); w.Code != 200 {
		t.Fatal("logout")
	}
	if w := call(s, "GET", "/api/session", nil, cookie, ""); w.Code != 401 {
		t.Fatal("logout cookie survived")
	}
	cookie, csrf = loginWithoutAudit(t, s)
	expired := s.sessions[cookie.Value]
	expired.Expires = time.Now().Add(-time.Second)
	s.sessions[cookie.Value] = expired
	if w := call(s, "GET", "/api/session", nil, cookie, csrf); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	s.loginWindow = time.Now()
	s.loginCount = 20
	if w := call(s, "POST", "/api/login", map[string]string{}, nil, ""); w.Code != 429 {
		t.Fatal("no rate limit")
	}
	r := httptest.NewRequest("GET", "http://localhost/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("plaintext accepted")
	}
	data, err := os.ReadFile(filepath.Join(s.store.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-password-123") || strings.Contains(string(data), csrf) {
		t.Fatal("audit leaked credentials")
	}
}
func TestManagedExclusiveLockAndCorruptState(t *testing.T) {
	s := setup(t)
	if other, _, err := Open(s.store.dir, "missing"); err == nil {
		other.Close()
		t.Fatal("two owners accepted")
	}
	dir := s.store.dir
	s.store.Close()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if other, _, err := Open(dir, filepath.Join(dir, "bootstrap.yaml")); err == nil {
		other.Close()
		t.Fatal("corrupt state silently reset")
	}
}
func TestRuleCapabilitiesAndGRPCValidation(t *testing.T) {
	s := setup(t)
	for _, protocol := range []string{"http2", "grpc"} {
		raw := strings.Replace(fixture, "protocol: http1", "protocol: "+protocol, 1)
		raw = strings.Replace(raw, "action: respond, phase: before_upstream_request, status: 503", "action: delay, phase: before_upstream_request, duration: 10ms", 1)
		doc, err := config.Parse([]byte(raw), "fixture.yaml")
		if err != nil {
			t.Fatal(err)
		}
		s.service, err = control.NewManaged(doc, 1, func(*config.Document, control.Result) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		draft := draftFor(s, 1)
		if _, err := s.document(draft); err != nil {
			t.Fatalf("%s %v", protocol, err)
		}
		draft.Proxies[0].Rules[0].Fault = config.Fault{Action: "close_connection", Phase: config.BeforeUpstreamRequest}
		if _, err := s.document(draft); err == nil {
			t.Fatalf("%s close accepted", protocol)
		}
	}
}

func TestCrashCommitBoundary(t *testing.T) {
	if dir := os.Getenv("FAULTLINE_COMMIT_CHILD"); dir != "" {
		store, doc, err := Open(dir, "unused")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		service, err := control.NewManaged(doc, store.state.Revision, func(next *config.Document, result control.Result) error {
			if os.Getenv("FAULTLINE_COMMIT_POINT") == "before" {
				os.Exit(23)
			}
			if err := store.apply("tester", next, result); err != nil {
				return err
			}
			os.Exit(23)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		c := doc.Config()
		p := 1.0
		c.Proxies[0].Rules[0].Select.Probability = &p
		encoded, _ := config.Encode(c)
		next, err := config.Parse(encoded, "child.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ApplyRevision(next, store.state.Revision); err != nil {
			t.Fatal(err)
		}
		t.Fatal("child did not crash")
	}
	for _, point := range []string{"before", "after"} {
		t.Run(point, func(t *testing.T) {
			s := setup(t)
			dir := s.store.dir
			s.store.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestCrashCommitBoundary$")
			cmd.Env = append(os.Environ(), "FAULTLINE_COMMIT_CHILD="+dir, "FAULTLINE_COMMIT_POINT="+point)
			out, err := cmd.CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 {
				t.Fatalf("child: %v %s", err, out)
			}
			store, doc, err := Open(dir, "unused")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			wantRevision, wantProbability := uint64(1), 0.0
			if point == "after" {
				wantRevision, wantProbability = 2, 1
			}
			if store.state.Revision != wantRevision || *doc.Config().Proxies[0].Rules[0].Select.Probability != wantProbability {
				t.Fatalf("%s inconsistent recovery", point)
			}
			if point == "after" && (store.state.LastApply.Info.Revision != 2 || store.state.Audit[len(store.state.Audit)-1].Operation != "apply") {
				t.Fatal("commit lost apply result or audit")
			}
		})
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	s := setup(t)
	cookie, _ := login(t, s)
	if err := SetUser(s.store.dir, "tester", "editor", "replacement-password", false); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/api/session", nil, cookie, ""); w.Code != 401 {
		t.Fatal("password reset did not revoke session")
	}
	if w := call(s, "POST", "/api/login", map[string]string{"name": "tester", "password": "test-password-123"}, nil, ""); w.Code != 401 {
		t.Fatal("old password accepted")
	}
	if w := call(s, "POST", "/api/login", map[string]string{"name": "tester", "password": "replacement-password"}, nil, ""); w.Code != 200 {
		t.Fatal("new password rejected")
	}
}

func TestPathPatternAPIRejectsInvalidDraft(t *testing.T) {
	s := setup(t)
	cookie, csrf := login(t, s)
	for _, pattern := range []string{"/payment/*", "/:id/:id", "payment/:id"} {
		d := draftFor(s, 1)
		d.Proxies[0].Rules[0].Match.PathPattern = pattern
		for _, op := range []string{"validate", "apply"} {
			w := call(s, "POST", "/api/"+op, d, cookie, csrf)
			if w.Code != 422 || !strings.Contains(w.Body.String(), "match.path_pattern") {
				t.Fatalf("%s: %d %s", op, w.Code, w.Body)
			}
		}
	}
	if s.service.Acquire().Info().Revision != 1 {
		t.Fatal("invalid pattern changed revision")
	}
}
