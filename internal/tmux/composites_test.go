package tmux_test

import (
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// The composites (Ensure, Attach, SwitchTarget) carry the create-if-missing
// and switch-vs-attach policy. They are covered here against the stateful
// fake — no argument arrays.

func TestSwitchTargetInsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true}

	if err := tmux.SwitchTarget(f, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Errorf("Switched = %v, want [%%5]", f.Switched)
	}
	if len(f.Attached) != 0 {
		t.Errorf("Attached = %v, want none", f.Attached)
	}
}

func TestSwitchTargetOutsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: false}

	if err := tmux.SwitchTarget(f, "work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Attached) != 1 || f.Attached[0] != "work" {
		t.Errorf("Attached = %v, want [work]", f.Attached)
	}
	if len(f.Switched) != 0 {
		t.Errorf("Switched = %v, want none", f.Switched)
	}
}

func TestEnsureCreatesWhenMissing(t *testing.T) {
	f := &tmuxtest.Fake{}

	if err := tmux.Ensure(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
}

func TestEnsureNoopWhenPresent(t *testing.T) {
	f := &tmuxtest.Fake{Live: map[string]string{"work": "/old"}}

	if err := tmux.Ensure(f, "work", "/new"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/old" {
		t.Errorf("Live[work] = %q, want /old (unchanged)", f.Live["work"])
	}
}

func TestAttachNewSessionInsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true}

	if err := tmux.Attach(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
	if len(f.Switched) != 1 || f.Switched[0] != "work" {
		t.Errorf("Switched = %v, want [work]", f.Switched)
	}
}

func TestAttachExistingSessionOutsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: false, Live: map[string]string{"work": "/proj"}}

	if err := tmux.Attach(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Attached) != 1 || f.Attached[0] != "work" {
		t.Errorf("Attached = %v, want [work]", f.Attached)
	}
	if len(f.Switched) != 0 {
		t.Errorf("Switched = %v, want none (session already existed)", f.Switched)
	}
}

// EnsureTaggedPane and EnsureWindow carry the spawn-flow policy (create session,
// find-or-create window, reuse-by-tag vs split+retile). Covered here against the
// stateful fake — no argument arrays.

func TestEnsureTaggedPaneFreshWindowReusesInitialPane(t *testing.T) {
	f := &tmuxtest.Fake{}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "/proj", "set-1", "run it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
	if panes := f.Windows["work"]["pop-queue"]; len(panes) != 1 || panes[0] != pane {
		t.Errorf("drain window panes = %v, want just the returned pane %q", panes, pane)
	}
	if got := f.PaneTagValues[pane][tmux.TagSet]; got != "set-1" {
		t.Errorf("tag = %q, want set-1", got)
	}
	if got := f.SentCommands[pane]; len(got) != 1 || got[0] != "run it Enter" {
		t.Errorf("sent = %v, want [\"run it Enter\"]", got)
	}
	if len(f.WindowRetiled) != 0 {
		t.Errorf("fresh single-pane window must not retile, got %v", f.WindowRetiled)
	}
}

func TestEnsureTaggedPaneExistingWindowSplitsAndRetiles(t *testing.T) {
	f := &tmuxtest.Fake{
		Live:    map[string]string{"work": "/proj"},
		Windows: map[string]map[string][]string{"work": {"pop-queue": {"%1"}}},
	}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagRoutine, "work", "/proj", "r1", "fire")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	panes := f.Windows["work"]["pop-queue"]
	if len(panes) != 2 || panes[1] != pane {
		t.Fatalf("panes = %v, want the sibling plus the split pane %q", panes, pane)
	}
	if got := f.PaneTagValues[pane][tmux.TagRoutine]; got != "r1" {
		t.Errorf("tag = %q, want r1", got)
	}
	if len(f.WindowRetiled) != 1 || f.WindowRetiled[0] != "work:pop-queue" {
		t.Errorf("WindowRetiled = %v, want [work:pop-queue]", f.WindowRetiled)
	}
}

func TestEnsureTaggedPaneReusesTaggedPane(t *testing.T) {
	f := &tmuxtest.Fake{}

	first, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "/proj", "set-1", "one")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "/proj", "set-1", "two")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second != first {
		t.Fatalf("second pane = %q, want reused %q", second, first)
	}
	if panes := f.Windows["work"]["pop-queue"]; len(panes) != 1 {
		t.Fatalf("panes = %v, want a single reused pane", panes)
	}
	if got := f.SentCommands[first]; len(got) != 2 {
		t.Fatalf("sent = %v, want two commands into the reused pane", got)
	}
}

func TestEnsureWindowCreatesAndReports(t *testing.T) {
	f := &tmuxtest.Fake{}

	pane, created, err := tmux.EnsureWindow(f, "work", "map-1", "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a fresh window")
	}
	if panes := f.Windows["work"]["map-1"]; len(panes) != 1 || panes[0] != pane {
		t.Fatalf("window panes = %v, want the returned pane %q", panes, pane)
	}

	again, created, err := tmux.EnsureWindow(f, "work", "map-1", "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("created = true, want false for an existing window")
	}
	if again != pane {
		t.Fatalf("reused pane = %q, want %q", again, pane)
	}
}

func TestFocusPaneSelectsThenSwitches(t *testing.T) {
	f := &tmuxtest.Fake{}

	if err := tmux.FocusPane(f, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Selected) != 1 || f.Selected[0] != "%5" {
		t.Errorf("Selected = %v, want [%%5]", f.Selected)
	}
	if len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Errorf("Switched = %v, want [%%5]", f.Switched)
	}
}
