package wayfinder

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
	Type      TicketType
	Status    TicketStatus
	BlockedBy []string // blocker ticket numbers, e.g. "01"
}

// Map is a parsed Wayfinder map folder.
type Map struct {
	ID          string
	Dir         string
	Status      MapStatus
	Destination string
	Archived    bool
	Tickets     []Ticket
	Malformed   bool
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

// StatusSnapshot is the pure data model for pop wayfinder status.
type StatusSnapshot struct {
	Rows []StatusRow
}
