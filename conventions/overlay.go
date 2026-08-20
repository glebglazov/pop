package conventions

import (
	"path/filepath"
)

// The Convention overlay is the layer that appends: the human's own prose, which
// rides along with whichever rank answered rather than displacing one, so it
// survives whatever a repository says (ADR-0211, ADR-0223 decision 3). This
// file is where it is found, and it exists because ADR-0212 decision 2 names it what an override *is*
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
