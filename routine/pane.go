package routine

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
)

const (
	RoutinesSessionName = "routines"
	drainWindowName     = "pop-queue"
)

// FirePaneWith spawns `pop routine fire <id>` into a tmux pane for the routine,
// reusing the same pane when one is already tagged for that routine.
func FirePaneWith(d *Deps, routineID string) error {
	if err := validateID(routineID); err != nil {
		return err
	}
	r, err := loadManifest(d, routineID)
	if err != nil {
		return err
	}
	session, dir := sessionAndDir(d, r.Manifest.BoundDirectory)
	return spawnFirePane(tmuxDeps(d), session, dir, routineID)
}

// PreviewPaneWith switches the active tmux client to the pane tagged for the
// routine. When no pane exists the call is a no-op.
func PreviewPaneWith(d *Deps, routineID string) error {
	if err := validateID(routineID); err != nil {
		return err
	}
	r, err := loadManifest(d, routineID)
	if err != nil {
		return err
	}
	session, dir := sessionAndDir(d, r.Manifest.BoundDirectory)
	windowTarget, _, err := resolveDrainWindowTarget(tmuxDeps(d), session, dir)
	if err != nil {
		return err
	}
	paneID, err := findRoutinePane(tmuxDeps(d), windowTarget, routineID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	tmux := tmuxDeps(d)
	if _, err := tmux.Command("select-pane", "-t", paneID); err != nil {
		return err
	}
	_, err = tmux.Command("switch-client", "-t", paneID)
	return err
}

func tmuxDeps(d *Deps) deps.Tmux {
	if d != nil && d.Tmux != nil {
		return d.Tmux
	}
	return deps.NewRealTmux()
}

func projectDeps(d *Deps) *project.Deps {
	if d != nil && d.Project != nil {
		return d.Project
	}
	return project.DefaultDeps()
}

func SessionAndDir(d *Deps, boundDir string) (session, dir string) {
	return sessionAndDir(d, boundDir)
}

func sessionAndDir(d *Deps, boundDir string) (session, dir string) {
	if _, err := project.DetectRepoContextFromPathWith(projectDeps(d), boundDir); err != nil {
		return RoutinesSessionName, boundDir
	}
	return project.SessionNameWith(projectDeps(d), boundDir), boundDir
}

func spawnFirePane(tmux deps.Tmux, session, dir, routineID string) error {
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
		tag, paneID, ok := parsePaneTagLine(line)
		if ok && tag == routineID {
			return paneID, nil
		}
	}
	return "", nil
}

func parsePaneTagLine(line string) (tag, paneID string, ok bool) {
	line = strings.TrimSpace(line)
	idx := strings.LastIndex(line, " %")
	if idx < 0 {
		return "", "", false
	}
	tag = strings.TrimSpace(line[:idx])
	paneID = strings.TrimSpace(line[idx+1:])
	return tag, paneID, tag != "" && paneID != ""
}

func resolveDrainWindowTarget(tmux deps.Tmux, session, dir string) (target, freshPaneID string, err error) {
	target = session + ":" + drainWindowName
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return "", "", fmt.Errorf("list windows in %q: %w", session, err)
	}
	for _, line := range splitLines(out) {
		if line == drainWindowName {
			return target, "", nil
		}
	}
	out, err = tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", drainWindowName, "-c", dir)
	if err != nil {
		return "", "", fmt.Errorf("create queue window in %q: %w", session, err)
	}
	freshPaneID = strings.TrimSpace(out)
	if freshPaneID == "" {
		return "", "", fmt.Errorf("create queue window in %q: tmux returned no pane id", session)
	}
	return target, freshPaneID, nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\'', '"', '\\', '$', '`', '!', '&', '|', ';', '(', ')', '<', '>':
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
