package dashboard

import (
	"fmt"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/ui"
)

// The Assist pane menu's content (ADR-0263). A Task set may hold up to nine
// assist panes, so the assist verb picks among them instead of launching: one
// line per slot the set already holds a pane on, plus `n` for another. The lines
// are derived from the live-pane poll rather than a tmux query of their own, so
// the menu opens at the speed of a keypress and its colours are the ones the
// row's own activity cluster is already showing.

// assistMenuNewKey opens another assist pane. It is a letter rather than the
// next free digit because the digits address panes: which digit `n` would take
// changes as panes come and go, and a key that moves is a key you cannot learn.
const assistMenuNewKey = "n"

// dashboardAssistMenu is the Assist pane menu opened by the assist verb over a
// Task set. Unlike the Status and Copy menus its items are the surface's own —
// a slot is a tmux fact, not kind knowledge — and what the kind's verb does now
// is open this rather than spawn.
type dashboardAssistMenu struct {
	row  DashboardRow
	list *ui.List[assistMenuEntry]
}

// assistMenuEntry is one line of the Assist pane menu: a slot that holds a pane,
// or the entry that opens one on the lowest free slot. Both halves are on one
// menu because reaching a conversation and starting one are the same gesture —
// you pressed assist because you have something to say.
type assistMenuEntry struct {
	key   string
	label string
	slot  tmuxmod.PaneSlot
	// state colours the digit by the live-pane affordance and is what makes a
	// digit respawn rather than jump (ADR-0158). The `n` entry carries none: it
	// opens a pane that does not exist yet.
	state   livePaneState
	newPane bool
}

// newDashboardAssistMenu opens the menu over row with the panes the last poll
// saw it holding. `n` is always the last line — with every slot taken as much as
// with none of them — so the keystrokes never change shape with tmux state the
// row cannot show; the full case refuses in a flash instead.
func newDashboardAssistMenu(row DashboardRow, panes []assistPane) *dashboardAssistMenu {
	entries := make([]assistMenuEntry, 0, len(panes)+1)
	for _, pane := range panes {
		entries = append(entries, assistMenuEntry{
			key:   pane.slot.String(),
			label: assistPaneLabel(pane.state),
			slot:  pane.slot,
			state: pane.state,
		})
	}
	entries = append(entries, assistMenuEntry{
		key:     assistMenuNewKey,
		label:   "open another assist pane",
		newPane: true,
	})
	return &dashboardAssistMenu{
		row:  row,
		list: ui.NewList(entries, ui.Opts[assistMenuEntry]{Wrap: true}),
	}
}

// assistPaneLabel says in words what the digit's colour says in green and grey.
// The two carry the same fact because the help overlay lists the labels with no
// colour at all, and an operator reading it must still know which digit restarts
// a session and which walks into one.
func assistPaneLabel(state livePaneState) string {
	if state == livePaneRunning {
		return "running"
	}
	return "exited · restart in place"
}

// full reports whether the set holds a pane on every addressable slot, which is
// what `n` refuses on: a tenth pane would be a session no digit could reach.
func (a *dashboardAssistMenu) full() bool {
	if a == nil {
		return false
	}
	held := 0
	for _, entry := range a.list.Items() {
		if !entry.newPane {
			held++
		}
	}
	return held >= int(tmuxmod.LastPaneSlot)
}

// assistMenuFullRefusal is what `n` answers with on a full set. It names the cap
// and the way past it, because the key did nothing and a silent key reads as a
// broken one (ADR-0236 decision 7).
func assistMenuFullRefusal() string {
	return fmt.Sprintf("all %d assist panes are open — close one to open another", tmuxmod.LastPaneSlot)
}
