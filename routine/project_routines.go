package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/project"
)

// projectRoutinesDirName is the committed, per-checkout home of Project routines
// (ADR-0138), under `.pop/`: each `<checkout>/.pop/routines/<name>.md` is one
// routine, its filename the name and its body the prompt.
const projectRoutinesDirName = "routines"

// projectRoutineExt is the extension every Project routine file carries.
const projectRoutineExt = ".md"

// ProjectOrigin is the explicit CLI addressing prefix for a Project routine
// (ADR-0138). It doubles as the origin marker in list output: an authored
// Routine renders its bare id, a Project routine renders `project:<name>`.
const ProjectOrigin = "project:"

// ProjectRoutine is a Project routine discovered live from a checkout's
// `.pop/routines/` (ADR-0138). It carries no pause state and no schedule: it is
// manual-fire-only by design, keyed by its committed file, and never registered
// in pop's data-dir routines registry (so the Queue daemon never sees it).
type ProjectRoutine struct {
	// Name is the routine's identifier — the filename without its `.md`
	// extension, validated by the authored-id rules.
	Name string
	// Prompt is the file body with the intent frontmatter stripped.
	Prompt string
	// Agents is the optional ordered runtime agent-preset list from frontmatter.
	Agents []string
	// Effort is the optional model-strength tier from frontmatter.
	Effort string
	// Dir is the checkout root the routine was discovered in.
	Dir string
}

// DiscoverProjectRoutines reads the current checkout's `.pop/routines/*.md`
// (ADR-0138). Discovery is virtual: nothing is written to the data dir and the
// routines are never registered, so the Queue daemon's registry-driven tick can
// never see them. Invoked outside any git checkout, it finds nothing. A per-file
// problem — an invalid filename, unparseable frontmatter, or an unreadable file
// — is a warning that skips only that file; the walk always succeeds.
func DiscoverProjectRoutines(d *Deps) ([]ProjectRoutine, []RoutineWarning) {
	root, ok := checkoutRoot(d)
	if !ok {
		return nil, nil
	}
	dir := filepath.Join(root, ".pop", projectRoutinesDirName)
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		// No committed routines directory in this checkout is the common case,
		// not an error. Any other read failure is reported once and swallowed so
		// listing still succeeds.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []RoutineWarning{{ID: ProjectOrigin + "*", Err: fmt.Errorf("read %s: %w", dir, err)}}
	}

	var routines []ProjectRoutine
	var warnings []RoutineWarning
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, projectRoutineExt) {
			continue
		}
		id := strings.TrimSuffix(name, projectRoutineExt)
		// A load may yield a routine, a warning, or both: a schedule/unknown key
		// is warn-and-ignore, so the routine still lists alongside its warning.
		r, w := loadProjectRoutine(d, dir, id)
		if w != nil {
			warnings = append(warnings, *w)
		}
		if r != nil {
			r.Dir = root
			routines = append(routines, *r)
		}
	}
	sort.Slice(routines, func(i, j int) bool { return routines[i].Name < routines[j].Name })
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].ID < warnings[j].ID })
	return routines, warnings
}

// loadProjectRoutine validates one `.pop/routines/<id>.md` file and parses it.
// It returns either a routine or a single skip-warning, never both.
func loadProjectRoutine(d *Deps, dir, id string) (*ProjectRoutine, *RoutineWarning) {
	if err := validateID(id); err != nil {
		return nil, &RoutineWarning{ID: ProjectOrigin + id, Err: err}
	}
	data, err := d.FS.ReadFile(filepath.Join(dir, id+projectRoutineExt))
	if err != nil {
		return nil, &RoutineWarning{ID: ProjectOrigin + id, Err: fmt.Errorf("read prompt: %w", err)}
	}
	fields, body, keys, err := frontmatter.ParseWithKeys(string(data))
	if err != nil {
		return nil, &RoutineWarning{ID: ProjectOrigin + id, Err: fmt.Errorf("invalid frontmatter: %w", err)}
	}
	// Project routines accept agents/effort only. A schedule — like any unknown
	// key — is warned about and ignored, never applied (ADR-0138). This is a
	// non-fatal notice: the routine still lists, minus the dropped keys.
	if dropped := disallowedProjectKeys(keys); len(dropped) > 0 {
		return &ProjectRoutine{
			Name:   id,
			Prompt: body,
			Agents: nonEmptyAgentSpecs(fields.Agents),
			Effort: strings.TrimSpace(fields.Effort),
		}, &RoutineWarning{ID: ProjectOrigin + id, Err: fmt.Errorf("ignoring unsupported frontmatter key(s) %s (Project routines take agents/effort only)", strings.Join(dropped, ", "))}
	}
	return &ProjectRoutine{
		Name:   id,
		Prompt: body,
		Agents: nonEmptyAgentSpecs(fields.Agents),
		Effort: strings.TrimSpace(fields.Effort),
	}, nil
}

// disallowedProjectKeys returns the frontmatter keys a Project routine does not
// honor — everything but agents and effort, schedule included.
func disallowedProjectKeys(keys []string) []string {
	var dropped []string
	for _, k := range keys {
		switch k {
		case "agents", "effort":
		default:
			dropped = append(dropped, k)
		}
	}
	return dropped
}

// checkoutRoot resolves the git worktree root of the current directory, the
// anchor `.pop/routines/` is read from. Not being in a checkout is the ordinary
// "no Project routines" case, reported as ok=false rather than an error.
func checkoutRoot(d *Deps) (string, bool) {
	root, err := project.CurrentCheckoutPathWith(projectDeps(d))
	if err != nil || strings.TrimSpace(root) == "" {
		return "", false
	}
	return root, true
}
