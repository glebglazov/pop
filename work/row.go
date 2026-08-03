// Package work is the Work seam: one `Kind` interface that every Work kind
// complies with, the plain data structs its methods pass around (Container,
// Item, Action, Outcome, Section), and the snapshot builder that walks a wired
// list of kinds. It imports no kind package — adapters live kind-side and `cmd`
// wires them — and it imports neither bubbletea nor lipgloss, so the styled
// render layer stays TUI-side (ADR-0143); guard tests enforce both boundaries.
//
// The transitional legacy row model (the Row alias, SetRef, and the unstyled
// cell composition around them) still lives here, folded into Container, so the
// consumers the contract slices have yet to migrate keep compiling.
package work

import (
	"time"
)

// SetStatus is the status vocabulary the legacy Row model carries. It lives here
// only because Row does: `tasks.TaskSetStatus` is an alias of it, so the Task-set
// kind keeps its own name and its own constants and nothing in the seam ever
// names a task-set status. The alias is what lets `work` hold the transitional
// row model without importing the kind that owns the vocabulary; type and alias
// both die with Row in the contract slices.
type SetStatus string

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
	RawStatus SetStatus
	// DoneStillManagedBound is true when a Done set still holds a
	// pop-provisioned (managed) Worktree binding. The dashboard keeps such a
	// row visible as a clean-up reminder until archived or unbound (ADR-0070).
	DoneStillManagedBound bool
	// PaneID is the tmux pane recorded for a drain of this set, empty if none
	// was recorded. Audit/bookkeeping only — live-pane affordance reads tmux
	// (ADR-0158), not this store field.
	PaneID string
	// LiveDrain is true when a live (PID-alive) Runtime execution lock holds
	// this set's checkout — the structured fact that replaced the retired DRAIN
	// column (ADR-0111). It lights the trailing ● live-drain indicator across every
	// status, and drives Sort's running tier, the header "N running" count, the
	// auto-drain suffix silencing (ADR-0108), and the READY→IN PROGRESS
	// refinement. Derived per-build from the live-drain snapshot, never a git fork.
	LiveDrain bool
}

// Row is the Work dashboard row, which is now the Work container itself: the
// transitional Task-set-only cells were absorbed into Container rather than kept
// beside it, so a row is a container and there is no second model to keep in
// step. The alias is what lets the consumers the contract slices have yet to
// migrate keep saying "row"; it dies with them.
type Row = Container

// Snapshot is the data model for `pop work dashboard`.
type Snapshot struct {
	// Containers are every loaded Work container in snapshot order — kind
	// precedence, then each kind's own comparator.
	Containers []Container
	// Rows is the same slice under the row model's name, for the consumers that
	// still speak in rows.
	Rows []Row
	// Summary is every kind's header phrases in kind order, already pluralised.
	// SummaryLine joins them.
	Summary []string
	// ModelSkips are the Effort model skips in force at build time (ADR-0168),
	// ordered by preset then model. They are machine-global rather than per-row,
	// which is why they ride the snapshot and render as a footer one-liner rather
	// than as a row cell. Empty is the steady state.
	ModelSkips []ModelSkip
}

// ModelSkip is one Effort model skip still in force: the preset whose ladder
// entry pop is walking past, the `--model` token that entry pins, and when the
// skip lifts. A zero Until is a permanent skip (ADR-0168).
type ModelSkip struct {
	Preset string
	Model  string
	Until  time.Time
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
