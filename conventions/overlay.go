package conventions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The Convention overlay is the layer that appends: the human's own prose, which
// rides along with whichever rank answered rather than displacing one, so it
// survives whatever a repository says (ADR-0211, ADR-0223 decision 3). This file is its write
// side, and it exists because ADR-0212 decision 2 names it what an override *is*
// for a convention — prose laid over the composed stack rather than a layer
// displacing another — so the surface that overrides a config key overrides a
// convention here, in the medium a convention is written in.
//
// It is global, not per repository: a constraint the human states holds in every
// repository, which is why it is derived from the home directory alone and can
// be written from outside a checkout. A constraint meant for one project is
// stated as a whole Project convention instead, a second appending rank being
// the composition ADR-0223 removed.

// overlayPathIn is the overlay's file under a documents directory. The stack
// derives it from roots it already holds and the write side derives it from the
// home directory; spelling it once is what keeps the layer pop edits the layer
// pop reads.
func overlayPathIn(agentsDocs string, kind Kind) string {
	return filepath.Join(agentsDocs, string(kind)+".overlay.md")
}

// OverlayPath returns where the Convention overlay for kind lives.
func OverlayPath(d *Deps, kind Kind) (string, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return "", err
	}
	return overlayPathIn(filepath.Join(home, ".agents", "docs"), kind), nil
}

// Overlay reads the overlay layer for kind on its own, through the same reader
// Resolve uses, so an editor opens exactly the prose the stack prints — and an
// absent overlay opens empty rather than seeded with a layer the human never
// wrote.
func Overlay(d *Deps, kind Kind) (Layer, error) {
	path, err := OverlayPath(d, kind)
	if err != nil {
		return Layer{}, err
	}
	layer := Layer{Origin: OriginOverlay, Path: path}
	readLayer(d, &layer)
	return layer, nil
}

// SetOverlay writes body as the Convention overlay for kind, replacing whatever
// was there.
func SetOverlay(d *Deps, kind Kind, body string) error {
	body = strings.TrimSpace(body)
	// A file holding only whitespace reads as an absent layer, so writing one
	// would leave the human believing they had stated something pop will never
	// print — the same refusal every writable rank makes.
	if body == "" {
		return ErrEmptyConvention
	}
	path, err := OverlayPath(d, kind)
	if err != nil {
		return err
	}
	if err := d.fs().MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return d.fs().WriteFile(path, []byte(body+"\n"), 0o644)
}

// ClearOverlay removes the overlay for kind. Nothing to remove is a fact about
// the stack rather than a failure: the caller asked for a state, and that state
// already holds.
func ClearOverlay(d *Deps, kind Kind) error {
	path, err := OverlayPath(d, kind)
	if err != nil {
		return err
	}
	if _, err := d.fs().Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return d.fs().RemoveAll(path)
}
