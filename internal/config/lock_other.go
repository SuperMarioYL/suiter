//go:build !unix

// Package config — non-unix (e.g. windows) lock fallback. flock(2) is not
// available, so flockFile/unlockFile are no-ops; the in-process sync.Mutex
// (store.go) still serializes goroutines and the atomic tmp+rename remains.
// Cross-process serialization on non-unix is a known limitation
// (fix-config-store-concurrent-write-race); the single-dev, local-machine
// scope (v0.1) is darwin/linux-primary.
package config

import "os"

func (s *Store) flockFile(f *os.File) error  { return nil }
func (s *Store) unlockFile(f *os.File) error { return nil }
