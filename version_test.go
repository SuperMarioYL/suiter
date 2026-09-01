package version

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestVersionEmbedMatchesVERSIONFile proves the go:embed of the canonical
// VERSION file is the value Version exposes (the single-source-of-truth
// contract). If someone moves or renames VERSION, this fails loudly instead of
// silently baking an empty string into the binary.
func TestVersionEmbedMatchesVERSIONFile(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if Version != want {
		t.Fatalf("version.Version = %q, want VERSION file %q (embed drifted from VERSION)", Version, want)
	}
}

// TestVersionSurfacesInLockstep (fix-cli-version-self-report-and-surface-drift)
// is the regression guard for the version-drift defect class: the v0.7.0
// release bumped only the VERSION file and left every derived surface stale —
// web/site.json content_version was still v0.6.0 and CHANGELOG.md had no v0.7.0
// entry, while the binary could not self-report at all (--version was not
// wired). This test FAILS on the shipped v0.7.0 tag (site.json=v0.6.0 and
// CHANGELOG head=[v0.6.0] vs VERSION=v0.7.0) and PASSES once every surface is
// brought into lockstep. It pins the embed (Version), the VERSION file,
// web/site.json content_version, and the CHANGELOG head `## [vX.Y.Z]` entry to
// the same value so the next bump cannot silently leave a surface behind.
func TestVersionSurfacesInLockstep(t *testing.T) {
	want := strings.TrimSpace(func() string {
		b, err := os.ReadFile("VERSION")
		if err != nil {
			t.Fatalf("read VERSION: %v", err)
		}
		return string(b)
	}())

	if Version != want {
		t.Errorf("version.Version = %q, want %q", Version, want)
	}

	// web/site.json content_version must equal the VERSION file.
	siteRaw, err := os.ReadFile("web/site.json")
	if err != nil {
		t.Fatalf("read web/site.json: %v", err)
	}
	var site struct {
		ContentVersion string `json:"content_version"`
	}
	if err := json.Unmarshal(siteRaw, &site); err != nil {
		t.Fatalf("parse web/site.json: %v", err)
	}
	if site.ContentVersion != want {
		t.Errorf("web/site.json content_version = %q, want %q", site.ContentVersion, want)
	}

	// The first `## [vX.Y.Z]` entry in CHANGELOG.md must equal the VERSION file.
	changelogRaw, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	re := regexp.MustCompile(`(?m)^## \[(v[0-9]+\.[0-9]+\.[0-9]+)\]`)
	m := re.FindSubmatch(changelogRaw)
	if m == nil {
		t.Fatalf("no `## [vX.Y.Z]` entry found in CHANGELOG.md")
	}
	headEntry := string(m[1])
	if headEntry != want {
		t.Errorf("CHANGELOG.md head entry = %q, want %q", headEntry, want)
	}
}
