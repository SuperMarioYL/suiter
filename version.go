// Package version holds the suiter release version. The VERSION file at the
// module root is the single source of truth; go:embed bakes it into the binary
// at build time so `suiter --version` self-reports without ldflags and a
// duplicated literal can never drift from the file. A go-installed binary
// embeds the VERSION file from the tagged source it was built from.
package version

import (
	"strings"

	_ "embed"
)

// versionRaw is the verbatim contents of the canonical VERSION file. The
// trailing newline is stripped in the exported Version so `suiter --version`
// does not emit a blank line.
//
//go:embed VERSION
var versionRaw string

// Version is the shipped release version (e.g. "v0.8.0"), read from VERSION
// and trimmed of surrounding whitespace.
var Version = strings.TrimSpace(versionRaw)
