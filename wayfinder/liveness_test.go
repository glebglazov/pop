package wayfinder

import (
	"errors"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// Every test here reads a claim without moving the clock: a claim ends when its
// grilling process does (ADR-0193), so a Map read one second after a session
// dies already offers the ticket again.

// grillingPane arranges a pane running an agent, the shape a live claim's owner
// has.
func grillingPane(fake *tmuxtest.Fake, paneID string) {
	if fake.PaneInfos == nil {
		fake.PaneInfos = map[string]tmux.PaneInfo{}
	}
	info := fake.PaneInfos[paneID]
	info.Command = "claude"
	fake.PaneInfos[paneID] = info
}

// dropToShell is a grilling session ending the way a human ends one: the agent
// exits and its pane falls back to a shell.
func dropToShell(fake *tmuxtest.Fake, paneID string) {
	if fake.PaneInfos == nil {
		fake.PaneInfos = map[string]tmux.PaneInfo{}
	}
	info := fake.PaneInfos[paneID]
	info.Command = "zsh"
	fake.PaneInfos[paneID] = info
}

// claimedTickets is what the Map read offers and what it holds back: the ids the
// frontier hands out, and the owner of every claim overlaid onto a ticket.
func claimedTickets(t *testing.T, d *Deps) (frontier []string, claimed map[string]string) {
	t.Helper()
	m, err := FindMap(d, "", claimMapID)
	if err != nil {
		t.Fatalf("FindMap: %v", err)
	}
	claimed = map[string]string{}
	for _, ticket := range m.Tickets {
		if ticket.Status == TicketClaimed {
			claimed[ticket.ID] = ticket.ClaimOwner
		}
	}
	for _, ticket := range Frontier(m.Tickets) {
		frontier = append(frontier, ticket.ID)
	}
	return frontier, claimed
}

// claimAs takes a claim on one ticket for an arbitrary owner string, arranging
// nothing about that owner: whether the window behind it is alive is the
// question each test then answers for itself.
func claimAs(t *testing.T, d *Deps, owner, ticketID string) {
	t.Helper()
	d.Owner = func() string { return owner }
	if _, err := ClaimTicket(d, "", claimMapID, ticketID); err != nil {
		t.Fatalf("claim %s as %s: %v", ticketID, owner, err)
	}
}

// TestPaneOwnerLivenessDecidesWhetherATicketIsOffered walks the three states a
// pane owner can be in — gone, sitting at a shell, running an agent — and the
// two answers they produce. Nothing advances a clock.
func TestPaneOwnerLivenessDecidesWhetherATicketIsOffered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		arrange    func(fake *tmuxtest.Fake)
		wantClaims bool
	}{
		{
			name:    "pane is gone",
			arrange: func(fake *tmuxtest.Fake) {},
		},
		{
			name:    "pane sits at a bare shell",
			arrange: func(fake *tmuxtest.Fake) { dropToShell(fake, "%7") },
		},
		{
			name:       "pane runs a grilling agent",
			arrange:    func(fake *tmuxtest.Fake) { grillingPane(fake, "%7") },
			wantClaims: true,
		},
		{
			name: "no tmux server at all",
			arrange: func(fake *tmuxtest.Fake) {
				grillingPane(fake, "%7")
				fake.AllPanesErr = errors.New("no server running")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, _ := claimFixture(t)
			fake := atTime(d, at(9))
			claimAs(t, d, "pane:%7", "01")
			tt.arrange(fake)

			frontier, claimed := claimedTickets(t, d)
			if tt.wantClaims {
				if len(claimed) != 1 || claimed["01"] != "pane:%7" {
					t.Fatalf("claims = %v, want 01 held by pane:%%7", claimed)
				}
				if strings.Join(frontier, ",") != "03" {
					t.Fatalf("frontier = %v, want only 03 — 01 is grilling and 02 is blocked", frontier)
				}
				return
			}
			if len(claimed) != 0 {
				t.Fatalf("claims = %v, want none — the grilling session is gone", claimed)
			}
			if strings.Join(frontier, ",") != "01,03" {
				t.Fatalf("frontier = %v, want the freed 01 back beside 03", frontier)
			}
		})
	}
}

// TestPidOwnerIsProbedForLife covers the owner a claim taken from a plain shell
// carries: a running pid holds its ticket, a dead one frees it.
func TestPidOwnerIsProbedForLife(t *testing.T) {
	t.Parallel()
	for _, alive := range []bool{true, false} {
		t.Run(map[bool]string{true: "live pid", false: "dead pid"}[alive], func(t *testing.T) {
			t.Parallel()
			d, _ := claimFixture(t)
			atTime(d, at(9))
			var probed []int
			d.PIDAlive = func(pid int) bool {
				probed = append(probed, pid)
				return alive
			}
			claimAs(t, d, "pid:4242", "01")

			frontier, claimed := claimedTickets(t, d)
			if len(probed) == 0 || probed[0] != 4242 {
				t.Fatalf("probed pids = %v, want the owner's 4242", probed)
			}
			if alive {
				if claimed["01"] != "pid:4242" || strings.Join(frontier, ",") != "03" {
					t.Fatalf("live pid: claims = %v, frontier = %v", claimed, frontier)
				}
				return
			}
			if len(claimed) != 0 || strings.Join(frontier, ",") != "01,03" {
				t.Fatalf("dead pid: claims = %v, frontier = %v", claimed, frontier)
			}
		})
	}
}

// TestPaneOwnerFormatsAreProbedInPlace pins how an owner string is read: the
// pre-pid form is probed by pane existence and bare-shell state alone, and one
// carrying the pane's pid is additionally held to the pane it named. Neither is
// migrated — the rows are short-lived, and a read folds nothing.
func TestPaneOwnerFormatsAreProbedInPlace(t *testing.T) {
	t.Parallel()
	fake := &tmuxtest.Fake{
		PaneInfos: map[string]tmux.PaneInfo{
			"%7": {Command: "claude"},
			"%8": {Command: "zsh"},
		},
		PanePIDs: map[string]int{"%7": 28405, "%8": 31000},
	}
	d := &Deps{Tmux: fake, PIDAlive: func(int) bool { return true }}
	live := d.ownerLive()

	cases := map[string]bool{
		"pane:%7":       true,  // pre-pid: existence and a running agent are the whole rule
		"pane:%8":       false, // pre-pid, but the agent is gone
		"pane:%7/28405": true,  // the pane it named, still grilling
		"pane:%7/999":   false, // a pane id tmux handed to somebody else
		"pane:%9":       false, // no such pane
		"pid:4242":      true,
		"":              false,
		"nonsense":      false,
	}
	for owner, want := range cases {
		if got := live(owner); got != want {
			t.Errorf("live(%q) = %v, want %v", owner, got, want)
		}
	}
}

// TestLivenessForksTmuxOncePerRead is the cost guarantee: a Map read carrying
// several claims lists the server's panes once, never once per claimed ticket.
func TestLivenessForksTmuxOncePerRead(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	for i, ticketID := range []string{"01", "02", "03"} {
		paneID := "%" + string(rune('7'+i))
		grillingPane(fake, paneID)
		claimAs(t, d, "pane:"+paneID, ticketID)
	}

	fake.AllPanesCalls = 0
	if _, claimed := claimedTickets(t, d); len(claimed) != 3 {
		t.Fatalf("claims = %v, want all three tickets held", claimed)
	}
	if fake.AllPanesCalls != 1 {
		t.Fatalf("a read over 3 claims forked list-panes %d times, want 1", fake.AllPanesCalls)
	}
}

// TestFanOutTopsUpADeadSessionsTicket: the same recovery through the other
// spawn verb. Re-running fan-out re-offers only the ticket whose session died
// and leaves the live pane beside it alone.
func TestFanOutTopsUpADeadSessionsTicket(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))

	out, err := FanOutFrontier(d, nil, "", claimMapID)
	if err != nil || len(out.Spawned) != 2 {
		t.Fatalf("fan-out = %+v (%v), want the frontier's two tickets", out, err)
	}
	spawned := map[string]string{}
	for _, s := range out.Spawned {
		spawned[s.Ticket.ID] = s.Pane.PaneID
	}

	dropToShell(fake, spawned["01"])
	again, err := FanOutFrontier(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("fan-out after a session died: %v", err)
	}
	if len(again.Spawned) != 1 || again.Spawned[0].Ticket.ID != "01" {
		t.Fatalf("fan-out re-run = %+v, want only the freed 01", again.Spawned)
	}
	if again.Spawned[0].Pane.PaneID != spawned["01"] {
		t.Fatalf("fan-out re-run spawned pane %q, want the idle %q", again.Spawned[0].Pane.PaneID, spawned["01"])
	}
	if panes := fake.Windows[MapSessionName(claimMapID)]["map"]; len(panes) != 2 {
		t.Fatalf("panes = %v, want the two tickets' panes and no third", panes)
	}
}

// TestClaimTicketTakesOverADeadOwnerAndRefusesALiveOne is `pop map claim` past
// the two owners it can meet: a session that died hands the ticket over and says
// whose drafts may be lying there, and a running one is refused.
func TestClaimTicketTakesOverADeadOwnerAndRefusesALiveOne(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	grillingPane(fake, "%7")
	claimAs(t, d, "pane:%7", "01")

	asWindow(d, "pane:%9", at(9))
	grillingPane(fake, "%9")
	_, err := ClaimTicket(d, "", claimMapID, "01")
	if err == nil || !strings.Contains(err.Error(), "pane:%7") {
		t.Fatalf("claiming a ticket held by a live session = %v, want a refusal naming pane:%%7", err)
	}
	if strings.Contains(err.Error(), "after") {
		t.Fatalf("the refusal still promises a timer: %v", err)
	}

	dropToShell(fake, "%7")
	taken, err := ClaimTicket(d, "", claimMapID, "01")
	if err != nil {
		t.Fatalf("claiming a ticket whose session died: %v", err)
	}
	if taken.Owner != "pane:%9" {
		t.Fatalf("claim owner = %q, want the caller pane:%%9", taken.Owner)
	}
	if taken.Reclaimed == nil || taken.Reclaimed.Owner != "pane:%7" {
		t.Fatalf("reclaim did not name the dead session: %+v", taken.Reclaimed)
	}
}
