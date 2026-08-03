package wayfinder

import (
	"errors"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func sessionTestDeps(fake *tmuxtest.Fake, trunk string) *Deps {
	return &Deps{
		Tmux:  fake,
		Trunk: func() (string, error) { return trunk, nil },
		Exe:   func() (string, error) { return "/opt/pop/bin/pop", nil },
	}
}

// The shape of a fresh Map session: one window running the overview, rooted at
// the Trunk, stamped with the Work container it hosts.
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
	if len(panes) != 1 {
		t.Fatalf("window 1 = %v, want one pane in a window named map", fake.Windows[name])
	}
	if got := strings.Join(fake.SentCommands[panes[0]], " "); !strings.Contains(got, "/opt/pop/bin/pop map status demo-map") {
		t.Fatalf("window 1 runs %q, want pop map status", got)
	}
	if stamp := fake.WorkStamps[name]; stamp.Kind != "map" || stamp.ID != "demo-map" {
		t.Fatalf("work stamp = %+v, want kind map with the map id", stamp)
	}

	// Auto-open is called on every write, so it has to be a no-op the second time.
	again, err := EnsureMapSession(d, "demo-map")
	if err != nil {
		t.Fatalf("second EnsureMapSession: %v", err)
	}
	if again.Created {
		t.Fatalf("second ensure = %+v, want an attach", again)
	}
	if len(fake.SentCommands[panes[0]]) != 1 {
		t.Fatalf("second ensure re-ran the overview: %v", fake.SentCommands[panes[0]])
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

// One window per ticket, named after the ticket, with the caller moved there
// only when the caller asks.
func TestOpenGrillingWindowPerTicket(t *testing.T) {
	t.Parallel()
	fake := &tmuxtest.Fake{Inside: true}
	d := sessionTestDeps(fake, "/repo/trunk")
	ticket := Ticket{ID: "01", File: "01-first.md"}

	win, err := OpenGrillingWindow(d, "demo-map", ticket, "claude --prompt grill")
	if err != nil {
		t.Fatalf("OpenGrillingWindow: %v", err)
	}
	if win.Window != "01-first" || win.Reused {
		t.Fatalf("window = %+v, want a fresh 01-first", win)
	}
	if got := strings.Join(fake.SentCommands[win.PaneID], " "); !strings.Contains(got, "claude --prompt grill") {
		t.Fatalf("grilling window runs %q", got)
	}
	if len(fake.Switched) != 0 {
		t.Fatalf("spawning switched the client on its own: %v", fake.Switched)
	}

	if err := FocusGrillingWindow(d, win); err != nil {
		t.Fatalf("FocusGrillingWindow: %v", err)
	}
	if len(fake.Switched) != 1 || fake.Switched[0] != win.PaneID {
		t.Fatalf("switched = %v, want the grilling pane", fake.Switched)
	}

	// A second ticket is a second window, not a second pane in the first.
	other, err := OpenGrillingWindow(d, "demo-map", Ticket{ID: "02", File: "02-second.md"}, "claude")
	if err != nil {
		t.Fatalf("second OpenGrillingWindow: %v", err)
	}
	if other.Window != "02-second" || other.PaneID == win.PaneID {
		t.Fatalf("second window = %+v, want its own window and pane", other)
	}
}

// A window whose agent is still alive is a jump target (ADR-0158); an idle one
// gets the command again.
func TestOpenGrillingWindowReusesLiveAndRespawnsIdle(t *testing.T) {
	t.Parallel()
	name := MapSessionName("demo-map")
	fake := &tmuxtest.Fake{
		Live:      map[string]string{name: "/repo/trunk"},
		Windows:   map[string]map[string][]string{name: {"01-first": {"%9"}}},
		PaneInfos: map[string]tmux.PaneInfo{"%9": {Session: name, Command: "claude"}},
	}
	d := sessionTestDeps(fake, "/repo/trunk")
	ticket := Ticket{ID: "01", File: "01-first.md"}

	win, err := OpenGrillingWindow(d, "demo-map", ticket, "claude --prompt grill")
	if err != nil {
		t.Fatalf("OpenGrillingWindow: %v", err)
	}
	if !win.Reused || win.PaneID != "%9" {
		t.Fatalf("window = %+v, want the live pane reused", win)
	}
	if fake.SentCommands["%9"] != nil {
		t.Fatalf("reuse re-sent work into a live agent: %v", fake.SentCommands)
	}

	fake.PaneInfos["%9"] = tmux.PaneInfo{Session: name, Command: "zsh"}
	idle, err := OpenGrillingWindow(d, "demo-map", ticket, "claude --prompt grill")
	if err != nil {
		t.Fatalf("OpenGrillingWindow on an idle window: %v", err)
	}
	if idle.Reused {
		t.Fatalf("window = %+v, want an idle window respawned", idle)
	}
	if len(fake.SentCommands["%9"]) == 0 {
		t.Fatal("an idle window was not respawned")
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
