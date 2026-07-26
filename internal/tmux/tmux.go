// Package tmux is the one home for tmux knowledge in pop: subcommand and
// format-string construction, output parsing into typed values, error
// mapping, and pop's own @pop_* option semantics. Callers depend on the
// Tmux interface and its typed verbs, never on the tmux CLI directly —
// no "name\tactivity" strings cross this boundary.
package tmux

// SessionActivity is a live tmux session with its last-activity timestamp
// (tmux's #{session_activity}, a unix time in seconds).
type SessionActivity struct {
	Name     string
	Activity int64
}

// Tmux is the public surface for tmux operations. Verbs are added per
// migration slice; a consumer that needs tmux gets a named verb, never a
// generic Command escape hatch.
type Tmux interface {
	// Sessions lists live tmux sessions with their last-activity timestamps.
	Sessions() ([]SessionActivity, error)
}

// realTmux implements Tmux against the tmux binary via the runner seam.
type realTmux struct {
	run runner
}

// New returns a Tmux backed by the real tmux binary.
func New() Tmux {
	return &realTmux{run: execRunner{}}
}
