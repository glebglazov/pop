package tasks

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// printHITLGateAdvice prints the recovery options when run-tasks stops on a
// Human-blocked Task set held by an open HITL task. The task body is shown
// verbatim so the human sees what to do without opening the file, then the
// three paths — finish it by hand, edit and re-run, or defer it — are surfaced
// with copy-paste-ready commands so the operator can act without recalling the
// command vocabulary.
func printHITLGateAdvice(d *Deps, w io.Writer, stem, dir string, task *Task) {
	out := outputFor(w)
	hint := taskPathHint(stem, task.File)
	fmt.Fprintln(out)
	out.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue. Options:", stem, task.ID)
	printHITLTaskBody(d, out, hint, filepath.Join(dir, task.File))
	fmt.Fprintln(out, "  finish by hand:")
	fmt.Fprintf(out, "                  %s\n", completeTaskHint(stem, task.File))
	fmt.Fprintln(out, "  edit & re-run:")
	fmt.Fprintf(out, "                  $EDITOR %s && pop tasks drain\n", hint)
	fmt.Fprintln(out, "  defer it:")
	fmt.Fprintf(out, "                  %s\n", skipTaskHint(stem, task.File))
}

// printHITLTaskBody prints the blocking task file verbatim between dim
// delimiters. Display is best-effort: a read failure prints a dim notice and
// leaves the surrounding advice intact.
func printHITLTaskBody(d *Deps, out *output, hint, path string) {
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

// printFailedStopAdvice prints recovery options for the failed tasks in a set
// that stopped run-tasks. Each failed task can be cleared and re-run, or
// finished by hand if the operator completed the work themselves. Prints
// nothing when no task in the manifest is failed.
func printFailedStopAdvice(w io.Writer, stem string, m *Manifest) {
	if m == nil {
		return
	}
	var failed []Task
	for _, task := range m.Tasks {
		if task.Status == "failed" {
			failed = append(failed, task)
		}
	}
	if len(failed) == 0 {
		return
	}
	out := outputFor(w)
	fmt.Fprintln(out)
	out.line(ansiRed, "Failed: clear the failure and re-run, or finish by hand. Options:")
	for _, task := range failed {
		fmt.Fprintf(out, "  %s/%s\n", stem, task.ID)
		fmt.Fprintln(out, "    re-run:")
		fmt.Fprintf(out, "                    %s\n", resetTaskHint(stem, task.File))
		fmt.Fprintln(out, "    finish by hand:")
		fmt.Fprintf(out, "                    %s\n", completeTaskHint(stem, task.File))
	}
}
