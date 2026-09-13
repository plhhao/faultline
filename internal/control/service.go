package control

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"faultline/internal/config"
	"faultline/internal/engine"
)

type Info struct {
	RunID           string
	Revision        uint64
	Enabled         bool
	ControlSequence uint64
	AppliedAt       time.Time
	StateChangedAt  time.Time
}

type revision struct {
	document config.Document
	engine   *engine.Engine
}

// Snapshot pins config, injection state and revision counters for a flow's lifetime.
// Old revisions are reclaimed when their last snapshot is released by the caller.
type Snapshot struct {
	info     Info
	revision *revision
}

func (s Snapshot) Info() Info            { return s.info }
func (s Snapshot) Config() config.Config { return s.revision.document.Config() }
func (s Snapshot) Decide(proxyID string, metadata engine.Metadata) (engine.Decision, error) {
	return s.revision.engine.Decide(proxyID, metadata, s.info.Enabled)
}
func (s Snapshot) Counters(proxyID, ruleID string) (engine.Counters, bool) {
	return s.revision.engine.Counters(proxyID, ruleID)
}

type Service struct {
	mu     sync.RWMutex
	active Snapshot
}

type Result struct {
	Changed bool
	Info    Info
}

func New(document *config.Document) (*Service, error) {
	evaluator, err := engine.New(document)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Service{active: Snapshot{
		info:     Info{RunID: rand.Text(), Revision: 1, AppliedAt: now, StateChangedAt: now},
		revision: &revision{document: *document, engine: evaluator},
	}}, nil
}

func (s *Service) Acquire() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Service) SetEnabled(enabled bool) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active.info.Enabled == enabled {
		return Result{Info: s.active.info}
	}
	s.active.info.Enabled = enabled
	s.active.info.ControlSequence++
	s.active.info.StateChangedAt = time.Now().UTC()
	return Result{Changed: true, Info: s.active.info}
}

// Apply publishes a fully validated revision atomically and preserves injection state.
func (s *Service) Apply(document *config.Document) (Result, error) {
	if !document.Valid() {
		return Result{}, errors.New("control: validated config is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.active.revision.document
	if !current.RestartCompatible(document) {
		return Result{}, errors.New("control: proxies, listeners, upstreams, TLS and runtime limits require restart")
	}
	if current.Equal(document) {
		return Result{Info: s.active.info}, nil
	}
	evaluator, err := engine.New(document)
	if err != nil {
		return Result{}, err
	}
	s.active.revision = &revision{document: *document, engine: evaluator}
	s.active.info.Revision++
	s.active.info.ControlSequence++
	s.active.info.AppliedAt = time.Now().UTC()
	return Result{Changed: true, Info: s.active.info}, nil
}

func (s *Service) Reload(data []byte, filename string) (Result, error) {
	document, err := config.Parse(data, filename)
	if err != nil {
		return Result{}, err
	}
	return s.Apply(document)
}

// ReloadFile rereads the root and all included files in this process's filesystem.
func (s *Service) ReloadFile(filename string) (Result, error) {
	document, err := config.Load(filename)
	if err != nil {
		return Result{}, err
	}
	return s.Apply(document)
}
