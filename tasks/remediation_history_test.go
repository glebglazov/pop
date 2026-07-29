package tasks

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRemediationProgress(t *testing.T, dir string, blocks ...string) {
	t.Helper()
	content := strings.Join(blocks, "\n---\n") + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "progress.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// CollectDoneRemediationHistory is remediation-only: ordinary done AFK work must
// not appear in the gate block (ADR-0154).
func TestCollectDoneRemediationHistoryScopesToRemediationOnly(t *testing.T) {
	dir := t.TempDir()
	writeRemediationProgress(t, dir,
		"2026-06-10T09:00:00Z [01-a.md] DONE\nlanded the storage layer",
		"2026-06-10T10:00:00Z [02-remediation.md] DONE\nfixed the flaky retry",
	)

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "Build", Type: "AFK", Status: "done"},
			{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: flaky retry", Type: "AFK", Status: "done"},
		},
	}

	entries := CollectDoneRemediationHistory(DefaultDeps(), m)
	if len(entries) != 1 {
		t.Fatalf("expected one remediation entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Title != "Remediation 1: flaky retry" {
		t.Fatalf("title = %q", entries[0].Title)
	}
	if entries[0].Summary != "fixed the flaky retry" {
		t.Fatalf("summary = %q", entries[0].Summary)
	}
}

// Every done remediation in the set is returned in manifest order, not just the
// latest verify episode.
func TestCollectDoneRemediationHistoryListsAllDoneInOrder(t *testing.T) {
	dir := t.TempDir()
	writeRemediationProgress(t, dir,
		"2026-06-10T09:00:00Z [02-remediation.md] DONE\nfirst repair",
		"2026-06-10T10:00:00Z [04-remediation.md] DONE\nsecond repair",
	)

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
			{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: first", Type: "AFK", Status: "done"},
			{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "done"},
			{ID: "04-remediation", File: "04-remediation.md", Title: "Remediation 2: second", Type: "AFK", Status: "done"},
			{ID: "05-remediation", File: "05-remediation.md", Title: "Remediation 3: open", Type: "AFK", Status: "open"},
		},
	}

	entries := CollectDoneRemediationHistory(DefaultDeps(), m)
	if len(entries) != 2 {
		t.Fatalf("expected two done remediations, got %d: %+v", len(entries), entries)
	}
	if got := entries[0].TaskID; got != "02-remediation" {
		t.Fatalf("first entry = %q, want 02-remediation", got)
	}
	if got := entries[1].TaskID; got != "04-remediation" {
		t.Fatalf("second entry = %q, want 04-remediation", got)
	}
}

// done→reset→done leaves two DONE records; only the latest summary is live.
func TestCollectDoneRemediationHistoryDedupesToLatestRecord(t *testing.T) {
	dir := t.TempDir()
	writeRemediationProgress(t, dir,
		"2026-06-10T09:00:00Z [02-remediation.md] DONE\nstale repair claim",
		"2026-06-10T10:00:00Z [02-remediation.md] RESET\nreset demo/02-remediation to open (was done)",
		"2026-06-10T11:00:00Z [02-remediation.md] DONE\ncurrent repair claim",
	)

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: retry", Type: "AFK", Status: "done"},
		},
	}

	entries := CollectDoneRemediationHistory(DefaultDeps(), m)
	if len(entries) != 1 || entries[0].Summary != "current repair claim" {
		t.Fatalf("expected latest summary, got %+v", entries)
	}
}

func TestCollectDoneRemediationHistoryEmptyWhenNoneDone(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation", Type: "AFK", Status: "open"},
		},
	}
	if entries := CollectDoneRemediationHistory(DefaultDeps(), m); entries != nil {
		t.Fatalf("expected nil with no done remediations, got %+v", entries)
	}
}

func TestCapRemediationSummaryLimitsLinesAndChars(t *testing.T) {
	manyLines := strings.Join([]string{
		"line 1", "line 2", "line 3", "line 4", "line 5",
		"line 6", "line 7", "line 8", "line 9", "line 10", "line 11",
	}, "\n")
	capped, truncated := CapRemediationSummary(manyLines)
	if !truncated {
		t.Fatal("expected line truncation")
	}
	if strings.Contains(capped, "line 11") {
		t.Fatalf("eleventh line must be clipped:\n%s", capped)
	}
	if !strings.Contains(capped, "line 10") {
		t.Fatalf("tenth line should remain:\n%s", capped)
	}

	longLine := strings.Repeat("x", remediationHistoryMaxChars+50)
	capped, truncated = CapRemediationSummary(longLine)
	if !truncated {
		t.Fatal("expected char truncation")
	}
	if len(capped) > remediationHistoryMaxChars+3 {
		t.Fatalf("capped length = %d, want <= %d", len(capped), remediationHistoryMaxChars+3)
	}
	if !strings.HasSuffix(capped, "…") {
		t.Fatalf("char truncation must be visible, got %q", capped)
	}
}

func TestFormatRemediationReviewBlockShowsStreamHintWhenTruncated(t *testing.T) {
	entries := []RemediationHistoryEntry{{
		TaskID:    "02-remediation",
		File:      "02-remediation.md",
		Title:     "Remediation 1: flaky retry",
		Summary:   "short claim",
		Truncated: true,
	}}
	block := FormatRemediationReviewBlock("demo", entries)
	for _, want := range []string{
		"Remediation work:",
		"Remediation 1: flaky retry",
		"short claim",
		"truncated; full narrative: pop tasks stream demo/02-remediation.md",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
}

func TestFormatRemediationReviewBlockEmptyWhenNoEntries(t *testing.T) {
	if block := FormatRemediationReviewBlock("demo", nil); block != "" {
		t.Fatalf("expected empty block, got %q", block)
	}
}

// HITL gate prints the remediation block above the menu when done remediations exist.
func TestHITLGatePrintsRemediationReviewBlock(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: retry cap", Type: "AFK", Status: "done"},
		{ID: "03-hitl", File: "03-hitl.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}, nil)
	writeRemediationProgress(t, m.Dir, "2026-06-10T09:00:00Z [02-remediation.md] DONE\nraised the retry cap to three")

	var out strings.Builder
	_, err := promptHITLGateAction(&out, d, "/rt", bufio.NewReader(strings.NewReader("0\n")), "demo", m, &m.Tasks[2], "## Acceptance criteria\n\n- [ ] ok\n", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Remediation work:",
		"Remediation 1: retry cap",
		"raised the retry cap to three",
		"1. Get agent assistance (default)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("HITL gate missing %q:\n%s", want, got)
		}
	}
	if idxBody := strings.Index(got, "--- 03-hitl.md ---"); idxBody < 0 {
		t.Fatalf("expected gate task body:\n%s", got)
	} else if idxRem := strings.Index(got, "Remediation work:"); idxRem < 0 {
		t.Fatalf("expected remediation block:\n%s", got)
	} else if idxMenu := strings.Index(got, "1. Get agent assistance"); idxMenu < 0 {
		t.Fatalf("expected menu:\n%s", got)
	} else if !(idxBody < idxRem && idxRem < idxMenu) {
		t.Fatalf("remediation block must sit between task body and menu:\n%s", got)
	}
}

// Verify-fail gate prints the same remediation block above its menu.
func TestVerifyFailedGatePrintsRemediationReviewBlock(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: policy", Type: "AFK", Status: "done"},
	}, nil)
	writeRemediationProgress(t, m.Dir, "2026-06-10T09:00:00Z [02-remediation.md] DONE\nnormalized the retry policy")

	var out strings.Builder
	_, err := promptVerifyFailedGateAction(&out, d, "/rt", bufio.NewReader(strings.NewReader("0\n")), "demo", m, "still flaky on CI", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Remediation work:",
		"Remediation 1: policy",
		"normalized the retry policy",
		"1. Accept (record a human-authored PASS)",
		"Findings:",
		"still flaky on CI",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("verify-fail gate missing %q:\n%s", want, got)
		}
	}
	if idxFindings := strings.Index(got, "Findings:"); idxFindings < 0 {
		t.Fatalf("expected findings:\n%s", got)
	} else if idxRem := strings.Index(got, "Remediation work:"); idxRem < 0 {
		t.Fatalf("expected remediation block:\n%s", got)
	} else if idxMenu := strings.Index(got, "1. Accept"); idxMenu < 0 {
		t.Fatalf("expected menu:\n%s", got)
	} else if !(idxFindings < idxRem && idxRem < idxMenu) {
		t.Fatalf("remediation block must sit between findings and menu:\n%s", got)
	}
}
