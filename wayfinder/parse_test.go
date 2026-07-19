package wayfinder

import (
	"strings"
	"testing"
)

func TestParseMapMarkdownDefaultsActive(t *testing.T) {
	status, dest, err := ParseMapMarkdown("## Destination\nBuild the thing\n")
	if err != nil {
		t.Fatal(err)
	}
	if status != MapActive {
		t.Fatalf("status = %q, want active", status)
	}
	if dest != "Build the thing" {
		t.Fatalf("destination = %q", dest)
	}
}

func TestParseMapMarkdownStatusAndDestination(t *testing.T) {
	content := "Status: done\n\n## Destination\n\nShip v1\nwith polish\n\n## Notes\nignored"
	status, dest, err := ParseMapMarkdown(content)
	if err != nil {
		t.Fatal(err)
	}
	if status != MapDone {
		t.Fatalf("status = %q, want done", status)
	}
	if dest != "Ship v1 with polish" {
		t.Fatalf("destination = %q", dest)
	}
}

func TestParseTicketMarkdownDefaultsOpen(t *testing.T) {
	ticket, err := ParseTicketMarkdown("03-slug.md", "Type: research\n")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ID != "03" || ticket.Number != 3 {
		t.Fatalf("ticket id/number = %q/%d", ticket.ID, ticket.Number)
	}
	if ticket.Type != TicketResearch {
		t.Fatalf("type = %q", ticket.Type)
	}
	if ticket.Status != TicketOpen {
		t.Fatalf("status = %q, want open", ticket.Status)
	}
}

func TestParseTicketMarkdownMetadata(t *testing.T) {
	content := "Type: grilling\nStatus: claimed\nBlocked by: 01, 2\n\n## Question\nbody"
	ticket, err := ParseTicketMarkdown("04-grill.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != TicketClaimed {
		t.Fatalf("status = %q", ticket.Status)
	}
	if ticket.Type != TicketGrilling {
		t.Fatalf("type = %q", ticket.Type)
	}
	want := []string{"01", "02"}
	if strings.Join(ticket.BlockedBy, ",") != strings.Join(want, ",") {
		t.Fatalf("blocked_by = %v, want %v", ticket.BlockedBy, want)
	}
}

func TestFrontierDerivation(t *testing.T) {
	tickets := []Ticket{
		{Number: 1, ID: "01", Status: TicketResolved},
		{Number: 2, ID: "02", Status: TicketOpen, BlockedBy: []string{"01"}},
		{Number: 3, ID: "03", Status: TicketClaimed},
		{Number: 4, ID: "04", Status: TicketOpen, BlockedBy: []string{"01"}},
		{Number: 5, ID: "05", Status: TicketOpen, BlockedBy: []string{"03"}},
		{Number: 6, ID: "06", Status: TicketOpen},
	}
	got := Frontier(tickets)
	if len(got) != 3 {
		t.Fatalf("frontier len = %d, want 3: %+v", len(got), got)
	}
	if got[0].ID != "02" || got[1].ID != "04" || got[2].ID != "06" {
		t.Fatalf("frontier order = %s,%s,%s, want 02,04,06", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestFrontierRequiresResolvedBlockers(t *testing.T) {
	tickets := []Ticket{
		{Number: 1, ID: "01", Status: TicketClaimed},
		{Number: 2, ID: "02", Status: TicketOpen, BlockedBy: []string{"01"}},
	}
	if len(Frontier(tickets)) != 0 {
		t.Fatal("expected empty frontier when blocker is not resolved")
	}
}
