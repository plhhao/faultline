package httpproxy

import (
	"context"
	"crypto/rand"
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

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/engine"
	"faultline/internal/fault"
)

type Report struct {
	FlowID     string
	Info       control.Info
	Decision   engine.Decision
	Reached    bool
	Applied    bool
	NotReached bool
	Outcome    string
	Err        error
}

type handler struct {
	service   *control.Service
	proxyID   string
	target    *url.URL
	transport *http.Transport
	runtime   config.Runtime
	slots     chan struct{}
	options   Options
}

type flow struct {
	writer         http.ResponseWriter
	conn           net.Conn
	method         string
	cancelUpstream context.CancelFunc
	body           io.ReadCloser
	terminal       bool
	disconnected   bool
}

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

func (f *flow) CloseConnection() error {
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
	if r.ProtoMajor != 1 || r.ProtoMinor != 1 || r.Method == http.MethodConnect || r.Header.Get("Upgrade") != "" || hasUpgrade(r.Header) || r.URL.IsAbs() {
		w.Header().Set("Connection", "close")
		http.Error(w, "only HTTP/1.1 reverse proxy requests are supported", http.StatusNotImplemented)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		w.Header().Set("Connection", "close")
		http.Error(w, "proxy inflight limit reached", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.runtime.RequestTimeout)
	defer cancel()
	conn := r.Context().Value(connectionKey{}).(net.Conn)
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { conn.Close(); close(closed) })
	defer func() {
		if !stop() {
			<-closed
		}
		conn.SetDeadline(time.Time{})
	}()
	// Allow upstream to respond without first draining a slow client upload.
	http.NewResponseController(w).EnableFullDuplex()
	upload := &uploadBody{ReadCloser: r.Body}
	upload.complete.Store(r.Body == nil || r.Body == http.NoBody)
	r.Body = upload
	upstreamCtx, cancelUpstream := context.WithCancel(ctx)
	f := &flow{writer: w, conn: conn, method: r.Method, cancelUpstream: cancelUpstream}
	report := Report{FlowID: rand.Text(), Info: snapshot.Info(), Outcome: "completed"}
	var discarded chan struct{}
	defer func() {
		// Transport may still be reading an upload after an early upstream response.
		if !upload.complete.Load() {
			if !f.disconnected && ctx.Err() == nil {
				http.NewResponseController(w).Flush()
			}
			conn.Close()
		}
		f.CancelUpstream()
		if discarded != nil {
			<-discarded
		}
		// Intentional connection closure may itself cancel the request context.
		if ctx.Err() != nil && (!f.disconnected || report.Err != nil) {
			report.Outcome, report.Err = "canceled", ctx.Err()
		}
		report.NotReached = report.Decision.Selected && !report.Reached
		if h.options.Observe != nil {
			h.options.Observe(report)
		}
	}()
	var err error
	report.Decision, err = snapshot.Decide(h.proxyID, engine.Metadata{Method: r.Method, Path: r.URL.Path, Headers: r.Header})
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
		if h.options.Executor == nil {
			return fault.ErrUnavailable
		}
		if report.Decision.Fault.Action == "hold_request" && !upload.complete.Load() {
			// Reading and discarding the withheld upload lets net/http detect client disconnects.
			discarded = make(chan struct{})
			go func() {
				defer close(discarded)
				io.Copy(io.Discard, upload)
			}()
		}
		var err error
		report.Applied, err = h.options.Executor.Execute(ctx, report.Decision.Fault.Clone(), f)
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
			return
		}
		if ctx.Err() != nil {
			report.Err = ctx.Err()
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
			w.Header().Set("Connection", "close")
			http.Error(w, http.StatusText(status), status)
		}
	}
	if err := apply(config.BeforeUpstreamRequest); err != nil {
		fail(w, r, err)
		return
	}
	proxy := httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(h.target)
			p.Out.URL.RawQuery = p.In.URL.RawQuery
			// The inbound body fills trailer values at EOF, after request cloning.
			p.Out.Trailer = p.In.Trailer
			p.Out.GetBody = nil
		},
		Transport: h.transport,
		ModifyResponse: func(response *http.Response) error {
			f.body = response.Body
			if response.StatusCode == http.StatusSwitchingProtocols {
				return errors.New("upstream protocol upgrade is unsupported")
			}
			return apply(config.AfterUpstreamHeaders)
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
