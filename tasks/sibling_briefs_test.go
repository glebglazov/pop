package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// done→reset→done leaves two DONE records for the same task in progress.txt;
// only the current one is a live brief. The join must dedupe to the latest.
func TestCompletedAFKProgressDedupesToLatestRecord(t *testing.T) {
	dir := t.TempDir()
	progress := strings.Join([]string{
		"2026-06-10T09:00:00Z [01-afk.md] DONE",
		"stale brief from the abandoned attempt",
		"---",
		"2026-06-10T10:00:00Z [01-afk.md] RESET",
		"reset demo/01-afk to open (was done)",
		"---",
		"2026-06-10T11:00:00Z [01-afk.md] DONE",
		"current brief after the rework",
		"---",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "progress.txt"), []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Title: "Build", Type: "AFK", Status: "done"},
		},
	}

	completed := completedAFKProgress(DefaultDeps(), m)
	if len(completed) != 1 {
		t.Fatalf("expected exactly one deduped brief, got %d: %+v", len(completed), completed)
	}
	if got := completed[0].Summary; got != "current brief after the rework" {
		t.Fatalf("expected the latest DONE summary, got %q", got)
	}
	if got := completed[0].Timestamp; got != "2026-06-10T11:00:00Z" {
		t.Fatalf("expected the latest timestamp, got %q", got)
	}
}

// The worker carries only done-sibling briefs: failed and reset siblings teach
// it nothing about unrelated files, so they are excluded (ADR 0023).
func TestFormatSiblingCompletedBriefsOnlyDoneSiblings(t *testing.T) {
	dir := t.TempDir()
	progress := strings.Join([]string{
		"2026-06-10T09:00:00Z [01-done.md] DONE",
		"landed the storage layer",
		"---",
		"2026-06-10T09:30:00Z [02-failed.md] FAILED",
		"failed after 3 attempts: missing TASK_COMPLETE sentinel",
		"---",
		"2026-06-10T10:00:00Z [03-reset.md] RESET",
		"reset demo/03-reset to open (was done)",
		"---",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "progress.txt"), []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-done", File: "01-done.md", Title: "Storage", Type: "AFK", Status: "done"},
			{ID: "02-failed", File: "02-failed.md", Title: "Broken", Type: "AFK", Status: "failed"},
			{ID: "03-reset", File: "03-reset.md", Title: "Reopened", Type: "AFK", Status: "open"},
		},
	}

	briefs := formatSiblingCompletedBriefs(DefaultDeps(), m)
	if briefs == "" {
		t.Fatal("expected sibling briefs for the done sibling")
	}
	if !strings.Contains(briefs, "01-done") || !strings.Contains(briefs, "landed the storage layer") {
		t.Fatalf("expected the done sibling brief, got:\n%s", briefs)
	}
	for _, unwanted := range []string{"02-failed", "missing TASK_COMPLETE sentinel", "03-reset", "reset demo/03-reset"} {
		if strings.Contains(briefs, unwanted) {
			t.Fatalf("worker brief must exclude failed/reset churn, found %q in:\n%s", unwanted, briefs)
		}
	}
}

// Empty when no sibling has completed — nothing to carry into the worker.
func TestFormatSiblingCompletedBriefsEmptyWhenNoneDone(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Title: "Build", Type: "AFK", Status: "open"},
		},
	}
	if briefs := formatSiblingCompletedBriefs(DefaultDeps(), m); briefs != "" {
		t.Fatalf("expected empty briefs with no done siblings, got:\n%s", briefs)
	}
}
