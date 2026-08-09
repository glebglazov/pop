package wayfinder

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func sessionTestDeps(fake *tmuxtest.Fake, trunk string) *Deps {
	return &Deps{
		Tmux:  fake,
		Trunk: func() (string, error) { return trunk, nil },
	}
}

// The shape of a fresh Map session: one window with nothing running in it, rooted
// at the Trunk, stamped with the Work container it hosts. The overview pane is
// gone — `pop map status` is a verb the human types (ADR-0182).
func TestEnsureMapSessionCreatesStampedSessionAtTheTrunk(t *testing.T) {
	t.Parallel()
	fake := &tmuxtest.Fake{}
	d := sessionTestDeps(fake, "/repo/trunk")

	session, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("EnsureMapSession: %v", err)
	}
	name := MapSessionName("demo-map")
	if session.Name != name || !session.Created || session.Dir != "/repo/trunk" {
		t.Fatalf("session = %+v, want a created %q at the trunk", session, name)
	}
	if got := fake.Live[name]; got != "/repo/trunk" {
		t.Fatalf("session cwd = %q, want the trunk", got)
	}
	panes := fake.Windows[name]["map"]
	if len(panes) != 1 || session.InitialPane != panes[0] {
		t.Fatalf("map window = %v, want the reported initial pane %q", fake.Windows[name], session.InitialPane)
	}
	if cmds := fake.SentCommands[panes[0]]; len(cmds) != 0 {
		t.Fatalf("a fresh session ran %v; the overview pane is deleted", cmds)
	}
	if stamp := fake.WorkStamps[name]; stamp.Kind != "map" || stamp.ID != "demo-map" {
		t.Fatalf("work stamp = %+v, want kind map with the map id", stamp)
	}

	// Auto-open is called on every write, so it has to be a no-op the second time.
	again, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("second EnsureMapSession: %v", err)
	}
	if again.Created || again.InitialPane != "" {
		t.Fatalf("second ensure = %+v, want an attach with no pane to adopt", again)
	}
	if len(fake.Switched) != 0 || len(fake.Attached) != 0 {
		t.Fatalf("ensure moved the caller: switched=%v attached=%v", fake.Switched, fake.Attached)
	}
}

// An unresolvable Trunk refuses and names the escape hatch — but only when a
// session actually has to be created.
func TestEnsureMapSessionWithoutTrunk(t *testing.T) {
	t.Parallel()
	fake := &tmuxtest.Fake{}
	d := &Deps{Tmux: fake}

	_, err := EnsureMapSession(d, "demo-map")
	if !errors.Is(err, ErrNoTrunk) {
		t.Fatalf("err = %v, want ErrNoTrunk", err)
	}
	if !strings.Contains(err.Error(), "--trunk <path>") {
		t.Fatalf("refusal %q does not name --trunk <path>", err)
	}
	if len(fake.Live) != 0 {
		t.Fatalf("a refused ensure created tmux state: %v", fake.Live)
	}

	// A live session already knows where it is rooted, so reporting it must not
	// depend on config it no longer needs.
	fake.Live = map[string]string{MapSessionName("demo-map"): "/repo/trunk"}
	session, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("EnsureMapSession on a live session: %v", err)
	}
	if session.Created || session.Name != MapSessionName("demo-map") {
		t.Fatalf("session = %+v, want the live one reported", session)
	}
}

// One tiled pane per ticket in the session's single window, tagged with the ticket
// id and titled with the ticket file's stem — and the caller moved only when the
// caller asks.
func TestOpenGrillingPanePerTicketInOneWindow(t *testing.T) {
	t.Parallel()
	fake := &tmuxtest.Fake{Inside: true}
	d := sessionTestDeps(fake, "/repo/trunk")
	name := MapSessionName("demo-map")

	session, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("EnsureMapSession: %v", err)
	}
	pane, err := openGrillingPane(d, *session, Ticket{ID: "01", File: "01-first.md"}, "claude --prompt grill", "")
	if err != nil {
		t.Fatalf("openGrillingPane: %v", err)
	}
	if pane.Window != "map" || pane.Title != "01-first" || pane.Reused {
		t.Fatalf("pane = %+v, want a fresh 01-first pane in the map window", pane)
	}
	// The session's own first pane became the agent's: no bare shell beside it.
	if pane.PaneID != session.InitialPane {
		t.Fatalf("pane = %q, want the session's initial pane %q adopted", pane.PaneID, session.InitialPane)
	}
	if got := strings.Join(fake.SentCommands[pane.PaneID], " "); !strings.Contains(got, "claude --prompt grill") {
		t.Fatalf("grilling pane runs %q", got)
	}
	if fake.PaneTitles[pane.PaneID] != "01-first" {
		t.Fatalf("pane titles = %v, want the ticket stem", fake.PaneTitles)
	}
	if len(fake.Switched) != 0 {
		t.Fatalf("spawning switched the client on its own: %v", fake.Switched)
	}

	// A second ticket is a second pane in the same window, not a second window.
	again, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("second EnsureMapSession: %v", err)
	}
	other, err := openGrillingPane(d, *again, Ticket{ID: "02", File: "02-second.md"}, "claude", "")
	if err != nil {
		t.Fatalf("second openGrillingPane: %v", err)
	}
	if other.Window != "map" || other.PaneID == pane.PaneID {
		t.Fatalf("second pane = %+v, want its own pane in the map window", other)
	}
	if got := fake.Windows[name]; len(got) != 1 || len(got["map"]) != 2 {
		t.Fatalf("session windows = %v, want two panes in one window named map", got)
	}
	if !slices.Contains(fake.WindowRetiled, name+":map") {
		t.Fatalf("retiled = %v, want the map window tiled after a split", fake.WindowRetiled)
	}

	// Focus is the window, no particular pane: choosing one agent for the human
	// out of a tiled frontier would be a guess.
	if err := FocusMapSession(d, other.Session); err != nil {
		t.Fatalf("FocusMapSession: %v", err)
	}
	if len(fake.Switched) != 1 || fake.Switched[0] != name {
		t.Fatalf("switched = %v, want the map session", fake.Switched)
	}
	if len(fake.Selected) != 0 {
		t.Fatalf("focus selected a pane: %v", fake.Selected)
	}
	if !slices.Contains(fake.SelectedWindows, name+":map") {
		t.Fatalf("selected windows = %v, want the map window", fake.SelectedWindows)
	}
}

// A pane whose agent is still alive is a jump target (ADR-0158); an idle one gets
// the command again. The ticket tag is the reuse key — the window says nothing
// about which ticket is where.
func TestOpenGrillingPaneReusesLiveAndRespawnsIdle(t *testing.T) {
	t.Parallel()
	name := MapSessionName("demo-map")
	fake := &tmuxtest.Fake{
		Live:      map[string]string{name: "/repo/trunk"},
		Windows:   map[string]map[string][]string{name: {"map": {"%9"}}},
		PaneInfos: map[string]tmux.PaneInfo{"%9": {Session: name, Command: "claude"}},
		PaneTagValues: map[string]map[tmux.PaneTag]string{
			"%9": {tmux.TagTicket: "01"},
		},
	}
	d := sessionTestDeps(fake, "/repo/trunk")
	ticket := Ticket{ID: "01", File: "01-first.md"}
	session, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("EnsureMapSession: %v", err)
	}

	pane, err := openGrillingPane(d, *session, ticket, "claude --prompt grill", "")
	if err != nil {
		t.Fatalf("openGrillingPane: %v", err)
	}
	if !pane.Reused || pane.PaneID != "%9" {
		t.Fatalf("pane = %+v, want the live pane reused", pane)
	}
	if fake.SentCommands["%9"] != nil {
		t.Fatalf("reuse re-sent work into a live agent: %v", fake.SentCommands)
	}

	fake.PaneInfos["%9"] = tmux.PaneInfo{Session: name, Command: "zsh"}
	idle, err := openGrillingPane(d, *session, ticket, "claude --prompt grill", "")
	if err != nil {
		t.Fatalf("openGrillingPane on an idle pane: %v", err)
	}
	if idle.Reused || idle.PaneID != "%9" {
		t.Fatalf("pane = %+v, want the idle pane respawned in place", idle)
	}
	if len(fake.SentCommands["%9"]) == 0 {
		t.Fatal("an idle pane was not respawned")
	}
}

func TestMapIDFromSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ session, want string }{
		{"pop-map-2026-08-03-demo", "2026-08-03-demo"},
		{"pop", ""},
		{"pop-work", ""},
		{"", ""},
	} {
		if got := MapIDFromSession(tc.session); got != tc.want {
			t.Fatalf("MapIDFromSession(%q) = %q, want %q", tc.session, got, tc.want)
		}
	}
}
