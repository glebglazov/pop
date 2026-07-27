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
// generic Command escape hatch. Higher-level composites (Ensure, Attach,
// SwitchTarget) are package functions over these primitives — see
// lifecycle.go.
type Tmux interface {
	// Sessions lists live tmux sessions with their last-activity timestamps.
	Sessions() ([]SessionActivity, error)
	// HasSession reports whether a session named name exists.
	HasSession(name string) bool
	// NewSession creates a new detached session named name rooted at dir.
	NewSession(name, dir string) error
	// SwitchClient switches the attached client to target (used inside tmux).
	SwitchClient(target string) error
	// AttachSession attaches the terminal to target (used outside tmux); it
	// wires the process's stdio to the tmux client.
	AttachSession(target string) error
	// KillSession kills the session named name.
	KillSession(name string) error
	// InTmux reports whether the caller is running inside a tmux client.
	InTmux() bool

	// PaneInfo reads a pane's session name and current foreground command.
	PaneInfo(paneID string) (PaneInfo, error)
	// PaneSession resolves just the session name owning a pane.
	PaneSession(paneID string) (string, error)
	// IsActivePane reports whether the pane is the attended pane (pane active
	// + window active + session attached). Lookup failure reports false.
	IsActivePane(paneID string) bool
	// LivePanes lists every live pane id across all sessions (liveness poll).
	LivePanes() ([]string, error)

	// InstallHook appends a global tmux hook for event.
	InstallHook(event, command string) error
	// GlobalHooks reads the installed global hooks, parsed into typed entries.
	GlobalHooks() ([]Hook, error)
	// UninstallHook removes the global hook at an indexed selector ("event[N]").
	UninstallHook(indexed string) error
}

// realTmux implements Tmux against the tmux binary via the runner seam.
type realTmux struct {
	run runner
}

// New returns a Tmux backed by the real tmux binary.
func New() Tmux {
	return &realTmux{run: execRunner{}}
}
