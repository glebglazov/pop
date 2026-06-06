package tasks

import (
	"strings"
	"testing"
)

func TestAssessCompletionSuccess(t *testing.T) {
	output := "working...\nSUMMARY_START\ndid the thing\nSUMMARY_END\nTASK_COMPLETE\n"
	md := []byte("## Acceptance criteria\n\n- [x] ok\n")
	a := AssessCompletion(output, md)
	if !a.Complete || !a.AllChecked || a.Summary != "did the thing" {
		t.Fatalf("assessment = %#v", a)
	}
}

func TestAssessCompletionMissingSentinel(t *testing.T) {
	a := AssessCompletion("done\n", []byte("- [x] ok\n"))
	if a.Complete || a.FailedReason == "" {
		t.Fatalf("assessment = %#v", a)
	}
}

func TestAssessCompletionTaskFailed(t *testing.T) {
	a := AssessCompletion("oops\nTASK_FAILED: blocked\n", nil)
	if a.Complete || a.FailedReason != "blocked" {
		t.Fatalf("assessment = %#v", a)
	}
}

func TestAssessCompletionUncheckedBoxes(t *testing.T) {
	output := "SUMMARY_START\nok\nSUMMARY_END\nTASK_COMPLETE"
	md := []byte("## Acceptance criteria\n\n- [ ] todo\n- [x] done\n")
	a := AssessCompletion(output, md)
	if a.Complete || !strings.Contains(a.FailedReason, "acceptance") {
		t.Fatalf("assessment = %#v", a)
	}
}

func TestCommitSubject(t *testing.T) {
	got := CommitSubject("feature", "01-a")
	if got != "tasks(feature): 01-a" {
		t.Fatalf("subject = %q", got)
	}
}

func TestCommitSubjectStripsTimestampPrefix(t *testing.T) {
	if got := CommitSubject("2026-06-06-feature", "01-a"); got != "tasks(feature): 01-a" {
		t.Fatalf("subject = %q", got)
	}
	if got := CommitSubject("2026-06-06-2036-feature", "01-a"); got != "tasks(feature): 01-a" {
		t.Fatalf("subject = %q", got)
	}
}

func TestDirtyCheckpointSubject(t *testing.T) {
	got := DirtyCheckpointSubject("feature", "01-a")
	if got != "tasks(feature): 01-a capturing dirty state" {
		t.Fatalf("subject = %q", got)
	}
}
