package dashboard

import (
	"github.com/glebglazov/pop/debug"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/work"
)

// The launch half of Pane work attribution (ADR-0201): the pane facts are read
// once, here, and handed to every snapshot build that pins rows from them —
// either page's, at entry and on every rebuild. Nothing downstream reads tmux
// again; the ladder that consumes them runs kind-side, where there is no tmux to
// reach.

// LaunchPaneFacts reads the pane the dashboard is being opened from. Outside
// tmux, with no tmux wired, or when the read fails it answers zero facts: a
// dashboard that cannot tell which pane it is in pins nothing and says nothing,
// which is the same silence an unrelated shell gets.
func LaunchPaneFacts(t tmuxmod.Tmux) work.PaneFacts {
	if t == nil {
		return work.PaneFacts{}
	}
	facts, err := t.CurrentPaneFacts()
	if err != nil {
		debug.Error("dashboard.LaunchPaneFacts: %v", err)
		return work.PaneFacts{}
	}
	return work.PaneFacts{
		PaneID:    facts.PaneID,
		Session:   facts.Session,
		Directory: facts.Directory,
		Set:       facts.Tag(tmuxmod.TagSet),
		Verify:    facts.Tag(tmuxmod.TagVerify),
		Fold:      facts.Tag(tmuxmod.TagFold),
		Assist:    facts.Tag(tmuxmod.TagAssist),
		Ticket:    facts.Tag(tmuxmod.TagTicket),
		Routine:   facts.Tag(tmuxmod.TagRoutine),
		WorkKind:  facts.WorkKind,
		WorkID:    facts.WorkID,
	}
}
