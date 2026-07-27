package routine

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// The refine spawn flow lands `pop routine edit <id>` in a window named after
// the routine. These tests drive it through the stateful fake and assert on the
// resulting window/pane state — never on tmux argument arrays (ADR-0142).

func TestRefinePaneWithFreshSpawn(t *testing.T) {
	d, home := routineDashboardDeps(t)
	f := &tmuxtest.Fake{}
	d.Tmux = f
	if _, err := AddWith(d, "fresh", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if err := RefinePaneWith(d, "fresh", ""); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	if f.Live[RoutinesSessionName] != home {
		t.Fatalf("Live[%s] = %q, want %q (detached session created)", RoutinesSessionName, f.Live[RoutinesSessionName], home)
	}
	panes := f.Windows[RoutinesSessionName]["fresh"]
	if len(panes) != 1 {
		t.Fatalf("window fresh panes = %v, want exactly one", panes)
	}
	sent := strings.Join(f.SentCommands[panes[0]], " ")
	if !strings.Contains(sent, "/mock/bin/pop routine edit fresh") {
		t.Fatalf("sent %q, want resolved executable running `routine edit fresh`", sent)
	}
}

func TestRefinePaneWithExistingWindowSendsNothing(t *testing.T) {
	d, home := routineDashboardDeps(t)
	f := &tmuxtest.Fake{
		Live:    map[string]string{RoutinesSessionName: home},
		Windows: map[string]map[string][]string{RoutinesSessionName: {"live": {"%5"}}},
	}
	d.Tmux = f
	if _, err := AddWith(d, "live", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if err := RefinePaneWith(d, "live", ""); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	if len(f.SentCommands) != 0 {
		t.Fatalf("must not send into a live refine window, sent=%v", f.SentCommands)
	}
	if len(f.Windows[RoutinesSessionName]["live"]) != 1 {
		t.Fatalf("must not add a pane to the live window, panes=%v", f.Windows[RoutinesSessionName]["live"])
	}
	if len(f.Selected) != 1 || f.Selected[0] != "%5" || len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Fatalf("must focus the existing pane %%5: selected=%v switched=%v", f.Selected, f.Switched)
	}
}

func TestRefinePaneWithForwardsRefineAgent(t *testing.T) {
	d, home := routineDashboardDeps(t)
	f := &tmuxtest.Fake{}
	d.Tmux = f
	if _, err := AddWith(d, "fwd", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if err := RefinePaneWith(d, "fwd", "claude"); err != nil {
		t.Fatalf("RefinePaneWith: %v", err)
	}
	panes := f.Windows[RoutinesSessionName]["fwd"]
	if len(panes) != 1 {
		t.Fatalf("window fwd panes = %v, want exactly one", panes)
	}
	sent := strings.Join(f.SentCommands[panes[0]], " ")
	if !strings.Contains(sent, "--refine-agent claude") {
		t.Fatalf("sent %q, want --refine-agent claude", sent)
	}
}

func TestRefinePaneWithOutsideTmuxRefuses(t *testing.T) {
	d, home := routineDashboardDeps(t)
	f := &tmuxtest.Fake{}
	d.Tmux = f
	d.InTmux = func() bool { return false }
	if _, err := AddWith(d, "cli", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	err := RefinePaneWith(d, "cli", "codex")
	if err == nil {
		t.Fatal("expected refusal outside tmux")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pop routine edit cli") {
		t.Fatalf("refusal = %q, want CLI equivalent", msg)
	}
	if !strings.Contains(msg, "--refine-agent codex") {
		t.Fatalf("refusal = %q, want refine-agent in suggested command", msg)
	}
	if len(f.Live) != 0 || len(f.Windows) != 0 || len(f.SentCommands) != 0 {
		t.Fatalf("must not touch tmux: live=%v windows=%v sent=%v", f.Live, f.Windows, f.SentCommands)
	}
}
