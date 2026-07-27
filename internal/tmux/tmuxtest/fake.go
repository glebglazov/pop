// Package tmuxtest provides the one shared, stateful in-memory fake for the
// tmux.Tmux interface. Consumer tests arrange in-memory state (sessions,
// and later windows/panes/options) and assert on that state — they never
// assert on tmux argument arrays. Func-field overrides exist only to inject
// failures. The fake grows one verb at a time alongside the module.
package tmuxtest

import (
	"fmt"

	"github.com/glebglazov/pop/internal/tmux"
)

// Fake is an in-memory tmux.Tmux.
type Fake struct {
	// SessionList is the arranged live-session state returned by Sessions.
	SessionList []tmux.SessionActivity

	// SessionsFunc, when set, replaces Sessions entirely — used to inject
	// failures.
	SessionsFunc func() ([]tmux.SessionActivity, error)

	// Live is the set of existing sessions (name -> creation dir). HasSession
	// reads it, NewSession adds to it, KillSession removes from it. Tests
	// arrange it to model pre-existing sessions and assert on it afterwards.
	Live map[string]string

	// Inside is the arranged inside-tmux state returned by InTmux; it drives
	// SwitchTarget's switch-vs-attach choice.
	Inside bool

	// Switched, Attached and Killed record verb targets in call order so
	// tests can assert which path ran.
	Switched []string
	Attached []string
	Killed   []string

	// PaneInfos maps pane id -> its session/command. PaneInfo and PaneSession
	// read it; an absent pane yields an error (matching tmux for a dead pane).
	PaneInfos map[string]tmux.PaneInfo

	// ActivePanes marks which panes IsActivePane reports as the attended pane.
	ActivePanes map[string]bool

	// LivePaneIDs is the set of pane ids returned by LivePanes.
	LivePaneIDs []string

	// InstalledHooks is the global-hook state: InstallHook appends, GlobalHooks
	// reads, UninstallHook removes by Index. Tests arrange and assert on it.
	InstalledHooks []tmux.Hook

	// Failure-injection overrides. When set, each replaces its verb entirely.
	NewSessionFunc    func(name, dir string) error
	SwitchClientFunc  func(target string) error
	AttachSessionFunc func(target string) error
	KillSessionFunc   func(name string) error
	PaneInfoFunc      func(paneID string) (tmux.PaneInfo, error)
	LivePanesFunc     func() ([]string, error)
}

var _ tmux.Tmux = (*Fake)(nil)

func (f *Fake) Sessions() ([]tmux.SessionActivity, error) {
	if f.SessionsFunc != nil {
		return f.SessionsFunc()
	}
	return f.SessionList, nil
}

func (f *Fake) HasSession(name string) bool {
	_, ok := f.Live[name]
	return ok
}

func (f *Fake) NewSession(name, dir string) error {
	if f.NewSessionFunc != nil {
		return f.NewSessionFunc(name, dir)
	}
	if f.Live == nil {
		f.Live = map[string]string{}
	}
	f.Live[name] = dir
	return nil
}

func (f *Fake) SwitchClient(target string) error {
	if f.SwitchClientFunc != nil {
		return f.SwitchClientFunc(target)
	}
	f.Switched = append(f.Switched, target)
	return nil
}

func (f *Fake) AttachSession(target string) error {
	if f.AttachSessionFunc != nil {
		return f.AttachSessionFunc(target)
	}
	f.Attached = append(f.Attached, target)
	return nil
}

func (f *Fake) KillSession(name string) error {
	if f.KillSessionFunc != nil {
		return f.KillSessionFunc(name)
	}
	f.Killed = append(f.Killed, name)
	delete(f.Live, name)
	return nil
}

func (f *Fake) InTmux() bool { return f.Inside }

func (f *Fake) PaneInfo(paneID string) (tmux.PaneInfo, error) {
	if f.PaneInfoFunc != nil {
		return f.PaneInfoFunc(paneID)
	}
	info, ok := f.PaneInfos[paneID]
	if !ok {
		return tmux.PaneInfo{}, fmt.Errorf("pane not found: %s", paneID)
	}
	return info, nil
}

func (f *Fake) PaneSession(paneID string) (string, error) {
	info, err := f.PaneInfo(paneID)
	if err != nil {
		return "", err
	}
	return info.Session, nil
}

func (f *Fake) IsActivePane(paneID string) bool {
	return f.ActivePanes[paneID]
}

func (f *Fake) LivePanes() ([]string, error) {
	if f.LivePanesFunc != nil {
		return f.LivePanesFunc()
	}
	return f.LivePaneIDs, nil
}

func (f *Fake) InstallHook(event, command string) error {
	f.InstalledHooks = append(f.InstalledHooks, tmux.Hook{Index: event, Command: command})
	return nil
}

func (f *Fake) GlobalHooks() ([]tmux.Hook, error) {
	return f.InstalledHooks, nil
}

func (f *Fake) UninstallHook(indexed string) error {
	kept := f.InstalledHooks[:0]
	for _, h := range f.InstalledHooks {
		if h.Index != indexed {
			kept = append(kept, h)
		}
	}
	f.InstalledHooks = kept
	return nil
}
