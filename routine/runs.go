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

// RunsWith lists run history for one Routine, newest first.
func RunsWith(d *Deps, id string, out io.Writer) error {
	if err := validateID(id); err != nil {
		return err
	}
	if _, err := loadManifest(d, id); err != nil {
		return err
	}

	s, err := openExecutionStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	rows, err := s.ListRoutineRuns(id)
	if err != nil {
		return fmt.Errorf("list routine runs: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, emptyRunsHint)
		return nil
	}

	runsDir := filepath.Join(routineDir(d, id), runsDirName)
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
