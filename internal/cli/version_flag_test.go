package cli

import (
	"strings"
	"testing"

	version "github.com/SuperMarioYL/suiter"
)

// TestVersionFlagWorks (fix-cli-version-self-report-and-surface-drift) proves
// the shipped binary can now self-report its version. On the shipped v0.7.0
// tag, `suiter --version` errored with "unknown flag: --version" because
// cobra's root.Version was never set; cobra only registers a --version flag
// when root.Version != "". Now NewRoot sets root.Version = version.Version
// (the go:embed of the canonical VERSION file), so `suiter --version` prints
// "suiter version v0.8.0" and exits 0.
func TestVersionFlagWorks(t *testing.T) {
	// Isolate $HOME so NewRoot's config.NewStore() (which mkdirs ~/.suiter) does
	// not touch the real home directory. os.UserHomeDir reads $HOME on Unix.
	t.Setenv("HOME", t.TempDir())

	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("suiter --version: %v (cobra --version should succeed when root.Version is set)", err)
	}
	got := out.String()
	if !strings.Contains(got, version.Version) {
		t.Fatalf("suiter --version output = %q, want it to contain %q (the embedded VERSION)", got, version.Version)
	}
}
