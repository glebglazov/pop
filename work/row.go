// Package work is the data core of the Work dashboard (ADR-0143): the row and
// snapshot types, the ADR-0121 sort tiers/bands/status order, the shared
// Done-inclusion row filter, and the unstyled cell composition (status
// label/cell, live indicator, worktree/destination label). It imports neither
// bubbletea nor lipgloss — the styled render layer lives queue-side and consumes
// this core (a guard test enforces the boundary). Snapshot *building* (the store
// read and repo scanning) stays in queue for now; queue assembles rows and work
// derives from them.
package work

import "github.com/glebglazov/pop/tasks"

// SetRef holds the resolved, fork-free coordinates of one registered Task set
// that the Queue write-path acts on, plus the per-build derived facts the
// write-path branches on. Nothing re-resolves these; they are carried,
// honoring the fork-free build (ADR-0060).
type SetRef struct {
	DefPath, StatePath, SetID string
	RepoKey, RepoCommonDir    string
	ProjectPath, RuntimePath  string
	// ProjectName is the pre-resolved project label (dashboardRepoStatic.projectName),
	// carried so the adopt path can skip DetectProject's per-project git fan-out
	// (ADR-0060).
	ProjectName string
	// Parked is true when the set's repeated abnormal terminals have parked it
	// (derived from Drain history); unpark writes a park-clear event (ADR-0055).
	// Bound is true when the set holds a Worktree binding with a non-blank
	// runtime path — the dedicated-checkout fact the action menu gates unbind
	// on. Derived per-build from the binding snapshot (no git fork), mirroring
	// dashboardSetBound.
	// Orphaned is true when the set's Worktree binding points at a checkout
	// that no longer exists on disk. Like Picked-up, it is a derived per-build
	// fact (a cheap filesystem stat, never a git fork), not a persisted
	// status, and is orthogonal to Task-set status — a set of any status may
	// be orphaned. A set with no binding can never be orphaned.
	Parked, Bound, Orphaned bool
	AutoDrain               bool
	// ConfigError is the message for a config-class defect that keeps the set
	// from routing to an integration target — a bare repo with no declared trunk
	// or an unsatisfiable worktree directive (ADR-0059/0060). Non-blank only when
	// the set is neither live-drained nor parked, preserving the mutual exclusion
	// the retired single-string DRAIN cell enforced. Rendered as the plain
	// ` · config error: <msg>` STATUS suffix (ADR-0111).
	ConfigError string
	// RawStatus is the underlying derived Task-set status, kept for counts and
	// comparisons so display relabels never leak into logic.
	RawStatus tasks.TaskSetStatus
	// DoneStillManagedBound is true when a Done set still holds a
	// pop-provisioned (managed) Worktree binding. The dashboard keeps such a
	// row visible as a clean-up reminder until archived or unbound (ADR-0070).
	DoneStillManagedBound bool
	// PaneID is the tmux pane recorded for a live drain of this set, empty if
	// none was recorded. It is the fact PreviewDrain branches on.
	PaneID string
	// LiveDrain is true when a live (PID-alive) Runtime execution lock holds
	// this set's checkout — the structured fact that replaced the retired DRAIN
	// column (ADR-0111). It lights the trailing ● live-drain indicator across every
	// status, and drives Sort's running tier, the header "N running" count, the
	// auto-drain suffix silencing (ADR-0108), and the READY→IN PROGRESS
	// refinement. Derived per-build from the live-drain snapshot, never a git fork.
	LiveDrain bool
}

// Row is one read-only Work dashboard table row.
type Row struct {
	SetRef

	Project string
	// Started mirrors tasks.Row.Started: a started READY set renders as
	// "IN PROGRESS". It is a presentational input to the render-time STATUS
	// composition (StatusCell), never a schedulability fact — logic keys on
	// RawStatus.
	Started bool
	// VerifiedAtSHA mirrors tasks.Row.VerifiedAtSHA: the short SHA of the episode's
	// PASS verdict when the set is terminal and cleared. DeriveVerifiedAtBadge maps
	// it with VerifiedAtDrifted to badge text for the STATUS cell (ADR-0156).
	VerifiedAtSHA string
	// VerifiedAtDrifted mirrors tasks.Row.VerifiedAtDrifted: HEAD has moved past
	// the PASS SHA on VerifiedAtSHA.
	VerifiedAtDrifted bool
	Worktree      string

	// IsMap marks a Wayfinder Map row (ADR-0130). Map rows reuse SetID for the
	// map id and leave Worktree blank; queue verbs (a/b/U) are inert on them.
	IsMap bool
	// MapOpen and MapFrontier are ticket tallies for map-row STATUS cells
	// (`WAYFINDING · N open / M frontier`). Zero on Task-set rows.
	MapOpen, MapFrontier int

	// CursorKey is the stable per-row identity the TUI pins cursor memory to
	// across refreshes. It is queue's navigation seam, carried on the row so the
	// model can restore the cursor by key after a rebuild; blank on rows built
	// outside the dashboard.
	CursorKey string
	// DestKind selects how the destination column is styled; Worktree holds the
	// plain label (branch name, "[managed wt]", or "needs bind"). It is the
	// style-selection fact the queue-side wrappers read (ADR-0143).
	DestKind DestKind
}

// Snapshot is the data model for `pop work dashboard`.
type Snapshot struct {
	Rows []Row
}

// DestKind selects how the WORKTREE destination column is styled. The plain
// label lives on Row.Worktree; the styled wrapper lives queue-side (ADR-0143).
type DestKind int

const (
	DestBound DestKind = iota
	DestManagedDirective
	DestNeedsBind
	DestDoneManagedBound
)

// Destination-column plain labels. WorktreeLabel composes the unstyled cell from
// these; the styled variants that colour them stay queue-side.
const (
	DestLabelManagedWt = "[managed wt]"
	DestLabelNeedsBind = "needs bind"
)
