package wayfinder

import (
	"fmt"
	"strings"
)

// SkillBaseName is the embedded planning skill's base name before the configured
// skills_prefix (default pop- → pop-wayfinder).
const SkillBaseName = "wayfinder"

// WorkModeInvocation returns the slash-command prompt that invokes the embedded
// wayfinder skill in work mode for mapID, optionally pinning ticketID.
func WorkModeInvocation(skillsPrefix, mapID, ticketID string) string {
	skill := strings.TrimSpace(skillsPrefix) + SkillBaseName
	inv := "/" + skill + " work " + strings.TrimSpace(mapID)
	if tid := strings.TrimSpace(ticketID); tid != "" {
		inv += " " + tid
	}
	return inv
}

// ErrEmptyFrontier is returned when a spawn is requested but no frontier ticket
// is available (every open ticket is blocked or claimed).
var ErrEmptyFrontier = fmt.Errorf("no frontier tickets to work")

// TargetTicket picks the ticket to spawn for map m. An empty ticketID selects
// the first frontier ticket; a non-empty ticketID must name a frontier ticket.
func TargetTicket(m Map, ticketID string) (Ticket, error) {
	frontier := Frontier(m.Tickets)
	if len(frontier) == 0 {
		return Ticket{}, ErrEmptyFrontier
	}
	if strings.TrimSpace(ticketID) == "" {
		return frontier[0], nil
	}
	tid := strings.TrimSpace(ticketID)
	for _, t := range frontier {
		if t.ID == tid {
			return t, nil
		}
	}
	return Ticket{}, fmt.Errorf("ticket %q is not on the frontier for map %q", tid, m.ID)
}
