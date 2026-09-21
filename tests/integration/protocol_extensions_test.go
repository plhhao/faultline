package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/fault"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
	"github.com/plhhao/faultline/internal/recorder"
	"golang.org/x/net/http2"
)

type testIdentity struct {
	cert              tls.Certificate
	certFile, keyFile string
	roots             *x509.CertPool
}

func identity(t *testing.T, expired bool) testIdentity {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	if expired {
		until = time.Now().Add(-time.Minute)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-time.Hour), NotAfter: until,
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost", "host.docker.internal"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
	dir := t.TempDir()
	c, k := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(c, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return testIdentity{cert, c, k, roots}
}

func wireProtocols(protocol string, secure bool) *http.Protocols {
	p := new(http.Protocols)
	if protocol == "http1" {
		p.SetHTTP1(true)
	} else if secure {
		p.SetHTTP2(true)
	} else {
		p.SetUnencryptedHTTP2(true)
	}
	return p
}

func extensionDoc(t *testing.T, protocol, addr, upstream, extra string) *config.Document {
	t.Helper()
	d, err := config.Parse([]byte(fmt.Sprintf("api_version: faultline/v1alpha1\nproxies:\n  - id: test\n    protocol: %s\n    listen: %s\n    upstream: %s\n%s", protocol, addr, upstream, extra)), t.TempDir()+"/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func extensionClient(t *testing.T, protocol string, secure bool, id testIdentity) *http.Client {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.Protocols = wireProtocols(protocol, secure)
	tr.TLSClientConfig = &tls.Config{RootCAs: id.roots, Certificates: []tls.Certificate{id.cert}}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

func extensionUpstream(t *testing.T, protocol string, mode int, id testIdentity, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.Config.Protocols = wireProtocols(protocol, mode > 0)
	if mode > 0 {
		s.EnableHTTP2 = protocol != "http1"
		s.TLS = &tls.Config{Certificates: []tls.Certificate{id.cert}}
		if mode == 2 {
			s.TLS.ClientCAs = id.roots
			s.TLS.ClientAuth = tls.RequireAndVerifyClientCert
		}
		s.StartTLS()
	} else {
		s.Start()
	}
	t.Cleanup(s.Close)
	return s
}

func tlsFields(in, out int, id testIdentity) string {
	extra := ""
	if in > 0 {
		extra = fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'", id.certFile, id.keyFile)
		if in == 2 {
			extra += fmt.Sprintf(", client_ca_file: '%s'", id.certFile)
		}
		extra += "}\n"
	}
	if out > 0 {
		extra += fmt.Sprintf("    upstream_tls: {ca_file: '%s'", id.certFile)
		if out == 2 {
			extra += fmt.Sprintf(", cert_file: '%s', key_file: '%s'", id.certFile, id.keyFile)
		}
		extra += "}\n"
	}
	return extra
}

func TestExtensionTLSMatrix(t *testing.T) {
	id := identity(t, false)
	for _, protocol := range []string{"http1", "http2"} {
		for in := 0; in <= 2; in++ {
			for out := 0; out <= 2; out++ {
				t.Run(fmt.Sprintf("%s/in%d/out%d", protocol, in, out), func(t *testing.T) {
					seen := make(chan int, 1)
					upstream := extensionUpstream(t, protocol, out, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.ProtoMajor; io.Copy(w, r.Body) }))
					addr := address(t)
					start(t, extensionDoc(t, protocol, addr, upstream.URL, tlsFields(in, out, id)), false, httpproxy.Options{})
					scheme := "http"
					if in > 0 {
						scheme = "https"
					}
					resp, err := extensionClient(t, protocol, in > 0, id).Post(scheme+"://"+addr, "application/octet-stream", strings.NewReader("hello"))
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					major := 1
					if protocol == "http2" {
						major = 2
					}
					if err != nil || string(body) != "hello" || resp.ProtoMajor != major || receive(t, seen) != major {
						t.Fatalf("%v %+v %q", err, resp, body)
					}
				})
			}
		}
	}
}

func bodyRule(action, direction string, n int) string {
	phase := "before_upstream_request"
	if direction == "response" {
		phase = "after_upstream_headers"
	}
	param := "bytes"
	if action == "throttle" {
		param = "bytes_per_second"
	}
	return fmt.Sprintf("    rules:\n    - id: body\n      match: {path: /chosen}\n      select: {probability: 1}\n      fault: {action: %s, direction: %s, phase: %s, %s: %d}\n", action, direction, phase, param, n)
}

func TestBodyFaultMatrix(t *testing.T) {
	id := identity(t, false)
	for _, protocol := range []string{"http1", "http2"} {
		for _, direction := range []string{"request", "response"} {
			for _, action := range []string{"truncate", "throttle"} {
				for mode := 0; mode <= 2; mode++ {
					t.Run(fmt.Sprintf("%s/%s/%s/mode%d", protocol, direction, action, mode), func(t *testing.T) {
						const size = 4096
						payload := bytes.Repeat([]byte("x"), size)
						type uploadResult struct {
							n   int
							err error
						}
						seen := make(chan uploadResult, 2)
						upstream := extensionUpstream(t, protocol, mode, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							b, err := io.ReadAll(r.Body)
							seen <- uploadResult{len(b), err}
							if err != nil {
								return
							}
							w.Header().Set("Content-Length", fmt.Sprint(size))
							w.Write(payload)
						}))
						addr := address(t)
						reports := make(chan httpproxy.Report, 4)
						limit := 17
						if action == "throttle" {
							limit = 16384
						}
						start(t, extensionDoc(t, protocol, addr, upstream.URL, tlsFields(mode, mode, id)+bodyRule(action, direction, limit)), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
						scheme := "http"
						if mode > 0 {
							scheme = "https"
						}
						c := extensionClient(t, protocol, mode > 0, id)
						before := time.Now()
						resp, err := c.Post(scheme+"://"+addr+"/chosen", "application/octet-stream", bytes.NewReader(payload))
						var b []byte
						if err == nil {
							b, err = io.ReadAll(resp.Body)
							resp.Body.Close()
						}
						elapsed := time.Since(before)
						got := receive(t, seen)
						report := receive(t, reports)
						if !report.Applied || !report.Decision.Selected {
							t.Fatalf("%+v", report)
						}
						if action == "truncate" {
							if direction == "request" && (got.n > limit || got.err == nil) {
								t.Fatalf("upload %+v", got)
							}
							if direction == "response" && (len(b) != limit || err == nil) {
								t.Fatalf("response bytes=%d err=%v", len(b), err)
							}
						} else {
							if err != nil || len(b) != size || got.n != size || elapsed < 230*time.Millisecond || elapsed > 1250*time.Millisecond {
								t.Fatalf("body=%d upload=%+v err=%v time=%s", len(b), got, err, elapsed)
							}
						}
						var reused bool
						req, _ := http.NewRequest("POST", scheme+"://"+addr+"/other", bytes.NewReader(payload))
						req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
						resp, err = c.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if protocol == "http2" && !reused {
							t.Fatal("fault closed shared HTTP/2 connection")
						}
					})
				}
			}
		}
	}
}

func TestHTTP2StreamIsolationAndSnapshots(t *testing.T) {
	id := identity(t, false)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := extensionUpstream(t, "http2", 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/chosen" {
			entered <- struct{}{}
			<-release
		}
		w.Write([]byte("payload"))
	}))
	addr := address(t)
	reports := make(chan httpproxy.Report, 4)
	doc := extensionDoc(t, "http2", addr, upstream.URL, bodyRule("truncate", "response", 2))
	service, _ := start(t, doc, true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
	c := extensionClient(t, "http2", false, id)
	result := make(chan error, 1)
	go func() {
		resp, err := c.Get("http://" + addr + "/chosen")
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		result <- err
	}()
	receive(t, entered)
	if _, err := service.Apply(extensionDoc(t, "http2", addr, upstream.URL, bodyRule("truncate", "response", 100))); err != nil {
		t.Fatal(err)
	}
	service.SetEnabled(false)
	var reused bool
	req, _ := http.NewRequest("GET", "http://"+addr+"/other", nil)
	req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	second := receive(t, reports)
	if !reused || second.Decision.Selected || second.Info.Revision != 2 {
		t.Fatalf("reused=%t %+v", reused, second)
	}
	close(release)
	if receive(t, result) == nil {
		t.Fatal("old snapshot did not truncate")
	}
	first := receive(t, reports)
	if first.Info.Revision != 1 || !first.Applied || calls.Load() != 2 {
		t.Fatalf("calls=%d %+v", calls.Load(), first)
	}
}

func TestMTLSAuthenticationFailures(t *testing.T) {
	trusted, wrong, expired := identity(t, false), identity(t, false), identity(t, true)
	for _, protocol := range []string{"http1", "http2"} {
		for _, leg := range []string{"listener", "upstream"} {
			for _, kind := range []string{"missing", "wrong", "expired"} {
				t.Run(protocol+"/"+leg+"/"+kind, func(t *testing.T) {
					presented := trusted
					clientTrust := trusted
					if kind == "wrong" {
						presented = wrong
					}
					if kind == "expired" {
						presented = expired
						clientTrust = expired
					}
					var calls atomic.Int32
					upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
					upstream.Config.Protocols = wireProtocols(protocol, true)
					upstream.EnableHTTP2 = protocol == "http2"
					upstream.TLS = &tls.Config{Certificates: []tls.Certificate{trusted.cert}}
					if leg == "upstream" {
						upstream.TLS.ClientAuth = tls.RequireAndVerifyClientCert
						upstream.TLS.ClientCAs = clientTrust.roots
					}
					upstream.StartTLS()
					defer upstream.Close()
					extra := tlsFields(1, 1, trusted) + rule
					if leg == "listener" {
						extra = strings.Replace(extra, "key_file: '"+trusted.keyFile+"'", "key_file: '"+trusted.keyFile+"', client_ca_file: '"+clientTrust.certFile+"'", 1)
					} else if kind != "missing" {
						extra = strings.Replace(extra, "ca_file: '"+trusted.certFile+"'", "ca_file: '"+trusted.certFile+"', cert_file: '"+presented.certFile+"', key_file: '"+presented.keyFile+"'", 1)
					}
					addr := address(t)
					reports := make(chan httpproxy.Report, 1)
					_, proxy := start(t, extensionDoc(t, protocol, addr, upstream.URL, extra), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
					c := extensionClient(t, protocol, true, trusted)
					if leg == "listener" {
						tc := c.Transport.(*http.Transport).TLSClientConfig
						tc.Certificates = nil
						if kind != "missing" {
							tc.Certificates = []tls.Certificate{presented.cert}
						}
					}
					resp, err := c.Get("https://" + addr)
					if resp != nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
					if leg == "listener" {
						if err == nil {
							t.Fatal("invalid client was authenticated")
						}
						proxy.Close()
						select {
						case report := <-reports:
							t.Fatalf("handshake created flow: %+v", report)
						default:
						}
					} else {
						if err != nil || resp.StatusCode != 502 {
							t.Fatalf("%v %+v", err, resp)
						}
						report := receive(t, reports)
						if report.Applied || !report.NotReached {
							t.Fatalf("authentication failure treated as fault: %+v", report)
						}
					}
					if calls.Load() != 0 {
						t.Fatal("unauthenticated request reached application")
					}
				})
			}
		}
	}
}

func TestTruncateHTTP1FramingBoundaries(t *testing.T) {
	id := identity(t, false)
	for _, direction := range []string{"request", "response"} {
		for _, chunked := range []bool{false, true} {
			for _, limit := range []int{0, 2, 4, 9} {
				t.Run(fmt.Sprintf("%s/chunked%t/N%d", direction, chunked, limit), func(t *testing.T) {
					type result struct {
						body string
						err  error
					}
					seen := make(chan result, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						b, err := io.ReadAll(r.Body)
						seen <- result{string(b), err}
						if err != nil {
							return
						}
						if !chunked {
							w.Header().Set("Content-Length", "4")
						} else {
							w.WriteHeader(200)
							w.(http.Flusher).Flush()
						}
						w.Write([]byte("abcd"))
					}))
					defer upstream.Close()
					addr := address(t)
					reports := make(chan httpproxy.Report, 1)
					start(t, extensionDoc(t, "http1", addr, upstream.URL, bodyRule("truncate", direction, limit)), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
					req, _ := http.NewRequest("POST", "http://"+addr+"/chosen", strings.NewReader("abcd"))
					if chunked {
						req.ContentLength = -1
					}
					resp, err := extensionClient(t, "http1", false, id).Do(req)
					var body []byte
					if resp != nil {
						body, err = io.ReadAll(resp.Body)
						resp.Body.Close()
					}
					got := receive(t, seen)
					report := receive(t, reports)
					cut := limit < 4
					if report.Applied != cut {
						t.Fatalf("%+v", report)
					}
					if direction == "request" {
						if len(got.body) > min(limit, 4) || (got.err != nil) != cut {
							t.Fatalf("%+v", got)
						}
					} else if len(body) != min(limit, 4) || (err != nil) != cut {
						t.Fatalf("%q %v", body, err)
					}
				})
			}
		}
	}
}

func TestHTTP2CancellationAndHold(t *testing.T) {
	id := identity(t, false)
	for _, action := range []string{"delay", "hold_request", "hold_response", "truncate", "throttle"} {
		for _, ending := range []string{"client", "timeout", "shutdown"} {
			t.Run(action+"/"+ending, func(t *testing.T) {
				started := make(chan struct{}, 1)
				upstream := extensionUpstream(t, "http2", 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/other" {
						w.Write([]byte("ok"))
						return
					}
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					started <- struct{}{}
					if action == "throttle" {
						w.Write(bytes.Repeat([]byte("x"), 1<<20))
					} else {
						<-r.Context().Done()
					}
				}))
				var rules string
				switch action {
				case "delay", "hold_request":
					param := "duration"
					if action == "hold_request" {
						param = "max_duration"
					}
					rules = fmt.Sprintf("    rules:\n    - id: chosen\n      match: {path: /chosen}\n      select: {probability: 1}\n      fault: {action: %s, phase: before_upstream_request, %s: 5s}\n", action, param)
				case "hold_response":
					rules = "    rules:\n    - id: chosen\n      match: {path: /chosen}\n      select: {probability: 1}\n      fault: {action: hold_response, phase: after_upstream_headers, max_duration: 5s}\n"
				default:
					rules = bodyRule(action, "response", 1)
				}
				addr := address(t)
				reports := make(chan httpproxy.Report, 4)
				doc := extensionDoc(t, "http2", addr, upstream.URL, rules)
				if ending == "timeout" {
					raw := fmt.Sprintf("api_version: faultline/v1alpha1\nruntime: {request_timeout: 100ms}\nproxies:\n- id: test\n  protocol: http2\n  listen: %s\n  upstream: %s\n%s", addr, upstream.URL, strings.ReplaceAll(rules, "    ", "  "))
					var err error
					doc, err = config.Parse([]byte(raw), "test.yaml")
					if err != nil {
						t.Fatal(err)
					}
				}
				_, proxy := start(t, doc, true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
				c := extensionClient(t, "http2", false, id)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/chosen", nil)
				done := make(chan error, 1)
				go func() {
					resp, err := c.Do(req)
					if err == nil {
						_, err = io.ReadAll(resp.Body)
						resp.Body.Close()
					}
					done <- err
				}()
				if action != "delay" && action != "hold_request" {
					receive(t, started)
				} else {
					time.Sleep(30 * time.Millisecond)
				}
				switch ending {
				case "client":
					cancel()
				case "shutdown":
					proxy.Close()
				}
				if receive(t, done) == nil {
					t.Fatal("canceled stream succeeded")
				}
				report := receive(t, reports)
				if report.Outcome != "canceled" && ending != "timeout" {
					t.Fatalf("%+v", report)
				}
				if ending != "shutdown" {
					if status, body := readResponse(t, c, "http://"+addr+"/other"); status != 200 || body != "ok" {
						t.Fatal(status, body)
					}
				}
			})
		}
	}
}

func TestHTTP2NoRetryOnRefusedStream(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var connections, attempts atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				preface := make([]byte, len(http2.ClientPreface))
				if _, err := io.ReadFull(conn, preface); err != nil {
					return
				}
				framer := http2.NewFramer(conn, conn)
				framer.WriteSettings()
				for {
					frame, err := framer.ReadFrame()
					if err != nil {
						return
					}
					switch f := frame.(type) {
					case *http2.SettingsFrame:
						if !f.IsAck() {
							framer.WriteSettingsAck()
						}
					case *http2.HeadersFrame:
						attempts.Add(1)
						framer.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
					}
				}
			}()
		}
	}()
	addr := address(t)
	start(t, extensionDoc(t, "http2", addr, "http://"+l.Addr().String(), ""), false, httpproxy.Options{})
	if code, _ := readResponse(t, extensionClient(t, "http2", false, identity(t, false)), "http://"+addr); code != 502 {
		t.Fatal(code)
	}
	if attempts.Load() != 1 || connections.Load() != 1 {
		t.Fatalf("retry: connections=%d attempts=%d", connections.Load(), attempts.Load())
	}
	l.Close()
	<-done
}

func TestHTTP2RejectsTLSFallback(t *testing.T) {
	id := identity(t, false)
	var calls atomic.Int32
	upstream := extensionUpstream(t, "http1", 1, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	addr := address(t)
	reports := make(chan httpproxy.Report, 1)
	start(t, extensionDoc(t, "http2", addr, upstream.URL, tlsFields(0, 1, id)+rule), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
	code, _ := readResponse(t, extensionClient(t, "http2", false, id), "http://"+addr)
	report := receive(t, reports)
	if code != 502 || report.Applied || !report.NotReached || calls.Load() != 0 {
		t.Fatalf("status=%d calls=%d %+v", code, calls.Load(), report)
	}
}

func TestHTTP2BuiltinCapabilities(t *testing.T) {
	id := identity(t, false)
	for _, spec := range []string{
		"action: delay, phase: before_upstream_request, duration: 1ms",
		"action: delay, phase: after_upstream_headers, duration: 1ms",
		"action: respond, phase: before_upstream_request, status: 503, body: injected",
		"action: hold_request, phase: before_upstream_request, max_duration: 20ms",
		"action: hold_response, phase: after_upstream_headers, max_duration: 20ms",
	} {
		t.Run(spec, func(t *testing.T) {
			upstream := extensionUpstream(t, "http2", 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
			addr := address(t)
			reports := make(chan httpproxy.Report, 2)
			rules := "    rules:\n    - id: chosen\n      match: {path: /chosen}\n      select: {probability: 1}\n      fault: {" + spec + "}\n"
			start(t, extensionDoc(t, "http2", addr, upstream.URL, rules), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			c := extensionClient(t, "http2", false, id)
			resp, err := c.Get("http://" + addr + "/chosen")
			var body []byte
			if err == nil {
				body, err = io.ReadAll(resp.Body)
				resp.Body.Close()
			}
			report := receive(t, reports)
			if !report.Applied {
				t.Fatalf("%+v", report)
			}
			if strings.Contains(spec, "hold_") {
				if err == nil {
					t.Fatal("hold should abort stream")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(spec, "respond") && (resp.StatusCode != 503 || string(body) != "injected") {
				t.Fatalf("%+v %q", resp, body)
			}
			var reused bool
			req, _ := http.NewRequest("GET", "http://"+addr+"/other", nil)
			req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}))
			resp, err = c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if !reused {
				t.Fatal("connection closed")
			}
		})
	}
}

type endlessBody struct{}

func (endlessBody) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestHTTP2BodyFaultResourceBound(t *testing.T) {
	id := identity(t, false)
	for _, direction := range []string{"request", "response"} {
		t.Run(direction, func(t *testing.T) {
			entered := make(chan struct{}, 8)
			upstream := extensionUpstream(t, "http2", 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				entered <- struct{}{}
				if direction == "request" {
					io.Copy(io.Discard, r.Body)
				} else {
					io.CopyN(w, endlessBody{}, 512<<20)
				}
			}))
			addr := address(t)
			reports := make(chan httpproxy.Report, 8)
			_, proxy := start(t, extensionDoc(t, "http2", addr, upstream.URL, bodyRule("throttle", direction, 100)), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
			c := extensionClient(t, "http2", false, id)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{}, 8)
			for range 8 {
				go func() {
					defer func() { done <- struct{}{} }()
					var body io.Reader
					if direction == "request" {
						body = io.LimitReader(endlessBody{}, 512<<20)
					}
					req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/chosen", body)
					resp, err := c.Do(req)
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}()
			}
			for range 8 {
				receive(t, entered)
			}
			time.Sleep(250 * time.Millisecond)
			runtime.ReadMemStats(&after)
			growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			started := time.Now()
			cancel()
			proxy.Close()
			for range 8 {
				receive(t, done)
				receive(t, reports)
			}
			if growth > 128<<20 {
				t.Fatalf("buffered virtual 4GiB transfer: heap growth=%d", growth)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("cleanup %s", elapsed)
			}
			t.Logf("%s: eight 512MiB virtual flows, heap growth=%d bytes, cleanup=%s", direction, growth, time.Since(started))
		})
	}
}

func TestIndependentHTTPVersions(t *testing.T) {
	id := identity(t, false)
	for _, in := range []string{"http1", "http2"} {
		for _, out := range []string{"http1", "http2"} {
			for _, mode := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/%s/mode%d", in, out, mode), func(t *testing.T) {
					seen := make(chan int, 1)
					upstream := extensionUpstream(t, out, mode, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.ProtoMajor; w.Write([]byte("ok")) }))
					addr := address(t)
					start(t, extensionDoc(t, in, addr, upstream.URL, tlsFields(mode, mode, id)+"    upstream_protocol: "+out+"\n"), false, httpproxy.Options{})
					scheme := "http"
					if mode > 0 {
						scheme = "https"
					}
					if code, body := readResponse(t, extensionClient(t, in, mode > 0, id), scheme+"://"+addr); code != 200 || body != "ok" {
						t.Fatal(code, body)
					}
					want := 1
					if out == "http2" {
						want = 2
					}
					if got := receive(t, seen); got != want {
						t.Fatalf("upstream protocol %d", got)
					}
				})
			}
		}
	}
}

func TestBodylessResponsesAreNotTruncated(t *testing.T) {
	id := identity(t, false)
	for _, protocol := range []string{"http1", "http2"} {
		upstream := extensionUpstream(t, protocol, 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		addr := address(t)
		reports := make(chan httpproxy.Report, 1)
		start(t, extensionDoc(t, protocol, addr, upstream.URL, bodyRule("truncate", "response", 0)), true, httpproxy.Options{Executor: fault.Builtin{}, Observe: func(r httpproxy.Report) { reports <- r }})
		if code, _ := readResponse(t, extensionClient(t, protocol, false, id), "http://"+addr+"/chosen"); code != 204 {
			t.Fatal(code)
		}
		if r := receive(t, reports); r.Applied || !r.Reached {
			t.Fatalf("%+v", r)
		}
	}
}

func TestHTTP2DialFailureDoesNotDrainUpload(t *testing.T) {
	addr := address(t)
	start(t, extensionDoc(t, "http1", addr, "http://"+address(t), "    upstream_protocol: http2\n"), false, httpproxy.Options{})
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addr, reader)
	done := make(chan error, 1)
	go func() {
		resp, err := client(t).Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != 502 {
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
		}
		done <- err
	}()
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("upstream dial failure waited for unfinished upload")
	}
}

func TestBodyFaultAppliedEventRetainsHeaderStatus(t *testing.T) {
	id := identity(t, false)
	for _, direction := range []string{"request", "response"} {
		t.Run(direction, func(t *testing.T) {
			upstream := extensionUpstream(t, "http2", 0, id, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(201)
				w.Write([]byte("body"))
			}))
			var output bytes.Buffer
			records := recorder.New(&output, 32)
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			start(t, extensionDoc(t, "http2", addr, upstream.URL, bodyRule("throttle", direction, 1000)), true, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records, Observe: func(r httpproxy.Report) { reports <- r }})
			resp, err := extensionClient(t, "http2", false, id).Post("http://"+addr+"/chosen", "application/octet-stream", strings.NewReader("body"))
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			receive(t, reports)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := records.Close(ctx); err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(&output)
			found := 0
			for {
				var e recorder.Event
				err := decoder.Decode(&e)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if e.Type == "fault_applied" {
					found++
					want := 0
					if direction == "response" {
						want = 201
					}
					if e.UpstreamStatus != want || !e.Applied || !e.Reached {
						t.Fatalf("%+v", e)
					}
				}
			}
			if found != 1 {
				t.Fatalf("applied events=%d", found)
			}
		})
	}
}
