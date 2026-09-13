package config

import (
	"crypto/sha256"
	"encoding/json"
	"maps"
	"slices"
	"time"
)

const APIVersion = "faultline/v1alpha1"
const (
	BeforeUpstreamRequest = "before_upstream_request"
	AfterUpstreamHeaders  = "after_upstream_headers"
)

type Config struct {
	APIVersion string  `yaml:"api_version"`
	Seed       uint64  `yaml:"seed"`
	Runtime    Runtime `yaml:"runtime"`
	Proxies    []Proxy `yaml:"proxies"`
}

type Runtime struct {
	MaxInflightRequests int           `yaml:"max_inflight_requests"`
	RequestTimeout      time.Duration `yaml:"request_timeout"`
}

type Proxy struct {
	ID          string       `yaml:"id"`
	Protocol    string       `yaml:"protocol"`
	Listen      string       `yaml:"listen"`
	Upstream    string       `yaml:"upstream"`
	TLS         *ListenerTLS `yaml:"tls"`
	UpstreamTLS *UpstreamTLS `yaml:"upstream_tls"`
	Rules       []Rule       `yaml:"rules"`
}

type ListenerTLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type UpstreamTLS struct {
	CAFile string `yaml:"ca_file"`
}

type Rule struct {
	ID      string   `yaml:"id"`
	Enabled bool     `yaml:"enabled"`
	Match   Matcher  `yaml:"match"`
	Select  Selector `yaml:"select"`
	Fault   Fault    `yaml:"fault"`
}

type Matcher struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

type Selector struct {
	Probability *float64 `yaml:"probability"`
	Nth         *uint64  `yaml:"nth"`
	Every       *uint64  `yaml:"every"`
}

type Fault struct {
	Phase       string         `yaml:"phase"`
	Action      string         `yaml:"action"`
	Duration    *time.Duration `yaml:"duration"`
	MaxDuration *time.Duration `yaml:"max_duration"`
	Status      *int           `yaml:"status"`
	Body        *string        `yaml:"body"`
}

// Document owns validated, normalized configuration. Accessors return copies.
type Document struct {
	config             Config
	valid              bool
	fingerprint        [32]byte
	restartFingerprint [32]byte
}

func (d *Document) Valid() bool    { return d != nil && d.valid }
func (d *Document) Config() Config { return cloneConfig(d.config) }
func (d *Document) Equal(other *Document) bool {
	return d.Valid() && other.Valid() && d.fingerprint == other.fingerprint
}
func (d *Document) RestartCompatible(other *Document) bool {
	return d.Valid() && other.Valid() && d.restartFingerprint == other.restartFingerprint
}

func DefaultRuntime() Runtime {
	return Runtime{MaxInflightRequests: 1000, RequestTimeout: 30 * time.Second}
}

func (f Fault) Clone() Fault {
	f.Duration = clonePointer(f.Duration)
	f.MaxDuration = clonePointer(f.MaxDuration)
	f.Status = clonePointer(f.Status)
	f.Body = clonePointer(f.Body)
	return f
}

func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneConfig(c Config) Config {
	c.Proxies = slices.Clone(c.Proxies)
	for i := range c.Proxies {
		p := &c.Proxies[i]
		p.TLS = clonePointer(p.TLS)
		p.UpstreamTLS = clonePointer(p.UpstreamTLS)
		p.Rules = slices.Clone(p.Rules)
		for j := range p.Rules {
			r := &p.Rules[j]
			r.Match.Headers = maps.Clone(r.Match.Headers)
			r.Select.Probability = clonePointer(r.Select.Probability)
			r.Select.Nth = clonePointer(r.Select.Nth)
			r.Select.Every = clonePointer(r.Select.Every)
			r.Fault = r.Fault.Clone()
		}
	}
	return c
}

func newDocument(c Config, tlsDigests map[string][32]byte) (*Document, error) {
	canonical := cloneConfig(c)
	slices.SortFunc(canonical.Proxies, func(a, b Proxy) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	hash := func(c Config) ([32]byte, error) {
		data, err := json.Marshal(struct {
			Config Config
			TLS    map[string][32]byte
		}{c, tlsDigests})
		if err != nil {
			return [32]byte{}, err
		}
		return sha256.Sum256(data), nil
	}
	full, err := hash(canonical)
	if err != nil {
		return nil, err
	}
	canonical.Seed = 0
	for i := range canonical.Proxies {
		canonical.Proxies[i].Rules = nil
	}
	restart, err := hash(canonical)
	if err != nil {
		return nil, err
	}
	return &Document{config: c, valid: true, fingerprint: full, restartFingerprint: restart}, nil
}
