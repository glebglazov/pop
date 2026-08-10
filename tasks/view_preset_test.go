package tasks

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

func TestMatchesPresetShippedActive(t *testing.T) {
	t.Parallel()
	active, ok := config.ShippedWorkViewPreset("active")
	if !ok {
		t.Fatal("shipped active missing")
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	ready := ViewFacts{ID: "2026-08-01-ready", Status: string(StatusReady)}
	if !MatchesPreset(ready, active, now) {
		t.Fatal("READY hidden by active")
	}
	foldedDone := ViewFacts{ID: "2026-08-01-done", Status: string(StatusDone), Unfolded: false}
	if MatchesPreset(foldedDone, active, now) {
		t.Fatal("folded DONE shown by active")
	}
	unfoldedDone := ViewFacts{ID: "2026-08-01-done", Status: string(StatusDone), Unfolded: true}
	if !MatchesPreset(unfoldedDone, active, now) {
		t.Fatal("unfolded DONE hidden by active")
	}
	archived := ViewFacts{ID: "2026-08-01-arch", Status: string(StatusReady), Archived: true}
	if MatchesPreset(archived, active, now) {
		t.Fatal("archived shown by active")
	}
}

// The mute fact is a comparison against the matching instant, not a stored
// flag: one set of facts reads muted before its window and unmuted after, with
// nothing in between having been written (ADR-0200 decision 8).
func TestMatchesPresetMutedIsEvaluatedAgainstNow(t *testing.T) {
	t.Parallel()
	active, _ := config.ShippedWorkViewPreset("active")
	muted, _ := config.ShippedWorkViewPreset("muted")
	all, _ := config.ShippedWorkViewPreset("all")

	until := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	facts := ViewFacts{ID: "2026-08-01-set", Status: string(StatusReady), MutedUntil: until}
	during := until.Add(-time.Hour)
	after := until.Add(time.Second)

	if MatchesPreset(facts, active, during) {
		t.Fatal("a live mute is still in the default view")
	}
	if !MatchesPreset(facts, muted, during) {
		t.Fatal("a live mute is missing from the muted preset")
	}
	if !MatchesPreset(facts, active, after) {
		t.Fatal("an elapsed mute did not return to the default view")
	}
	if MatchesPreset(facts, muted, after) {
		t.Fatal("an elapsed mute still reads as muted")
	}

	// A preset that says nothing about mute admits both — the tri-state's unset
	// arm, which is why only `active` had to change.
	if !MatchesPreset(facts, all, during) || !MatchesPreset(facts, all, after) {
		t.Fatal("all must be indifferent to mute")
	}
}

func TestMatchesPresetUnfoldedAndRecency(t *testing.T) {
	t.Parallel()
	unfolded, _ := config.ShippedWorkViewPreset("unfolded")
	recent7d, _ := config.ShippedWorkViewPreset("recent-7d")
	all, _ := config.ShippedWorkViewPreset("all")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	if !MatchesPreset(ViewFacts{ID: "x", Status: string(StatusDone), Unfolded: true}, unfolded, now) {
		t.Fatal("unfolded preset missed unfolded row")
	}
	if MatchesPreset(ViewFacts{ID: "x", Status: string(StatusReady), Unfolded: false}, unfolded, now) {
		t.Fatal("unfolded preset admitted folded row")
	}

	if !MatchesPreset(ViewFacts{ID: "2026-08-08-fresh", Status: string(StatusReady)}, recent7d, now) {
		t.Fatal("recent-7d missed in-window id")
	}
	if MatchesPreset(ViewFacts{ID: "2026-07-01-old", Status: string(StatusReady)}, recent7d, now) {
		t.Fatal("recent-7d admitted out-of-window id")
	}
	if MatchesPreset(ViewFacts{ID: "no-date-slug", Status: string(StatusReady)}, recent7d, now) {
		t.Fatal("recent-7d admitted undated id")
	}
	// The asymmetry a refactor would flatten: a window includes archived rows,
	// a state-scoped preset still files them away.
	archivedFresh := ViewFacts{ID: "2026-08-08-fresh", Status: string(StatusDone), Archived: true}
	if !MatchesPreset(archivedFresh, recent7d, now) {
		t.Fatal("recent-7d hid archived in-window id")
	}
	active, _ := config.ShippedWorkViewPreset("active")
	if MatchesPreset(archivedFresh, active, now) {
		t.Fatal("archived shown by active")
	}

	if !MatchesPreset(ViewFacts{ID: "x", Status: string(StatusDone), Archived: true}, all, now) {
		t.Fatal("all missed archived DONE")
	}
}

func TestIDCreatedAt(t *testing.T) {
	t.Parallel()
	got, ok := IDCreatedAt("2026-08-09-slug")
	if !ok || !got.Equal(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("date-only = %v ok=%v", got, ok)
	}
	got, ok = IDCreatedAt("2026-08-09-1430-slug")
	if !ok || !got.Equal(time.Date(2026, 8, 9, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("date-time = %v ok=%v", got, ok)
	}
	if _, ok := IDCreatedAt("no-prefix"); ok {
		t.Fatal("undated id parsed")
	}
}
