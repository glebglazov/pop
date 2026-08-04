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
// reusing the same pane when one is already tagged for that routine, and returns
// that pane. The id may be an authored routine or a Project routine's
// `project:<name>` (ADR-0138); the pane runs in the routine's bound directory (a
// Project routine's checkout).
func FirePaneWith(d *Deps, routineID string) (string, error) {
	dir, err := paneBoundDir(d, routineID)
	if err != nil {
		return "", err
	}
	session, paneDir := sessionAndDir(d, dir)
	command := fmt.Sprintf("pop routine fire %s", shellQuote(routineID))
	return tmuxmod.EnsureTaggedPane(tmuxDeps(d), tmuxmod.TagRoutine, session, tmuxmod.DrainWindow, paneDir, routineID, command)
}

// RunPaneWith returns the pane tagged for the routine, empty when the routine has
// none. It spawns nothing: it is the lookup behind the preview verb, which takes
// the operator to the pane a fire is running in and says so when there is none.
func RunPaneWith(d *Deps, routineID string) (string, error) {
	dir, err := paneBoundDir(d, routineID)
	if err != nil {
		return "", err
	}
	session, _ := sessionAndDir(d, dir)
	paneID, err := tmuxDeps(d).FindTaggedPane(session, tmuxmod.DrainWindow, tmuxmod.TagRoutine, routineID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(paneID), nil
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

// sessionAndDir places a routine's pane: in the session of the directory it is
// bound to, which is the same rule every Task-set pane follows (ADR-0180), and in
// the shared routines session when that directory is not a checkout at all.
func sessionAndDir(d *Deps, boundDir string) (session, dir string) {
	return project.CheckoutSessionOrWith(projectDeps(d), boundDir, RoutinesSessionName), boundDir
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
