package wayfinder

import "time"

// MapStatus is the declared lifecycle of a Map (map.md Status: line).
type MapStatus string

const (
	MapActive     MapStatus = "active"
	MapDone       MapStatus = "done"
	MapAbandoned  MapStatus = "abandoned"
	MapMalformed  MapStatus = "malformed"
)

// TicketType classifies a Decision ticket.
type TicketType string

const (
	TicketResearch  TicketType = "research"
	TicketPrototype TicketType = "prototype"
	TicketGrilling  TicketType = "grilling"
	TicketTask      TicketType = "task"
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
	Number    int
	Slug      string
	ID        string // zero-padded ticket number, e.g. "01"
	File      string // markdown filename under issues/, e.g. "01-first.md"
	Title     string // manifest-only; empty on header-parsed tickets
	Type      TicketType
	Status    TicketStatus
	BlockedBy []string // blocker ticket numbers, e.g. "01"
	// ClaimOwner and ClaimedAt carry the live claim on this ticket, overlaid from
	// pop.db at scan time. They are the only ticket state no file holds: a claim
	// belongs to a grilling window, and the TTL that frees an abandoned one can
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
	ID              string
	Dir             string
	Status          MapStatus
	Destination     string
	DecisionsSoFar  string
	Archived        bool
	Tickets         []Ticket
	Malformed       bool
	// MalformedReason is set when Malformed is true.
	MalformedReason string
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
	Malformed       bool
	MalformedSummary string
}

// StatusSnapshot is the pure data model for pop map status.
type StatusSnapshot struct {
	Rows []StatusRow
}
