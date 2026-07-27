package routine

import (
	"fmt"
	"strings"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
)

const (
	RoutinesSessionName = "routines"
)

// FirePaneWith spawns `pop routine fire <id>` into a tmux pane for the routine,
// reusing the same pane when one is already tagged for that routine. The id may
// be an authored routine or a Project routine's `project:<name>` (ADR-0138);
// the pane runs in the routine's bound directory (a Project routine's checkout).
func FirePaneWith(d *Deps, routineID string) error {
	dir, err := paneBoundDir(d, routineID)
	if err != nil {
		return err
	}
	session, paneDir := sessionAndDir(d, dir)
	command := fmt.Sprintf("pop routine fire %s", shellQuote(routineID))
	_, err = tmuxmod.EnsureTaggedPane(tmuxDeps(d), tmuxmod.TagRoutine, session, paneDir, routineID, command)
	return err
}

// PreviewPaneWith switches the active tmux client to the pane tagged for the
// routine. When no pane exists the call is a no-op.
func PreviewPaneWith(d *Deps, routineID string) error {
	dir, err := paneBoundDir(d, routineID)
	if err != nil {
		return err
	}
	session, _ := sessionAndDir(d, dir)
	tmux := tmuxDeps(d)
	paneID, err := tmux.FindTaggedPane(session, tmuxmod.TagRoutine, routineID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	return tmuxmod.FocusPane(tmux, paneID)
}

// paneBoundDir resolves the directory a routine's pane runs in. An authored
// routine's bound directory comes from its manifest; a Project routine's
// (`project:<name>`) comes from the checkout it was discovered in (ADR-0138).
func paneBoundDir(d *Deps, routineID string) (string, error) {
	if name, ok := parseProjectRef(routineID); ok {
		pr, err := findProjectRoutine(d, name)
		if err != nil {
			return "", err
		}
		return pr.Dir, nil
	}
	if err := validateID(routineID); err != nil {
		return "", err
	}
	r, err := loadManifest(d, routineID)
	if err != nil {
		return "", err
	}
	return r.Manifest.BoundDirectory, nil
}

func tmuxDeps(d *Deps) tmuxmod.Tmux {
	if d != nil && d.Tmux != nil {
		return d.Tmux
	}
	return tmuxmod.New()
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
