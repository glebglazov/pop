package ui

import (
	tea "charm.land/bubbletea/v2"
)

// The Config dashboard as a modal over the picker (ADR-0202 decisions 10 and
// 11). The project picker and the worktree picker are the component's second and
// third hosts; the seam they reuse is written down in confighost's package doc.
//
// Both halves of the host contract bite harder here than on the Work dashboard.
// The picker binds ctrl+x to *force delete worktree* and the component binds it
// to *remove the override*, so a picker that kept its keys live would destroy a
// checkout when the human meant to drop an override. And the picker's result is
// its stdout — `cd "$(pop worktree dashboard)"` — so neither the modal nor this
// host may print: a failure to resolve config is an error row inside the
// component's own frame.
//
// The picker takes an opener rather than config itself: config resolution lives
// in confighost, which imports this package.

// WithConfigDashboard lets the global chord open the Config dashboard over the
// picker. open is called on each press, so the component always resolves the
// layers as they stand now. Left unset, the chord does nothing and the help
// overlay does not advertise it.
func WithConfigDashboard(open func() *ConfigDashboard) PickerOption {
	return func(p *Picker) {
		p.openConfig = open
	}
}

// configDashboardBound reports whether the chord opens the component here. A
// human who bound the same chord to a command of their own keeps it: every other
// built-in key in this picker yields to a user-defined command, and a global key
// that could not be reassigned would be the only exception.
func (p *Picker) configDashboardBound() bool {
	return p.openConfig != nil && !p.isKeyOverridden(ConfigDashboardKey)
}

// openConfigModal opens the component over the list. Nothing about the picker
// changes: it keeps its filter text, its cursor and its scroll, and is exactly
// where it was when the modal closes.
func (p *Picker) openConfigModal() {
	modal := p.openConfig()
	if modal == nil {
		return
	}
	modal.SetSize(p.width, p.windowHeight)
	p.configModal = modal
}

// updateConfigModal drives the open modal. Its tea.Quit is how it says it is
// closed, and it is dropped here: a component that ended the host program would
// quit the picker with no result, which in the worktree host means an empty
// `cd` argument.
//
// The picker has nothing to re-read after a write. It builds its items before it
// runs and holds no live config, so a write shows up the next time the chord
// opens the component — the one hot reload in the design belongs to the Work
// dashboard, which does render config (ADR-0202 decision 14).
func (p *Picker) updateConfigModal(msg tea.Msg) tea.Cmd {
	updated, cmd := p.configModal.Update(msg)
	if m, ok := updated.(*ConfigDashboard); ok {
		p.configModal = m
	}
	if p.configModal.Done() {
		p.configModal = nil
		return nil
	}
	return cmd
}

// ConfigModalOpen reports whether the Config modal is showing, for tests.
func (p *Picker) ConfigModalOpen() bool { return p.configModal != nil }
