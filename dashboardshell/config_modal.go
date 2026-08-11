package dashboardshell

import (
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/confighost"
	"github.com/glebglazov/pop/ui"
)

// The Config dashboard as a modal over whichever page is showing (ADR-0202
// decisions 10, 11 and 14). The Work dashboard is the first of the component's
// three hosts, and the seam it must reuse is written down in confighost's
// package doc — read that before adding the next one.
//
// The modal is hosted here rather than on a page because both halves of the
// contract are the shell's to keep. `v` is the shell's own key, so a modal a
// page owned could still be paged out from under itself; and config is loaded
// once here and handed to every page, so the shell is the only place that can
// re-read after a write and hand the new value on.

// openConfigModal opens the Config dashboard over the page in focus. Nothing
// about the page changes: it keeps its cursor, its filter and its poll, and is
// exactly where it was when the modal closes.
func (s Shell) openConfigModal() Shell {
	s.configModal = s.buildConfigModal()
	s.configModal.SetSize(s.width, s.height)
	return s
}

func (s Shell) buildConfigModal() *ui.ConfigDashboard {
	if s.openConfig != nil {
		return s.openConfig()
	}
	return confighost.Open(config.DefaultDeps(), s.cfgPath)
}

// updateConfigModal drives the open modal. Its tea.Quit is how it says it is
// closed, and it is dropped here: a component that ends the host program would
// make every host's quit key a trapdoor.
func (s Shell) updateConfigModal(msg tea.Msg) (Shell, tea.Cmd) {
	updated, cmd := s.configModal.Update(msg)
	if m, ok := updated.(*ui.ConfigDashboard); ok {
		s.configModal = m
	}
	if !s.configModal.Done() {
		return s, cmd
	}
	wrote := s.configModal.Wrote()
	s.configModal = nil
	if wrote {
		s = s.reloadPagesConfig()
	}
	return s, nil
}

// reloadPagesConfig re-reads and re-merges config after a write, and hands the
// result to every page that exists. Without it the shell would keep rendering
// the value the human has just changed: it loads config once in newShell and
// each page holds that value for its renders and for the kinds its next poll
// builds. This is the only hot reload the design has (ADR-0202 decision 14) —
// the supervisor already re-reads every pass, each drain it spawns is a fresh
// process, and an in-flight drain finishes on the list it started with.
func (s Shell) reloadPagesConfig() Shell {
	cfg, err := s.reloadedConfig()
	if err == nil {
		s.cfg = cfg
	}
	for id, page := range s.pages {
		s.pages[id] = page.AfterConfigReload(cfg, err)
	}
	return s
}

func (s Shell) reloadedConfig() (*config.Config, error) {
	if s.reloadConfig != nil {
		return s.reloadConfig()
	}
	if s.d != nil && s.d.LoadConfig != nil {
		return s.d.LoadConfig(s.cfgPath)
	}
	return config.Load(s.cfgPath)
}
