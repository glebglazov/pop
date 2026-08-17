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

// The sentinel opens the final line. Prose glued after it is tolerated — that
// is how several models close out from a single message — while a sentinel
// narrated mid-sentence, or mentioned before the run's real close, is not.
func TestAssessCompletionRequiresFinalLineToOpenOnSentinel(t *testing.T) {
	md := []byte("## Acceptance criteria\n\n- [x] ok\n")

	for _, tc := range []struct {
		name   string
		output string
		want   bool
	}{
		{"own line", "SUMMARY_START\nok\nSUMMARY_END\nTASK_COMPLETE\n", true},
		// The shape cursor/grok emits: sign-off glued to the sentinel inside
		// one assistant message, so no transcript framing can split it.
		{"glued sign-off", "SUMMARY_START\nok\nSUMMARY_END\nTASK_COMPLETEThe work landed: tests pass.\n", true},
		{"buried mid-sentence", "SUMMARY_START\nok\nSUMMARY_END\nAll done TASK_COMPLETE for real.\n", false},
		{"narrated, then kept working", "I'll print TASK_COMPLETE once green.\nSUMMARY_START\nok\nSUMMARY_END\nstill running the suite\n", false},
		{"sentinel not the last word", "SUMMARY_START\nok\nSUMMARY_END\nTASK_COMPLETE\n\nOne more thought:\nlet me reconsider.\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := AssessCompletion(tc.output, md)
			if a.Complete != tc.want {
				t.Fatalf("Complete = %v, want %v (reason %q)", a.Complete, tc.want, a.FailedReason)
			}
			if !tc.want && a.FailedReason != reasonMissingSentinel {
				t.Fatalf("reason = %q, want %q", a.FailedReason, reasonMissingSentinel)
			}
		})
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
