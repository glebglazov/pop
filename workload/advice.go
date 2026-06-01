package workload

import (
	"fmt"
	"io"
)

// printHITLGateAdvice prints the recovery options when run-issues stops on a
// Human-blocked Issue set held by an open HITL issue. The three paths — finish
// it by hand, edit and re-run, or defer it — are surfaced with copy-paste-ready
// commands so the operator can act without recalling the command vocabulary.
func printHITLGateAdvice(w io.Writer, stem string, issue *Issue) {
	hint := issuePathHint(stem, issue.File)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Human-blocked: %s/%s needs human work before the set can continue. Options:\n", stem, issue.ID)
	fmt.Fprintf(w, "  finish by hand: %s\n", completeIssueHint(stem, issue.File))
	fmt.Fprintf(w, "  edit & re-run:  edit %s, then pop workload run-issues\n", hint)
	fmt.Fprintf(w, "  defer it:       %s\n", skipIssueHint(stem, issue.File))
}

// printFailedStopAdvice prints recovery options for the failed issues in a set
// that stopped run-issues. Each failed issue can be cleared and re-run, or
// finished by hand if the operator completed the work themselves. Prints
// nothing when no issue in the manifest is failed.
func printFailedStopAdvice(w io.Writer, stem string, m *Manifest) {
	if m == nil {
		return
	}
	var failed []Issue
	for _, issue := range m.Issues {
		if issue.Status == "failed" {
			failed = append(failed, issue)
		}
	}
	if len(failed) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Failed: clear the failure and re-run, or finish by hand. Options:")
	for _, issue := range failed {
		fmt.Fprintf(w, "  %s/%s\n", stem, issue.ID)
		fmt.Fprintf(w, "    re-run:         %s\n", resetIssueHint(stem, issue.File))
		fmt.Fprintf(w, "    finish by hand: %s\n", completeIssueHint(stem, issue.File))
	}
}
