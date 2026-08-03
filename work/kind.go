package work

import (
	"errors"
	"fmt"

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
	// ItemActions returns the verbs that apply to one item of a container.
	ItemActions(Container, Item) []Action
	// Perform runs a verb. The item is nil for a container-level verb.
	Perform(Container, *Item, Verb) (Outcome, error)
	// Summary returns this kind's header phrases for the containers on a page,
	// already pluralised. The builder joins every kind's phrases with " · " in
	// kind order.
	Summary([]Container) []string
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
	// Broken marks a container whose definition could not be read at all, with
	// BrokenReason carrying what went wrong. A broken container still renders —
	// hiding a container because it is unreadable hides the thing that needs
	// fixing.
	Broken       bool
	BrokenReason string
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

	// ── Transitional Task-set-only cells ───────────────────────────────────────
	// What follows is the legacy Work-dashboard row, absorbed into the container
	// rather than hung off it: `Row` is an alias of `Container`, so the dashboard
	// row has no parallel model left to drift from. The Task-set kind is what
	// fills these — a Map leaves all but the tally pair blank — and the consumers
	// that still read them (the Queue write path, livepane, Map spawning) are the
	// ones the contract slices have yet to migrate. The block is deleted whole
	// once they are.
	SetRef
	// Started mirrors tasks.Row.Started: a started READY set renders as
	// "IN PROGRESS". It is a presentational input to the STATUS composition,
	// never a schedulability fact — logic keys on RawStatus.
	Started bool
	// VerifiedAtSHA is the short SHA of the episode's PASS verdict when the set is
	// terminal and cleared, and VerifiedAtDrifted reports HEAD having moved past
	// it. DeriveVerifiedAtBadge maps the pair to the STATUS badge (ADR-0156).
	VerifiedAtSHA     string
	VerifiedAtDrifted bool
	Worktree          string
	// MapOpen and MapFrontier are the Map ticket tallies its STATUS cell reports.
	// Zero on Task-set rows.
	MapOpen, MapFrontier int
	// DestKind selects how the destination column is styled; Worktree holds the
	// plain label (branch name, "[managed wt]", or "needs bind"). It is the
	// style-selection fact the queue-side wrappers read (ADR-0143).
	DestKind DestKind
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
	// Target is the tmux session (or session:window) to switch to, for
	// HandoffTmux.
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
