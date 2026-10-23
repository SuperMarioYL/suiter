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
//
// fix-version-lockstep-guard-blind-spots extends it to four surfaces the
// original guard could not see — the README.md / README.en.md hero version line
// and web/site.json's meta.content_version + footer.tag — so a bump can no
// longer leave the public README or the site's visible version a release behind
// while `go test ./...` stays green. docs/demo-results.json is deliberately NOT
// pinned: it records the version of the run that produced the recorded demo, so
// it only moves when the demo is re-run for real.
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

	// The README hero version line (both locales) is a hand-maintained version
	// surface: `` `vX.Y.Z` · `Go 1.24+` · [MIT](LICENSE) ``.
	for _, readme := range []string{"README.md", "README.en.md"} {
		raw, err := os.ReadFile(readme)
		if err != nil {
			t.Fatalf("read %s: %v", readme, err)
		}
		m := readmeVersionLineRe.FindSubmatch(raw)
		if m == nil {
			t.Fatalf("no `` `vX.Y.Z` `` hero version line found in %s", readme)
		}
		if got := string(m[1]); got != want {
			t.Errorf("%s hero version line = %q, want %q", readme, got, want)
		}
	}

	// web/site.json: the top-level content_version, the meta.content_version and
	// the visible footer.tag prefix must all equal the VERSION file.
	siteRaw, err := os.ReadFile("web/site.json")
	if err != nil {
		t.Fatalf("read web/site.json: %v", err)
	}
	var site struct {
		ContentVersion string `json:"content_version"`
		Meta           struct {
			ContentVersion string `json:"content_version"`
		} `json:"meta"`
		Footer struct {
			Tag string `json:"tag"`
		} `json:"footer"`
	}
	if err := json.Unmarshal(siteRaw, &site); err != nil {
		t.Fatalf("parse web/site.json: %v", err)
	}
	if site.ContentVersion != want {
		t.Errorf("web/site.json content_version = %q, want %q", site.ContentVersion, want)
	}
	if got := "v" + strings.TrimPrefix(site.Meta.ContentVersion, "v"); got != want {
		t.Errorf("web/site.json meta.content_version = %q, want %q", site.Meta.ContentVersion, want)
	}
	if got := strings.TrimSpace(strings.SplitN(site.Footer.Tag, "·", 2)[0]); got != strings.TrimPrefix(want, "v") {
		t.Errorf("web/site.json footer.tag = %q (version part %q), want it to start with %q", site.Footer.Tag, got, strings.TrimPrefix(want, "v"))
	}

	// The first `## [vX.Y.Z]` entry in CHANGELOG.md must equal the VERSION file.
	changelogRaw, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	m := changelogHeadRe.FindSubmatch(changelogRaw)
	if m == nil {
		t.Fatalf("no `## [vX.Y.Z]` entry found in CHANGELOG.md")
	}
	headEntry := string(m[1])
	if headEntry != want {
		t.Errorf("CHANGELOG.md head entry = %q, want %q", headEntry, want)
	}
}

// readmeVersionLineRe matches the README hero line's leading version token,
// e.g. "`v0.9.0` · `Go 1.24+` · [MIT](LICENSE)".
var readmeVersionLineRe = regexp.MustCompile("(?m)^`(v[0-9]+\\.[0-9]+\\.[0-9]+)`\\s*·")

// changelogHeadRe matches the first `## [vX.Y.Z]` heading in CHANGELOG.md.
var changelogHeadRe = regexp.MustCompile(`(?m)^## \[(v[0-9]+\.[0-9]+\.[0-9]+)\]`)
