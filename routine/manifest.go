package routine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Manifest is the on-disk record for a Routine.
type Manifest struct {
	BoundDirectory string `json:"bound_directory"`
	Schedule       string `json:"schedule"`
	// Agents is the Routine's own ordered runtime agent-preset list (ADR-0128).
	// When set it becomes the head of fire-time resolution, ahead of
	// [routines].agents and the resolved implement list. Absent ⇒ config
	// resolution, exactly as before this field existed.
	Agents []string `json:"agents,omitempty"`
	// Effort selects the Routine's model-strength tier (light, standard, heavy)
	// for the chosen preset via the [effort.<agent>] ladder. Absent ⇒ standard.
	Effort      string      `json:"effort,omitempty"`
	Paused      bool        `json:"paused"`
	PauseReason PauseReason `json:"pause_reason,omitempty"`
	CreatedAt   string      `json:"created_at"`
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
	path := filepath.Join(dir, manifestFileName)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("routine %q not found", id)
		}
		return nil, fmt.Errorf("read routine manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse routine manifest %q: %w", id, err)
	}
	// An unscheduled Routine is a durable manual-only state (ADR-0134): the
	// absence is handled before the parser, which still rejects an empty
	// expression. Only a present schedule is parsed.
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

func writeManifest(d *Deps, id string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode routine manifest: %w", err)
	}
	path := filepath.Join(routineDir(d, id), manifestFileName)
	if err := d.FS.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write routine manifest: %w", err)
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
