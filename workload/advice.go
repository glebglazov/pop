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
	out := outputFor(w)
	hint := issuePathHint(stem, issue.File)
	fmt.Fprintln(out)
	out.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue. Options:", stem, issue.ID)
	fmt.Fprintln(out, "  finish by hand:")
	fmt.Fprintf(out, "                  %s\n", completeIssueHint(stem, issue.File))
	fmt.Fprintln(out, "  edit & re-run:")
	fmt.Fprintf(out, "                  $EDITOR %s && pop workload run-issues\n", hint)
	fmt.Fprintln(out, "  defer it:")
	fmt.Fprintf(out, "                  %s\n", skipIssueHint(stem, issue.File))
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
	out := outputFor(w)
	fmt.Fprintln(out)
	out.line(ansiRed, "Failed: clear the failure and re-run, or finish by hand. Options:")
	for _, issue := range failed {
		fmt.Fprintf(out, "  %s/%s\n", stem, issue.ID)
		fmt.Fprintln(out, "    re-run:")
		fmt.Fprintf(out, "                    %s\n", resetIssueHint(stem, issue.File))
		fmt.Fprintln(out, "    finish by hand:")
		fmt.Fprintf(out, "                    %s\n", completeIssueHint(stem, issue.File))
	}
}
