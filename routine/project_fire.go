package routine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// projectRoutinesDataRoot is the data-dir root that holds per-user, per-checkout
// state for Project routines (ADR-0138): memory and run reports under
// `project-routines/<checkout-key>/<name>/{memory,runs}`. It is deliberately
// separate from `routines/`, the Queue daemon's discovery registry, so a Project
// routine's state never leaks into the daemon's world.
const projectRoutinesDataRoot = "project-routines"

// parseProjectRef reports whether id is the explicit `project:<name>` form and
// returns the bare name. The escape hatch that reaches a Project routine even
// when an authored id of the same name shadows it (ADR-0138).
func parseProjectRef(id string) (name string, ok bool) {
	if strings.HasPrefix(id, ProjectOrigin) {
		return strings.TrimPrefix(id, ProjectOrigin), true
	}
	return "", false
}

// projectRoutineName strips an optional `project:` prefix, yielding the bare
// Project routine name for display and file paths.
func projectRoutineName(id string) string {
	if name, ok := parseProjectRef(id); ok {
		return name
	}
	return id
}

// authoredRoutineExists reports whether an authored Routine of this id is
// registered in the data-dir routines/ registry. It is a pure existence check
// used only for addressing; a present-but-broken routine still counts, so firing
// surfaces its load error rather than silently falling through to a Project
// routine.
func authoredRoutineExists(d *Deps, id string) bool {
	if err := validateID(id); err != nil {
		return false
	}
	if _, err := d.FS.Stat(routineDir(d, id)); err != nil {
		return false
	}
	return true
}

// projectRoutineExists reports whether the current checkout carries a
// `.pop/routines/<name>.md` Project routine. Outside a checkout there are none.
func projectRoutineExists(d *Deps, name string) bool {
	if err := validateID(name); err != nil {
		return false
	}
	root, ok := checkoutRoot(d)
	if !ok {
		return false
	}
	path := filepath.Join(root, ".pop", projectRoutinesDirName, name+projectRoutineExt)
	if _, err := d.FS.Stat(path); err != nil {
		return false
	}
	return true
}

// resolvesToProjectRoutine reports whether id addresses a Project routine — the
// explicit `project:` form, or a bare name that has no authored routine but does
// have a Project routine in the current checkout. Schedule-edit surfaces use it
// to reject Project routines outright (ADR-0138).
func resolvesToProjectRoutine(d *Deps, id string) bool {
	if _, ok := parseProjectRef(id); ok {
		return true
	}
	return !authoredRoutineExists(d, id) && projectRoutineExists(d, id)
}

// findProjectRoutine loads the current checkout's Project routine by name,
// resolving from live `.pop/routines/` (ADR-0138). A missing checkout or a
// missing/invalid file is an error — a deleted or renamed prompt simply vanishes
// from surfaces. Any non-fatal warning (a schedule or unknown key) is printed
// but does not stop the fire.
func findProjectRoutine(d *Deps, name string) (*ProjectRoutine, error) {
	if err := validateID(name); err != nil {
		return nil, err
	}
	root, ok := checkoutRoot(d)
	if !ok {
		return nil, fmt.Errorf("not in a git checkout; Project routine %q can only be addressed from inside its checkout", name)
	}
	dir := filepath.Join(root, ".pop", projectRoutinesDirName)
	if _, err := d.FS.Stat(filepath.Join(dir, name+projectRoutineExt)); err != nil {
		return nil, fmt.Errorf("project routine %q not found in this checkout", name)
	}
	r, w := loadProjectRoutine(d, dir, name)
	if r == nil {
		if w != nil {
			return nil, w.Err
		}
		return nil, fmt.Errorf("project routine %q could not be loaded", name)
	}
	if w != nil {
		fmt.Fprintf(fireWarnWriter(d), "warning: routine %s: %v\n", w.ID, w.Err)
	}
	r.Dir = root
	return r, nil
}

// fireProjectRoutine runs one Project routine in the checkout it was triggered
// from (ADR-0138). Its run rows key on a synthetic per-checkout id and its
// memory/reports live under project-routines/<checkout-key>/<name>/, so sibling
// worktrees share the definition but never share history, memory, or exclusivity.
// A Project routine has no pause state, so a failed run records failed and stops.
func fireProjectRoutine(d *Deps, name string) (*FireResult, error) {
	pr, err := findProjectRoutine(d, name)
	if err != nil {
		return nil, err
	}
	key := checkoutKey(pr.Dir)
	root := projectRoutineDataDir(d, key, name)
	if err := d.FS.MkdirAll(filepath.Join(root, memoryDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create project routine memory directory: %w", err)
	}
	if err := d.FS.MkdirAll(filepath.Join(root, runsDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create project routine runs directory: %w", err)
	}
	return executeFire(d, firePlan{
		storeID:   projectStoreID(key, name),
		displayID: ProjectOrigin + name,
		boundDir:  pr.Dir,
		prompt:    pr.Prompt,
		root:      root,
		agents:    pr.Agents,
		effort:    pr.Effort,
		// A Project routine carries no schedule; its fingerprint hashes the body
		// plus agents/effort only.
		fingerprint: fingerprintOf(pr.Prompt, Manifest{Agents: pr.Agents, Effort: pr.Effort}),
		onFail:      nil,
	})
}

// projectRoutineDataDir is the per-user, per-checkout home of a Project routine's
// memory and run reports (ADR-0138).
func projectRoutineDataDir(d *Deps, key, name string) string {
	return filepath.Join(popDataDir(d), projectRoutinesDataRoot, key, name)
}

// checkoutKey derives a stable, filesystem-safe key from a canonical checkout
// path. It is the seam that gives each worktree its own history, memory, and
// exclusivity: two worktrees of the same repo have different paths and therefore
// different keys.
func checkoutKey(checkoutPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(checkoutPath)))
	return hex.EncodeToString(sum[:])[:16]
}

// projectStoreID is the synthetic per-checkout routine id that Project-routine
// run rows key on: checkout-key + name, namespaced under the `project:` prefix so
// it can never collide with an authored routine id (which forbids path
// separators but this id embeds `:` and a hash). Per-routine exclusivity,
// `pop routine runs`, and crash reconcile all key on this.
func projectStoreID(key, name string) string {
	return ProjectOrigin + key + ":" + name
}
