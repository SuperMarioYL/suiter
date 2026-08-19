package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// newTestStore wires a Store at a temp path so tests do not touch the real
// ~/.suiter/tokens.json. The Store's unexported `path` is set directly (same
// package); lockPath() derives the sibling .lock from it.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{path: filepath.Join(t.TempDir(), "tokens.json")}
}

// TestSave_LoadRoundTrip is a basic sanity check that the locking + atomic
// rename path still persists and reads back a single suite's token (and that
// the sibling .lock file is created, not the data file used as the lock).
func TestSave_LoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save("feishu", map[string]string{"access_token": "f-tok"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var v map[string]string
	ok, err := s.Load("feishu", &v)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if v["access_token"] != "f-tok" {
		t.Fatalf("token = %v, want f-tok", v)
	}
	if _, err := os.Stat(s.lockPath()); err != nil {
		t.Fatalf("lock file not created alongside tokens.json: %v", err)
	}
	// the .tmp staging file must not linger after a successful atomic rename
	if _, err := os.Stat(s.path + ".tmp"); err == nil {
		t.Fatalf("staging .tmp file lingered after rename")
	}
}

// TestDelete_LoadSurvives proves Delete clears one suite without clobbering
// the others (the lock now guards the load-modify-flush).
func TestDelete_LoadSurvives(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save("feishu", map[string]string{"access_token": "f"}); err != nil {
		t.Fatalf("Save feishu: %v", err)
	}
	if err := s.Save("dingtalk", map[string]string{"access_token": "d"}); err != nil {
		t.Fatalf("Save dingtalk: %v", err)
	}
	if err := s.Delete("feishu"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var v map[string]string
	if ok, _ := s.Load("feishu", &v); ok {
		t.Fatal("feishu should be deleted")
	}
	if ok, _ := s.Load("dingtalk", &v); !ok {
		t.Fatal("dingtalk should survive Delete of feishu")
	}
}

// TestSave_ConcurrentNoLoss (fix-config-store-concurrent-write-race) proves N
// concurrent Save calls for different suites do not drop any suite's token.
// Before the fix, Save did a non-atomic load-modify-flush with NO lock: two
// concurrent `suiter login` goroutines both loaded the same initial map, each
// added only its own key, and the second atomic rename silently erased the
// first suite's just-cached token (last rename wins). The mutex (in-process)
// + exclusive flock (cross-process) now serialize the read-modify-write.
// Run with `go test -race` — the mutex provides happens-before so the race
// detector stays quiet AND the data-loss assertion catches the original bug.
func TestSave_ConcurrentNoLoss(t *testing.T) {
	s := newTestStore(t)
	suites := []string{"feishu", "dingtalk", "wework", "tencentdocs", "a", "b", "c", "d", "e", "f", "g", "h"}

	var wg sync.WaitGroup
	for _, name := range suites {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if err := s.Save(n, map[string]string{"access_token": n + "-tok"}); err != nil {
				t.Errorf("Save(%s): %v", n, err)
			}
		}(name)
	}
	wg.Wait()

	for _, name := range suites {
		var v map[string]string
		ok, err := s.Load(name, &v)
		if err != nil || !ok {
			t.Fatalf("suite %q lost after concurrent saves: ok=%v err=%v (the last-rename-wins race)", name, ok, err)
		}
		if v["access_token"] != name+"-tok" {
			t.Fatalf("suite %q token = %v, want %q-tok (overwritten by a concurrent save)", name, v, name)
		}
	}
}

// TestSave_DeleteConcurrentNoCorrupt proves concurrent Save and Delete on
// different suites do not corrupt the store (the lock serializes both).
func TestSave_DeleteConcurrentNoCorrupt(t *testing.T) {
	s := newTestStore(t)
	// prime the store so Delete has something to remove.
	for _, name := range []string{"feishu", "dingtalk", "wework"} {
		if err := s.Save(name, map[string]string{"access_token": name}); err != nil {
			t.Fatalf("prime %s: %v", name, err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := s.Save("tencentdocs", map[string]string{"access_token": "t"}); err != nil {
				t.Errorf("Save tencentdocs: %v", err)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := s.Delete("feishu"); err != nil {
				t.Errorf("Delete feishu: %v", err)
			}
		}
	}()
	wg.Wait()

	// the store must still be valid JSON (not half-written) and tencentdocs
	// (concurrently saved) must survive.
	var v map[string]string
	if ok, err := s.Load("tencentdocs", &v); err != nil || !ok || v["access_token"] != "t" {
		t.Fatalf("tencentdocs lost/corrupt after concurrent Save+Delete: ok=%v err=%v v=%v", ok, err, v)
	}
}
