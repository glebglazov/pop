package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/ui"
)

// The Document peek renders in the Terminal appearance in force, and caches on
// it as well as on the width and the document's own text (ADR-0230, ADR-0242). That is the whole of its part in
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

	// No key moved, so the peek must not render again. Planting a sentinel in the
	// cache behind it is how that is observed.
	peek.rendered = "the cached rendering"
	if again := peek.body(80, ui.AppearanceDark); again != "the cached rendering" {
		t.Fatalf("the peek re-rendered at an unchanged width, appearance and text:\n%q", again)
	}

	// The text is a cache key too, so a document that changed on disk under an
	// open peek is never read through the previous text's rendering
	// (ADR-0242 decision 6).
	peek.text = "# A completely different document\n"
	edited := peek.body(80, ui.AppearanceDark)
	if !strings.Contains(edited, "completely different") || strings.Contains(edited, "Heading") {
		t.Fatalf("the peek kept the previous text's rendering:\n%q", edited)
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
