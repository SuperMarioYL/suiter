//go:build unix

// Package config — unix (darwin/linux/bsd) cross-process lock for the token
// store. flock(2) serializes load-modify-flush across concurrent `suiter`
// processes (fix-config-store-concurrent-write-race); the in-process
// sync.Mutex (store.go) handles goroutines. flock is auto-released when the fd
// is closed or the process exits, so a crash never leaves a stale lock.
package config

import (
	"os"
	"syscall"
)

// flockFile acquires an exclusive advisory lock on the lock file.
func (s *Store) flockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockFile releases the exclusive advisory lock.
func (s *Store) unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
