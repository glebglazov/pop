// Package work is the Work seam: one `Kind` interface that every Work kind
// complies with, the plain data structs its methods pass around (Container,
// Item, Action, Outcome, Section), and the snapshot builder that walks a wired
// list of kinds. It imports no kind package — adapters live kind-side and `cmd`
// wires them — and it imports neither bubbletea nor lipgloss, so the styled
// render layer stays TUI-side (ADR-0143); guard tests enforce both boundaries.
//
// There is no row model beside the container: a dashboard row is a `Container`,
// and the cells the Task-set columns show are fields on it.
package work

import (
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// KindID identifies one Work kind. It is ref's closed enum rather than a fresh
// string type: a kind is never a plugin registration, it is a deliberate edit to
// that list, and the same enum keys the `(kind, id)` Work-container registry.
type KindID = ref.Kind

// Kind is the seam every Work kind complies with. It is the whole of what `work`
// knows: a kind loads its containers, orders them among themselves, answers what
// verbs apply, performs one, and summarises a page of them. Nothing here names a
// task set, a Map or a Routine — the unification is behavioural, with no shared
// status vocabulary and no shared status taxonomy.
//
// Import direction is the property that makes this work: kinds comply and import
// `work`; `work` imports no kind (an import guard test enforces it). Adapters
// live kind-side and `cmd` constructs each with its own dependencies captured,
// handing a `[]Kind` to the snapshot builder. `work` importing every kind would
// make it a hub that grows an import per future kind, and a future kind
// consuming `work` would cycle; `init()` self-registration would hide the
// wiring. The accepted cost is an explicit wiring list in `cmd`.
type Kind interface {
	// ID is the kind's member of the closed enum.
	ID() KindID
	// Load reads every container of this kind that is worth showing, in any
	// order — the snapshot builder sorts.
	Load() ([]Container, error)
	// Less orders two of this kind's own containers. It is never asked to compare
	// across kinds: kind precedence decides that, and this comparator only ever
	// sees containers it produced.
	Less(a, b Container) bool
	// StatusCell composes one container's STATUS cell as un-styled, tone-tagged
	// segments: the kind's status label first, then whatever kind-local suffixes
	// belong beside it. It is a method rather than a container field because the
	// facts behind those suffixes are the kind's own — `work` could not compose
	// the cell if it wanted to — and because a stored copy beside a composer
	// would be a second source of truth for the same string.
	StatusCell(Container) []StatusSegment
	// Actions returns the container-level verbs that apply right now. It is called
	// when a menu opens over one container, not per container at build time, so it
	// may consult state that moved since the snapshot.
	Actions(Container) []Action
	// StatusActions returns the verbs that write this container's status, for the
	// surface that offers them behind VerbStatus. It is a second list rather than
	// part of Actions because a status write is a different act from a row verb — a
	// reader opens the submenu to change what a container *is*, not to run
	// something on it — and it is the kind's own because no two kinds share a
	// status vocabulary: a task set completes and skips tasks, a Map is abandoned
	// and reopened. A kind whose containers carry no writable status returns none
	// and offers no opener; every verb here is performed through Perform like any
	// other, so there is no second dispatch anywhere.
	StatusActions(Container) []Action
	// ItemActions returns the verbs that apply to one item of a container.
	ItemActions(Container, Item) []Action
	// Perform runs a verb. The item is nil for a container-level verb.
	Perform(Container, *Item, Verb) (Outcome, error)
	// Summary returns this kind's header phrases for the containers on a page,
	// already pluralised. The builder joins every kind's phrases with " · " in
	// kind order.
	Summary([]Container) []string
	// Columns returns the column headers a page of this kind's containers reads
	// under, in cell order. It belongs to the kind because the kind authors the
	// cells: a dashboard page takes its header from its primary kind, and a
	// non-primary kind on that page fills those same columns (a Map filling the
	// Task-set ones is the standing example). Declaring headers surface-side would
	// separate the header from the kind that fills it, and make a page for a
	// future kind cost custom dashboard code.
	Columns() []string
}

// SkipSource is the optional extension a kind implements when it carries
// machine-global footnotes that ride the snapshot rather than a container: the
// Task-set kind's Effort model skips (ADR-0168) are global to the machine, so
// they cannot be a container field. A kind that has none simply does not
// implement it — the builder type-asserts, the same way the supervisor asserts
// for an advanceable kind.
type SkipSource interface {
	ModelSkips() ([]ModelSkip, error)
}

// Container is one Work container as every consumer reads it: a task set, a Map,
// later a Routine. It is a plain data struct, not an interface — every consumer
// reads its fields to render them, so a method per cell would only add
// indirection, and `Container` as an interface would buy lazy cell computation
// nobody needs.
//
// It carries the kind's own status label and nothing shared beyond it. A shared
// status facet taxonomy was rejected as a shared vocabulary in disguise: the
// accepted cost is that an urgent Map can never outrank a DONE task set.
type Container struct {
	// Kind and ID together are the container's identity — the same pair the
	// `(kind, id)` registry is keyed on.
	Kind KindID
	ID   string
	// Project is the repository-group label the container belongs to, the label
	// every read surface groups by.
	Project string
	// Status is the kind's own status label (`READY`, `WAYFINDING`, …). `work`
	// never interprets it. The composed STATUS cell is Kind.StatusCell's, not a
	// field here.
	Status string
	// Checkout is the filesystem directory a shell or handoff verb runs in, empty
	// when the kind resolves none.
	Checkout string
	// CursorKey is the stable per-container identity a TUI pins cursor memory to
	// across rebuilds.
	CursorKey string
	// Pinned marks a container the pane this build was launched from is
	// attributed to. The builder stamps it while lifting the row to the top of
	// the snapshot, so a surface that renders the mark reads one field instead of
	// re-deriving the attribution its rows already went through (ADR-0209).
	Pinned bool
	// Broken marks a container whose definition could not be read at all, with
	// BrokenReason carrying what went wrong. A broken container still renders —
	// hiding a container because it is unreadable hides the thing that needs
	// fixing.
	Broken       bool
	BrokenReason string
	// Archived reports that the human filed this container away. It is the one
	// cross-kind status fact — every kind's archived bit is the same registry
	// bit — and it is on the container because a surface that lists archived rows
	// has to say which ones they are; a row that looked active but refused to be
	// archived again would be the whole point of the field missing.
	Archived bool
	// MutedUntil is when a live Mute on this container ends, zero when nothing
	// mutes it right now. An expired mute reads exactly like no mute at all,
	// which is the whole of how a mute ends: the kind compares the stored instant
	// against the load's `now` and carries the answer, so no sweeper and no
	// cleanup job ever writes when a mute runs out (ADR-0200 decision 1).
	MutedUntil time.Time
	// MuteSecret marks a mute taken with the random default window. Every read
	// surface must render such a mute as `[?]` rather than its instant, and no
	// preset may sort by it: position in a list of six muted rows discloses the
	// roll as surely as a date does (ADR-0200 decision 6).
	MuteSecret bool
	// Items are the container's Work items in the kind's own order.
	Items []Item
	// DetailSections are the kind-authored prose blocks a detail view renders
	// above the item list.
	DetailSections []Section
	// Headline is the kind's one-line suffix for a detail header — a task set's
	// task progress, and nothing at all for a kind with no such phrase. It is the
	// container's own sentence about itself, which is why it is a field rather
	// than a section: a section is a block of prose, this is part of the title
	// line.
	Headline string

	// ── Repository-group coordinates ───────────────────────────────────────────
	// Where the container's repository group is, resolved once per build and
	// carried rather than re-derived: nothing downstream forks git to find them
	// again, which is what keeps the build fork-free (ADR-0060). A registered kind
	// always fills them — registration happens per repository group — and the
	// write verbs act on them. A kind whose membership is *discovered* fills none:
	// a Routine belongs to the directory it was created in, which may be no
	// repository at all, so it carries a project label and leaves these blank.
	DefPath, StatePath     string
	RepoKey, RepoCommonDir string
	// ProjectPath is the repository-group checkout the container was found
	// through, which is not always where a verb runs: a bound task set runs in its
	// worktree (Checkout), while its registration still lives here.
	ProjectPath string

	// ── Task-set cells ─────────────────────────────────────────────────────────
	// The Work dashboard's columns are the Task-set columns, and a container of
	// any kind fills them (a Map fills the tally pair and leaves the rest blank),
	// so they are fields on the container rather than the Task-set kind's private
	// state. Everything here is derived per build — a filesystem stat or a store
	// read, never a git fork — and none of it is a persisted status.
	//
	// RuntimePath is the dedicated checkout the set's Worktree binding names,
	// blank when it holds none.
	RuntimePath string
	// Parked is true when the set's repeated abnormal terminals have parked it
	// (derived from Drain history); unpark writes a park-clear event (ADR-0055).
	// Bound is true when the set holds a Worktree binding with a non-blank
	// runtime path — the dedicated-checkout fact the action menu gates unbind
	// on, mirroring dashboardSetBound.
	// Orphaned is true when that binding points at a checkout that no longer
	// exists on disk. It is orthogonal to Task-set status — a set of any status
	// may be orphaned — and a set with no binding can never be orphaned.
	Parked, Bound, Orphaned bool
	// Provisioned is the Worktree binding's managed bit when Bound is true —
	// true when the checkout lives under the managed-worktree root. Unfolded
	// (and DoneStillManagedBound) read it; an adopted checkout is Bound but not
	// Provisioned.
	Provisioned bool
	AutoDrain   bool
	// ConfigError is the message for a config-class defect that keeps the set
	// from routing to an integration target — a bare repo with no declared trunk
	// or an unsatisfiable worktree directive (ADR-0059/0060). Non-blank only when
	// the set is neither live-drained nor parked, preserving the mutual exclusion
	// the retired single-string DRAIN cell enforced. Rendered as the plain
	// ` · config error: <msg>` STATUS suffix (ADR-0111).
	ConfigError string
	// RawStatus is the underlying derived Task-set status, kept beside the
	// displayed Status so display relabels never leak into logic.
	RawStatus SetStatus
	// DoneStillManagedBound is true when a Done set is Unfolded — the Done-only
	// special case of the shared Unfolded predicate (managed binding still held).
	// The dashboard keeps such a row visible as a clean-up reminder until
	// archived or unbound (ADR-0070, ADR-0197).
	DoneStillManagedBound bool
	// PaneID is the tmux pane recorded for a drain of this set, empty if none
	// was recorded. Audit/bookkeeping only — the live-pane affordance reads tmux
	// (ADR-0158), not this store field.
	PaneID string
	// LiveDrain is true when a live (PID-alive) Runtime execution lock holds
	// this set's checkout — the structured fact that replaced the retired DRAIN
	// column (ADR-0111). It lights the trailing ● live-drain indicator across every
	// status, and drives Less's running tier, the header "N running" count, the
	// auto-drain suffix silencing (ADR-0108), and the READY→IN PROGRESS
	// refinement.
	LiveDrain bool
	// Started mirrors tasks.Row.Started: a started READY set renders as
	// "IN PROGRESS". It is a presentational input to the STATUS composition,
	// never a schedulability fact — logic keys on RawStatus.
	Started bool
	// VerifiedAtSHA is the short SHA of the episode's PASS verdict when the set is
	// terminal and cleared, and VerifiedAtDrifted reports HEAD having moved past
	// it. DeriveVerifiedAtBadge maps the pair to the STATUS badge (ADR-0156).
	VerifiedAtSHA     string
	VerifiedAtDrifted bool
	// VerifyMark is the verification outcome riding beside the status — verified,
	// unverified, or verify-failed — for a terminal set, blank otherwise. It is a
	// separate fact from RawStatus: a human-completed set stays DONE and carries
	// its verification outcome here, so a verdict never contradicts the human.
	VerifyMark VerifyMark
	Worktree   string
	// MapOpen and MapFrontier are the Map ticket tallies its STATUS cell reports.
	// Zero on Task-set rows.
	MapOpen, MapFrontier int
	// DestKind selects how the destination column is styled; Worktree holds the
	// plain label (branch name, "[managed wt]", or "needs bind"). It is the
	// style-selection fact the queue-side wrappers read (ADR-0143).
	DestKind DestKind

	// ── Routine cells ──────────────────────────────────────────────────────────
	// The Routine page's columns, filled the way the Task-set cells above are:
	// derived per build, none of them a persisted status, blank on a container of
	// another kind.
	//
	// RoutineDirectory is the bound-directory cell — the canonical cwd stamped at
	// the Routine's creation, or `(missing)` when that directory is gone. It is a
	// display cell, never a path to act on: a Routine whose directory vanished
	// carries an empty Checkout, so a shell or fire refuses instead of landing
	// nowhere, and the Routine is still listed rather than pruned.
	RoutineDirectory string
	// RoutineSchedule is the rendered recurrence (`manual` for an unscheduled
	// Routine and for every Project routine) and RoutineLastRun the last fire
	// instant, `never` before the first.
	RoutineSchedule, RoutineLastRun string
	// RoutinePaused is the persisted pause bit — the one enable/disable state a
	// Routine has, and a state a Project routine never carries.
	RoutinePaused bool
	// RoutineLastReport is the absolute path of the newest run's report, empty
	// when the Routine has never fired or its newest run wrote none (skipped, or
	// still running). It is not a column cell: the row-level copy-report-path verb
	// copies it and the detail sections name it, and both need the path that
	// exists rather than the one a run would have written.
	RoutineLastReport string
	// RoutineTier is the kind-local relevance tier stamped at load time: the
	// checkout the reader stands in, another checkout of the same project, then
	// everything else. `work` never interprets it — it is the Routine
	// comparator's first key. Stamped rather than derived in Less because a
	// comparator is pure over two containers and cannot consult a cwd.
	RoutineTier int
	// Badge is the marker glyph a kind prefixes the container's id cell with,
	// empty for a container needing none. A Project routine's is the only one
	// today: it is the same Work kind as an authored Routine and says so in a
	// cell, rather than being a fourth kind for a display distinction.
	Badge string
}

// Ref names the container independently of the kind that produced it.
func (c Container) Ref() ref.WorkRef {
	return ref.WorkRef{Kind: c.Kind, ContainerID: c.ID}
}

// Item is one Work item inside a container: a task, a Decision ticket, later a
// Routine run. Like Container it is plain data; item statuses are kind-local and
// opaque here.
type Item struct {
	ID    string
	Title string
	// Type is the kind's own item classification (a task's `AFK`, a Decision
	// ticket's `research`), empty when the kind classifies none.
	Type string
	// Status is the kind's own item status label — the token the kind's own
	// ItemActions keys on, so it stays the machine-readable one.
	Status string
	// StatusLabel is what a reader should see instead of Status when the kind has
	// more to say about it than the status word (a task's `failed(2)` retry
	// count). Empty means Status renders as it stands.
	StatusLabel string
	// Blocked reports that the item cannot be advanced yet, with BlockedBy naming
	// the item ids holding it.
	Blocked   bool
	BlockedBy []string
	// File is the absolute path to the item's text, empty when the kind stores
	// none. Absolute because the surfaces that read it — a detail peek, an editor
	// handoff — have no directory of the kind's to resolve it against.
	File string
}

// DisplayStatus is the item status a reader sees: the kind's embellished label
// when it wrote one, the plain status otherwise.
func (i Item) DisplayStatus() string {
	if i.StatusLabel != "" {
		return i.StatusLabel
	}
	return i.Status
}

// ItemRef names one item of a container.
func (c Container) ItemRef(item Item) ref.WorkRef {
	return ref.WorkRef{Kind: c.Kind, ContainerID: c.ID, ItemID: item.ID}
}

// Verb is the stable id of one Work verb. Two verbs are shared by every kind and
// keep the same key everywhere; every other verb is kind-local and opaque to
// `work`.
type Verb string

const (
	// VerbCopyName copies the container's or item's name to the clipboard.
	VerbCopyName Verb = "copy-name"
	// VerbShell opens a shell in the container's checkout.
	VerbShell Verb = "shell"
	// VerbStatus opens the container's status submenu. It is shared because
	// opening the submenu is the *surface's* act — the items inside it are the
	// kind's StatusActions — so a surface can recognise the opener without naming
	// a kind, and every kind that has a status to write offers it on one key.
	VerbStatus Verb = "status"
	// VerbMute opens the container's mute submenu. Shared for the same reason
	// VerbStatus is, and for one more: the submenu's entries are dates derived
	// from today, which is not kind knowledge — three kinds returning the
	// identical six actions would be the copy-paste the seam exists to prevent
	// (ADR-0200 decisions 4 and 5). A kind never performs it; the surface owns
	// the submenu and calls Muter with the instant the human chose.
	VerbMute Verb = "mute"
	// VerbUnmute clears a container's mute. It is offered only on an
	// already-muted row and never occupies one of the submenu's numbered slots:
	// it is not a window, so it should neither compete for a window's digit nor
	// make the date list change length with state.
	VerbUnmute Verb = "unmute"
)

// Action is one verb offered over a container or an item. Keys follow ADR-0158's
// case rule: an uppercase key hands off (spawns or focuses a pane and leaves the
// surface), a lowercase key acts in place.
type Action struct {
	Verb  Verb
	Key   string
	Label string
}

// OutcomeKind classifies what a caller must do with a performed verb.
type OutcomeKind int

const (
	// OutcomeMessage surfaces Message and nothing else.
	OutcomeMessage OutcomeKind = iota
	// OutcomeRefresh asks the caller to rebuild its snapshot.
	OutcomeRefresh
	// OutcomeHandoff carries a process handoff the caller performs (ADR-0158).
	OutcomeHandoff
	// OutcomeCallerModal says the verb needs a modal the caller owns. The
	// Task-set drain, bind and abandon pickers stay caller-side in this version;
	// moving them behind Perform needs a modal-capable Outcome and is deferred.
	OutcomeCallerModal
	// OutcomeDetail asks the caller to open the container's detail view — its
	// items, as the caller already renders them. It exists because a kind may
	// offer a verb whose whole content is "show me the items" (a Routine's `runs`),
	// and the alternative was the caller recognising that verb by id, which would
	// put one kind's vocabulary back into the surface.
	OutcomeDetail
)

// Outcome is what performing a verb produced: a message, a refresh, a handoff,
// or a hand-back to a caller-owned modal.
type Outcome struct {
	Kind OutcomeKind
	// Message is the one-line report for the caller to surface.
	Message string
	// Clipboard is text the caller should put on the clipboard.
	Clipboard string
	// Handoff is meaningful when Kind is OutcomeHandoff.
	Handoff Handoff
}

// HandoffKind splits ADR-0158's two handoff shapes.
type HandoffKind int

const (
	// HandoffTmux spawns or focuses a tmux target and switches the client to it.
	HandoffTmux HandoffKind = iota
	// HandoffExec replaces the current process.
	HandoffExec
)

// Handoff describes a process handoff a verb could not complete on its own.
type Handoff struct {
	Kind HandoffKind
	// Target is the tmux target to switch to, for HandoffTmux. A pane id is the
	// precise form and the one to prefer: the surface focuses the target before
	// switching to it, so a session or window name lands the operator on whatever
	// pane that target already had active.
	Target string
	// Dir is the working directory the command runs in.
	Dir string
	// Command is the argv to run, empty when Target alone says everything.
	Command []string
}

// Section is one titled prose block of a container's detail view.
type Section struct {
	Title string
	Body  string
}

// ErrUnknownVerb is returned by Perform for a verb the kind does not offer.
var ErrUnknownVerb = errors.New("unknown verb for this Work kind")

// UnknownVerb wraps ErrUnknownVerb with the offending verb and kind, so a
// mis-wired menu names itself.
func UnknownVerb(kind KindID, verb Verb) error {
	return fmt.Errorf("%w: %s/%s", ErrUnknownVerb, kind, verb)
}

// kindRank is a kind's fixed precedence: the order of the closed enum, task sets
// then Maps then Routines. Precedence is fixed rather than negotiated so the
// wiring order in `cmd` cannot silently reorder the view.
func kindRank(id KindID) int {
	for i, k := range ref.Kinds() {
		if k == id {
			return i
		}
	}
	return len(ref.Kinds())
}
