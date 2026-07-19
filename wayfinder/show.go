package wayfinder

import (
	"fmt"
	"io"
	"strings"
)

const decisionsSoFarGistMaxLen = 48

// TicketGroups partitions a map's tickets for display.
type TicketGroups struct {
	Frontier []Ticket
	Blocked  []Ticket
	Claimed  []Ticket
	Resolved []Ticket
}

// GroupTickets classifies tickets into frontier, blocked, claimed, and resolved.
func GroupTickets(tickets []Ticket) TicketGroups {
	frontier := Frontier(tickets)
	frontierIDs := make(map[string]bool, len(frontier))
	for _, t := range frontier {
		frontierIDs[t.ID] = true
	}

	var groups TicketGroups
	groups.Frontier = frontier
	for _, t := range tickets {
		switch t.Status {
		case TicketResolved:
			groups.Resolved = append(groups.Resolved, t)
		case TicketClaimed:
			groups.Claimed = append(groups.Claimed, t)
		case TicketOpen:
			if !frontierIDs[t.ID] {
				groups.Blocked = append(groups.Blocked, t)
			}
		}
	}
	return groups
}

// RenderShow prints one map's detail as plain text.
func RenderShow(out io.Writer, m Map) error {
	status := string(m.Status)
	if m.Malformed {
		status = string(MapMalformed)
	}
	if m.Archived {
		status += " [archived]"
	}

	fmt.Fprintf(out, "Map: %s\n", m.ID)
	fmt.Fprintf(out, "Status: %s\n", status)
	if m.Malformed {
		if m.MalformedReason != "" {
			fmt.Fprintf(out, "Malformed: %s\n", m.MalformedReason)
		}
		return nil
	}

	fmt.Fprintf(out, "Destination: %s\n", m.Destination)
	decisions := DestinationGist(m.DecisionsSoFar, decisionsSoFarGistMaxLen)
	if decisions == "" {
		decisions = "(none)"
	}
	fmt.Fprintf(out, "Decisions so far: %s\n", decisions)

	groups := GroupTickets(m.Tickets)
	writeTicketGroup(out, "Frontier", groups.Frontier, false)
	writeTicketGroup(out, "Blocked", groups.Blocked, true)
	writeTicketGroup(out, "Claimed", groups.Claimed, false)
	writeTicketGroup(out, "Resolved", groups.Resolved, false)
	return nil
}

func writeTicketGroup(out io.Writer, title string, tickets []Ticket, showBlockers bool) {
	fmt.Fprintf(out, "\n%s:\n", title)
	if len(tickets) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	for _, t := range tickets {
		fmt.Fprintln(out, formatTicketLine(t, showBlockers))
	}
}

func formatTicketLine(t Ticket, showBlockers bool) string {
	name := ticketDisplayName(t)
	line := fmt.Sprintf("  %s  %s  %s", name, t.Type, t.Status)
	if showBlockers && len(t.BlockedBy) > 0 {
		line += fmt.Sprintf("  (blocked by %s)", strings.Join(t.BlockedBy, ", "))
	}
	return line
}

func ticketDisplayName(t Ticket) string {
	if t.Slug != "" {
		return fmt.Sprintf("%s-%s", t.ID, t.Slug)
	}
	return t.ID
}

// ShowWith renders one map by identifier using injected dependencies.
func ShowWith(d *Deps, out io.Writer, cwd, mapID string) error {
	m, err := FindMap(d, cwd, mapID)
	if err != nil {
		return err
	}
	return RenderShow(out, m)
}
