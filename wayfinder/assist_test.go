package wayfinder

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
)

// The whole of assist's mechanics: one pane per Map, tagged and titled as the
// Map's own, running the skill in assist mode with no ticket named — and a second
// call landing in that same pane rather than opening a session that could race it
// on the Map's prose.
func TestAssistOpensOneReusedPanePerMap(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	session := MapSessionName(claimMapID)

	pane, err := AssistMap(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("AssistMap: %v", err)
	}
	if pane.MapID != claimMapID || pane.Window != "map" || pane.Reused {
		t.Fatalf("pane = %+v, want a fresh assist pane in the map window", pane)
	}
	if got, _ := fake.PaneTagValue(pane.PaneID, tmux.TagAssist); got != claimMapID {
		t.Fatalf("pane tag = %q, want the map id under @pop_assist", got)
	}
	wantAssistTitle := "assist · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(nil, nil))
	if fake.PaneTitles[pane.PaneID] != wantAssistTitle {
		t.Fatalf("pane titles = %v, want %q", fake.PaneTitles, wantAssistTitle)
	}
	command := strings.Join(fake.SentCommands[pane.PaneID], " ")
	if !strings.Contains(command, "assist "+claimMapID) {
		t.Fatalf("assist pane runs %q, want the skill in assist mode for the map", command)
	}
	// The mode word is the whole fence: a session handed no ticket has none to
	// resolve, so nothing in the seed can name one or name the resolve verb.
	for _, forbidden := range []string{"work " + claimMapID, "resolve"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("assist pane runs %q, which reaches %q", command, forbidden)
		}
	}
	if len(fake.Switched) != 0 || len(fake.Attached) != 0 {
		t.Fatalf("assist moved the operator: switched=%v attached=%v", fake.Switched, fake.Attached)
	}

	// A second call is the same pane — which is what dissolves the
	// two-sessions-one-map race — and the live agent in it is never sent work twice
	// (ADR-0158).
	fake.PaneInfos = map[string]tmux.PaneInfo{pane.PaneID: {Session: session, Command: "claude"}}
	sentBefore := len(fake.SentCommands[pane.PaneID])
	again, err := AssistMap(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("second AssistMap: %v", err)
	}
	if !again.Reused || again.PaneID != pane.PaneID {
		t.Fatalf("second assist = %+v, want the first pane %q reused", again, pane.PaneID)
	}
	if got := len(fake.SentCommands[pane.PaneID]); got != sentBefore {
		t.Fatalf("re-assist re-sent work into a live pane (%d sends, was %d)", got, sentBefore)
	}
	if panes := fake.Windows[session]["map"]; len(panes) != 1 {
		t.Fatalf("panes = %v, want the single assist pane", panes)
	}

	// An assist pane whose agent has exited is respawned in place rather than
	// joined by a second one.
	fake.PaneInfos[pane.PaneID] = tmux.PaneInfo{Session: session, Command: "zsh"}
	idle, err := AssistMap(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("third AssistMap: %v", err)
	}
	if idle.Reused || idle.PaneID != pane.PaneID {
		t.Fatalf("assist over an idle pane = %+v, want %q respawned in place", idle, pane.PaneID)
	}
	if got := len(fake.SentCommands[pane.PaneID]); got != sentBefore+1 {
		t.Fatalf("an idle assist pane was not respawned (%d sends, was %d)", got, sentBefore)
	}
	if panes := fake.Windows[session]["map"]; len(panes) != 1 {
		t.Fatalf("panes = %v, want still the single assist pane", panes)
	}
}

// Assist claims nothing, so it consumes no frontier: the whole frontier is still
// there for the grilling verbs afterwards, and no ticket carries an owner.
func TestAssistClaimsNothing(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	atTime(d, at(9))

	if _, err := AssistMap(d, nil, "", claimMapID); err != nil {
		t.Fatalf("AssistMap: %v", err)
	}
	m, err := FindMap(d, "", claimMapID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ticket := range m.Tickets {
		if ticket.ClaimOwner != "" {
			t.Fatalf("ticket %s claimed by %q after an assist session", ticket.ID, ticket.ClaimOwner)
		}
	}
	if got := len(Frontier(m.Tickets)); got != 2 {
		t.Fatalf("frontier = %d tickets, want the untouched two", got)
	}
	// And nothing in the Map's state changed either: assist resolves nothing.
	for _, ticket := range m.Tickets {
		if ticket.Status != TicketOpen {
			t.Fatalf("ticket %s is %s after an assist session, want every ticket left open", ticket.ID, ticket.Status)
		}
	}
}

// The frontier is not consulted at all, which is the point: a Map whose tickets
// are every one of them claimed or resolved is exactly when a session scoped to
// the Map itself is needed.
func TestAssistReachableWithAFullyClaimedFrontier(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	asWindow(d, "pane:%elsewhere", at(9))
	for _, ticket := range []string{"01", "03"} {
		if _, err := ClaimTicket(d, "", claimMapID, ticket); err != nil {
			t.Fatalf("claim %s: %v", ticket, err)
		}
	}
	// The verb the frontier does gate refuses here, which is what makes the
	// comparison worth pinning.
	if _, err := NextFrontierTicket(d, nil, "", claimMapID); err == nil {
		t.Fatal("next over a fully-claimed frontier should refuse")
	}

	pane, err := AssistMap(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("AssistMap over a fully-claimed frontier = %v, want success", err)
	}
	if pane.PaneID == "" {
		t.Fatalf("assist pane = %+v, want a pane", pane)
	}
}

// Assist and grilling share the window and nothing else: neither adopts the
// other's pane, and each keeps its own tag.
func TestAssistAndGrillingPanesCoexist(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	fake := atTime(d, at(9))
	session := MapSessionName(claimMapID)

	assist, err := AssistMap(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("AssistMap: %v", err)
	}
	out, err := NextFrontierTicket(d, nil, "", claimMapID)
	if err != nil {
		t.Fatalf("NextFrontierTicket: %v", err)
	}
	grilling := out.Spawned[0].Pane
	if grilling.PaneID == assist.PaneID {
		t.Fatalf("the grilling spawn adopted the assist pane %q", assist.PaneID)
	}
	if panes := fake.Windows[session]["map"]; len(panes) != 2 {
		t.Fatalf("panes = %v, want the assist pane plus one grilling pane", panes)
	}
	if got, _ := fake.PaneTagValue(assist.PaneID, tmux.TagTicket); got != "" {
		t.Fatalf("assist pane picked up ticket tag %q", got)
	}
	if got, _ := fake.PaneTagValue(grilling.PaneID, tmux.TagAssist); got != "" {
		t.Fatalf("grilling pane picked up assist tag %q", got)
	}
}

// The seed prompt is the mode boundary made concrete: assist mode, one map, no
// ticket — where work mode always names the ticket it claimed.
func TestAssistModeInvocationNamesNoTicket(t *testing.T) {
	t.Parallel()
	assist := AssistModeInvocation("pop-", "2026-08-03-demo")
	if assist != "/pop-wayfinder assist 2026-08-03-demo" {
		t.Fatalf("assist invocation = %q", assist)
	}
	if work := WorkModeInvocation("pop-", "2026-08-03-demo", "03"); !strings.HasSuffix(work, " 03") {
		t.Fatalf("work invocation = %q, want the ticket named — the contrast assist rests on", work)
	}
}
