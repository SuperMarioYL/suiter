package cli

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/suiter/internal/config"
	"github.com/SuperMarioYL/suiter/internal/suite"
)

// TestLogout (feat-logout-command) proves `suiter logout <suite>` clears the
// cached token from the shared TokenStore — wiring the previously-dead
// Store.Delete (internal/config/store.go:69 had ZERO non-test callers) and
// completing the login→use→logout lifecycle the v0.2.0–v0.4.0 cached-token
// expiry fixes implicitly assume. Mirrors `suiter login <suite>`: same
// reg.Get(name) "unknown suite" guard, same store==nil-at-use guard.
//
// Each subtest isolates the token cache by pointing $HOME at a temp dir
// (config.NewStore reads $HOME on darwin), so the on-disk cache is per-test.
func TestLogout(t *testing.T) {
	reg := suite.NewRegistry()
	reg.Register(&fakeSuite{name: "fakesuite"})

	t.Run("clears_token", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		store, err := config.NewStore()
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		if err := store.Save("fakesuite", suite.Token{AccessToken: "at"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		cmd := newLogoutCommand(reg, store)
		out, err := runCmd(t, cmd, "fakesuite")
		if err != nil {
			t.Fatalf("logout: %v (out=%s)", err, out)
		}
		if !strings.Contains(out, "token cleared") {
			t.Fatalf("out = %q, want 'token cleared' message", out)
		}
		var tok suite.Token
		ok, err := store.Load("fakesuite", &tok)
		if err != nil {
			t.Fatalf("Load after logout: %v", err)
		}
		if ok {
			t.Fatal("want cached token gone after logout, but Load still found an entry")
		}
	})

	t.Run("idempotent_missing_entry", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		store, err := config.NewStore()
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		cmd := newLogoutCommand(reg, store)
		out, err := runCmd(t, cmd, "fakesuite")
		if err != nil {
			t.Fatalf("logout on empty store: %v (out=%s) — want idempotent exit 0", err, out)
		}
		if !strings.Contains(out, "not logged in") {
			t.Fatalf("out = %q, want 'not logged in' message", out)
		}
	})

	t.Run("unknown_suite", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		store, err := config.NewStore()
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		cmd := newLogoutCommand(reg, store)
		_, err = runCmd(t, cmd, "ghost")
		if err == nil {
			t.Fatal("want error for unknown suite, got nil")
		}
		if !strings.Contains(err.Error(), "unknown suite") {
			t.Fatalf("err = %q, want 'unknown suite' message", err)
		}
	})

	t.Run("nil_store_errors_at_use", func(t *testing.T) {
		cmd := newLogoutCommand(reg, nil)
		_, err := runCmd(t, cmd, "fakesuite")
		if err == nil {
			t.Fatal("want error when token store is unavailable, got nil")
		}
	})
}
