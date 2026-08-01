package work

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// seedModelSkip records one Effort model skip in the isolated store the deps
// resolve. A zero until is a permanent skip.
func seedModelSkip(t *testing.T, d *Deps, preset, model string, until time.Time) {
	t.Helper()
	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutAgentModelCooldown(preset, model, until); err != nil {
		t.Fatal(err)
	}
}

// TestBuildSnapshotCarriesModelSkips pins that the snapshot carries the Effort
// model skips in force (ADR-0168) in preset-then-model order, so the dashboard
// footer can group them, and that they survive a build with no renderable repo
// group — the skip list is machine-global, not row-derived.
func TestBuildSnapshotCarriesModelSkips(t *testing.T) {
	d := &Deps{Tasks: workDataDeps(t)}
	now := time.Now().UTC()
	seedModelSkip(t, d, "kimi", "k2.7-code-highspeed", time.Time{})
	seedModelSkip(t, d, "cursor", "claude-sonnet-5", now.Add(2*time.Hour))
	seedModelSkip(t, d, "cursor", "claude-opus-5-thinking-high", now.Add(47*time.Minute))

	snap, err := BuildSnapshot(d, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelSkip{
		{Preset: "cursor", Model: "claude-opus-5-thinking-high"},
		{Preset: "cursor", Model: "claude-sonnet-5"},
		{Preset: "kimi", Model: "k2.7-code-highspeed"},
	}
	if len(snap.ModelSkips) != len(want) {
		t.Fatalf("ModelSkips = %+v, want %d entries", snap.ModelSkips, len(want))
	}
	for i, w := range want {
		got := snap.ModelSkips[i]
		if got.Preset != w.Preset || got.Model != w.Model {
			t.Fatalf("ModelSkips[%d] = %s/%s, want %s/%s", i, got.Preset, got.Model, w.Preset, w.Model)
		}
	}
	if !snap.ModelSkips[2].Until.IsZero() {
		t.Fatalf("permanent skip carries Until = %v, want zero", snap.ModelSkips[2].Until)
	}
}

// TestBuildSnapshotOmitsExpiredAndAbsentModelSkips pins the steady state: a
// machine that has skipped nothing, and one whose only skip has lapsed, both
// report none — the footer is hidden by having nothing to show.
func TestBuildSnapshotOmitsExpiredAndAbsentModelSkips(t *testing.T) {
	d := &Deps{Tasks: workDataDeps(t)}
	snap, err := BuildSnapshot(d, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.ModelSkips) != 0 {
		t.Fatalf("ModelSkips = %+v, want none for a machine with no skips", snap.ModelSkips)
	}

	seedModelSkip(t, d, "cursor", "claude-opus-5-thinking-high", time.Now().UTC().Add(-time.Minute))
	snap, err = BuildSnapshot(d, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.ModelSkips) != 0 {
		t.Fatalf("ModelSkips = %+v, want none for a lapsed skip", snap.ModelSkips)
	}
}
