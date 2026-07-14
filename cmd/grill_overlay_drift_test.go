package cmd

import (
	"os"
	"strings"
	"testing"
)

// grillOverlayPinnedFiles maps each marked-overlay source (in skillFiles) to
// the vendored domain-modeling fixture its above-marker region must stay
// byte-identical to. Per ADR-0112, drift review reduces to diffing this
// region against the pinned domain-modeling@391a2701 source; this test makes
// that diff mechanical instead of archaeological.
var grillOverlayPinnedFiles = map[string]string{
	"skills/pop/grill-with-docs/CONTEXT-FORMAT.md": "testdata/domain-modeling-pin/CONTEXT-FORMAT.md",
	"skills/pop/grill-with-docs/ADR-FORMAT.md":     "testdata/domain-modeling-pin/ADR-FORMAT.md",
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

// TestGrillOverlayBaseMatchesPin operationalizes ADR-0112: it extracts the
// above-"POP OVERLAY"-marker region of each overlaid skill doc and asserts it
// is byte-identical to the vendored domain-modeling base at the pinned ref
// recorded in the file's provenance header. A pin bump or an accidental edit
// to the base region fails this test instead of requiring manual diffing.
func TestGrillOverlayBaseMatchesPin(t *testing.T) {
	for srcPath, fixturePath := range grillOverlayPinnedFiles {
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
				t.Fatalf("%s above-marker region drifted from pinned domain-modeling base %s:\n got: %q\nwant: %q", srcPath, fixturePath, got, string(want))
			}
		})
	}
}
