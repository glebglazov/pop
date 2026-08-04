package wayfinder

import "time"

// MapStatus is the declared lifecycle of a Map (map.md Status: line).
type MapStatus string

const (
	MapActive MapStatus = "active"
	// MapArrived is the terminal status of a Map that reached its destination,
	// written by `pop map arrive` and reversed by `pop map open`. It replaced
	// `done` outright — the Work dashboard hides DONE work by default, and a Map's
	// terminal state must not inherit a hiding rule written for Task sets
	// (ADR-0172).
	MapArrived MapStatus = "arrived"
	// MapAbandoned is the terminal status of an effort dropped rather than
	// finished, written by `pop map abandon` and the dashboard's status submenu and
	// reversed by `pop map open`. Unlike arrival it hides the Map: an abandoned Map
	// is not a lineage record anybody reads, it is a decision to stop.
	MapAbandoned MapStatus = "abandoned"
	// MapBroken is pop's verdict for a Map it cannot read: an unrecognised Status:
	// word, an unreadable map.md, a manifest that does not validate. Uppercase
	// because it is the one value no human ever writes into map.md, and the fix
	// travels beside it in BrokenReason. Registration's own diagnostics stay
	// MALFORMED: that is a fix loop over the manifest, not a row label.
	MapBroken MapStatus = "BROKEN"
)

// authorableMapStatuses is map.md's whole Status: vocabulary, in lifecycle
// order. The parser accepts these words and nothing else, and the authoring
// guide prints this same slice — one list, so a printed status is always a
// status pop reads back. MapBroken is absent by construction: it is pop's
// verdict on a Map it cannot read, never a word a session writes.
var authorableMapStatuses = []MapStatus{MapActive, MapArrived, MapAbandoned}

// TicketType classifies a Decision ticket.
type TicketType string

const (
	TicketResearch  TicketType = "research"
	TicketPrototype TicketType = "prototype"
	TicketGrilling  TicketType = "grilling"
	TicketTask      TicketType = "task"
)

// manifestTicketTypeOrder and manifestTicketStatusOrder are the manifest's two
// enums in the order a guide reads best. The validator's lookup maps are built
// from these slices (see manifest.go), so there is one list per enum rather than
// a set to enforce against and a list to print.
var (
	manifestTicketTypeOrder   = []TicketType{TicketResearch, TicketPrototype, TicketGrilling, TicketTask}
	manifestTicketStatusOrder = []TicketStatus{TicketOpen, TicketResolved}
)

// TicketStatus is the workflow state of a Decision ticket.
type TicketStatus string

const (
	TicketOpen     TicketStatus = "open"
	TicketClaimed  TicketStatus = "claimed"
	TicketResolved TicketStatus = "resolved"
)

// Ticket is one Decision ticket under a Map's issues/ directory.
type Ticket struct {
	Number int
	Slug   string
	ID     string // zero-padded ticket number, e.g. "01"
	File   string // markdown filename under issues/, e.g. "01-first.md"
	Title  string // manifest-only; empty on header-parsed tickets
	Type   TicketType
	Status TicketStatus
	// OutOfScope marks a ticket resolved by being ruled beyond the destination
	// rather than answered. It decides which generated section of map.md the
	// ticket renders into.
	OutOfScope bool
	BlockedBy  []string // blocker ticket numbers, e.g. "01"
	// ClaimOwner and ClaimedAt carry the live claim on this ticket, overlaid from
	// pop.db at scan time. They are the only ticket state no file holds: a claim
	// belongs to a grilling pane, and the TTL that frees an abandoned one can
	// only run against a timestamp pop writes.
	ClaimOwner string
	ClaimedAt  time.Time
	// ADRDrafts and ContextDrafts name the draft files a resolution declared,
	// relative to the Map folder. Manifest-only; the implementing slice mints
	// them into the repository.
	ADRDrafts     []string
	ContextDrafts []string
}

// Map is a parsed Wayfinder map folder.
type Map struct {
	ID             string
	Dir            string
	Status         MapStatus
	Destination    string
	DecisionsSoFar string
	Archived       bool
	Tickets        []Ticket
	// SpawnedSets holds the ids of the Task sets this Map handed off, in the
	// order they were recorded. Ids only: what each one is doing right now is read
	// fresh from the sets themselves at render time (ResolveSpawnedSets).
	SpawnedSets []string
	Broken      bool
	// BrokenReason is set when Broken is true, and carries the corrective: what a
	// human has to change in the Map's files to make pop able to read it again.
	BrokenReason string
	// Warnings are the manifest's advisory problems, carried on every Map every
	// surface reads — the point of validating on load rather than in one verb is
	// that a problem introduced after charting is visible without re-registering.
	// They never make a Map BROKEN and never refuse a write.
	Warnings []string
}

// TicketCounts tallies tickets by workflow status.
type TicketCounts struct {
	Open     int
	Claimed  int
	Resolved int
}

// StatusRow is one line in the wayfinder status table.
type StatusRow struct {
	ID              string
	Status          MapStatus
	DestinationGist string
	Counts          TicketCounts
	FrontierSize    int
	Archived        bool
	Broken          bool
	BrokenSummary   string
	// Warnings are the Map's advisory manifest problems. They have no column —
	// they are prose, not a tally — so the table prints them under it.
	Warnings []string
}

// StatusSnapshot is the pure data model for pop map status.
type StatusSnapshot struct {
	Rows []StatusRow
}
