package conventions

import (
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// An Overlay is the layer that appends: prose that rides along with whichever
// document answered rather than displacing one (ADR-0247 decisions 3 and 4). It
// is keyed on a *named document* — a Convention kind, or a step prompt such as
// `refine` — and exists at two ranks that both append, neither replacing:
//
//	~/.agents/docs/<name>.overlay.md     the human's (OriginOverlay)
//	docs/agents/<name>.overlay.md        the team's, in version control
//	                                     (OriginRepositoryOverlay)
//
// The human's is what an *override* is for a convention (ADR-0212 decision 2)
// and what the Config dashboard's write note points at; the repository rank is
// what makes "not overridable" principled rather than lossy for a step whose
// procedure is pop's. A whitespace-only body is refused rather than written at
// either rank, a file reading as an absent layer being worse than no file.

// overlayPathIn is the human's overlay under a documents directory. The stack
// derives it from roots it already holds and the write side derives it from the
// home directory; spelling it once is what keeps the layer pop edits the layer
// pop reads.
func overlayPathIn(agentsDocs, name string) string {
	return filepath.Join(agentsDocs, name+".overlay.md")
}

// repositoryOverlayPathIn is the team's overlay under a checkout.
func repositoryOverlayPathIn(topLevel, name string) string {
	return filepath.Join(topLevel, "docs", "agents", name+".overlay.md")
}

// OverlayPath returns where the human's Overlay for name lives.
func OverlayPath(d *Deps, name string) (string, error) {
	home, err := d.fs().UserHomeDir()
	if err != nil {
		return "", err
	}
	return overlayPathIn(filepath.Join(home, ".agents", "docs"), name), nil
}

// RepositoryOverlayPath returns where the team's Overlay for name lives in the
// repository owning cwd.
func RepositoryOverlayPath(d *Deps, name, cwd string) (string, error) {
	topLevel, err := tasks.NormalizeProjectPathWith(d.tasksDeps(), cwd)
	if err != nil {
		return "", err
	}
	return repositoryOverlayPathIn(topLevel, name), nil
}

// Overlay reads the human's overlay for name on its own, through the same reader
// Resolve uses, so an editor opens exactly the prose the stack prints — and an
// absent overlay opens empty rather than seeded with a layer the human never
// wrote.
func Overlay(d *Deps, name string) (Layer, error) {
	path, err := OverlayPath(d, name)
	if err != nil {
		return Layer{}, err
	}
	layer := Layer{Origin: OriginOverlay, Path: path}
	readLayer(d, &layer)
	return layer, nil
}

// overlayAppendOrder is the order overlays are appended beneath an answer: the
// human's first, then the team's. Both append; neither replaces (ADR-0247).
var overlayAppendOrder = []Origin{OriginOverlay, OriginRepositoryOverlay}

// ResolveOverlays reads every Overlay rank for a named document in the
// repository owning cwd. A name need not be a Convention kind — `refine` is the
// reason the key is a string — and ranks that hold nothing are omitted, so a
// caller that asks "is there an overlay" reads an empty slice rather than a
// stack of absences.
func ResolveOverlays(d *Deps, name, cwd string) ([]Layer, error) {
	roots, err := resolveStackRoots(d, cwd)
	if err != nil {
		return nil, err
	}
	candidates := []Layer{
		{Origin: OriginOverlay, Path: overlayPathIn(roots.agentsDocs, name)},
		{Origin: OriginRepositoryOverlay, Path: repositoryOverlayPathIn(roots.topLevel, name)},
	}
	present := make([]Layer, 0, len(candidates))
	for i := range candidates {
		readLayer(d, &candidates[i])
		if candidates[i].Present {
			present = append(present, candidates[i])
		}
	}
	return present, nil
}

// OverlayProse renders the Overlay ranks a prompt or a reader is handed: each
// present layer as an APPENDED block labelled with its origin and reach. An
// empty slice yields the empty string, which is how a prompt seam that found
// nothing stays silent.
func OverlayProse(layers []Layer) string {
	if len(layers) == 0 {
		return ""
	}
	var b strings.Builder
	for i, overlay := range layers {
		if i > 0 {
			b.WriteByte('\n')
		}
		appendOverlayBlock(&b, overlay)
	}
	return b.String()
}

// appendOverlayBlock writes one Overlay's labelled block.
func appendOverlayBlock(b *strings.Builder, overlay Layer) {
	b.WriteString("----- APPENDED: ")
	b.WriteString(strings.ToUpper(string(overlay.Origin)))
	b.WriteString(" (")
	b.WriteString(overlay.Origin.Scope())
	b.WriteString(") -----\n")
	b.WriteString(overlay.Path)
	b.WriteString("\n\n")
	b.WriteString(overlay.Body)
	b.WriteByte('\n')
}
