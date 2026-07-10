package store

import (
	"testing"
	"time"
)

func TestGetVerifyVerdictMissingReturnsNil(t *testing.T) {
	s := openTestStore(t)
	got, err := s.GetVerifyVerdict("/repo/.git", "set-a", "sha1")
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("GetVerifyVerdict = %+v, want nil", got)
	}
}

func TestPutGetVerifyVerdictRoundTrip(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	v := VerifyVerdict{
		Repo:       "/repo/.git",
		SetID:      "set-a",
		WorkSHA:    "sha1",
		Verdict:    "FIXABLE",
		Findings:   "criterion 2 unmet",
		ComputedAt: now,
	}
	if err := s.PutVerifyVerdict(v); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
	got, err := s.GetVerifyVerdict("/repo/.git", "set-a", "sha1")
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	if got == nil {
		t.Fatalf("GetVerifyVerdict = nil, want a row")
	}
	if got.Verdict != "FIXABLE" || got.Findings != "criterion 2 unmet" {
		t.Fatalf("GetVerifyVerdict = %+v, want FIXABLE / criterion 2 unmet", got)
	}
	if !got.ComputedAt.Equal(now) {
		t.Fatalf("ComputedAt = %v, want %v", got.ComputedAt, now)
	}
}

// TestPutGetVerifyVerdictHumanAuthoredRoundTrip: an Accepted verdict (ADR-0103)
// round-trips its human-authored provenance and note, and GetLatestAcceptedNote
// returns that note. Pre-existing agent-authored rows read as not-human with no
// note, and never surface through GetLatestAcceptedNote.
func TestPutGetVerifyVerdictHumanAuthoredRoundTrip(t *testing.T) {
	s := openTestStore(t)
	accepted := VerifyVerdict{
		Repo:          "/repo/.git",
		SetID:         "set-a",
		WorkSHA:       "sha1",
		Verdict:       "PASS",
		Scope:         2,
		HumanAuthored: true,
		Note:          "the timeout is intentional",
	}
	if err := s.PutVerifyVerdict(accepted); err != nil {
		t.Fatalf("PutVerifyVerdict accepted: %v", err)
	}
	got, err := s.GetVerifyVerdict("/repo/.git", "set-a", "sha1")
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	if got == nil || !got.HumanAuthored || got.Note != "the timeout is intentional" {
		t.Fatalf("GetVerifyVerdict = %+v, want human-authored PASS carrying the note", got)
	}
	// The latest PASS read carries provenance too (status derivation is unchanged;
	// this only proves the columns survive the PASS query path).
	pass, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if pass == nil || !pass.HumanAuthored || pass.Note != "the timeout is intentional" {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want human-authored PASS carrying the note", pass)
	}
	note, err := s.GetLatestAcceptedNote("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestAcceptedNote: %v", err)
	}
	if note != "the timeout is intentional" {
		t.Fatalf("GetLatestAcceptedNote = %q, want the accepted note", note)
	}
}

// TestGetLatestAcceptedNoteIgnoresAgentAndEmptyNotes: an agent-authored verdict
// (the default shape) never surfaces as an accepted note, and a human-authored
// row with an empty note is skipped in favour of the most recent noted accept.
func TestGetLatestAcceptedNoteIgnoresAgentAndEmptyNotes(t *testing.T) {
	s := openTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	// An ordinary agent PASS (no provenance, no note).
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-agent", Verdict: "PASS", ComputedAt: base.Add(-2 * time.Hour)}); err != nil {
		t.Fatalf("PutVerifyVerdict agent: %v", err)
	}
	// No accepted note yet.
	if note, err := s.GetLatestAcceptedNote("/repo/.git", "set-a"); err != nil || note != "" {
		t.Fatalf("GetLatestAcceptedNote = (%q, %v), want empty before any accept", note, err)
	}
	// A human accept carrying a note.
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-accept", Verdict: "PASS", HumanAuthored: true, Note: "known non-issue", ComputedAt: base}); err != nil {
		t.Fatalf("PutVerifyVerdict accept: %v", err)
	}
	note, err := s.GetLatestAcceptedNote("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestAcceptedNote: %v", err)
	}
	if note != "known non-issue" {
		t.Fatalf("GetLatestAcceptedNote = %q, want the human accept note", note)
	}
}

func TestPutVerifyVerdictOverwritesSameSHA(t *testing.T) {
	s := openTestStore(t)
	base := VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha1", Verdict: "FIXABLE", Findings: "first"}
	if err := s.PutVerifyVerdict(base); err != nil {
		t.Fatalf("PutVerifyVerdict first: %v", err)
	}
	base.Verdict = "PASS"
	base.Findings = ""
	if err := s.PutVerifyVerdict(base); err != nil {
		t.Fatalf("PutVerifyVerdict second: %v", err)
	}
	got, err := s.GetVerifyVerdict("/repo/.git", "set-a", "sha1")
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	if got == nil || got.Verdict != "PASS" || got.Findings != "" {
		t.Fatalf("GetVerifyVerdict = %+v, want overwritten PASS with no findings", got)
	}
	// Exactly one row survived the overwrite.
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM verify_verdicts`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
}

func TestGetLatestPassVerifyVerdictMissingReturnsNil(t *testing.T) {
	s := openTestStore(t)
	got, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want nil", got)
	}
}

func TestGetLatestPassVerifyVerdictNonPassReturnsNil(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha1", Verdict: "NEEDS-HUMAN"}); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
	got, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want nil", got)
	}
}

func TestGetLatestPassVerifyVerdictReturnsMostRecentPass(t *testing.T) {
	s := openTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-old", Verdict: "PASS", ComputedAt: base.Add(-time.Hour)}); err != nil {
		t.Fatalf("PutVerifyVerdict old: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-new", Verdict: "PASS", ComputedAt: base}); err != nil {
		t.Fatalf("PutVerifyVerdict new: %v", err)
	}
	got, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got == nil || got.WorkSHA != "sha-new" {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want sha-new", got)
	}
}

func TestGetLatestPassVerifyVerdictIgnoresNonPassRows(t *testing.T) {
	s := openTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-pass", Verdict: "PASS", ComputedAt: base.Add(-time.Hour)}); err != nil {
		t.Fatalf("PutVerifyVerdict pass: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha-fail", Verdict: "NEEDS-HUMAN", Findings: "nope", ComputedAt: base}); err != nil {
		t.Fatalf("PutVerifyVerdict fail: %v", err)
	}
	got, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got == nil || got.WorkSHA != "sha-pass" {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want sha-pass", got)
	}
}

func TestGetLatestPassVerifyVerdictIsolatedByRepoAndSet(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo-a/.git", SetID: "set-a", WorkSHA: "sha1", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict repo-a: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo-b/.git", SetID: "set-a", WorkSHA: "sha1", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict repo-b: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo-a/.git", SetID: "set-b", WorkSHA: "sha1", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict set-b: %v", err)
	}
	got, err := s.GetLatestPassVerifyVerdict("/repo-a/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got == nil || got.Repo != "/repo-a/.git" || got.SetID != "set-a" {
		t.Fatalf("GetLatestPassVerifyVerdict = %+v, want repo-a set-a", got)
	}
}

func TestVerifyVerdictKeyedBySHA(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "old", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict old: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "new", Verdict: "NEEDS-HUMAN", Findings: "n"}); err != nil {
		t.Fatalf("PutVerifyVerdict new: %v", err)
	}
	// A verdict is looked up by the current work SHA; the other SHA is a separate row.
	old, err := s.GetVerifyVerdict("/repo/.git", "set-a", "old")
	if err != nil {
		t.Fatalf("GetVerifyVerdict old: %v", err)
	}
	if old == nil || old.Verdict != "PASS" {
		t.Fatalf("old verdict = %+v, want PASS", old)
	}
	fresh, err := s.GetVerifyVerdict("/repo/.git", "set-a", "new")
	if err != nil {
		t.Fatalf("GetVerifyVerdict new: %v", err)
	}
	if fresh == nil || fresh.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("new verdict = %+v, want NEEDS-HUMAN", fresh)
	}
}

func TestInvalidateVerifyVerdictsDeletesAllRowsForRepoSet(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha1", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict sha1: %v", err)
	}
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-a", WorkSHA: "sha2", Verdict: "FIXABLE", Findings: "x"}); err != nil {
		t.Fatalf("PutVerifyVerdict sha2: %v", err)
	}
	// A different set in the same repo should be untouched.
	if err := s.PutVerifyVerdict(VerifyVerdict{Repo: "/repo/.git", SetID: "set-b", WorkSHA: "sha1", Verdict: "PASS"}); err != nil {
		t.Fatalf("PutVerifyVerdict set-b: %v", err)
	}

	if err := s.InvalidateVerifyVerdicts("/repo/.git", "set-a"); err != nil {
		t.Fatalf("InvalidateVerifyVerdicts: %v", err)
	}

	got, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-a")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("set-a still has a verdict after invalidation: %+v", got)
	}
	other, err := s.GetLatestPassVerifyVerdict("/repo/.git", "set-b")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict set-b: %v", err)
	}
	if other == nil {
		t.Fatal("set-b verdict was incorrectly invalidated")
	}
}

func TestInvalidateVerifyVerdictsMissingIsNoOp(t *testing.T) {
	s := openTestStore(t)
	if err := s.InvalidateVerifyVerdicts("/repo/.git", "set-a"); err != nil {
		t.Fatalf("InvalidateVerifyVerdicts on empty store: %v", err)
	}
}
