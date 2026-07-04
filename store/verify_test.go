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
