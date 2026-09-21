package remote

import (
	"strings"
	"testing"

	"github.com/plhhao/faultline/internal/config"
)

func TestRabbitMQManagedAPI(t *testing.T) {
	source := strings.NewReplacer("protocol: http1", "protocol: rabbitmq", "http://127.0.0.1:19090", "amqp://127.0.0.1:5672", "action: respond, phase: before_upstream_request, status: 503", "action: delay, phase: after_publish_confirm, duration: 100ms").Replace(fixture)
	s := setupConfig(t, source)
	cookie, csrf := login(t, s)
	w := call(s, "GET", "/api/capabilities", nil, cookie, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rabbitmq":["delay","hold_response","close_connection"]`) || !strings.Contains(w.Body.String(), `"rabbitmq":["after_publish_confirm"]`) {
		t.Fatalf("capabilities: %d %s", w.Code, w.Body)
	}
	draft := draftFor(s, 1)
	draft.Proxies[0].Rules[0].Match.Exchange = "events"
	draft.Proxies[0].Rules[0].Match.RoutingKey = "payment.created"
	draft.Proxies[0].Rules[0].Fault = config.Fault{Action: "close_connection", Phase: config.AfterPublishConfirm}
	for _, op := range []string{"validate", "apply"} {
		w := call(s, "POST", "/api/"+op, draft, cookie, csrf)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", op, w.Code, w.Body)
		}
	}
	bad := draftFor(s, 1)
	bad.Proxies[0].Rules[0].Match.Method = "GET"
	if w := call(s, "POST", "/api/validate", bad, cookie, csrf); w.Code == 200 {
		t.Fatal("HTTP matcher accepted")
	}
}
