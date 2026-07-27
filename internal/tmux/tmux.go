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

	// CurrentSession returns the session name of the client displaying the
	// caller (display-message -p #S). An empty result means no session.
	CurrentSession() (string, error)

	// --- generic windows / panes (spawn composites: EnsureWindow, EnsureTaggedPane) ---

	// WindowExists reports whether session owns a window named name.
	WindowExists(session, name string) (bool, error)
	// NewWindow creates a detached window named name in session rooted at dir
	// and returns its initial pane's id.
	NewWindow(session, name, dir string) (string, error)
	// SplitWindow splits a new pane into session's window name rooted at dir
	// and returns the new pane's id.
	SplitWindow(session, name, dir string) (string, error)
	// RetileWindow re-tiles session's window name (select-layout tiled).
	RetileWindow(session, name string) error
	// WindowPanes lists the pane ids in session's window name.
	WindowPanes(session, name string) ([]string, error)
	// SelectPane makes paneID the active pane in its window.
	SelectPane(paneID string) error

	// --- tagged panes (@pop_routine / @pop_set; the shared drain window) ---

	// TagPane sets a pane's @pop_* tag (selected by PaneTag) to value.
	TagPane(paneID string, tag PaneTag, value string) error
	// FindTaggedPane returns the pane in session's drain window tagged
	// tag=value, or "" when none (an absent window included).
	FindTaggedPane(session string, tag PaneTag, value string) (string, error)

	// --- agentic panes: the shared "agent" window (glossary: Agentic pane) ---

	// HasAgentWindow reports whether session owns the shared "agent" window.
	HasAgentWindow(session string) bool
	// AgentPanes lists the named panes in session's agent window (title + id).
	AgentPanes(session string) ([]AgentPane, error)
	// FindAgentPane resolves a pane id by title in session's agent window.
	FindAgentPane(session, title string) (string, error)
	// NewAgentWindow creates session's agent window rooted at dir and returns
	// the new pane's id (glossary: Pane ID target).
	NewAgentWindow(session, dir string) (string, error)
	// SplitAgentPane splits a new pane into session's agent window rooted at
	// dir and returns the new pane's id.
	SplitAgentPane(session, dir string) (string, error)
	// RetileAgentWindow re-tiles session's agent window (select-layout tiled).
	RetileAgentWindow(session string) error

	// --- pane-id primitives (glossary: Pane ID target) ---

	// SetPaneTitle sets a pane's title (select-pane -T).
	SetPaneTitle(paneID, title string) error
	// SetRemainOnExit toggles a pane's remain-on-exit option.
	SetRemainOnExit(paneID string, on bool) error
	// SendKeys sends literal keys to a pane (send-keys); keys are not
	// auto-terminated with Enter.
	SendKeys(paneID string, keys ...string) error
	// KillPane kills a pane.
	KillPane(paneID string) error
	// CapturePane captures a pane's content plus 50 lines of scrollback.
	CapturePane(paneID string) (string, error)
	// PaneDead reports whether a pane's process has exited. A lookup failure
	// reports false.
	PaneDead(paneID string) bool

	// --- Topic (@pop_topic / @pop_topic_kind; glossary: Topic, Topic provenance) ---

	// ReadTopic reads a pane's Topic, its provenance, and its owning session in
	// one round-trip.
	ReadTopic(paneID string) (TopicState, error)
	// SetTopic writes a pane's Topic without touching its provenance.
	SetTopic(paneID, topic string) error
	// SetTopicWithKind writes a pane's Topic and its provenance together.
	SetTopicWithKind(paneID, topic, kind string) error
	// ClearTopic clears a pane's Topic and provenance.
	ClearTopic(paneID string) error
	// PaneTopics maps every pane id carrying a non-empty Topic to its Topic
	// text, across all sessions.
	PaneTopics() (map[string]string, error)
}

// realTmux implements Tmux against the tmux binary via the runner seam.
type realTmux struct {
	run runner
}

// New returns a Tmux backed by the real tmux binary.
func New() Tmux {
	return &realTmux{run: execRunner{}}
}
