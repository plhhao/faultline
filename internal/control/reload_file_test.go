package control_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"faultline/internal/config"
	"faultline/internal/engine"
)

func TestReloadFileAtomicityAndNoop(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	fragment := filepath.Join(dir, "proxy.yaml")
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, proxies, _ := strings.Cut(yamlConfig(42, "every: 2"), "proxies:\n")
	write(root, "api_version: faultline/v1alpha1\nseed: 42\ninclude: [proxy.yaml]\n")
	write(fragment, "proxies:\n"+proxies)
	doc, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := service(t, doc)
	s.SetEnabled(true)
	old := s.Acquire()
	decide(t, old)
	result, err := s.ReloadFile(root)
	if err != nil || result.Changed || !decide(t, s.Acquire()).Selected {
		t.Fatalf("no-op lost counters: %v", err)
	}
	before := s.Acquire().Info()
	extra := "  - id: inventory\n    protocol: http1\n    listen: :8081\n    upstream: http://localhost\n    rules: []\n"
	write(fragment, "proxies:\n"+proxies+extra)
	if _, err := s.ReloadFile(root); err == nil {
		t.Fatal("adding a proxy must require restart")
	}
	withExtra, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	second := service(t, withExtra)
	write(fragment, "proxies:\n"+proxies)
	if _, err := second.ReloadFile(root); err == nil {
		t.Fatal("removing a proxy must require restart")
	}
	for _, bad := range []string{"proxies: [", "proxies:\n" + proxies + proxies, strings.Replace("proxies:\n"+proxies, ":8080", ":8081", 1), "proxies: []"} {
		write(fragment, bad)
		if _, err := s.ReloadFile(root); err == nil {
			t.Fatal("invalid fragment applied")
		}
		if s.Acquire().Info() != before {
			t.Fatal("failed reload changed snapshot/state")
		}
		c, _ := s.Acquire().Counters("payment", "r")
		if c.Eligible != 2 {
			t.Fatal("failed reload reset counters")
		}
	}
	write(fragment, strings.Replace("proxies:\n"+proxies, "every: 2", "every: 3", 1))
	result, err = s.ReloadFile(root)
	if err != nil || !result.Changed || !result.Info.Enabled {
		t.Fatalf("changed fragment: %v", err)
	}
	if decide(t, s.Acquire()).EligibleSequence != 1 || decide(t, old).EligibleSequence != 3 {
		t.Fatal("revision counter ownership")
	}
	before = s.Acquire().Info()
	write(root, yamlConfig(42, "every: 3"))
	result, err = s.ReloadFile(root)
	if err != nil || result.Changed || result.Info != before {
		t.Fatalf("moving inline changed revision: %v", err)
	}
	s.SetEnabled(false)
	write(root, yamlConfig(42, "nth: 1"))
	if _, err := s.ReloadFile(root); err != nil {
		t.Fatal(err)
	}
	if s.Acquire().Info().Enabled {
		t.Fatal("reload enabled injection")
	}
}

func TestReloadFileConcurrentSnapshots(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	fragment := filepath.Join(dir, "proxy.yaml")
	if err := os.WriteFile(root, []byte("api_version: faultline/v1alpha1\nseed: 42\ninclude: [proxy.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, proxies, _ := strings.Cut(yamlConfig(42, "nth: 1"), "proxies:\n")
	if err := os.WriteFile(fragment, []byte("proxies:\n"+proxies), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := service(t, doc)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 50; i++ {
			body := proxies
			if i%2 == 0 {
				body = strings.Replace(body, "nth: 1", "nth: 2", 1)
			}
			if err := os.WriteFile(fragment, []byte("proxies:\n"+body), 0600); err != nil {
				t.Error(err)
				return
			}
			if _, err := s.ReloadFile(root); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 4 {
		wg.Go(func() {
			for range 100 {
				s.SetEnabled(true)
				snap := s.Acquire()
				want := uint64(1)
				if snap.Info().Revision%2 == 0 {
					want = 2
				}
				if *snap.Config().Proxies[0].Rules[0].Select.Nth != want {
					t.Error("mixed revision and fragment")
				}
				if _, err := snap.Decide("payment", engine.Metadata{}); err != nil {
					t.Error(err)
				}
				s.SetEnabled(false)
			}
		})
	}
	wg.Wait()
}
