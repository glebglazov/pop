package routine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/tasks"
)

// Refine runs the routine refinement gate using default dependencies. An empty
// agentOverride resolves the authoring agent from config; a non-empty value
// overrides the preset for this gate session's chats (ADR-0125).
func Refine(id, agentOverride string) error {
	return RefineWith(defaultDeps, id, agentOverride)
}

// RefineWith runs the HITL refinement loop for a Routine (ADR-0125). It is the
// gate `pop routine new` drops into after scaffolding on a TTY and the gate bare
// `pop routine edit <id>` opens. The menu is the shared inline GateMenu
// (ADR-0196) — same keys and feel as the Task-set gates. The loop only makes
// sense on an interactive session, so a non-interactive call errors and names
// the prompt path so the caller can edit it directly (or use
// `pop routine edit --schedule`).
func RefineWith(d *Deps, id, agentOverride string) error {
	// A Project routine is a committed prompt refined in its checkout, not an
	// authored manifest in pop's data dir (ADR-0138): it has no schedule and no
	// pause state, so it runs its own gate with a project-aware briefing.
	if resolvesToProjectRoutine(d, id) {
		return refineProjectRoutine(d, id, agentOverride)
	}
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
	reader := tty.NewReader(in)

	for {
		r, err := loadManifest(d, id)
		if err != nil {
			return err
		}
		choice, err := promptRoutineGateMenu(out, in, reader, refineGateSpec(id, r, lastRunSummary(d, id)), d)
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			authoringSessionFromGate(d, out, id, agentOverride)
		case "2":
			fireFromGate(d, out, id)
		case "3":
			viewLastReport(d, out, id, filepath.Join(routineDir(d, id), runsDirName))
		case "4":
			if _, err := EditWith(d, id, "", false); err != nil {
				fmt.Fprintf(out, "Could not open the prompt: %v\n", err)
			}
		case "5":
			editScheduleFromGate(d, out, reader, id)
		case "6":
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
		case "0", "":
			fmt.Fprintf(out, "Leaving routine %q paused.\n", id)
			return nil
		}
	}
}

// lastRunSummary describes the routine's most recent run for the gate header, or
// "no runs yet" when it has never fired (or the store is not reachable).
func lastRunSummary(d *Deps, id string) string {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil || !ok {
		return "no runs yet"
	}
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
// expected path when there is no report to show yet. It keys the run lookup on
// storeID and resolves a report-less run against runsDir, so authored and
// per-checkout Project routines (ADR-0138) share the one seam.
func viewLastReport(d *Deps, out io.Writer, storeID, runsDir string) {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil {
		fmt.Fprintf(out, "Could not open the run store: %v\n", err)
		return
	}
	if !ok {
		fmt.Fprintln(out, "No runs yet. Fire a test run first (option 2).")
		return
	}
	run, err := s.LastRoutineRun(storeID)
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
		path = reportPathForRun(runsDir, run.FiredAt)
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
func editScheduleFromGate(d *Deps, out io.Writer, reader *tty.Reader, id string) {
	for {
		fmt.Fprintf(out, "New schedule (blank to cancel): ")
		line, err := readRoutineGateLine(reader, out, "")
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
// terminates instead of spinning on empty reads. Like the tasks gates, the read
// asserts that Pop owns the terminal foreground first — these menus re-prompt
// after an attended authoring session, whose leftovers may hold the terminal.
func readRoutineGateLine(reader *tty.Reader, out io.Writer, eofDefault string) (string, error) {
	answer, err := reader.ReadLine(func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
	})
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read gate selection: %w", err)
	}
	if err == io.EOF && answer == "" {
		return eofDefault, nil
	}
	return strings.TrimRight(answer, "\r\n"), nil
}
