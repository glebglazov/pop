package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/ui"
)

// The Document peek renders in the Terminal appearance in force, and caches on
// it as well as on the width (ADR-0230). That is the whole of its part in
// following a theme switch live: a reader who flips their terminal from dark to
// light misses the cache on the next frame and reads the document repainted,
// while a frame where nothing moved re-uses the rendering it already has.
func TestDocumentPeekCachesOnAppearanceAsWellAsWidth(t *testing.T) {
	const document = "# Heading\n\nSome prose with a `literal` in it.\n"
	peek := &documentPeek{path: "/tmp/spec.md", text: document}

	dark := peek.body(80, ui.AppearanceDark)
	if !strings.Contains(dark, "\x1b[") {
		t.Fatalf("dark rendering carries no colour:\n%q", dark)
	}

	// Neither key moved, so the peek must not look at the text again. Changing
	// it behind the cache is how that is observed.
	peek.text = "# A completely different document\n"
	if again := peek.body(80, ui.AppearanceDark); again != dark {
		t.Fatalf("the peek re-rendered at an unchanged width and appearance:\n%q\nwant:\n%q", again, dark)
	}

	peek.text = document
	light := peek.body(80, ui.AppearanceLight)
	if light == dark {
		t.Fatal("the same document at the same width rendered identically in dark and light — the appearance is not a cache key")
	}
	if peek.renderedAppearance != ui.AppearanceLight {
		t.Fatalf("cached appearance is %v after a light render, want light", peek.renderedAppearance)
	}
}
