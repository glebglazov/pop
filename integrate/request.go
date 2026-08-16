package integrate

// Request carries per-invocation intent for an integrate run. Mode flags and
// component selections live here; injection closures stay on Deps.
type Request struct {
	// Agent is the target coding agent (claude, codex, pi, opencode, cursor,
	// kimi).
	Agent string

	// Components is the optional-component baseline to install alongside
	// status-wiring (pane-skills, task-skills, …). Status-wiring is always
	// implied unless CoreOnly is set.
	Components []ComponentID

	// ExplicitOptOuts names components the caller is removing and recording
	// as opted out (e.g. --no-pane-skill).
	ExplicitOptOuts map[ComponentID]bool

	// RemoveComponents is the set to remove for Remove. Empty means every
	// currently installed component for the agent.
	RemoveComponents []ComponentID

	// DryRun reports what would change without writing. Changed/Installed on
	// the returned Report reflect the dry-run probe.
	DryRun bool

	// OverwriteConflicts allows destroying unowned agent-location entries
	// that block pop's artifacts (after AssumeYes or ConfirmOverwrite).
	OverwriteConflicts bool

	// AssumeYes accepts all overwrite confirmations without prompting.
	AssumeYes bool

	// UpdateExisting selects the refresh-installed-agents path. Wired by cmd;
	// the dedicated refresh entry point lands in a later slice.
	UpdateExisting bool

	// Verbose controls outcome-line filtering when the caller renders Report.
	Verbose bool

	// CoreOnly limits the run to status-wiring only (former RunWith). Refresh
	// probes and status-wiring unit tests use this; the full integrate path
	// leaves it false.
	CoreOnly bool
}

// Report is what an integrate run did — returned from Install/Remove rather
// than mutated into Deps.
type Report struct {
	Changed     bool
	Installed   bool
	Overwritten []string
	Pruned      []string
	Outcomes    []Outcome
}
