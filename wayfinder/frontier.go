package wayfinder

import (
	"sort"
)

// Frontier returns open, unblocked, unclaimed tickets ordered by ticket number.
// A blocker is satisfied only when its status is resolved.
func Frontier(tickets []Ticket) []Ticket {
	byID := make(map[string]TicketStatus, len(tickets))
	for _, t := range tickets {
		byID[t.ID] = t.Status
	}
	var out []Ticket
	for _, t := range tickets {
		if t.Status != TicketOpen {
			continue
		}
		if !blockersSatisfied(t.BlockedBy, byID) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Number != out[j].Number {
			return out[i].Number < out[j].Number
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func blockersSatisfied(blockers []string, byID map[string]TicketStatus) bool {
	for _, blocker := range blockers {
		status, ok := byID[blocker]
		if !ok || status != TicketResolved {
			return false
		}
	}
	return true
}

// CountTickets tallies tickets by workflow status.
func CountTickets(tickets []Ticket) TicketCounts {
	var counts TicketCounts
	for _, t := range tickets {
		switch t.Status {
		case TicketOpen:
			counts.Open++
		case TicketClaimed:
			counts.Claimed++
		case TicketResolved:
			counts.Resolved++
		}
	}
	return counts
}
