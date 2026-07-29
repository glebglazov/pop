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
	if len(f.Respawned) != 0 {
		t.Fatalf("same directory must not respawn, got %v", f.Respawned)
	}
}

func TestEnsureTaggedPaneReusedPaneCorrectsDirectory(t *testing.T) {
	f := &tmuxtest.Fake{}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "/trunk", "set-1", "one")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if f.PaneCwd[pane] != "/trunk" {
		t.Fatalf("initial pane cwd = %q, want /trunk", f.PaneCwd[pane])
	}

	reused, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "/worktree/set-1", "set-1", "two")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if reused != pane {
		t.Fatalf("pane = %q, want reused %q", reused, pane)
	}
	if got := f.Respawned[pane]; got != "/worktree/set-1" {
		t.Fatalf("Respawned[%s] = %q, want /worktree/set-1", pane, got)
	}
	if f.PaneCwd[pane] != "/worktree/set-1" {
		t.Fatalf("pane cwd after respawn = %q, want /worktree/set-1", f.PaneCwd[pane])
	}
	if got := f.SentCommands[pane]; len(got) != 2 || got[1] != "two Enter" {
		t.Fatalf("sent = %v, want two commands ending with the worktree spawn", got)
	}
}

func TestEnsureTaggedPaneReusedPaneEmptyDirSkipsCorrection(t *testing.T) {
	f := &tmuxtest.Fake{
		Live:    map[string]string{"work": "/proj"},
		Windows: map[string]map[string][]string{"work": {"pop-queue": {"%1"}}},
		PaneCwd: map[string]string{"%1": "/stale"},
		PaneTagValues: map[string]map[tmux.PaneTag]string{
			"%1": {tmux.TagSet: "set-1"},
		},
	}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", "", "set-1", "run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pane != "%1" {
		t.Fatalf("pane = %q, want reused %%1", pane)
	}
	if len(f.Respawned) != 0 {
		t.Fatalf("empty dir must not respawn, got %v", f.Respawned)
	}
	if f.PaneCwd["%1"] != "/stale" {
		t.Fatalf("pane cwd = %q, want stale directory left unchanged", f.PaneCwd["%1"])
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

// SwitchAndZoom preserves the zoom-only-if-not-zoomed behaviour and the
// switch-vs-attach + zoom-order policy across the tmux boundary.

func TestSwitchAndZoomInsideZoomsUnzoomedWindow(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true}

	if err := tmux.SwitchAndZoom(f, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Errorf("Switched = %v, want [%%5]", f.Switched)
	}
	if !f.Zoomed["%5"] {
		t.Error("target window not zoomed")
	}
}

func TestSwitchAndZoomLeavesZoomedWindowMaximized(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true, Zoomed: map[string]bool{"%5": true}}

	if err := tmux.SwitchAndZoom(f, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Errorf("Switched = %v, want [%%5]", f.Switched)
	}
	// An already-zoomed window must not be toggled back to a split layout.
	if !f.Zoomed["%5"] {
		t.Error("already-zoomed window was toggled off")
	}
}

func TestSwitchAndZoomOutsideZoomsBeforeAttach(t *testing.T) {
	f := &tmuxtest.Fake{Inside: false}

	if err := tmux.SwitchAndZoom(f, "work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Attached) != 1 || f.Attached[0] != "work" {
		t.Errorf("Attached = %v, want [work]", f.Attached)
	}
	if len(f.Switched) != 0 {
		t.Errorf("Switched = %v, want none outside tmux", f.Switched)
	}
	if !f.Zoomed["work"] {
		t.Error("target window not zoomed before attach")
	}
}
