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

const emptyListHint = "No routines yet. Run `pop routine add <id> --schedule \"daily at 10:00\"` to create one."

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

	if len(routines) == 0 && len(warnings) == 0 {
		fmt.Fprintln(out, emptyListHint)
		return nil
	}

	if len(routines) > 0 {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tDIRECTORY\tSCHEDULE\tPAUSED")
		for _, r := range routines {
			paused := "no"
			if r.Manifest.Paused {
				paused = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Manifest.BoundDirectory, r.Manifest.Schedule, paused)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	for _, w := range warnings {
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
