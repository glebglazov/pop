package confighost

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/ui"
)

// The Config dashboard's convention rows (ADR-0212 decision 8). A Convention
// kind sits in the same list as a config leaf because the right pane answers the
// same question for both — what is in force here, and what produced it — and a
// kind answering in prose rather than in TOML is a difference of medium, not of
// question.
//
// Everything about a convention is resolved and rendered in `conventions`; this
// file decides only which rows exist. It writes nothing: a convention row is a
// read-only preview naming the documents a write would land in, because a
// convention is a document rather than a value, and the human's two writable
// ranks differ in reach — so one edit key could not serve both without hiding
// the more authoritative one behind a keystroke nobody discovers (ADR-0226).

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
		_, overlaid := stack.Overlay()
		rows = append(rows, ui.ConfigDashboardRow{
			Key:  conventions.RowKey(stack.Kind),
			Desc: stack.Kind.Desc(),
			// The marker means the human's own statement is in force, which for a
			// convention is the overlay.
			Overridden: overlaid,
			// A kind resolves to one answer, so a second rank holding something is a
			// first one quietly losing — the state that marker and that sort exist to
			// report (ADR-0223).
			Contested: stack.Contested(),
			Preview: ui.ConfigDashboardPreview{
				Layers: conventions.StackPreview(stack),
			},
		})
	}
	return rows
}
