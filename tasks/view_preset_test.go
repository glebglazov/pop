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
