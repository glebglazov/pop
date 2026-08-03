package routine

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
)

const emptyListHint = "No routines yet. Run `pop routine new <id> --schedule \"every 6h\"` to create one. " + ScheduleGrammar

// EmptyListHint is the same line for a surface outside this package — the Work
// dashboard's Routine page, which shows it when there is nothing to list.
const EmptyListHint = emptyListHint

// RoutineWarning names a routine whose manifest could not be loaded during
// listing. A broken manifest suspends only that routine; the rest are returned.
type RoutineWarning struct {
	ID  string
	Err error
}

// List prints all routines using default dependencies.
func List(out io.Writer) error {
	return ListWith(defaultDeps, out)
}

// ListWith discovers routines from pop's data dir and renders them, printing a
// warning line for each routine whose manifest could not be loaded.
func ListWith(d *Deps, out io.Writer) error {
	routines, warnings, err := ListRoutines(d)
	if err != nil {
		return err
	}
	// Project routines are discovered live from the current checkout and appended
	// to the authored table (ADR-0138); outside a checkout this is empty, leaving
	// output identical to authored-only listing.
	projectRoutines, projectWarnings := DiscoverProjectRoutines(d)

	if len(routines) == 0 && len(projectRoutines) == 0 && len(warnings) == 0 && len(projectWarnings) == 0 {
		fmt.Fprintln(out, emptyListHint)
		return nil
	}

	if len(routines) > 0 || len(projectRoutines) > 0 {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tDIRECTORY\tSCHEDULE\tPAUSED")
		for _, r := range routines {
			paused := "no"
			if r.Manifest.Paused {
				paused = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Manifest.BoundDirectory, ScheduleLabel(r.Manifest.Schedule), paused)
		}
		// Project routines render with the project: origin marker, a manual
		// schedule, and a bare `-` pause column — they carry no pause bit.
		for _, r := range projectRoutines {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ProjectOrigin+r.Name, r.Dir, "manual", "-")
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	for _, w := range warnings {
		fmt.Fprintf(out, "warning: routine %s: %v\n", w.ID, w.Err)
	}
	for _, w := range projectWarnings {
		fmt.Fprintf(out, "warning: routine %s: %v\n", w.ID, w.Err)
	}
	return nil
}

// ListRoutines returns discovered routines without rendering. A per-routine
// manifest load failure is collected as a warning rather than failing the list;
// only a directory-level read failure of the routines root is a hard error.
func ListRoutines(d *Deps) ([]*Routine, []RoutineWarning, error) {
	root := routinesRoot(d)
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("list routines: %w", err)
	}
	var routines []*Routine
	var warnings []RoutineWarning
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loadManifest(d, e.Name())
		if err != nil {
			warnings = append(warnings, RoutineWarning{ID: e.Name(), Err: err})
			continue
		}
		routines = append(routines, r)
	}
	sort.Slice(routines, func(i, j int) bool {
		return routines[i].ID < routines[j].ID
	})
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].ID < warnings[j].ID
	})
	return routines, warnings, nil
}

func defaultOpenEditor(path string) error {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultOpenPager(path string) error {
	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		pager = "less"
	}
	fields := strings.Fields(pager)
	cmd := exec.Command(fields[0], append(fields[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// Interactive reports whether the current session has an interactive TTY on
// stdin, so the CLI can decide between dropping into the refinement gate and
// falling back to a non-interactive scaffold + guidance path.
func Interactive() bool {
	return defaultIsInteractive()
}

// InteractiveWith answers the same question through the deps seam. A Deps with
// no IsInteractive wired is non-interactive, matching how every other verb in
// this package reads the seam.
func InteractiveWith(d *Deps) bool {
	if d == nil || d.IsInteractive == nil {
		return false
	}
	return d.IsInteractive()
}
