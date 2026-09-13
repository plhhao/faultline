package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"

	"faultline/internal/config"
)

// Metadata is supplied by an adapter; Headers retains individual values without joining them.
type Metadata struct {
	Method  string
	Path    string
	Headers map[string][]string
}

type Decision struct {
	ProxyID          string
	RuleID           string
	EligibleSequence uint64
	Selected         bool
	Selector         config.Selector
	Fault            config.Fault
}

type Counters struct{ Eligible, Selected uint64 }

type ruleState struct {
	rule     config.Rule
	mu       sync.Mutex
	counters Counters
	random   *rand.Rand
}

// Engine owns selector counters for one revision. It performs no protocol I/O.
type Engine struct{ proxies map[string][]*ruleState }

func New(document *config.Document) (*Engine, error) {
	if !document.Valid() {
		return nil, errors.New("engine: validated config is required")
	}
	c := document.Config()
	e := &Engine{proxies: make(map[string][]*ruleState, len(c.Proxies))}
	for _, p := range c.Proxies {
		states := make([]*ruleState, 0, len(p.Rules))
		for _, r := range p.Rules {
			seed := binary.LittleEndian.AppendUint64(nil, c.Seed)
			seed = append(seed, p.ID...)
			seed = append(seed, 0)
			seed = append(seed, r.ID...)
			hash := sha256.Sum256(seed)
			states = append(states, &ruleState{rule: r, random: rand.New(rand.NewPCG(binary.LittleEndian.Uint64(hash[:8]), binary.LittleEndian.Uint64(hash[8:16])))})
		}
		e.proxies[p.ID] = states
	}
	return e, nil
}

func (e *Engine) Decide(proxyID string, metadata Metadata, enabled bool) (Decision, error) {
	decision := Decision{ProxyID: proxyID}
	rules, ok := e.proxies[proxyID]
	if !ok {
		return decision, errors.New("engine: unknown proxy")
	}
	if !enabled {
		return decision, nil
	}
	for _, state := range rules {
		if !state.rule.Enabled || !matches(state.rule.Match, metadata) {
			continue
		}
		state.mu.Lock()
		state.counters.Eligible++
		sequence := state.counters.Eligible
		selected := state.selectAttempt(sequence)
		if selected {
			state.counters.Selected++
		}
		state.mu.Unlock()
		decision.RuleID = state.rule.ID
		decision.EligibleSequence = sequence
		decision.Selected = selected
		decision.Selector = state.rule.Select.Clone()
		decision.Fault = state.rule.Fault.Clone()
		return decision, nil
	}
	return decision, nil
}

func (e *Engine) Counters(proxyID, ruleID string) (Counters, bool) {
	for _, state := range e.proxies[proxyID] {
		if state.rule.ID == ruleID {
			state.mu.Lock()
			counters := state.counters
			state.mu.Unlock()
			return counters, true
		}
	}
	return Counters{}, false
}

func (r *ruleState) selectAttempt(sequence uint64) bool {
	s := r.rule.Select
	switch {
	case s.Nth != nil:
		return sequence == *s.Nth
	case s.Every != nil:
		return sequence%*s.Every == 0
	default:
		p := *s.Probability
		if p == 0 {
			return false
		}
		if p == 1 {
			return true
		}
		return r.random.Float64() < p
	}
}

func matches(m config.Matcher, metadata Metadata) bool {
	path, _, _ := strings.Cut(metadata.Path, "?")
	if m.Method != "" && m.Method != metadata.Method || m.Path != "" && m.Path != path {
		return false
	}
	for name, expected := range m.Headers {
		found := false
		for actual, values := range metadata.Headers {
			if strings.EqualFold(name, actual) {
				for _, value := range values {
					if value == expected {
						found = true
						break
					}
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
