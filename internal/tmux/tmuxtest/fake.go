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
}

var _ tmux.Tmux = (*Fake)(nil)

func (f *Fake) Sessions() ([]tmux.SessionActivity, error) {
	if f.SessionsFunc != nil {
		return f.SessionsFunc()
	}
	return f.SessionList, nil
}
