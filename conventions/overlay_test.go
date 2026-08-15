package conventions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// The Convention overlay is the layer an editing surface writes (ADR-0212
// decision 2), so it has to be reachable on its own: read, written and removed
// without resolving the rest of the stack, and — being the human's own file,
// global to every repository — without a repository at all.

func overlayDeps(t *testing.T) (*Deps, string) {
	t.Helper()
	home := t.TempDir()
	real := deps.NewRealFileSystem()
	return &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		ReadFileFunc:    real.ReadFile,
		WriteFileFunc:   real.WriteFile,
		MkdirAllFunc:    real.MkdirAll,
		StatFunc:        real.Stat,
		RemoveAllFunc:   real.RemoveAll,
	}}, home
}

func TestOverlayRoundTripsThroughTheLayerTheStackReads(t *testing.T) {
	d, home := overlayDeps(t)

	if layer, err := Overlay(d, KindCommits); err != nil || layer.Present {
		t.Fatalf("Overlay() = %+v, %v, want an absent layer before anything is written", layer, err)
	}
	if err := SetOverlay(d, KindCommits, "\nNever mention pop in a subject.\n"); err != nil {
		t.Fatalf("SetOverlay() error: %v", err)
	}

	want := filepath.Join(home, ".agents", "docs", "commits.overlay.md")
	layer, err := Overlay(d, KindCommits)
	if err != nil {
		t.Fatalf("Overlay() error: %v", err)
	}
	if layer.Path != want || !layer.Present || layer.Body != "Never mention pop in a subject." {
		t.Fatalf("Overlay() = %+v, want the written prose at %s", layer, want)
	}
	// The file is the human's own document, so it holds their prose and none of
	// pop's bookkeeping: the frontmatter exists to record what *pop* derived.
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	if strings.HasPrefix(string(raw), "---") {
		t.Errorf("the overlay carries frontmatter it has no derivation for:\n%s", raw)
	}

	if err := ClearOverlay(d, KindCommits); err != nil {
		t.Fatalf("ClearOverlay() error: %v", err)
	}
	if layer, _ := Overlay(d, KindCommits); layer.Present {
		t.Errorf("Overlay() = %+v after a clear, want it gone", layer)
	}
	// Nothing to remove is the state the caller asked for, already holding.
	if err := ClearOverlay(d, KindCommits); err != nil {
		t.Errorf("ClearOverlay() on an absent overlay = %v, want no failure", err)
	}
}

// A body that reads as an absent layer is refused rather than written: accepting
// it would leave the human believing they had stated something pop will never
// print.
func TestSetOverlayRefusesAnEmptyBody(t *testing.T) {
	d, home := overlayDeps(t)

	if err := SetOverlay(d, KindCommits, "  \n\t\n"); !errors.Is(err, ErrEmptyConvention) {
		t.Fatalf("SetOverlay() = %v, want ErrEmptyConvention", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "docs", "commits.overlay.md")); !os.IsNotExist(err) {
		t.Error("a refused body still produced a file")
	}
}
