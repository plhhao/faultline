package integration_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
	"github.com/plhhao/faultline/internal/recorder"
)

func BenchmarkHTTP(b *testing.B) {
	for _, mode := range []string{"direct", "pass-through", "delay", "hold-response"} {
		b.Run(mode, func(b *testing.B) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
			defer upstream.Close()
			url := upstream.URL
			var records *recorder.Recorder
			if mode != "direct" {
				rule := ""
				if mode == "delay" {
					rule = faultRule("delay", config.BeforeUpstreamRequest, ", duration: 5ms")
				} else if mode == "hold-response" {
					rule = faultRule("hold_response", config.AfterUpstreamHeaders, ", max_duration: 5ms")
				}
				doc, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {max_inflight_requests: 1000, request_timeout: 5s}\nproxies:\n  - id: test\n    protocol: http1\n    listen: %s\n    upstream: %s\n%s", address(b), upstream.URL, rule)), "/tmp/benchmark.yaml")
				if err != nil {
					b.Fatal(err)
				}
				service, err := control.New(doc)
				if err != nil {
					b.Fatal(err)
				}
				service.SetEnabled(mode != "pass-through")
				records = recorder.New(io.Discard, 1024)
				defer records.Close(context.Background())
				proxy, err := httpproxy.Start(service, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
				if err != nil {
					b.Fatal(err)
				}
				defer proxy.Close()
				url = "http://" + proxy.Listeners()[0].Address
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.MaxIdleConnsPerHost = 16
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			var failures atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					// POST prevents client-side automatic retry after a held connection closes.
					resp, err := client.Post(url, "text/plain", nil)
					if resp != nil {
						_, copyErr := io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if resp.StatusCode != 200 || copyErr != nil {
							failures.Add(1)
						}
					}
					if (err != nil) != (mode == "hold-response") {
						failures.Add(1)
					}
				}
			})
			b.StopTimer()
			if failures.Load() != 0 {
				b.Fatalf("unexpected results: %d", failures.Load())
			}
			if records != nil {
				if mode != "pass-through" && records.Counters().Applied != uint64(b.N) {
					b.Fatalf("expected %d applied faults: %+v", b.N, records.Counters())
				}
				b.ReportMetric(float64(records.Counters().Dropped), "dropped-events")
			}
		})
	}
}
