package httpproxy

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/fault"
	grpcproxy "github.com/plhhao/faultline/internal/proxy/grpc"
	"github.com/plhhao/faultline/internal/recorder"
)

type Report struct {
	Protocol       string
	GRPCStatus     string
	FlowID         string
	Info           control.Info
	Decision       engine.Decision
	Reached        bool
	Applied        bool
	NotReached     bool
	Outcome        string
	Err            error
	StartedAt      time.Time
	FinishedAt     time.Time
	UpstreamStatus int
	ErrorKind      string
}

type handler struct {
	service   *control.Service
	proxyID   string
	protocol  string
	target    *url.URL
	transport *http.Transport
	runtime   config.Runtime
	slots     chan struct{}
	options   Options
	shutdown  context.Context
}

type flow struct {
	writer         http.ResponseWriter
	conn           net.Conn
	method         string
	http2          bool
	cancelUpstream context.CancelFunc
	body           io.ReadCloser
	terminal       bool
	disconnected   bool
	onApplied      func()
}

func (f *flow) FaultApplied() { f.onApplied() }

func (f *flow) CancelUpstream() {
	f.cancelUpstream()
	if f.body != nil {
		f.body.Close()
	}
}

func (f *flow) Respond(status int, body string) error {
	if f.terminal {
		return errors.New("flow already completed")
	}
	f.terminal = true
	if status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusResetContent {
		if status == http.StatusResetContent {
			f.writer.Header().Set("Content-Length", "0")
		}
		f.writer.WriteHeader(status)
		return nil
	}
	// A known length keeps early responses complete even when an unfinished upload is closed.
	f.writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if f.method == http.MethodHead {
		if body != "" {
			f.writer.Header().Set("Content-Type", http.DetectContentType([]byte(body)))
		}
		f.writer.WriteHeader(status)
		return nil
	}
	f.writer.WriteHeader(status)
	_, err := io.WriteString(f.writer, body)
	return err
}

func (f *flow) EndStream() error {
	if !f.http2 {
		return f.CloseConnection()
	}
	f.terminal, f.disconnected = true, true
	return nil
}

func (f *flow) CloseConnection() error {
	if f.http2 {
		return errors.New("connection closure is unavailable for HTTP/2")
	}
	if f.terminal {
		return errors.New("flow already completed")
	}
	f.terminal = true
	f.disconnected = true
	return f.conn.Close()
}

var errTerminal = errors.New("flow completed by executor")

type uploadBody struct {
	io.ReadCloser
	complete atomic.Bool
}

func (b *uploadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.complete.Store(true)
	}
	return n, err
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snapshot := h.service.Acquire()
	report := Report{Protocol: h.protocol, FlowID: rand.Text(), Info: snapshot.Info(), StartedAt: time.Now().UTC(), Outcome: "completed"}
	record := func(kind string) {
		if h.options.Recorder != nil {
			h.options.Recorder.Record(event(kind, report, h.proxyID))
		}
	}
	record("flow_started")
	defer func() {
		report.FinishedAt = time.Now().UTC()
		report.NotReached = report.Decision.Selected && !report.Reached
		record("flow_finished")
		if h.options.Observe != nil {
			h.options.Observe(report)
		}
	}()
	if (h.protocol == "http1" && (r.ProtoMajor != 1 || r.ProtoMinor != 1)) || (h.protocol != "http1" && r.ProtoMajor != 2) || r.Method == http.MethodConnect || r.Header.Get("Upgrade") != "" || hasUpgrade(r.Header) || r.URL.IsAbs() {
		report.Outcome, report.ErrorKind = "unsupported_request", "unsupported_request"
		if r.ProtoMajor == 1 {
			w.Header().Set("Connection", "close")
		}
		http.Error(w, "unsupported reverse proxy request", http.StatusNotImplemented)
		return
	}
	metadata := engine.Metadata{Method: r.Method, Path: r.URL.Path, Headers: r.Header}
	if h.protocol == "grpc" {
		var valid bool
		metadata, valid = grpcproxy.Metadata(r)
		if !valid {
			report.Outcome, report.ErrorKind = "unsupported_request", "unsupported_request"
			grpcproxy.Error(w, "12", "invalid gRPC request")
			return
		}
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		report.Outcome, report.ErrorKind = "proxy_overload", "proxy_overload"
		if h.protocol == "grpc" {
			grpcproxy.Error(w, "8", "proxy inflight limit reached")
			return
		}
		if r.ProtoMajor == 1 {
			w.Header().Set("Connection", "close")
		}
		http.Error(w, "proxy inflight limit reached", http.StatusServiceUnavailable)
		return
	}
	timeout := h.runtime.RequestTimeout
	if h.protocol == "grpc" && r.Header.Get("Grpc-Timeout") != "" {
		rpcTimeout, valid := grpcproxy.Timeout(r.Header.Get("Grpc-Timeout"))
		if !valid {
			report.Outcome, report.ErrorKind = "unsupported_request", "unsupported_request"
			grpcproxy.Error(w, "3", "invalid grpc-timeout")
			return
		}
		timeout = min(timeout, rpcTimeout)
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	conn := r.Context().Value(connectionKey{}).(net.Conn)
	deadline, _ := ctx.Deadline()
	controller := http.NewResponseController(w)
	setDeadline := func(d time.Time) {
		if r.ProtoMajor == 2 {
			controller.SetReadDeadline(d)
			controller.SetWriteDeadline(d)
		} else {
			conn.SetDeadline(d)
		}
	}
	setDeadline(deadline)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		if r.ProtoMajor == 2 {
			setDeadline(time.Now())
		} else {
			conn.Close()
		}
		close(closed)
	})
	defer func() {
		if !stop() {
			<-closed
		}
		setDeadline(time.Time{})
	}()
	// Allow upstream to respond without first draining a slow client upload.
	http.NewResponseController(w).EnableFullDuplex()
	upload := &uploadBody{ReadCloser: r.Body}
	upload.complete.Store(r.Body == nil || r.Body == http.NoBody)
	r.Body = upload
	upstreamCtx, cancelUpstream := context.WithCancel(ctx)
	f := &flow{writer: w, conn: conn, method: r.Method, http2: r.ProtoMajor == 2, cancelUpstream: cancelUpstream}
	var applied atomic.Bool
	var appliedEvent recorder.Event
	f.onApplied = func() {
		if applied.CompareAndSwap(false, true) && h.options.Recorder != nil {
			h.options.Recorder.Record(appliedEvent)
		}
	}
	var discarded chan struct{}
	defer func() {
		// Transport may still be reading an upload after an early upstream response.
		if r.ProtoMajor == 1 && !upload.complete.Load() {
			if !f.disconnected && ctx.Err() == nil {
				http.NewResponseController(w).Flush()
			}
			conn.Close()
		}
		f.CancelUpstream()
		report.Applied = applied.Load()
		if discarded != nil {
			<-discarded
		}
		// Intentional connection closure may itself cancel the request context.
		if ctx.Err() != nil && (!f.disconnected || report.Err != nil) {
			report.Outcome, report.Err = "canceled", ctx.Err()
			switch {
			case h.shutdown.Err() != nil:
				report.ErrorKind = "shutdown"
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				report.ErrorKind = "proxy_timeout"
			default:
				report.ErrorKind = "client_canceled"
			}
		}
		if report.ErrorKind == "" && report.Err != nil {
			report.ErrorKind = errorKind(report.Err, report.Outcome)
		}
	}()
	var err error
	report.Decision, err = snapshot.Decide(h.proxyID, metadata)
	record("decision")
	if err != nil {
		report.Outcome, report.Err = "proxy_error", err
		http.Error(w, "proxy decision failed", http.StatusInternalServerError)
		return
	}
	apply := func(phase string) error {
		if !report.Decision.Selected || report.Decision.Fault.Phase != phase {
			return nil
		}
		report.Reached = true
		record("fault_reached")
		appliedEvent = event("fault_applied", report, h.proxyID)
		appliedEvent.Applied = true
		if report.Decision.Fault.Action == "truncate" || report.Decision.Fault.Action == "throttle" {
			return nil
		}
		if h.options.Executor == nil {
			return fault.ErrUnavailable
		}
		if r.ProtoMajor == 1 && report.Decision.Fault.Action == "hold_request" && !upload.complete.Load() {
			// Reading and discarding the withheld upload lets net/http detect client disconnects.
			discarded = make(chan struct{})
			go func() {
				defer close(discarded)
				io.Copy(io.Discard, upload)
			}()
		}
		var err error
		var wasApplied bool
		wasApplied, err = h.options.Executor.Execute(ctx, report.Decision.Fault.Clone(), f)
		if wasApplied {
			f.FaultApplied()
		}
		if err != nil {
			report.Outcome, report.Err = "executor_error", err
			return err
		}
		if f.terminal {
			return errTerminal
		}
		return nil
	}
	fail := func(w http.ResponseWriter, _ *http.Request, err error) {
		if errors.Is(err, errTerminal) {
			if f.http2 && f.disconnected {
				panic(http.ErrAbortHandler)
			}
			return
		}
		if ctx.Err() != nil {
			report.Err = ctx.Err()
			if f.http2 {
				panic(http.ErrAbortHandler)
			}
			f.CloseConnection()
			return
		}
		status := http.StatusBadGateway
		if errors.Is(err, fault.ErrUnavailable) {
			status = http.StatusNotImplemented
			report.Outcome = "executor_unavailable"
		} else if report.Outcome == "completed" {
			report.Outcome = "upstream_error"
		}
		report.Err = err
		if !f.terminal {
			if h.protocol == "grpc" {
				grpcproxy.Error(w, "14", "upstream unavailable")
				return
			}
			if r.ProtoMajor == 1 {
				w.Header().Set("Connection", "close")
			}
			http.Error(w, http.StatusText(status), status)
		}
	}
	if err := apply(config.BeforeUpstreamRequest); err != nil {
		fail(w, r, err)
		return
	}
	wrap := func(body io.ReadCloser) io.ReadCloser {
		if body == nil || body == http.NoBody {
			return body
		}
		return fault.WrapBody(upstreamCtx, body, report.Decision.Fault.Clone(), f.FaultApplied)
	}
	bodyAction := report.Decision.Selected && (report.Decision.Fault.Action == "truncate" || report.Decision.Fault.Action == "throttle")
	var outgoingBody io.ReadCloser
	if bodyAction && report.Decision.Fault.Direction == "request" {
		outgoingBody = wrap(r.Body)
		r.Body = outgoingBody
		defer func() {
			cancelUpstream()
			if !upload.complete.Load() {
				controller.SetReadDeadline(time.Now())
			}
			outgoingBody.Close()
			report.Applied = applied.Load()
		}()
	}
	var transport http.RoundTripper = h.transport
	if h.transport.Protocols.HTTP2() || h.transport.Protocols.UnencryptedHTTP2() {
		transport = singleAttempt{h.transport}
	}
	var upstreamResponse *http.Response
	proxy := httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(h.target)
			p.Out.URL.RawQuery = p.In.URL.RawQuery
			// The inbound body fills trailer values at EOF, after request cloning.
			p.Out.Trailer = p.In.Trailer
			p.Out.GetBody = nil
			if h.protocol == "grpc" {
				p.Out.Header.Set("Grpc-Timeout", grpcproxy.TimeoutHeader(time.Until(deadline)))
			}
		},
		Transport: transport,
		ModifyResponse: func(response *http.Response) error {
			report.UpstreamStatus = response.StatusCode
			f.body = response.Body
			if response.StatusCode == http.StatusSwitchingProtocols {
				return errors.New("upstream protocol upgrade is unsupported")
			}
			upstreamResponse = response
			if err := apply(config.AfterUpstreamHeaders); err != nil {
				return err
			}
			if bodyAction && report.Decision.Fault.Direction == "response" && r.Method != "HEAD" {
				response.Body = wrap(response.Body)
				f.body = response.Body
			}
			return nil
		},
		ErrorHandler: fail, FlushInterval: -1,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	defer func() {
		if value := recover(); value != nil {
			report.Outcome = "stream_error"
			report.Err = http.ErrAbortHandler
			panic(value)
		}
	}()
	proxy.ServeHTTP(w, r.WithContext(upstreamCtx))
	if h.protocol == "grpc" && upstreamResponse != nil {
		report.GRPCStatus = upstreamResponse.Trailer.Get("Grpc-Status")
		if report.GRPCStatus == "" {
			report.GRPCStatus = upstreamResponse.Header.Get("Grpc-Status")
		}
		if report.GRPCStatus != "0" {
			report.Outcome, report.ErrorKind = "rpc_error", "upstream_rpc"
		}
	}
}

func event(kind string, r Report, proxyID string) recorder.Event {
	s := r.Decision.Selector.Clone()
	e := recorder.Event{Info: r.Info, Type: kind, FlowID: r.FlowID, ProxyID: proxyID, Protocol: r.Protocol, GRPCStatus: r.GRPCStatus, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		RuleID: r.Decision.RuleID, EligibleSequence: r.Decision.EligibleSequence, Probability: s.Probability, Nth: s.Nth, Every: s.Every,
		Selected: r.Decision.Selected, Reached: r.Reached, Applied: r.Applied, NotReached: r.NotReached, Phase: r.Decision.Fault.Phase,
		Action: r.Decision.Fault.Action, Outcome: r.Outcome, ErrorKind: r.ErrorKind, UpstreamStatus: r.UpstreamStatus}
	switch {
	case s.Probability != nil:
		e.Selector = "probability"
	case s.Nth != nil:
		e.Selector = "nth"
	case s.Every != nil:
		e.Selector = "every"
	}
	if kind != "flow_finished" {
		e.Outcome = ""
	} else if r.Outcome == "completed" {
		if r.Applied {
			e.Outcome = "fault_applied"
		} else {
			e.Outcome = "pass_through"
		}
	} else if r.Outcome == "canceled" {
		e.Outcome = r.ErrorKind
	}
	return e
}

func errorKind(err error, fallback string) string {
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	if errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &certificate) {
		return "upstream_tls"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "upstream_timeout"
	}
	return fallback
}

func hasUpgrade(headers http.Header) bool {
	for _, value := range headers.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}
