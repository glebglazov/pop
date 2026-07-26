package routine

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"
	"time"
)

const emptyRunsHint = "No runs yet. Fire the routine with `pop routine fire <id>` to create one."

// Runs prints a Routine's run history using default dependencies.
func Runs(id string, out io.Writer) error {
	return RunsWith(defaultDeps, id, out)
}

// RunsWith lists run history for one Routine, newest first. Addressing follows
// ADR-0138: `project:<name>` (or a bare name that resolves to a Project routine)
// lists the current checkout's Project routine history, keyed per checkout.
func RunsWith(d *Deps, id string, out io.Writer) error {
	if resolvesToProjectRoutine(d, id) {
		name := id
		if bare, ok := parseProjectRef(id); ok {
			name = bare
		}
		return projectRoutineRuns(d, name, out)
	}
	if err := validateID(id); err != nil {
		return err
	}
	if _, err := loadManifest(d, id); err != nil {
		return err
	}

	runsDir := filepath.Join(routineDir(d, id), runsDirName)
	return renderRuns(d, id, runsDir, out)
}

// projectRoutineRuns lists a Project routine's per-checkout run history (ADR-0138).
func projectRoutineRuns(d *Deps, name string, out io.Writer) error {
	pr, err := findProjectRoutine(d, name)
	if err != nil {
		return err
	}
	key := checkoutKey(pr.Dir)
	runsDir := filepath.Join(projectRoutineDataDir(d, key, name), runsDirName)
	return renderRuns(d, projectStoreID(key, name), runsDir, out)
}

// renderRuns prints the run history for one store routine id, newest first.
func renderRuns(d *Deps, storeID, runsDir string, out io.Writer) error {
	s, err := openExecutionStore(d)
	if err != nil {
		return err
	}

	rows, err := s.ListRoutineRuns(storeID)
	if err != nil {
		return fmt.Errorf("list routine runs: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, emptyRunsHint)
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIRED AT\tOUTCOME\tREPORT")
	for _, row := range rows {
		report := row.ReportPath
		if report == "" {
			report = reportPathForRun(runsDir, row.FiredAt)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			row.FiredAt.UTC().Format("2006-01-02T15:04:05Z"),
			row.Outcome,
			report,
		)
	}
	return tw.Flush()
}

func reportPathForRun(runsDir string, firedAt time.Time) string {
	name := firedAt.UTC().Format("2006-01-02T15-04-05Z") + ".md"
	return filepath.Join(runsDir, name)
}
