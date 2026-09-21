package remote

import (
	"strings"
	"testing"

	"faultline/internal/config"
)

func TestMySQLManagedAPI(t *testing.T) {
	source := strings.NewReplacer("protocol: http1", "protocol: mysql", "http://127.0.0.1:19090", "mysql://127.0.0.1:3306", "action: respond, phase: before_upstream_request, status: 503", "action: delay, phase: after_commit, duration: 100ms").Replace(fixture)
	s := setupConfig(t, source)
	cookie, csrf := login(t, s)
	w := call(s, "GET", "/api/capabilities", nil, cookie, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mysql":["delay","hold_response","close_connection"]`) || !strings.Contains(w.Body.String(), `"mysql":["after_commit"]`) {
		t.Fatalf("capabilities: %d %s", w.Code, w.Body)
	}
	draft := draftFor(s, 1)
	draft.Proxies[0].Rules[0].Fault = config.Fault{Action: "close_connection", Phase: config.AfterCommit}
	for _, op := range []string{"validate", "apply"} {
		w := call(s, "POST", "/api/"+op, draft, cookie, csrf)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", op, w.Code, w.Body)
		}
	}
	if s.service.Acquire().Info().Revision != 2 {
		t.Fatal("not applied")
	}
	bad := draftFor(s, 1)
	bad.Proxies[0].Rules[0].Match.Method = "GET"
	if w := call(s, "POST", "/api/validate", bad, cookie, csrf); w.Code == 200 {
		t.Fatal("HTTP matcher accepted")
	}
	if w := call(s, "POST", "/api/enable", map[string]any{}, cookie, csrf); w.Code != 200 || !s.service.Acquire().Info().Enabled {
		t.Fatal("enable failed")
	}
}
