package routine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// Refine runs the routine refinement gate using default dependencies. An empty
// agentOverride resolves the authoring agent from config; a non-empty value
// overrides the preset for this gate session's chats (ADR-0125).
func Refine(id, agentOverride string) error {
	return RefineWith(defaultDeps, id, agentOverride)
}

// RefineWith runs the HITL refinement loop for a Routine (ADR-0125). It is the
// gate `pop routine add` drops into after scaffolding on a TTY and the gate bare
// `pop routine edit <id>` opens. The menu follows the house numbered gate
// grammar — line-based items, a `Choose [1]:` prompt read through a shared
// reader, word aliases accepted, static default 1 — not the dashboards'
// single-key TUI grammar. The loop only makes sense on an interactive session,
// so a non-interactive call errors and names the prompt path so the caller can
// edit it directly (or use `pop routine edit --schedule`).
func RefineWith(d *Deps, id, agentOverride string) error {
	if err := validateID(id); err != nil {
		return err
	}
	if _, err := loadManifest(d, id); err != nil {
		return err
	}
	// Validate the --agent override up front so an unknown preset is rejected
	// before the gate opens rather than only when the session is chosen.
	if strings.TrimSpace(agentOverride) != "" {
		if _, err := tasks.ResolveAgentAdapter(agentOverride); err != nil {
			return err
		}
	}

	out := d.Stdout
	if out == nil {
		out = os.Stdout
	}
	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	if d.IsInteractive == nil || !d.IsInteractive() {
		return fmt.Errorf("cannot refine routine %q in a non-interactive session; edit the prompt directly at %s or use pop routine edit --schedule", id, promptPath)
	}

	in := d.Stdin
	if in == nil {
		in = os.Stdin
	}
	reader := bufio.NewReader(in)

	for {
		r, err := loadManifest(d, id)
		if err != nil {
			return err
		}
		renderRefineMenu(out, id, r, lastRunSummary(d, id))
		fmt.Fprintf(out, "Choose [1]: ")
		answer, err := readRoutineGateLine(reader, "0")
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "1":
			authoringSessionFromGate(d, out, id, agentOverride)
		case "2", "fire":
			fireFromGate(d, out, id)
		case "3":
			viewLastReport(d, out, id)
		case "4":
			if _, err := EditWith(d, id, "", false); err != nil {
				fmt.Fprintf(out, "Could not open the prompt: %v\n", err)
			}
		case "5":
			editScheduleFromGate(d, out, reader, id)
		case "6", "resume":
			res, err := ResumeWith(d, id)
			if err != nil {
				return err
			}
			if res.NotPaused {
				fmt.Fprintf(out, "Routine %q was already resumed.\n", id)
			} else {
				fmt.Fprintf(out, "Resumed routine %q; scheduled firing is now armed.\n", id)
			}
			return nil
		case "0", "q", "quit", "exit":
			fmt.Fprintf(out, "Leaving routine %q paused.\n", id)
			return nil
		default:
			fmt.Fprintln(out, "Choose 1, 2, 3, 4, 5, 6, or 0.")
		}
	}
}

func renderRefineMenu(out io.Writer, id string, r *Routine, lastRun string) {
	state := "resumed"
	if r.Manifest.Paused {
		state = pausedStatusLabel(r.Manifest.PauseReason)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Refine routine %q — %s, schedule %q, %s\n", id, state, r.Manifest.Schedule, lastRun)
	fmt.Fprintln(out, "  1. Agent session (default)")
	fmt.Fprintln(out, "  2. Fire test run")
	fmt.Fprintln(out, "  3. View last report")
	fmt.Fprintln(out, "  4. Edit prompt")
	fmt.Fprintln(out, "  5. Edit schedule")
	fmt.Fprintln(out, "  6. Resume routine & exit")
	fmt.Fprintln(out, "  0. Exit (stay paused)")
}

// lastRunSummary describes the routine's most recent run for the gate header, or
// "no runs yet" when it has never fired (or the store is not reachable).
func lastRunSummary(d *Deps, id string) string {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil || !ok {
		return "no runs yet"
	}
	defer func() { _ = s.Close() }()
	run, err := s.LastRoutineRun(id)
	if err != nil || run == nil {
		return "no runs yet"
	}
	return fmt.Sprintf("last run %s (%s)", run.FiredAt.UTC().Format("2006-01-02T15:04:05Z"), run.Outcome)
}

// fireFromGate runs one real foreground Routine run from the gate. The run
// streams to the terminal, records a run row, keeps its report, and becomes the
// schedule anchor (ADR-0124); a failed run reports and the loop continues.
func fireFromGate(d *Deps, out io.Writer, id string) {
	res, err := FireWith(d, id)
	if err != nil {
		fmt.Fprintf(out, "Fire failed: %v\n", err)
		return
	}
	fmt.Fprintf(out, "Fire succeeded (agent %s). Report: %s\n", res.AgentPreset, res.ReportPath)
}

// viewLastReport opens the most recent run's report in $PAGER, or prints the
// expected path when there is no report to show yet.
func viewLastReport(d *Deps, out io.Writer, id string) {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil {
		fmt.Fprintf(out, "Could not open the run store: %v\n", err)
		return
	}
	if !ok {
		fmt.Fprintln(out, "No runs yet. Fire a test run first (option 2).")
		return
	}
	run, err := s.LastRoutineRun(id)
	_ = s.Close()
	if err != nil {
		fmt.Fprintf(out, "Could not read the last run: %v\n", err)
		return
	}
	if run == nil {
		fmt.Fprintln(out, "No runs yet. Fire a test run first (option 2).")
		return
	}
	path := run.ReportPath
	if path == "" {
		path = reportPathForRun(filepath.Join(routineDir(d, id), runsDirName), run.FiredAt)
	}
	if _, err := d.FS.Stat(path); err != nil {
		fmt.Fprintf(out, "No report for the last run yet: %s\n", path)
		return
	}
	if d.OpenPager == nil {
		fmt.Fprintf(out, "Report: %s\n", path)
		return
	}
	if err := d.OpenPager(path); err != nil {
		fmt.Fprintf(out, "Could not open the pager (report at %s): %v\n", path, err)
	}
}

// editScheduleFromGate reads a schedule expression and validates it through the
// schedule parser (via UpdateScheduleWith), re-prompting on a parse error. A
// blank line cancels, leaving the schedule unchanged.
func editScheduleFromGate(d *Deps, out io.Writer, reader *bufio.Reader, id string) {
	for {
		fmt.Fprintf(out, "New schedule (blank to cancel): ")
		line, err := readRoutineGateLine(reader, "")
		if err != nil {
			fmt.Fprintf(out, "Could not read the schedule: %v\n", err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprintln(out, "Schedule unchanged.")
			return
		}
		m, err := UpdateScheduleWith(d, id, line)
		if err != nil {
			fmt.Fprintf(out, "%v\n", err)
			continue
		}
		fmt.Fprintf(out, "Schedule updated to %q.\n", m.Schedule)
		return
	}
}

// readRoutineGateLine reads one gate line, mirroring the tasks-package prompt
// reader: a closed input with nothing pending resolves to eofDefault so the loop
// terminates instead of spinning on empty reads.
func readRoutineGateLine(reader *bufio.Reader, eofDefault string) (string, error) {
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read gate selection: %w", err)
	}
	if err == io.EOF && answer == "" {
		return eofDefault, nil
	}
	return strings.TrimRight(answer, "\r\n"), nil
}
