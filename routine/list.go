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

// List prints all routines using default dependencies.
func List(out io.Writer) error {
	return ListWith(defaultDeps, out)
}

// ListWith discovers routines from pop's data dir and renders them.
func ListWith(d *Deps, out io.Writer) error {
	root := routinesRoot(d)
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, emptyListHint)
			return nil
		}
		return fmt.Errorf("list routines: %w", err)
	}

	var routines []*Routine
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loadManifest(d, e.Name())
		if err != nil {
			return err
		}
		routines = append(routines, r)
	}

	if len(routines) == 0 {
		fmt.Fprintln(out, emptyListHint)
		return nil
	}

	sort.Slice(routines, func(i, j int) bool {
		return routines[i].ID < routines[j].ID
	})

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDIRECTORY\tSCHEDULE\tPAUSED")
	for _, r := range routines {
		paused := "no"
		if r.Manifest.Paused {
			paused = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Manifest.BoundDirectory, r.Manifest.Schedule, paused)
	}
	return tw.Flush()
}

// ListRoutines returns discovered routines without rendering.
func ListRoutines(d *Deps) ([]*Routine, error) {
	root := routinesRoot(d)
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list routines: %w", err)
	}
	var routines []*Routine
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := loadManifest(d, e.Name())
		if err != nil {
			return nil, err
		}
		routines = append(routines, r)
	}
	sort.Slice(routines, func(i, j int) bool {
		return routines[i].ID < routines[j].ID
	})
	return routines, nil
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

func defaultIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
