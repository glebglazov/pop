package workload

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// printHITLGateAdvice prints the recovery options when run-issues stops on a
// Human-blocked Issue set held by an open HITL issue. The issue body is shown
// verbatim so the human sees what to do without opening the file, then the
// three paths — finish it by hand, edit and re-run, or defer it — are surfaced
// with copy-paste-ready commands so the operator can act without recalling the
// command vocabulary.
func printHITLGateAdvice(d *Deps, w io.Writer, stem, dir string, issue *Issue) {
	out := outputFor(w)
	hint := issuePathHint(stem, issue.File)
	fmt.Fprintln(out)
	out.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue. Options:", stem, issue.ID)
	printHITLIssueBody(d, out, hint, filepath.Join(dir, issue.File))
	fmt.Fprintln(out, "  finish by hand:")
	fmt.Fprintf(out, "                  %s\n", completeIssueHint(stem, issue.File))
	fmt.Fprintln(out, "  edit & re-run:")
	fmt.Fprintf(out, "                  $EDITOR %s && pop workload run-issues\n", hint)
	fmt.Fprintln(out, "  defer it:")
	fmt.Fprintf(out, "                  %s\n", skipIssueHint(stem, issue.File))
}

// printHITLIssueBody prints the blocking issue file verbatim between dim
// delimiters. Display is best-effort: a read failure prints a dim notice and
// leaves the surrounding advice intact.
func printHITLIssueBody(d *Deps, out *output, hint, path string) {
	fmt.Fprintln(out)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		out.line(ansiDim, "  (could not read %s: %v)", hint, err)
		fmt.Fprintln(out)
		return
	}
	out.line(ansiDim, "--- %s ---", hint)
	fmt.Fprintln(out, strings.TrimRight(string(data), "\n"))
	out.line(ansiDim, "--- end ---")
	fmt.Fprintln(out)
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
