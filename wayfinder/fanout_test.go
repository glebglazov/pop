package wayfinder

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
)

// The wall a fan-out leaves behind: one tiled pane per frontier ticket in the
// session's single window, each tagged with its ticket and titled with the ticket
// file's stem, each claim owned by the pane that got the work.
func TestFanOutSpawnsOnePanePerFrontierTicketInOneWindow(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	session := MapSessionName(claimMapID)

	out, err := FanOutFrontier(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("FanOutFrontier: %v", err)
	}
	if out.Frontier != 2 || len(out.Spawned) != 2 || out.Lost != 0 {
		t.Fatalf("fan-out = %+v, want the whole two-ticket frontier spawned", out)
	}
	if out.Spawned[0].Ticket.ID != "01" || out.Spawned[1].Ticket.ID != "03" {
		t.Fatalf("spawned %s and %s, want 01 then 03 — 02 is blocked",
			out.Spawned[0].Ticket.ID, out.Spawned[1].Ticket.ID)
	}
	windows := fake.Windows[session]
	if len(windows) != 1 || len(windows["map"]) != 2 {
		t.Fatalf("session windows = %v, want two panes in one window named map", windows)
	}
	for _, spawned := range out.Spawned {
		pane := spawned.Pane
		if got, _ := fake.PaneTagValue(pane.PaneID, tmux.TagTicket); got != spawned.Ticket.ID {
			t.Fatalf("pane %s tagged %q, want ticket %s", pane.PaneID, got, spawned.Ticket.ID)
		}
		if fake.PaneTitles[pane.PaneID] != strings.TrimSuffix(spawned.Ticket.File, ".md")+" · "+tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(nil, nil)) {
			t.Fatalf("pane titles = %v, want the ticket file stems with attended entry", fake.PaneTitles)
		}
		if spawned.Claim.Owner != ownerOfPane(fake, pane.PaneID) {
			t.Fatalf("claim owner = %q, want the spawned pane %q", spawned.Claim.Owner, pane.PaneID)
		}
		if cmd := strings.Join(fake.SentCommands[pane.PaneID], " "); !strings.Contains(cmd, claimMapID+" "+spawned.Ticket.ID) {
			t.Fatalf("pane %s runs %q, want the work-mode invocation for one ticket", pane.PaneID, cmd)
		}
	}
	if len(fake.Switched) != 0 || len(fake.Attached) != 0 {
		t.Fatalf("fan-out moved the operator: switched=%v attached=%v", fake.Switched, fake.Attached)
	}

	// Re-running tops up: everything is claimed, so there is nothing to spawn and
	// no live pane is sent work twice.
	sentBefore := len(fake.SentCommands[out.Spawned[0].Pane.PaneID])
	again, err := FanOutFrontier(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("second FanOutFrontier: %v", err)
	}
	if again.Frontier != 0 || len(again.Spawned) != 0 {
		t.Fatalf("re-run = %+v, want nothing left on the frontier", again)
	}
	if len(fake.Windows[session]["map"]) != 2 {
		t.Fatalf("re-run changed the pane wall: %v", fake.Windows[session])
	}
	if got := len(fake.SentCommands[out.Spawned[0].Pane.PaneID]); got != sentBefore {
		t.Fatalf("re-run re-sent work into a live pane (%d sends, was %d)", got, sentBefore)
	}
}

// A ticket a parallel session takes between the entry scan and its turn in the loop
// costs one idle pane and nothing else: the rest of the frontier still goes out.
func TestFanOutLosesATicketMidLoopAndKeepsGoing(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))

	// The scan the loop is bounded by, taken before the rival claim lands.
	m, err := FindMap(d, "", claimMapID)
	if err != nil {
		t.Fatal(err)
	}
	asWindow(d, "pane:%rival", at(9))
	if _, err := ClaimTicket(d, "", claimMapID, "03"); err != nil {
		t.Fatalf("rival claim: %v", err)
	}

	out, err := SpawnFrontier(d, nil, m, 0)
	if err != nil {
		t.Fatalf("SpawnFrontier: %v", err)
	}
	if out.Frontier != 2 || len(out.Spawned) != 1 || out.Lost != 1 {
		t.Fatalf("fan-out = %+v, want one ticket spawned and one lost", out)
	}
	if out.Spawned[0].Ticket.ID != "01" {
		t.Fatalf("kept ticket %q, want 01", out.Spawned[0].Ticket.ID)
	}
	// The lost ticket still cost its pane — the accepted price of claiming for the
	// pane rather than for the caller.
	if panes := fake.Windows[MapSessionName(claimMapID)]["map"]; len(panes) != 2 {
		t.Fatalf("panes = %v, want two: a claimed one and the idle loss", panes)
	}
	scanned, err := FindMap(d, "", claimMapID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ticket := range scanned.Tickets {
		if ticket.ID == "03" && ticket.ClaimOwner != "pane:%rival" {
			t.Fatalf("ticket 03 owner = %q, want the rival's claim left standing", ticket.ClaimOwner)
		}
	}
}

// An empty frontier is a message, not a refusal — fan-out is safe to type
// speculatively — and it creates no tmux state at all.
func TestFanOutOnAnEmptyFrontierSpawnsNothingAndSucceeds(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	asWindow(d, "pane:%elsewhere", at(9))
	for _, ticket := range []string{"01", "03"} {
		if _, err := ClaimTicket(d, "", claimMapID, ticket); err != nil {
			t.Fatalf("claim %s: %v", ticket, err)
		}
	}

	out, err := FanOutFrontier(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("FanOutFrontier over an empty frontier = %v, want success", err)
	}
	if out.Frontier != 0 || len(out.Spawned) != 0 || out.Session.Name != "" {
		t.Fatalf("fan-out = %+v, want nothing spawned and no session", out)
	}
	if len(fake.Live) != 0 || len(fake.Windows) != 0 {
		t.Fatalf("an empty fan-out created tmux state: live=%v windows=%v", fake.Live, fake.Windows)
	}
}
