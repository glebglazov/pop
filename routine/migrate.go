package routine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/internal/frontmatter"
)

// legacyManifest is the pre-ADR-0139 combined manifest.json shape. It is parsed
// only by the one-shot migration below; runtime code never reads it for intent.
type legacyManifest struct {
	BoundDirectory string      `json:"bound_directory"`
	Schedule       string      `json:"schedule"`
	Agents         []string    `json:"agents,omitempty"`
	Effort         string      `json:"effort,omitempty"`
	Paused         bool        `json:"paused"`
	PauseReason    PauseReason `json:"pause_reason,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

// MigrationFailure names a legacy routine directory that could not be migrated
// and why. It is left untouched (conservative: never half-written).
type MigrationFailure struct {
	ID  string
	Err error
}

// MigrateManifests runs the one-shot manifest.json -> frontmatter + state.json
// migration (ADR-0139) using default dependencies.
func MigrateManifests(out io.Writer) error {
	return MigrateManifestsWith(defaultDeps, out)
}

// MigrateManifestsWith walks pop's routines directory and, for each routine
// still carrying a legacy manifest.json, lifts schedule/agents/effort into
// prompt.md frontmatter, writes state.json with the pause bit, created-at, and
// bound directory, then removes manifest.json. It is idempotent — a directory
// with no manifest.json (already migrated, or never legacy) is left alone — and
// conservative: a directory whose manifest.json or prompt.md cannot be parsed
// is reported and skipped without writing anything.
func MigrateManifestsWith(d *Deps, out io.Writer) error {
	root := routinesRoot(d)
	entries, err := d.FS.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, "No routines directory found; nothing to migrate.")
			return nil
		}
		return fmt.Errorf("list routines: %w", err)
	}

	var migrated []string
	var failures []MigrationFailure
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		dir := routineDir(d, id)
		manifestPath := filepath.Join(dir, legacyManifestFileName)
		if _, err := d.FS.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			failures = append(failures, MigrationFailure{ID: id, Err: fmt.Errorf("stat legacy manifest: %w", err)})
			continue
		}
		if err := migrateRoutineDir(d, id, dir, manifestPath); err != nil {
			failures = append(failures, MigrationFailure{ID: id, Err: err})
			continue
		}
		migrated = append(migrated, id)
	}

	sort.Strings(migrated)
	sort.Slice(failures, func(i, j int) bool { return failures[i].ID < failures[j].ID })

	for _, id := range migrated {
		fmt.Fprintf(out, "migrated routine %q\n", id)
	}
	for _, f := range failures {
		fmt.Fprintf(out, "warning: routine %q: %v\n", f.ID, f.Err)
	}
	if len(migrated) == 0 && len(failures) == 0 {
		fmt.Fprintln(out, "No legacy routine manifests found; nothing to migrate.")
	}
	return nil
}

// migrateRoutineDir migrates a single legacy routine directory. Everything is
// read and validated before any write, so a parse failure at any point leaves
// the directory exactly as it was.
func migrateRoutineDir(d *Deps, id, dir, manifestPath string) error {
	data, err := d.FS.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read legacy manifest: %w", err)
	}
	var lm legacyManifest
	if err := json.Unmarshal(data, &lm); err != nil {
		return fmt.Errorf("parse legacy manifest: %w", err)
	}
	schedule := strings.TrimSpace(lm.Schedule)
	if schedule != "" {
		if _, err := ParseSchedule(schedule); err != nil {
			return fmt.Errorf("invalid schedule %q: %w", schedule, err)
		}
	}

	promptPath := filepath.Join(dir, promptFileName)
	promptData, err := d.FS.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("read routine prompt: %w", err)
	}
	// A legacy prompt.md carries no frontmatter of its own, but parsing it
	// through the same splitter recovers the body whether or not a partial
	// prior migration attempt already added a fence.
	_, body, err := frontmatter.Parse(string(promptData))
	if err != nil {
		return fmt.Errorf("parse routine prompt: %w", err)
	}

	m := Manifest{
		BoundDirectory: lm.BoundDirectory,
		Schedule:       schedule,
		Agents:         nonEmptyAgentSpecs(lm.Agents),
		Effort:         strings.TrimSpace(lm.Effort),
		Paused:         lm.Paused,
		PauseReason:    lm.PauseReason,
		CreatedAt:      lm.CreatedAt,
	}

	out, err := frontmatter.Marshal(manifestFields(m), body)
	if err != nil {
		return fmt.Errorf("encode routine frontmatter: %w", err)
	}
	if err := d.FS.WriteFile(promptPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write routine prompt: %w", err)
	}
	if err := writeState(d, id, m); err != nil {
		return err
	}
	if err := d.FS.RemoveAll(manifestPath); err != nil {
		return fmt.Errorf("remove legacy manifest: %w", err)
	}
	return nil
}
