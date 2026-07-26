package routine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/internal/frontmatter"
)

// PauseReason records why a Routine is paused (ADR-0128). Legacy manifests
// predate the field; an empty value reads as a plain, reasonless pause.
type PauseReason string

const (
	// PauseReasonCreated marks the initial paused-on-creation state.
	PauseReasonCreated PauseReason = "created"
	// PauseReasonManual marks a user-initiated pause (verb or dashboard).
	PauseReasonManual PauseReason = "manual"
	// PauseReasonFailure marks a pause triggered by a failed run.
	PauseReasonFailure PauseReason = "failure"
	// PauseReasonChanged marks a pause triggered by the fingerprint/chokepoint
	// slice detecting a drifted binding; written elsewhere.
	PauseReasonChanged PauseReason = "changed"
)

// Manifest is the in-memory aggregate of a Routine's two on-disk halves
// (ADR-0139): authored intent read from prompt.md frontmatter (schedule, agents,
// effort) and machine state read from state.json (bound directory, pause bit,
// created-at). It is never serialized whole; loadManifest merges the halves and
// the write seam splits them back apart.
type Manifest struct {
	BoundDirectory string
	Schedule       string
	// Agents is the Routine's own ordered runtime agent-preset list (ADR-0128).
	// When set it becomes the head of fire-time resolution, ahead of
	// [routines].agents and the resolved implement list. Absent ⇒ config
	// resolution, exactly as before this field existed.
	Agents []string
	// Effort selects the Routine's model-strength tier (light, standard, heavy)
	// for the chosen preset via the [effort.<agent>] ladder. Absent ⇒ standard.
	Effort      string
	Paused      bool
	PauseReason PauseReason
	CreatedAt   string
}

// stateFile is the slim machine-state sidecar persisted to state.json (ADR-0139).
// The pause bit is machine-mutated (the daemon flips it on failure and drift),
// and the bound directory is a registry fact, not intent — so both stay in a
// JSON file pop owns rather than in the human-edited prompt.md frontmatter.
type stateFile struct {
	BoundDirectory string      `json:"bound_directory"`
	Paused         bool        `json:"paused"`
	PauseReason    PauseReason `json:"pause_reason,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

// pausedStatusLabel renders a paused Routine's status for the dashboard and the
// refinement-loop header. Created/manual/legacy paused routines read as plain
// "paused"; failure and changed carry their cause in parentheses.
func pausedStatusLabel(reason PauseReason) string {
	switch reason {
	case PauseReasonFailure:
		return "paused (failed)"
	case PauseReasonChanged:
		return "paused (changed)"
	default:
		return "paused"
	}
}

// IsScheduled reports whether the Routine carries a schedule. An absent schedule
// is a durable manual-fire-only state (ADR-0134): the Queue daemon never fires
// it, and surfaces render it as "manual".
func (m Manifest) IsScheduled() bool {
	return strings.TrimSpace(m.Schedule) != ""
}

// ScheduleLabel renders a manifest schedule for display; an absent schedule
// reads as "manual" (ADR-0134).
func ScheduleLabel(schedule string) string {
	if strings.TrimSpace(schedule) == "" {
		return "manual"
	}
	return schedule
}

// Routine is a discovered Routine with its identifier and parsed manifest.
type Routine struct {
	ID       string
	Manifest Manifest
	Schedule Schedule
}

func loadManifest(d *Deps, id string) (*Routine, error) {
	dir := routineDir(d, id)
	st, err := readState(d, dir, id)
	if err != nil {
		return nil, err
	}
	fields, _, err := readPromptFrontmatter(d, dir, id)
	if err != nil {
		return nil, err
	}
	m := Manifest{
		BoundDirectory: st.BoundDirectory,
		Schedule:       strings.TrimSpace(fields.Schedule),
		Agents:         nonEmptyAgentSpecs(fields.Agents),
		Effort:         strings.TrimSpace(fields.Effort),
		Paused:         st.Paused,
		PauseReason:    st.PauseReason,
		CreatedAt:      st.CreatedAt,
	}
	// An unscheduled Routine is a durable manual-only state (ADR-0134): the
	// absence is handled before the parser, which still rejects an empty
	// expression. Only a present schedule is parsed; a schedule the parser
	// rejects suspends only this Routine with a warning (ADR-0139).
	var sched Schedule
	if m.IsScheduled() {
		var err error
		sched, err = ParseSchedule(m.Schedule)
		if err != nil {
			return nil, fmt.Errorf("routine %q has invalid schedule: %w", id, err)
		}
	}
	return &Routine{ID: id, Manifest: m, Schedule: sched}, nil
}

// readState reads and parses the state.json sidecar. A missing state.json means
// the Routine is not present; a directory carrying only the legacy manifest.json
// earns a distinct error so listing warns about it rather than silently loading
// stale intent (ADR-0139).
func readState(d *Deps, dir, id string) (stateFile, error) {
	data, err := d.FS.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		if os.IsNotExist(err) {
			if _, statErr := d.FS.Stat(filepath.Join(dir, legacyManifestFileName)); statErr == nil {
				return stateFile{}, fmt.Errorf("routine %q has a legacy %s but no %s; its intent is not read — re-create it", id, legacyManifestFileName, stateFileName)
			}
			return stateFile{}, fmt.Errorf("routine %q not found", id)
		}
		return stateFile{}, fmt.Errorf("read routine state: %w", err)
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return stateFile{}, fmt.Errorf("parse routine state %q: %w", id, err)
	}
	return st, nil
}

// readPromptFrontmatter reads prompt.md and splits its authored-intent
// frontmatter from the body. Unparseable frontmatter suspends only this Routine
// with a warning (ADR-0139).
func readPromptFrontmatter(d *Deps, dir, id string) (frontmatter.Fields, string, error) {
	data, err := d.FS.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return frontmatter.Fields{}, "", fmt.Errorf("routine %q has no %s", id, promptFileName)
		}
		return frontmatter.Fields{}, "", fmt.Errorf("read routine prompt: %w", err)
	}
	fields, body, err := frontmatter.Parse(string(data))
	if err != nil {
		return frontmatter.Fields{}, "", fmt.Errorf("routine %q has invalid frontmatter: %w", id, err)
	}
	return fields, body, nil
}

// manifestFields projects the authored-intent half of a Manifest into the
// frontmatter shape written at the top of prompt.md.
func manifestFields(m Manifest) frontmatter.Fields {
	return frontmatter.Fields{
		Schedule: strings.TrimSpace(m.Schedule),
		Agents:   nonEmptyAgentSpecs(m.Agents),
		Effort:   strings.TrimSpace(m.Effort),
	}
}

// writeState persists the machine-state half of a Manifest to state.json. Pause
// toggles and daemon-driven pauses go through here and never touch the
// human-edited prompt.md (ADR-0139).
func writeState(d *Deps, id string, m Manifest) error {
	st := stateFile{
		BoundDirectory: m.BoundDirectory,
		Paused:         m.Paused,
		PauseReason:    m.PauseReason,
		CreatedAt:      m.CreatedAt,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode routine state: %w", err)
	}
	path := filepath.Join(routineDir(d, id), stateFileName)
	if err := d.FS.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write routine state: %w", err)
	}
	return nil
}

// writeFrontmatter rewrites the authored-intent frontmatter at the top of
// prompt.md while preserving the body verbatim. It re-reads the current file to
// recover the body, so callers must have loaded the Manifest (which validates
// the frontmatter) first.
func writeFrontmatter(d *Deps, id string, m Manifest) error {
	dir := routineDir(d, id)
	_, body, err := readPromptFrontmatter(d, dir, id)
	if err != nil {
		return err
	}
	out, err := frontmatter.Marshal(manifestFields(m), body)
	if err != nil {
		return fmt.Errorf("encode routine frontmatter: %w", err)
	}
	if err := d.FS.WriteFile(filepath.Join(dir, promptFileName), []byte(out), 0o644); err != nil {
		return fmt.Errorf("write routine prompt: %w", err)
	}
	return nil
}

func validateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("routine id is required")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("invalid routine id %q", id)
	}
	if strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid routine id %q: must not contain path separators", id)
	}
	return nil
}

func canonicalBoundDirectory(d *Deps, cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine working directory: %w", err)
		}
	}
	expanded := expandHome(d, cwd)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve bound directory: %w", err)
	}
	clean := filepath.Clean(abs)
	resolved, err := d.FS.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	return resolved, nil
}

func expandHome(d *Deps, path string) string {
	if path == "~" {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func nowUTC(d *Deps) time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
