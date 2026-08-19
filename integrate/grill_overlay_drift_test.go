package integrate

import (
	"os"
	"strings"
	"testing"
)

// overlayPinnedFiles maps each marked-overlay source (in skillFiles) to the
// vendored upstream fixture its above-marker region must stay byte-identical
// to. Per ADR-0112/ADR-0136, drift review reduces to diffing this region
// against the pinned upstream source; this test makes that diff mechanical
// instead of archaeological. It covers the domain-modeling body and the two
// format documents it owns (pinned to domain-modeling@8b78b53) and the
// setup-matt-pocock-skills seed templates (pinned to mattpocock/skills@8b78b53).
//
// grill-with-docs is absent: since ADR-0225 it inlines no upstream text at all —
// it loads grilling and domain-modeling instead — so it has no region to pin.
var overlayPinnedFiles = map[string]string{
	"skills/pop/domain-modeling/SKILL.md":                         "testdata/domain-modeling-pin/SKILL.md",
	"skills/pop/domain-modeling/CONTEXT-FORMAT.md":                "testdata/domain-modeling-pin/CONTEXT-FORMAT.md",
	"skills/pop/domain-modeling/ADR-FORMAT.md":                    "testdata/domain-modeling-pin/ADR-FORMAT.md",
	"skills/pop/setup-matt-pocock-skills/domain.md":               "testdata/setup-skill-pin/domain.md",
	"skills/pop/setup-matt-pocock-skills/issue-tracker-github.md": "testdata/setup-skill-pin/issue-tracker-github.md",
	"skills/pop/setup-matt-pocock-skills/issue-tracker-gitlab.md": "testdata/setup-skill-pin/issue-tracker-gitlab.md",
	"skills/pop/setup-matt-pocock-skills/issue-tracker-local.md":  "testdata/setup-skill-pin/issue-tracker-local.md",
}

// verbatimPinnedFiles maps each source that is upstream all the way down to its
// pinned fixture. grilling is the only one: ADR-0225 leaves the interview
// primitive with no pop overlay, so everything below its provenance header — not
// merely everything above a marker — must match productivity/grilling@8b78b53.
// A pop rule edited into it would fail this test rather than quietly turning the
// shared primitive into a pop-shaped one.
var verbatimPinnedFiles = map[string]string{
	"skills/pop/grilling/SKILL.md": "testdata/grilling-pin/SKILL.md",
}

// belowHeaderRegion returns everything after the provenance header comment: the
// whole upstream region of a file that carries no overlay.
func belowHeaderRegion(t *testing.T, src string) string {
	t.Helper()

	headerClose := strings.Index(src, "-->")
	if headerClose == -1 {
		t.Fatalf("no provenance header comment (missing \"-->\") in source")
	}
	start := headerClose + len("-->")
	// Skip the header line's own newline, then one blank separator line.
	if start < len(src) && src[start] == '\n' {
		start++
	}
	if start < len(src) && src[start] == '\n' {
		start++
	}
	return src[start:]
}

// aboveMarkerRegion strips the provenance header comment and returns the
// verbatim upstream content that precedes the "POP OVERLAY" marker comment.
func aboveMarkerRegion(t *testing.T, src string) string {
	t.Helper()

	headerClose := strings.Index(src, "-->")
	if headerClose == -1 {
		t.Fatalf("no provenance header comment (missing \"-->\") in source")
	}
	start := headerClose + len("-->")
	// Skip the header line's own newline, then one blank separator line.
	if start < len(src) && src[start] == '\n' {
		start++
	}
	if start < len(src) && src[start] == '\n' {
		start++
	}

	// The provenance header's own prose also mentions "POP OVERLAY" in
	// quotes, so search for the marker only after the header closes.
	markerIdx := strings.Index(src[start:], "POP OVERLAY")
	if markerIdx == -1 {
		t.Fatalf("no \"POP OVERLAY\" marker after provenance header")
	}
	markerIdx += start
	lineStart := strings.LastIndex(src[:markerIdx], "\n") + 1

	return src[start:lineStart]
}

// TestOverlayBaseMatchesPin operationalizes ADR-0112: it extracts the
// above-"POP OVERLAY"-marker region of each overlaid skill doc and asserts it
// is byte-identical to the vendored upstream base at the pinned ref recorded
// in the file's provenance header. A pin bump or an accidental edit to the
// base region fails this test instead of requiring manual diffing.
func TestOverlayBaseMatchesPin(t *testing.T) {
	t.Parallel()
	for srcPath, fixturePath := range overlayPinnedFiles {
		t.Run(srcPath, func(t *testing.T) {
			src, err := skillFiles.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read embedded source %s: %v", srcPath, err)
			}
			want, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read pinned fixture %s: %v", fixturePath, err)
			}

			got := aboveMarkerRegion(t, string(src))
			if got != string(want) {
				t.Fatalf("%s above-marker region drifted from pinned upstream base %s:\n got: %q\nwant: %q", srcPath, fixturePath, got, string(want))
			}
		})
	}
}

// TestVerbatimPinMatchesUpstream is TestOverlayBaseMatchesPin's counterpart for
// a file pop ships unaltered: the region below the provenance header is the
// whole file, so the fixture is the whole upstream skill. The two tests together
// are the drift gate — a source belongs to exactly one of them, and a pop rule
// added to a verbatim file has to move it between them deliberately.
func TestVerbatimPinMatchesUpstream(t *testing.T) {
	t.Parallel()
	for srcPath, fixturePath := range verbatimPinnedFiles {
		t.Run(srcPath, func(t *testing.T) {
			src, err := skillFiles.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("read embedded source %s: %v", srcPath, err)
			}
			if strings.Contains(string(src), "POP OVERLAY") {
				t.Fatalf("%s carries a pop overlay marker but is pinned as verbatim upstream", srcPath)
			}
			want, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read pinned fixture %s: %v", fixturePath, err)
			}
			if got := belowHeaderRegion(t, string(src)); got != string(want) {
				t.Fatalf("%s drifted from pinned upstream %s:\n got: %q\nwant: %q", srcPath, fixturePath, got, string(want))
			}
		})
	}
}
