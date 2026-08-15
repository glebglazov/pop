package confighost

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/ui"
)

// The Config dashboard's convention rows (ADR-0212 decision 8). A Convention
// kind sits in the same list as a config leaf because the right pane answers the
// same question for both — what is in force here, and what produced it — and
// differing only in that a key resolves to one value and a kind to a labelled
// stack is a difference of shape, not of question.
//
// Everything about a convention is resolved and rendered in `conventions`; this
// file decides only which rows exist and what the three keystrokes mean for one.
// The layer an edit here writes is the Convention overlay, because that is what
// an override *is* for a convention: prose laid over the composed stack rather
// than a layer displacing another (ADR-0212 decision 2).

// conventionsDeps derives the Repo convention seam from the config seam. The two
// read the same machine through the same filesystem, so a host that has sandboxed
// one has sandboxed both — and a host that injects neither gets the real thing.
func conventionsDeps(d *config.Deps) *conventions.Deps {
	cd := conventions.DefaultDeps()
	if d != nil && d.FS != nil {
		cd.FS = d.FS
	}
	return cd
}

// conventionRows resolves every Convention kind for the repository the dashboard
// was opened in.
//
// A checkout that resolves to no repository yields no rows rather than an error:
// a convention is a repository's, so outside one there is nothing to show, and
// the two picker hosts open wherever the human happens to be standing. It is the
// same rule the repository-scope config rows follow.
func (w Writer) conventionRows() []ui.ConfigDashboardRow {
	if w.checkout == "" {
		return nil
	}
	kinds := conventions.Kinds()
	stacks, err := conventions.ResolveAll(w.conventions, w.checkout, kinds...)
	if err != nil {
		return nil
	}
	rows := make([]ui.ConfigDashboardRow, 0, len(stacks))
	for _, stack := range stacks {
		overlay, present := overlayLayer(stack)
		rows = append(rows, ui.ConfigDashboardRow{
			Key:  conventions.RowKey(stack.Kind),
			Desc: stack.Kind.Desc(),
			// The marker means the human's own statement is in force, which for a
			// convention is the overlay. Contested it never is: the layers of a stack
			// compose, so a second one speaking is not a first one quietly losing —
			// the state that marker exists to report.
			Overridden: present,
			Preview: ui.ConfigDashboardPreview{
				Layers:   conventions.StackPreview(stack),
				EditSeed: conventionEditSeed(stack.Kind, overlay),
			},
		})
	}
	return rows
}

// overlayLayer picks the layer an edit writes out of a resolved stack.
func overlayLayer(stack conventions.Stack) (conventions.Layer, bool) {
	for _, layer := range stack.Layers {
		if layer.Origin == conventions.OriginOverlay {
			return layer, layer.Present
		}
	}
	return conventions.Layer{}, false
}

// conventionEditSeed is what $EDITOR opens on for a convention row: the overlay
// as it stands, under a note saying which of the four layers this is. The note
// is pop's own and comes back out of the buffer, so a human who writes nothing
// under it has cancelled rather than stated the note.
func conventionEditSeed(kind conventions.Kind, overlay conventions.Layer) string {
	note := ui.ConfigEditorNote(fmt.Sprintf(
		"your %s overlay — the top layer of the stack, in every repository.\n"+
			"%s\n"+
			"It composes over the layers below rather than replacing them. Leave this\n"+
			"buffer empty to change nothing; ctrl+x removes the overlay.",
		kind, overlay.Path))
	if overlay.Present {
		return note + overlay.Body + "\n"
	}
	return note
}

// storeConvention makes the edited prose the human's overlay for kind. There is
// no problem to hand back: prose has no schema to fail, and the one refusal —
// an empty body — the component already reads as a cancel before it gets here.
func (w Writer) storeConvention(kind conventions.Kind, buffer string) (string, error) {
	return "", conventions.SetOverlay(w.conventions, kind, buffer)
}

// copySourceConvention refuses. Copying the source down assumes one value below
// the override to copy, and a convention has a composed stack instead: flattening
// it into the overlay would be pop merging prose, which is the one thing this
// whole family declines to do (ADR-0211).
func copySourceConvention(kind conventions.Kind) error {
	return fmt.Errorf("the %s layers compose, so there is no single value to copy down; "+
		"press enter to write your overlay", kind)
}
