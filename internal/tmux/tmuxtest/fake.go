// Package tmuxtest provides the one shared, stateful in-memory fake for the
// tmux.Tmux interface. Consumer tests arrange in-memory state (sessions,
// and later windows/panes/options) and assert on that state — they never
// assert on tmux argument arrays. Func-field overrides exist only to inject
// failures. The fake grows one verb at a time alongside the module.
package tmuxtest

import "github.com/glebglazov/pop/internal/tmux"

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

	// Failure-injection overrides. When set, each replaces its verb entirely.
	NewSessionFunc    func(name, dir string) error
	SwitchClientFunc  func(target string) error
	AttachSessionFunc func(target string) error
	KillSessionFunc   func(name string) error
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
