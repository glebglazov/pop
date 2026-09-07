package tmux_test

import (
	"errors"
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

// TestReadSurfacesNeverStartAServer covers ADR-0199 decision 8: listing verbs
// and the Ensure-free read path leave Fake.Live empty. Session-creating
// commands still populate it on demand.
func TestReadSurfacesNeverStartAServer(t *testing.T) {
	f := &tmuxtest.Fake{}

	if _, err := f.Sessions(); err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if _, err := f.WorkSessions(); err != nil {
		t.Fatalf("WorkSessions: %v", err)
	}
	if _, err := f.ListActivityPanes(); err != nil {
		t.Fatalf("ListActivityPanes: %v", err)
	}
	if _, err := f.ListWindowPanes(); err != nil {
		t.Fatalf("ListWindowPanes: %v", err)
	}
	if _, err := f.PaneCommands(); err != nil {
		t.Fatalf("PaneCommands: %v", err)
	}
	if _, err := f.AllPanes(); err != nil {
		t.Fatalf("AllPanes: %v", err)
	}
	if len(f.Live) != 0 {
		t.Fatalf("Live = %v after reads, want empty (no server started)", f.Live)
	}

	if err := tmux.Ensure(f, "work", "/proj"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Fatalf("Live[work] = %q after Ensure, want /proj (session-creating path starts the server)", f.Live["work"])
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

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "/proj", "set-1", "run it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
	if panes := f.Windows["work"]["pop-work"]; len(panes) != 1 || panes[0] != pane {
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
		Windows: map[string]map[string][]string{"work": {"pop-work": {"%1"}}},
	}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagRoutine, "work", tmux.DrainWindow, "/proj", "r1", "fire")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	panes := f.Windows["work"]["pop-work"]
	if len(panes) != 2 || panes[1] != pane {
		t.Fatalf("panes = %v, want the sibling plus the split pane %q", panes, pane)
	}
	if got := f.PaneTagValues[pane][tmux.TagRoutine]; got != "r1" {
		t.Errorf("tag = %q, want r1", got)
	}
	if len(f.WindowRetiled) != 1 || f.WindowRetiled[0] != "work:pop-work" {
		t.Errorf("WindowRetiled = %v, want [work:pop-work]", f.WindowRetiled)
	}
}

func TestEnsureTaggedPaneReusesTaggedPane(t *testing.T) {
	f := &tmuxtest.Fake{}

	first, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "/proj", "set-1", "one")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "/proj", "set-1", "two")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second != first {
		t.Fatalf("second pane = %q, want reused %q", second, first)
	}
	if panes := f.Windows["work"]["pop-work"]; len(panes) != 1 {
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

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "/trunk", "set-1", "one")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if f.PaneCwd[pane] != "/trunk" {
		t.Fatalf("initial pane cwd = %q, want /trunk", f.PaneCwd[pane])
	}

	reused, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "/worktree/set-1", "set-1", "two")
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
		Windows: map[string]map[string][]string{"work": {"pop-work": {"%1"}}},
		PaneCwd: map[string]string{"%1": "/stale"},
		PaneTagValues: map[string]map[tmux.PaneTag]string{
			"%1": {tmux.TagSet: "set-1"},
		},
	}

	pane, err := tmux.EnsureTaggedPane(f, tmux.TagSet, "work", tmux.DrainWindow, "", "set-1", "run")
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

func TestSpawnFreshPaneAlwaysCreatesUntagged(t *testing.T) {
	f := &tmuxtest.Fake{}

	first, err := tmux.SpawnFreshPane(f, "work", "/proj", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := tmux.SpawnFreshPane(f, "work", "/proj", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second == first {
		t.Fatalf("second pane = %q, want a fresh pane distinct from %q", second, first)
	}
	panes := f.Windows["work"]["pop-work"]
	if len(panes) != 2 {
		t.Fatalf("panes = %v, want two untagged shells", panes)
	}
	if len(f.PaneTagValues[first]) != 0 || len(f.PaneTagValues[second]) != 0 {
		t.Fatalf("shell panes must stay untagged, got %v", f.PaneTagValues)
	}
	if len(f.SentCommands[first]) != 0 || len(f.SentCommands[second]) != 0 {
		t.Fatalf("empty command must not send-keys, got %v", f.SentCommands)
	}
}

// EnsureSlottedPane and LowestFreePaneSlot are the plural of the tagged-pane
// flow: one container holds up to nine panes for an activity, each addressed by
// its own digit (ADR-0263).

func TestSlottedPanesFillTheRangeAndRefuseATenth(t *testing.T) {
	f := &tmuxtest.Fake{}

	byslot := map[tmux.PaneSlot]string{}
	for i := tmux.FirstPaneSlot; i <= tmux.LastPaneSlot; i++ {
		slot, err := tmux.LowestFreePaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1")
		if err != nil {
			t.Fatalf("free slot %d: %v", i, err)
		}
		if slot != i {
			t.Fatalf("free slot = %d, want the lowest free %d", slot, i)
		}
		pane, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, slot, "work", tmux.DrainWindow, "/proj", "set-1", "talk")
		if err != nil {
			t.Fatalf("spawn slot %d: %v", slot, err)
		}
		byslot[slot] = pane
	}

	if panes := f.Windows["work"]["pop-work"]; len(panes) != int(tmux.LastPaneSlot) {
		t.Fatalf("panes = %v, want %d distinct panes", panes, tmux.LastPaneSlot)
	}
	if _, err := tmux.LowestFreePaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1"); !errors.Is(err, tmux.ErrNoFreePaneSlot) {
		t.Fatalf("tenth slot error = %v, want ErrNoFreePaneSlot", err)
	}

	// Every pane still carries the bare container id, so every existing reader
	// of the activity tag sees what it always saw; the digit sits beside it.
	for slot, pane := range byslot {
		if got := f.PaneTagValues[pane][tmux.TagAssist]; got != "set-1" {
			t.Errorf("pane %s @pop_assist = %q, want set-1", pane, got)
		}
		if got := f.PaneTagValues[pane][tmux.TagSlot]; got != slot.String() {
			t.Errorf("pane %s slot = %q, want %s", pane, got, slot)
		}
	}

	// A named slot reaches the same pane for as long as that pane lives.
	reused, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, 3, "work", tmux.DrainWindow, "/proj", "set-1", "again")
	if err != nil {
		t.Fatalf("reuse slot 3: %v", err)
	}
	if reused != byslot[3] {
		t.Fatalf("slot 3 = %q, want the pane already there %q", reused, byslot[3])
	}
	if panes := f.Windows["work"]["pop-work"]; len(panes) != int(tmux.LastPaneSlot) {
		t.Fatalf("reuse spawned a twin: %v", panes)
	}
}

func TestLowestFreePaneSlotSkipsOnlyLiveSiblings(t *testing.T) {
	f := &tmuxtest.Fake{}

	first, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, tmux.FirstPaneSlot, "work", tmux.DrainWindow, "/proj", "set-1", "talk")
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	if _, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, 2, "work", tmux.DrainWindow, "/proj", "set-1", "talk"); err != nil {
		t.Fatalf("slot 2: %v", err)
	}
	// A pane spawned for another container never occupies this one's digits.
	if _, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, tmux.FirstPaneSlot, "work", tmux.DrainWindow, "/proj", "set-2", "talk"); err != nil {
		t.Fatalf("other set slot 1: %v", err)
	}
	slot, err := tmux.LowestFreePaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1")
	if err != nil {
		t.Fatalf("free slot: %v", err)
	}
	if slot != 3 {
		t.Fatalf("free slot = %d, want 3", slot)
	}

	// Closing the middle pane leaves its digit free rather than renumbering.
	f.Windows["work"]["pop-work"] = []string{first}
	if slot, err = tmux.LowestFreePaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1"); err != nil || slot != 2 {
		t.Fatalf("free slot after close = %d (%v), want 2", slot, err)
	}
}

// LowestHeldPaneSlot is where a caller that named no slot lands. The rule worth
// pinning is which pane wins when the container holds both: a live conversation
// on a higher digit beats an idle pane on a lower one, because landing in the
// idle one would restart a session nobody asked to restart.
func TestLowestHeldPaneSlotPrefersALiveSiblingOverALowerIdleOne(t *testing.T) {
	f := &tmuxtest.Fake{}
	if got, err := tmux.LowestHeldPaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1"); err != nil || got != tmux.FirstPaneSlot {
		t.Fatalf("held slot with no panes = %d (%v), want the first", got, err)
	}

	idle, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, tmux.FirstPaneSlot, "work", tmux.DrainWindow, "/proj", "set-1", "talk")
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	live, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, 2, "work", tmux.DrainWindow, "/proj", "set-1", "talk")
	if err != nil {
		t.Fatalf("slot 2: %v", err)
	}
	f.PaneInfos = map[string]tmux.PaneInfo{
		idle: {Session: "work", Command: "zsh"},
		live: {Session: "work", Command: "claude"},
	}
	if got, err := tmux.LowestHeldPaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1"); err != nil || got != 2 {
		t.Fatalf("held slot = %d (%v), want the live pane's 2", got, err)
	}

	// With nothing running, the lowest pane it holds is where it lands — the
	// respawn-in-place case.
	f.PaneInfos[live] = tmux.PaneInfo{Session: "work", Command: "zsh"}
	if got, err := tmux.LowestHeldPaneSlot(f, tmux.TagAssist, "work", tmux.DrainWindow, "set-1"); err != nil || got != tmux.FirstPaneSlot {
		t.Fatalf("held slot with none live = %d (%v), want the first", got, err)
	}
}

func TestEnsureSlottedPaneRefusesSlotOutsideRange(t *testing.T) {
	f := &tmuxtest.Fake{}
	if _, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, tmux.LastPaneSlot+1, "work", tmux.DrainWindow, "/proj", "set-1", "talk"); err == nil {
		t.Fatal("slot 10 spawned a pane, want a refusal")
	}
	if panes := f.Windows["work"]["pop-work"]; len(panes) != 0 {
		t.Fatalf("panes = %v, want none", panes)
	}
}

func TestEnsureSlottedPaneAdoptsThePreSlotPane(t *testing.T) {
	// The single-pane assist paths tagged their pane with no slot at all. Asking
	// for the lowest slot must land in that pane rather than beside it.
	f := &tmuxtest.Fake{}
	legacy, err := tmux.EnsureTaggedPane(f, tmux.TagAssist, "work", tmux.DrainWindow, "/proj", "set-1", "talk")
	if err != nil {
		t.Fatalf("legacy spawn: %v", err)
	}
	if got := f.PaneTagValues[legacy][tmux.TagSlot]; got != "" {
		t.Fatalf("EnsureTaggedPane stamped a slot %q, want none", got)
	}
	pane, err := tmux.EnsureSlottedPane(f, tmux.TagAssist, tmux.FirstPaneSlot, "work", tmux.DrainWindow, "/proj", "set-1", "again")
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	if pane != legacy {
		t.Fatalf("slot 1 = %q, want the pre-slot pane %q", pane, legacy)
	}
}
