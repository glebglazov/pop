package dashboard

import (
	"strings"

	"github.com/glebglazov/pop/debug"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// The launch half of Pane work attribution (ADR-0201): the pane facts are read
// once, here, and handed to every snapshot build that lifts rows from them —
// either page's, at entry and on every rebuild. Nothing downstream reads tmux or
// git again; the ladder that consumes them runs kind-side, where there is neither
// to reach.

// LaunchPaneFacts reads the pane the dashboard is being opened from: one tmux
// round-trip for the pane's own coordinates and tags, and one git fork for the
// repository it stands in. Outside tmux, with no tmux wired, or when the read
// fails it answers zero facts: a dashboard that cannot tell which pane it is in
// lifts nothing and says nothing, which is the same silence an unrelated shell
// gets.
func LaunchPaneFacts(t tmuxmod.Tmux, td *tasks.Deps) work.PaneFacts {
	if t == nil {
		return work.PaneFacts{}
	}
	facts, err := t.CurrentPaneFacts()
	if err != nil {
		debug.Error("dashboard.LaunchPaneFacts: %v", err)
		return work.PaneFacts{}
	}
	return work.PaneFacts{
		PaneID:        facts.PaneID,
		Session:       facts.Session,
		Directory:     facts.Directory,
		Set:           facts.Tag(tmuxmod.TagSet),
		Verify:        facts.Tag(tmuxmod.TagVerify),
		Fold:          facts.Tag(tmuxmod.TagFold),
		Assist:        facts.Tag(tmuxmod.TagAssist),
		Ticket:        facts.Tag(tmuxmod.TagTicket),
		Routine:       facts.Tag(tmuxmod.TagRoutine),
		WorkKind:      facts.WorkKind,
		WorkID:        facts.WorkID,
		RepoCommonDir: paneRepository(td, facts.Directory),
	}
}

// paneRepository resolves the pane's repository to the identity Task storage is
// keyed under, by the same derivation every other repository read uses rather
// than a second one that could disagree with it (ADR-0241 decision 4).
//
// It forks git exactly once, here, and never during a snapshot build: the memo a
// build holds lives for one load, the dashboard rebuilds every two seconds, and
// the answer cannot change — the pane's directory is read once and never
// re-read (decision 5).
//
// A pane standing outside any repository resolves nothing. That is the ordinary
// shell, not a fault, so it is not reported: the pass above it is silent for the
// same directory and says nothing either.
func paneRepository(td *tasks.Deps, dir string) string {
	if td == nil || td.Git == nil || td.FS == nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	id, err := tasks.ResolveRepositoryIdentity(td, dir)
	if err != nil {
		return ""
	}
	return id.CommonDir
}
