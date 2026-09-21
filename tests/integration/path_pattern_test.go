package integration_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
)

func TestPathPatternForwardingAndReload(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			seen := make(chan string, 1)
			upstream := extensionUpstream(t, protocol, 0, testIdentity{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.URL.RequestURI(); w.Write([]byte("ok")) }))
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			rules := `    rules:
    - id: pattern
      match: {path_pattern: /payment/:id}
      select: {probability: 1}
      fault: {action: delay, phase: before_upstream_request, duration: 1ms}
`
			service, _ := start(t, extensionDoc(t, protocol, addr, upstream.URL, rules), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			client := extensionClient(t, protocol, false, testIdentity{})
			for _, tc := range []struct {
				uri      string
				selected bool
			}{
				{"/payment/123?x=1&x=2&empty=&encoded=%2F+%20", true},
				{"/payment/history", true}, {"/payment/%61bc", true},
				{"/payment/a%2Fb?x=%26", false}, {"/payment/%252F", true},
				{"/payment/123/", false}, {"/payment/123/items", false},
			} {
				resp, err := client.Get("http://" + addr + tc.uri)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || string(body) != "ok" {
					t.Fatalf("%s: %q %v", tc.uri, body, err)
				}
				if got := receive(t, seen); got != tc.uri {
					t.Fatalf("upstream URI %q want %q", got, tc.uri)
				}
				report := receive(t, reports)
				if report.Decision.Selected != tc.selected || report.Applied != tc.selected {
					t.Fatalf("%s: %+v", tc.uri, report)
				}
			}
			old := service.Acquire()
			next := extensionDoc(t, protocol, addr, upstream.URL, strings.Replace(rules, "/payment/:id", "/other/:id", 1))
			data, err := config.Encode(next.Config())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "reload.yaml")
			if err = os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			if result, err := service.ReloadFile(file); err != nil || !result.Changed {
				t.Fatalf("reload: %+v %v", result, err)
			}
			if result, err := service.ReloadFile(file); err != nil || result.Changed {
				t.Fatalf("no-op: %+v %v", result, err)
			}
			if old.Config().Proxies[0].Rules[0].Match.PathPattern != "/payment/:id" {
				t.Fatal("old snapshot changed")
			}
			for _, uri := range []string{"/payment/1", "/other/1"} {
				resp, err := client.Get("http://" + addr + uri)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				receive(t, seen)
				r := receive(t, reports)
				if r.Info.Revision != 2 || r.Decision.Selected != (uri == "/other/1") {
					t.Fatal(r)
				}
			}
		})
	}
}
