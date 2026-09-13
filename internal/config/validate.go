package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func validate(c *Config, base string) (map[string][32]byte, error) {
	if c.APIVersion != APIVersion {
		return nil, invalid("api_version", "unsupported or missing version")
	}
	if c.Runtime.MaxInflightRequests <= 0 {
		return nil, invalid("runtime.max_inflight_requests", "must be positive")
	}
	if c.Runtime.RequestTimeout <= 0 {
		return nil, invalid("runtime.request_timeout", "must be positive")
	}
	if len(c.Proxies) == 0 {
		return nil, invalid("proxies", "at least one proxy is required")
	}
	listeners := map[string]bool{}
	digests := map[string][32]byte{}
	for i := range c.Proxies {
		p := &c.Proxies[i]
		path := fmt.Sprintf("proxies[%d]", i)
		if !identifier(p.ID) {
			return nil, invalid(path+".id", "expected a nonempty identifier")
		}
		if p.Protocol != "http1" {
			return nil, invalid(path+".protocol", "only http1 is supported")
		}
		host, port, err := net.SplitHostPort(p.Listen)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 1 || number > 65535 || (host != "" && !hostname(host)) {
			return nil, invalid(path+".listen", "expected host:port with port 1–65535")
		}
		if host == "" {
			host = "127.0.0.1"
		}
		p.Listen = net.JoinHostPort(strings.ToLower(host), strconv.Itoa(number))
		if listeners[p.Listen] {
			return nil, invalid(path+".listen", "duplicate listener")
		}
		listeners[p.Listen] = true
		u, err := url.Parse(p.Upstream)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !hostname(u.Hostname()) || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.Contains(p.Upstream, "#") {
			return nil, invalid(path+".upstream", "expected an HTTP(S) origin without credentials, path, query or fragment")
		}
		port = u.Port()
		if port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return nil, invalid(path+".upstream", "invalid port")
			}
			port = strconv.Itoa(n)
		} else if strings.HasSuffix(u.Host, ":") {
			return nil, invalid(path+".upstream", "invalid port")
		}
		host = strings.ToLower(u.Hostname())
		if port == "80" && u.Scheme == "http" || port == "443" && u.Scheme == "https" {
			port = ""
		}
		if port != "" {
			host = net.JoinHostPort(host, port)
		} else if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		p.Upstream = u.Scheme + "://" + host
		if err := validateTLS(p, base, path, digests); err != nil {
			return nil, err
		}
		if p.Rules == nil {
			p.Rules = []Rule{}
		}
		for j := range p.Rules {
			r := &p.Rules[j]
			rp := fmt.Sprintf("%s.rules[%d]", path, j)
			if !identifier(r.ID) {
				return nil, invalid(rp+".id", "expected a nonempty identifier")
			}
			if err := validateRule(r, rp); err != nil {
				return nil, err
			}
		}
	}
	return digests, nil
}

func identifier(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func hostname(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func token(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func validateRule(r *Rule, path string) error {
	if r.Match.Method != "" && !token(r.Match.Method) {
		return invalid(path+".match.method", "invalid method token")
	}
	if r.Match.Path != "" && (!strings.HasPrefix(r.Match.Path, "/") || strings.ContainsAny(r.Match.Path, "?#\r\n")) {
		return invalid(path+".match.path", "expected an exact path without query or fragment")
	}
	headers := map[string]string{}
	for name, value := range r.Match.Headers {
		if !token(name) || strings.ContainsAny(value, "\r\n\x00") {
			return invalid(path+".match.headers", "invalid header name or value")
		}
		name = strings.ToLower(name)
		if _, exists := headers[name]; exists {
			return invalid(path+".match.headers", "duplicate case-insensitive header name")
		}
		headers[name] = value
	}
	r.Match.Headers = headers
	s := r.Select
	count := 0
	if s.Probability != nil {
		count++
		if *s.Probability == 0 {
			*s.Probability = 0
		}
		if math.IsNaN(*s.Probability) || math.IsInf(*s.Probability, 0) || *s.Probability < 0 || *s.Probability > 1 {
			return invalid(path+".select.probability", "must be finite and between 0 and 1")
		}
	}
	if s.Nth != nil {
		count++
		if *s.Nth == 0 {
			return invalid(path+".select.nth", "must be positive")
		}
	}
	if s.Every != nil {
		count++
		if *s.Every == 0 {
			return invalid(path+".select.every", "must be positive")
		}
	}
	if count != 1 {
		return invalid(path+".select", "exactly one selector is required")
	}
	if err := validateFault(r.Fault, path+".fault"); err != nil {
		return err
	}
	if r.Fault.Action == "respond" && r.Fault.Body == nil {
		body := ""
		r.Fault.Body = &body
	}
	return nil
}

func validateFault(f Fault, path string) error {
	before, after := f.Phase == BeforeUpstreamRequest, f.Phase == AfterUpstreamHeaders
	if !before && !after {
		return invalid(path+".phase", "unsupported phase for http1")
	}
	switch f.Action {
	case "delay":
		if f.Duration == nil || *f.Duration <= 0 {
			return invalid(path+".duration", "positive duration is required")
		}
	case "respond":
		if !before {
			return invalid(path+".phase", "respond requires before_upstream_request")
		}
		if f.Status == nil || *f.Status < 200 || *f.Status > 599 {
			return invalid(path+".status", "status 200–599 is required")
		}
	case "hold_request", "hold_response":
		if f.Action == "hold_request" && !before || f.Action == "hold_response" && !after {
			return invalid(path+".phase", "unsupported action/phase combination")
		}
		if f.MaxDuration == nil || *f.MaxDuration <= 0 {
			return invalid(path+".max_duration", "positive max_duration is required")
		}
	case "close_connection":
	default:
		return invalid(path+".action", "unsupported action for http1")
	}
	if f.Duration != nil && f.Action != "delay" {
		return invalid(path+".duration", "not supported by this action")
	}
	if f.MaxDuration != nil && f.Action != "hold_request" && f.Action != "hold_response" {
		return invalid(path+".max_duration", "not supported by this action")
	}
	if f.Status != nil && f.Action != "respond" {
		return invalid(path+".status", "not supported by this action")
	}
	if f.Body != nil && f.Action != "respond" {
		return invalid(path+".body", "not supported by this action")
	}
	return nil
}
