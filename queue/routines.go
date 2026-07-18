package queue

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

const routinesSessionName = "routines"

// tickRoutines evaluates every discovered routine's schedule and spawns a pane
// fire for each due, non-paused routine that is not already running.
func tickRoutines(d *Deps, out io.Writer) {
	rd := d.routineDeps()
	routines, err := routine.ListRoutines(rd)
	if err != nil {
		fmt.Fprintf(out, "queue: routines: %v\n", err)
		return
	}
	if len(routines) == 0 {
		return
	}

	s, ok, err := openRoutineStore(rd)
	if err != nil {
		fmt.Fprintf(out, "queue: routines: %v\n", err)
		return
	}
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()

	now := d.now().UTC()
	isAlive := func(run store.RoutineRun) bool {
		return tasks.ProcessLiveWithToken(d.Tasks, run.PID, run.ProcStart)
	}

	for _, r := range routines {
		if r.Manifest.Paused {
			continue
		}
		lastFired, err := routine.LastFireTime(s, r.ID)
		if err != nil {
			fmt.Fprintf(out, "queue: routine %s: last fire: %v\n", r.ID, err)
			continue
		}
		if !routine.IsDue(r.Schedule, lastFired, now) {
			continue
		}
		if live, err := s.LiveRoutineRun(r.ID, isAlive); err != nil {
			fmt.Fprintf(out, "queue: routine %s: live run: %v\n", r.ID, err)
			continue
		} else if live != nil {
			if _, err := s.InsertSkippedRoutineRun(store.RoutineRun{
				RoutineID:  r.ID,
				FiredAt:    now,
				SkipReason: routine.SkipReasonOverlap,
			}); err != nil {
				fmt.Fprintf(out, "queue: routine %s: record skip: %v\n", r.ID, err)
				continue
			}
			fmt.Fprintf(out, "queue: routine %s: skipped fire (%s)\n", r.ID, routine.SkipReasonOverlap)
			continue
		}
		session, dir := routineSessionAndDir(d, r.Manifest.BoundDirectory)
		if err := spawnRoutineFire(d.Tmux, session, dir, r.ID); err != nil {
			fmt.Fprintf(out, "queue: routine %s: spawn: %v\n", r.ID, err)
			continue
		}
		fmt.Fprintf(out, "queue: routine %s: spawned fire\n", r.ID)
	}
}

func (d *Deps) routineDeps() *routine.Deps {
	rd := routine.DefaultDeps()
	rd.Now = d.now
	if d.Tasks != nil {
		rd.Tasks = d.Tasks
		if d.Tasks.FS != nil {
			rd.FS = d.Tasks.FS
		}
	}
	rd.ProcessAlive = func(pid int, procStart string) bool {
		return tasks.ProcessLiveWithToken(d.Tasks, pid, procStart)
	}
	rd.ProcStartToken = func(pid int) (string, bool) {
		if d.Tasks != nil && d.Tasks.ProcessStartToken != nil {
			return d.Tasks.ProcessStartToken(pid)
		}
		return "", false
	}
	rd.PID = os.Getpid
	return rd
}

func openRoutineStore(d *routine.Deps) (*store.Store, bool, error) {
	path := routineStorePath(d)
	if _, err := d.FS.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

func routineStorePath(d *routine.Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "pop.db")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "pop.db")
	}
	return filepath.Join(home, ".local", "share", "pop", "pop.db")
}

func routineSessionAndDir(d *Deps, boundDir string) (session, dir string) {
	if _, err := project.DetectRepoContextFromPathWith(d.projectDeps(), boundDir); err != nil {
		return routinesSessionName, boundDir
	}
	return project.SessionNameWith(d.projectDeps(), boundDir), boundDir
}

func spawnRoutineFire(tmux deps.Tmux, session, dir, routineID string) error {
	command := fmt.Sprintf("pop routine fire %s", shellQuote(routineID))
	if !tmux.HasSession(session) {
		if err := tmux.NewSession(session, dir); err != nil {
			return fmt.Errorf("create session %q: %w", session, err)
		}
	}

	windowTarget, freshPaneID, err := resolveDrainWindowTarget(tmux, session, dir)
	if err != nil {
		return err
	}

	paneID, err := findRoutinePane(tmux, windowTarget, routineID)
	if err != nil {
		return err
	}
	if paneID != "" {
		if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
			return fmt.Errorf("send routine fire command: %w", err)
		}
		return nil
	}

	if freshPaneID != "" {
		paneID = freshPaneID
	} else {
		out, err := tmux.Command("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", windowTarget, "-c", dir)
		if err != nil {
			return fmt.Errorf("create routine pane: %w", err)
		}
		paneID = strings.TrimSpace(out)
		if paneID == "" {
			return fmt.Errorf("create routine pane: tmux returned no pane id")
		}
		if _, err := tmux.Command("select-layout", "-t", windowTarget, "tiled"); err != nil {
			return fmt.Errorf("retile routine window: %w", err)
		}
	}

	if _, err := tmux.Command("set-option", "-p", "-t", paneID, "@pop_routine", routineID); err != nil {
		return fmt.Errorf("tag routine pane: %w", err)
	}
	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		return fmt.Errorf("send routine fire command: %w", err)
	}
	return nil
}

func findRoutinePane(tmux deps.Tmux, windowTarget, routineID string) (string, error) {
	out, err := tmux.Command("list-panes", "-t", windowTarget, "-F", "#{@pop_routine} #{pane_id}")
	if err != nil {
		return "", fmt.Errorf("list routine panes in %q: %w", windowTarget, err)
	}
	for _, line := range splitLines(out) {
		tag, paneID, ok := parseDrainPaneTagLine(line)
		if ok && tag == routineID {
			return paneID, nil
		}
	}
	return "", nil
}
