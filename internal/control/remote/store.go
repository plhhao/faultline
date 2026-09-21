package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/control"
)

const auditLimit = 1000

type Audit struct {
	Time      time.Time `json:"time"`
	Actor     string    `json:"actor"`
	Operation string    `json:"operation"`
	Revision  uint64    `json:"revision"`
	Outcome   string    `json:"outcome"`
}
type state struct {
	Version   int            `json:"version"`
	Config    string         `json:"config"`
	Revision  uint64         `json:"revision"`
	LastApply control.Result `json:"last_apply"`
	Audit     []Audit        `json:"audit"`
}
type Store struct {
	dir      string
	lock     *os.File
	state    state
	poisoned bool
}

func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("data directory must be private (0700), not a symlink")
	}
	return nil
}

// atomicFile commits on rename, then syncs the directory before acknowledging durability.
func atomicFile(path string, data []byte) (renamed bool, err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return false, err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return true, err
	}
	defer dir.Close()
	return true, dir.Sync()
}

func Open(dir, bootstrap string) (*Store, *config.Document, error) {
	if err := privateDir(dir); err != nil {
		return nil, nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, nil, errors.New("managed data is already in use")
	}
	s := &Store{dir: dir, lock: lock}
	doc, err := s.load(bootstrap)
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	return s, doc, nil
}
func (s *Store) Close() {
	if s.lock != nil {
		s.lock.Close()
	}
}
func (s *Store) load(bootstrap string) (*config.Document, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		doc, err := config.Load(bootstrap)
		if err != nil {
			return nil, err
		}
		encoded, err := config.Encode(doc.Config())
		if err != nil {
			return nil, err
		}
		st := state{Version: 1, Config: string(encoded), Revision: 1}
		st.Audit = []Audit{{time.Now().UTC(), "operator", "bootstrap", 1, "ok"}}
		if err := s.save(st); err != nil {
			return nil, err
		}
		return doc, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, errors.New("invalid managed state")
	}
	if s.state.Version != 1 || s.state.Revision == 0 {
		return nil, errors.New("unsupported managed state")
	}
	return config.Parse([]byte(s.state.Config), filepath.Join(s.dir, "active.yaml"))
}
func (s *Store) save(next state) error {
	if s.poisoned {
		return errors.New("storage commit uncertain; restart required")
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	renamed, err := atomicFile(filepath.Join(s.dir, "state.json"), data)
	if err != nil {
		s.poisoned = renamed
		return fmt.Errorf("persist managed state: %w", err)
	}
	s.state = next
	return nil
}
func appendAudit(st state, actor, operation, outcome string, revision uint64) state {
	st.Audit = append(append([]Audit(nil), st.Audit...), Audit{time.Now().UTC(), actor, operation, revision, outcome})
	if len(st.Audit) > auditLimit {
		st.Audit = st.Audit[len(st.Audit)-auditLimit:]
	}
	return st
}
func (s *Store) audit(actor, operation, outcome string, revision uint64) error {
	return s.save(appendAudit(s.state, actor, operation, outcome, revision))
}
func (s *Store) apply(actor string, doc *config.Document, result control.Result) error {
	data, err := config.Encode(doc.Config())
	if err != nil {
		return err
	}
	next := appendAudit(s.state, actor, "apply", "ok", result.Info.Revision)
	next.Config, next.Revision, next.LastApply = string(data), result.Info.Revision, result
	return s.save(next)
}

// Configure replaces infrastructure only while the instance is stopped.
func Configure(dir, filename string) error {
	s, current, err := Open(dir, filename)
	if err != nil {
		return err
	}
	defer s.Close()
	doc, err := config.Load(filename)
	if err != nil {
		return err
	}
	if current.Equal(doc) {
		return s.audit("operator", "configure", "no_op", s.state.Revision)
	}
	data, err := config.Encode(doc.Config())
	if err != nil {
		return err
	}
	next := appendAudit(s.state, "operator", "configure", "ok", s.state.Revision+1)
	next.Config = string(data)
	next.Revision++
	next.LastApply = control.Result{Changed: true, Info: control.Info{Revision: next.Revision, AppliedAt: time.Now().UTC()}}
	return s.save(next)
}
