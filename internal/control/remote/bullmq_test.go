package remote

import (
	"strings"
	"testing"

	"github.com/plhhao/faultline/internal/config"
)

func TestBullMQManagedAPI(t *testing.T) {
	source := strings.NewReplacer("protocol: http1", "protocol: bullmq", "http://127.0.0.1:19090", "redis://127.0.0.1:6379", "action: respond, phase: before_upstream_request, status: 503", "action: delay, phase: after_job_add, duration: 100ms").Replace(fixture)
	s := setupConfig(t, source)
	cookie, csrf := login(t, s)
	w := call(s, "GET", "/api/capabilities", nil, cookie, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bullmq":["delay","hold_response","close_connection"]`) || !strings.Contains(w.Body.String(), `"bullmq":["after_job_add"]`) {
		t.Fatalf("capabilities: %d %s", w.Code, w.Body)
	}
	draft := draftFor(s, 1)
	draft.Proxies[0].Rules[0].Match.Queue = "orders"
	draft.Proxies[0].Rules[0].Fault = config.Fault{Action: "close_connection", Phase: config.AfterJobAdd}
	for _, op := range []string{"validate", "apply"} {
		w := call(s, "POST", "/api/"+op, draft, cookie, csrf)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", op, w.Code, w.Body)
		}
	}
	bad := draftFor(s, 1)
	bad.Proxies[0].Rules[0].Match.RoutingKey = "payment.created"
	if w := call(s, "POST", "/api/validate", bad, cookie, csrf); w.Code == 200 {
		t.Fatal("RabbitMQ matcher accepted")
	}
}
