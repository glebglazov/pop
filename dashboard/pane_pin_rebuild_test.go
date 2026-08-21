package dashboard

import (
	"slices"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
)

// A pin is current, not historical (ADR-0209 decision 5): every poll asks the
// kinds again, so the block follows what the pane belongs to *now*. Each test
// here drives a real launch and then a real poll — the model's own reload command
// and the message it returns — because the whole claim is about the second build,
// which no launch-only test can see.

// bindStemTo binds one more registered set to the checkout mid-session, the way
// binding a set to a worktree does while the dashboard is open.
func bindStemTo(t *testing.T, d *drain.Deps, repo, stem, checkout string) {
	t.Helper()
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	key := drain.SetScopedKey(repoKey, stem)
	if err := binding.Put(d.Tasks, key, binding.Binding{RuntimePath: checkout, Branch: "wt/shared"}); err != nil {
		t.Fatal(err)
	}
}

// A drain starting in the checkout the pane sits in pins that set on the very next
// poll, with no relaunch — and when the drain ends, the pin goes with it and the
// row falls back to wherever the sort puts it.
func TestADrainGoingLiveInThePanesCheckoutPinsAndUnpinsAcrossRebuilds(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t)
	baseline := unpinnedOrder(t, d, cfg)

	m := openFromPane(t, d, cfg)
	if got := pinnedBlock(t, m); len(got) != 0 {
		t.Fatalf("pinned %v at launch, want nothing: no work is bound or running here yet", got)
	}

	drained := stems[len(stems)-1]
	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{{SetID: drained, RuntimePath: checkout, PID: 4242}}, nil
	}

	m = rebuild(t, m)
	if got := pinnedBlock(t, m); !slices.Equal(got, []string{drained}) {
		t.Fatalf("pinned %v after the drain went live, want %q — the pin appears the moment its cause does", got, drained)
	}
	if got, want := rowIDs(m), wantPinnedFirst(baseline, drained); !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence: the pin says it", m.flash.Text())
	}

	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }

	m = rebuild(t, m)
	if got := pinnedBlock(t, m); len(got) != 0 {
		t.Fatalf("pinned %v after the drain ended, want nothing: a pin that loses its cause un-pins", got)
	}
	if got := rowIDs(m); !slices.Equal(got, baseline) {
		t.Fatalf("rows = %v, want the sorted order the page has with no pin, %v", got, baseline)
	}
}

// Binding a second set to the pane's checkout joins it to the block on the next
// poll: the block is the answer to a question re-asked, not a list fixed at launch.
func TestASetBoundMidSessionJoinsThePinnedBlock(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "2026-01-01-done-1", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, _, rt := dashboardLaunchFixture(t, repo, setID)
	stems := registerDoneSets(t, repo, 3)
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	d.ViewPreset.Pin = true
	checkout := bindStemsToOneCheckout(t, d, repo, stems, 1)
	inPane(rt.Fake, "editor", "%7")
	rt.Fake.PaneCwd = map[string]string{"%7": checkout}

	m := openFromPane(t, d, cfg)
	if got := pinnedBlock(t, m); !slices.Equal(got, []string{stems[1]}) {
		t.Fatalf("pinned %v at launch, want the one bound set %q", got, stems[1])
	}

	bindStemTo(t, d, repo, stems[2], checkout)

	m = rebuild(t, m)
	got := pinnedBlock(t, m)
	if len(got) != 2 || !slices.Contains(got, stems[1]) || !slices.Contains(got, stems[2]) {
		t.Fatalf("pinned %v, want both sets now bound to the checkout (%q, %q)", got, stems[1], stems[2])
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence", m.flash.Text())
	}
}

// The re-derived answer costs no tmux: the facts are read once at launch and
// carried, because the dashboard's own pane cannot move while it runs.
func TestRebuildsReadThePaneNoSecondTime(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 1)
	fake := d.Tmux.(*queuetest.RecordingTmux).Fake

	m := openFromPane(t, d, cfg)
	launchReads := fake.CurrentPaneFactsCalls
	if launchReads != 1 {
		t.Fatalf("read the pane %d times at launch, want one round-trip", launchReads)
	}

	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{{SetID: stems[2], RuntimePath: checkout, PID: 7}}, nil
	}
	m = rebuild(t, m)
	m = rebuild(t, m)

	if fake.CurrentPaneFactsCalls != launchReads {
		t.Fatalf("read the pane %d times over two polls, want the launch's %d", fake.CurrentPaneFactsCalls, launchReads)
	}
	if got := pinnedBlock(t, m); !slices.Equal(got, []string{stems[2]}) {
		t.Fatalf("pinned %v, want the drained set %q — the carried facts still attribute", got, stems[2])
	}
}

// Rows rearrange under a human who is reading; their selection does not move. The
// block grows beneath the cursor and the same row stays selected.
func TestTheCursorIsUntouchedWhenThePinnedBlockChangesBeneathIt(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 1)

	m := openFromPane(t, d, cfg)
	// Somewhere other than the pinned row, so a block that grows above the cursor
	// really does change the index it sits at.
	var moved string
	for _, row := range m.snap.Containers {
		if !row.Pinned {
			moved = row.ID
			break
		}
	}
	if !m.list.SetCursorToKey(rowKeyFor(t, m, moved)) {
		t.Fatalf("could not move the cursor to %q", moved)
	}
	before := m.ListCursor()

	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{{SetID: stems[0], RuntimePath: checkout, PID: 11}}, nil
	}
	m = rebuild(t, m)

	if got := pinnedBlock(t, m); !slices.Equal(got, []string{stems[0]}) {
		t.Fatalf("pinned %v, want the newly drained set %q — the fixture must actually move rows", got, stems[0])
	}
	if got := cursorRow(t, m); got != moved {
		t.Fatalf("cursor on %q after the block changed, want the row the human left it on (%q)", got, moved)
	}
	if m.ListCursor() == before && rowIDs(m)[before] != moved {
		t.Fatalf("cursor stayed at index %d, which now holds another row: the selection is the row, not the index", before)
	}
}

// Decision 7 holds on a rebuild as much as on a launch: switching to a preset that
// hides the pinned row un-pins it, silently, and never widens the view to keep it.
func TestSwitchingToAPresetThatHidesAPinnedRowUnpinsItSilently(t *testing.T) {
	d, cfg, stems, _ := boundCheckoutFixture(t, 1)

	m := openFromPane(t, d, cfg)
	if got := pinnedBlock(t, m); !slices.Equal(got, []string{stems[1]}) {
		t.Fatalf("pinned %v at launch, want %q", got, stems[1])
	}

	// What the filter menu does when the operator picks a preset: install it on the
	// deps and reload.
	d.ViewPreset = config.WorkViewPreset{
		Name:  "_hide-done",
		Label: "in flight",
		Pin:   true,
		Hide:  &config.WorkViewPresetFilter{Status: []string{"done"}},
	}
	m = rebuild(t, m)

	if len(m.snap.Containers) != 0 {
		t.Fatalf("rows = %v, want none — the pin must not widen the preset the operator chose", rowIDs(m))
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence about a row nobody can see", m.flash.Text())
	}
}
