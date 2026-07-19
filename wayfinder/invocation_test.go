package wayfinder

import "testing"

func TestWorkModeInvocation(t *testing.T) {
	got := WorkModeInvocation("pop-", "2026-07-01-active", "01")
	want := "/pop-wayfinder work 2026-07-01-active 01"
	if got != want {
		t.Fatalf("WorkModeInvocation() = %q, want %q", got, want)
	}
}

func TestWorkModeInvocationMapOnly(t *testing.T) {
	got := WorkModeInvocation("", "demo-map", "")
	want := "/wayfinder work demo-map"
	if got != want {
		t.Fatalf("WorkModeInvocation() = %q, want %q", got, want)
	}
}

func TestTargetTicketNextFrontier(t *testing.T) {
	m := Map{
		ID: "demo",
		Tickets: []Ticket{
			{ID: "01", Status: TicketOpen},
			{ID: "02", Status: TicketOpen, BlockedBy: []string{"01"}},
			{ID: "03", Status: TicketClaimed},
		},
	}
	ticket, err := TargetTicket(m, "")
	if err != nil {
		t.Fatalf("TargetTicket: %v", err)
	}
	if ticket.ID != "01" {
		t.Fatalf("ticket = %q, want 01", ticket.ID)
	}
}

func TestTargetTicketExplicitFrontier(t *testing.T) {
	m := Map{
		ID: "demo",
		Tickets: []Ticket{
			{ID: "01", Status: TicketResolved},
			{ID: "02", Status: TicketOpen},
		},
	}
	ticket, err := TargetTicket(m, "02")
	if err != nil {
		t.Fatalf("TargetTicket: %v", err)
	}
	if ticket.ID != "02" {
		t.Fatalf("ticket = %q, want 02", ticket.ID)
	}
}

func TestTargetTicketEmptyFrontier(t *testing.T) {
	m := Map{
		ID: "demo",
		Tickets: []Ticket{
			{ID: "01", Status: TicketOpen, BlockedBy: []string{"99"}},
		},
	}
	_, err := TargetTicket(m, "")
	if err != ErrEmptyFrontier {
		t.Fatalf("err = %v, want ErrEmptyFrontier", err)
	}
}

func TestTargetTicketNotOnFrontier(t *testing.T) {
	m := Map{
		ID: "demo",
		Tickets: []Ticket{
			{ID: "01", Status: TicketOpen},
			{ID: "02", Status: TicketOpen, BlockedBy: []string{"01"}},
		},
	}
	_, err := TargetTicket(m, "02")
	if err == nil || err == ErrEmptyFrontier {
		t.Fatalf("err = %v, want not-on-frontier error", err)
	}
}
