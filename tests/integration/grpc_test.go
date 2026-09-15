package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"faultline/examples/grpc/unary"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func rpcServer(t *testing.T, mode int, id testIdentity, service unary.Service) (string, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		calls.Add(1)
		return handler(ctx, req)
	})}
	scheme := "http"
	if mode > 0 {
		scheme = "https"
		tc := &tls.Config{Certificates: []tls.Certificate{id.cert}}
		if mode == 2 {
			tc.ClientCAs = id.roots
			tc.ClientAuth = tls.RequireAndVerifyClientCert
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tc)))
	}
	s := grpc.NewServer(opts...)
	unary.Register(s, service)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(l)
	t.Cleanup(s.Stop)
	return scheme + "://" + l.Addr().String(), calls
}

func rpcClient(t *testing.T, address string, mode int, id testIdentity) *grpc.ClientConn {
	t.Helper()
	var creds credentials.TransportCredentials = insecure.NewCredentials()
	if mode > 0 {
		tc := &tls.Config{RootCAs: id.roots}
		if mode == 2 {
			tc.Certificates = []tls.Certificate{id.cert}
		}
		creds = credentials.NewTLS(tc)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestGRPCUnaryMatrix(t *testing.T) {
	id := identity(t, false)
	for in := 0; in <= 2; in++ {
		for out := 0; out <= 2; out++ {
			t.Run(fmt.Sprintf("in%d/out%d", in, out), func(t *testing.T) {
				upstream, calls := rpcServer(t, out, id, unary.Echo{})
				addr := address(t)
				reports := make(chan httpproxy.Report, 4)
				start(t, extensionDoc(t, "grpc", addr, upstream, tlsFields(in, out, id)), false, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
				conn := rpcClient(t, addr, in, id)
				for _, size := range []int{3, 1 << 20} {
					payload := bytes.Repeat([]byte("x"), size)
					ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("demo-metadata", "one", "demo-metadata", "two", "demo-bin", string([]byte{0, 1, 255}))), 5*time.Second)
					var header, trailer metadata.MD
					response := new(wrapperspb.BytesValue)
					err := conn.Invoke(ctx, unary.Method, wrapperspb.Bytes(payload), response, grpc.Header(&header), grpc.Trailer(&trailer), grpc.UseCompressor("gzip"))
					cancel()
					if err != nil || !bytes.Equal(response.Value, payload) || len(header.Get("demo-header")) != 1 || len(trailer.Get("demo-trailer")) != 1 || len(header.Get("demo-metadata")) != 2 || len(header.Get("demo-bin")) != 1 || header.Get("demo-bin")[0] != string([]byte{0, 1, 255}) {
						t.Fatalf("err=%v bytes=%d header=%v trailer=%v", err, len(response.Value), header, trailer)
					}
					report := receive(t, reports)
					if report.GRPCStatus != "0" || report.Applied || report.Protocol != "grpc" {
						t.Fatalf("%+v", report)
					}
				}
				ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("demo-error", "true")), 5*time.Second)
				defer cancel()
				err := conn.Invoke(ctx, unary.Method, wrapperspb.Bytes([]byte("x")), new(wrapperspb.BytesValue))
				report := receive(t, reports)
				if status.Code(err) != codes.FailedPrecondition || report.GRPCStatus != "9" || report.ErrorKind != "upstream_rpc" || report.Applied || calls.Load() != 3 {
					t.Fatalf("%v calls=%d %+v", err, calls.Load(), report)
				}
			})
		}
	}
}

func TestGRPCBodyFaultMatrix(t *testing.T) {
	id := identity(t, false)
	for _, direction := range []string{"request", "response"} {
		for _, action := range []string{"truncate", "throttle"} {
			for mode := 0; mode <= 2; mode++ {
				t.Run(fmt.Sprintf("%s/%s/mode%d", action, direction, mode), func(t *testing.T) {
					upstream, calls := rpcServer(t, mode, id, unary.Echo{})
					limit := 3
					if action == "throttle" {
						limit = 16384
					}
					rules := strings.Replace(bodyRule(action, direction, limit), "match: {path: /chosen}", "match: {service: faultline.demo.Echo, method: Call, headers: {x-select: yes}}", 1)
					addr := address(t)
					reports := make(chan httpproxy.Report, 3)
					start(t, extensionDoc(t, "grpc", addr, upstream, tlsFields(mode, mode, id)+rules), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
					conn := rpcClient(t, addr, mode, id)
					ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-select", "yes")), 5*time.Second)
					defer cancel()
					response := new(wrapperspb.BytesValue)
					payload := bytes.Repeat([]byte("x"), 4096)
					started := time.Now()
					err := conn.Invoke(ctx, unary.Method, wrapperspb.Bytes(payload), response)
					elapsed := time.Since(started)
					report := receive(t, reports)
					if !report.Applied || !report.Reached {
						t.Fatalf("%+v", report)
					}
					if action == "truncate" && err == nil {
						t.Fatal("truncated RPC succeeded")
					}
					if action == "throttle" && (err != nil || !bytes.Equal(payload, response.Value) || elapsed < 230*time.Millisecond || elapsed > 1250*time.Millisecond) {
						t.Fatalf("%v bytes=%d elapsed=%s", err, len(response.Value), elapsed)
					}
					ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel2()
					if err := conn.Invoke(ctx2, unary.Method, wrapperspb.Bytes(payload), response); err != nil {
						t.Fatal(err)
					}
					next := receive(t, reports)
					if next.Decision.Selected {
						t.Fatalf("metadata matcher failed: %+v", next)
					}
					expected := int32(2)
					if direction == "request" && action == "truncate" {
						expected = 1
					}
					if calls.Load() != expected {
						t.Fatalf("proxy retried: calls=%d expected=%d", calls.Load(), expected)
					}
				})
			}
		}
	}
}

func TestGRPCSelectorsAndReload(t *testing.T) {
	id := identity(t, false)
	for _, selector := range []string{"probability: 0", "probability: 1", "nth: 2", "every: 2"} {
		t.Run(selector, func(t *testing.T) {
			upstream, _ := rpcServer(t, 0, id, unary.Echo{})
			rules := "    rules:\n    - id: first\n      match: {service: faultline.demo.Echo, method: Call}\n      select: {" + selector + "}\n      fault: {action: delay, phase: before_upstream_request, duration: 1ms}\n    - id: second\n      select: {probability: 1}\n      fault: {action: delay, phase: before_upstream_request, duration: 1ms}\n"
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			doc := extensionDoc(t, "grpc", addr, upstream, rules)
			service, _ := start(t, doc, false, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			conn := rpcClient(t, addr, 0, id)
			call := func() httpproxy.Report {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := conn.Invoke(ctx, unary.Method, wrapperspb.Bytes([]byte("x")), new(wrapperspb.BytesValue)); err != nil {
					t.Fatal(err)
				}
				return receive(t, reports)
			}
			if r := call(); r.Decision.EligibleSequence != 0 || r.Decision.RuleID != "" {
				t.Fatalf("disabled %+v", r)
			}
			service.SetEnabled(true)
			for i := 1; i <= 4; i++ {
				r := call()
				selected := selector == "probability: 1" || selector == "nth: 2" && i == 2 || selector == "every: 2" && i%2 == 0
				if r.Decision.RuleID != "first" || r.Decision.EligibleSequence != uint64(i) || r.Applied != selected {
					t.Fatalf("%+v", r)
				}
			}
			result, err := service.Apply(doc)
			if err != nil || result.Changed {
				t.Fatal(result, err)
			}
			counts, _ := service.Acquire().Counters("test", "first")
			if counts.Eligible != 4 {
				t.Fatal(counts)
			}
			changed := extensionDoc(t, "grpc", addr, upstream, strings.ReplaceAll(rules, "duration: 1ms", "duration: 2ms"))
			if _, err := service.Apply(changed); err != nil {
				t.Fatal(err)
			}
			if r := call(); r.Decision.EligibleSequence != 1 || r.Info.Revision != 2 {
				t.Fatalf("%+v", r)
			}
		})
	}
}

type rpcFunc func(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)

func (f rpcFunc) Call(ctx context.Context, r *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	return f(ctx, r)
}

func TestGRPCConcurrentSnapshotAndDeadline(t *testing.T) {
	id := identity(t, false)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	upstream, calls := rpcServer(t, 0, id, rpcFunc(func(ctx context.Context, r *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
		if string(r.Value) == "wait" {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return unary.Echo{}.Call(ctx, r)
	}))
	rules := strings.Replace(bodyRule("truncate", "response", 3), "match: {path: /chosen}", "match: {headers: {x-select: yes}}", 1)
	addr := address(t)
	reports := make(chan httpproxy.Report, 4)
	svc, proxy := start(t, extensionDoc(t, "grpc", addr, upstream, rules), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
	conn := rpcClient(t, addr, 0, id)
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-select", "yes")), 3*time.Second)
		defer cancel()
		result <- conn.Invoke(ctx, unary.Method, wrapperspb.Bytes([]byte("wait")), new(wrapperspb.BytesValue))
	}()
	receive(t, entered)
	next := extensionDoc(t, "grpc", addr, upstream, strings.Replace(rules, "bytes: 3", "bytes: 999", 1))
	if _, err := svc.Apply(next); err != nil {
		t.Fatal(err)
	}
	svc.SetEnabled(false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Invoke(ctx, unary.Method, wrapperspb.Bytes([]byte("ok")), new(wrapperspb.BytesValue)); err != nil {
		t.Fatal(err)
	}
	second := receive(t, reports)
	if second.Info.Revision != 2 || second.Decision.Selected {
		t.Fatalf("%+v", second)
	}
	close(release)
	if receive(t, result) == nil {
		t.Fatal("old snapshot not retained")
	}
	first := receive(t, reports)
	if first.Info.Revision != 1 || !first.Applied || calls.Load() != 2 {
		t.Fatalf("%+v calls=%d", first, calls.Load())
	}
	// An expired RPC is still one flow and must release its slot before shutdown.
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel2()
	if err := conn.Invoke(ctx2, unary.Method, wrapperspb.Bytes([]byte("x")), new(wrapperspb.BytesValue)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatal(err)
	}
	proxy.Close()
}

func TestGRPCActiveCancellation(t *testing.T) {
	id := identity(t, false)
	for _, ending := range []string{"deadline", "client", "shutdown"} {
		t.Run(ending, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			exited := make(chan struct{}, 1)
			upstream, _ := rpcServer(t, 0, id, rpcFunc(func(ctx context.Context, r *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
				entered <- struct{}{}
				<-ctx.Done()
				exited <- struct{}{}
				return nil, ctx.Err()
			}))
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			_, proxy := start(t, extensionDoc(t, "grpc", addr, upstream, rule), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			conn := rpcClient(t, addr, 0, id)
			duration := 3 * time.Second
			if ending == "deadline" {
				duration = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- conn.Invoke(ctx, unary.Method, wrapperspb.Bytes([]byte("x")), new(wrapperspb.BytesValue))
			}()
			receive(t, entered)
			if ending == "client" {
				cancel()
			}
			if ending == "shutdown" {
				proxy.Close()
			}
			if err := receive(t, done); err == nil {
				t.Fatal("canceled RPC succeeded")
			}
			receive(t, exited)
			r := receive(t, reports)
			if r.Applied || !r.NotReached || r.Outcome != "canceled" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
