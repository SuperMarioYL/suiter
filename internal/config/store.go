// Package config implements the TokenStore: a per-suite-scoped JSON file at
// ~/.suiter/tokens.json with 0600 perms (single-dev, local-only for v0.1).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is the per-suite token cache. It is a thin JSON map keyed by suite
// name, persisted at ~/.suiter/tokens.json with filesystem permissions only
// (v0.1 scope: single dev, local machine — no cloud vault, no multi-user).
type Store struct {
	path string
	// mu serializes load-modify-flush within THIS process. fix-config-store-
	// concurrent-write-race: two concurrent `suiter login` goroutines used to
	// both load the same initial map, each add only its own suite key, and
	// the second atomic rename silently erased the first suite's token. The
	// mutex makes the read-modify-write section safe in-process; flockFile
	// (a sibling .lock file) extends the serialization across PROCESSES.
	mu sync.Mutex
}

// NewStore opens (or creates) the token cache at ~/.suiter/tokens.json.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".suiter")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "tokens.json")}, nil
}

// Path returns the on-disk location of the token cache.
func (s *Store) Path() string { return s.path }

// Save marshals v and stores it under the given suite key.
func (s *Store) Save(suite string, v any) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	all, err := s.load()
	if err != nil {
		return err
	}
	if all == nil {
		all = map[string]json.RawMessage{}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s token: %w", suite, err)
	}
	all[suite] = data
	return s.flush(all)
}

// Load reads the entry for suite and unmarshals it into v. ok is false if no
// entry exists.
func (s *Store) Load(suite string, v any) (ok bool, err error) {
	all, err := s.load()
	if err != nil {
		return false, err
	}
	raw, exists := all[suite]
	if !exists {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("unmarshal %s token: %w", suite, err)
	}
	return true, nil
}

// Delete removes the entry for a suite (e.g. on revoke).
func (s *Store) Delete(suite string) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	all, err := s.load()
	if err != nil {
		return err
	}
	if all == nil {
		return nil
	}
	delete(all, suite)
	return s.flush(all)
}

func (s *Store) load() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return all, nil
}

func (s *Store) flush(all map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, s.path)
}

// lockPath is the sibling lock file used for cross-process serialization.
func (s *Store) lockPath() string { return s.path + ".lock" }

// lock serializes the load-modify-flush critical section against BOTH
// in-process goroutines (sync.Mutex) and concurrent `suiter` PROCESSES
// (exclusive flock on the sibling .lock file). fix-config-store-concurrent-
// write-race: without it, two concurrent `suiter login` invocations both
// loaded the same initial map, each added only its own suite key, and the
// second atomic rename silently erased the first suite's just-cached token
// (last rename wins). The returned release func releases flock + mutex; it
// is nil-safe so callers can `defer release()` even on a lock() error path
// where release is nil. flock is auto-released when the fd is closed or the
// process exits, so a crash never leaves a stale lock.
func (s *Store) lock() (release func(), err error) {
	s.mu.Lock()
	lf, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("open lock %s: %w", s.lockPath(), err)
	}
	if err := s.flockFile(lf); err != nil {
		lf.Close()
		s.mu.Unlock()
		return nil, fmt.Errorf("lock %s: %w", s.lockPath(), err)
	}
	return func() {
		_ = s.unlockFile(lf)
		_ = lf.Close()
		s.mu.Unlock()
	}, nil
}
