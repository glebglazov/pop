package routine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// EditResult is the outcome of a `pop routine edit`.
type EditResult struct {
	RoutineID       string
	PromptPath      string
	ScheduleUpdated bool
	Schedule        string
	Opened          bool
}

// UpdateSchedule rewrites a Routine's schedule using default dependencies.
func UpdateSchedule(id, scheduleRaw string) (Manifest, error) {
	return UpdateScheduleWith(defaultDeps, id, scheduleRaw)
}

// UpdateScheduleWith validates scheduleRaw through the schedule parser and, only
// if it parses, persists it to the routine's manifest (read-modify-write). An
// unparseable expression is rejected before anything is written. The bound
// directory and creation time are preserved. Editing the schedule is a
// run-affecting change, so this eager chokepoint pauses the Routine with reason
// `changed` (ADR-0128). The dashboard's edit-schedule modal and the refinement
// loop call this same helper.
func UpdateScheduleWith(d *Deps, id, scheduleRaw string) (Manifest, error) {
	if err := validateID(id); err != nil {
		return Manifest{}, err
	}
	trimmed := strings.TrimSpace(scheduleRaw)
	if _, err := ParseSchedule(trimmed); err != nil {
		return Manifest{}, err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return Manifest{}, err
	}
	r.Manifest.Schedule = trimmed
	r.Manifest.Paused = true
	r.Manifest.PauseReason = PauseReasonChanged
	if err := writeManifest(d, id, r.Manifest); err != nil {
		return Manifest{}, err
	}
	return r.Manifest, nil
}

// Edit edits a Routine's prompt or schedule using default dependencies.
func Edit(id, scheduleRaw string, scheduleSet bool) (*EditResult, error) {
	return EditWith(defaultDeps, id, scheduleRaw, scheduleSet)
}

// EditWith edits an existing Routine. With scheduleSet it rewrites the manifest
// schedule and opens no editor. Otherwise it opens the routine's prompt.md in
// $EDITOR, which only happens on an interactive TTY; a non-interactive plain
// edit errors and names the prompt path so the user can edit it directly.
// Edit scope is prompt + schedule only — the bound directory and id are fixed
// at creation.
func EditWith(d *Deps, id, scheduleRaw string, scheduleSet bool) (*EditResult, error) {
	if scheduleSet {
		m, err := UpdateScheduleWith(d, id, scheduleRaw)
		if err != nil {
			return nil, err
		}
		return &EditResult{RoutineID: id, ScheduleUpdated: true, Schedule: m.Schedule}, nil
	}

	if err := validateID(id); err != nil {
		return nil, err
	}
	if _, err := loadManifest(d, id); err != nil {
		return nil, err
	}

	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	if d.IsInteractive == nil || !d.IsInteractive() {
		return nil, fmt.Errorf("cannot open an editor in a non-interactive session; edit the prompt directly at %s", promptPath)
	}
	if d.OpenEditor == nil {
		return nil, fmt.Errorf("no editor available; edit the prompt directly at %s", promptPath)
	}
	if err := d.OpenEditor(promptPath); err != nil {
		return nil, fmt.Errorf("open prompt in editor: %w", err)
	}
	// Opening the prompt editor is a run-affecting edit chokepoint: pause with
	// reason `changed` (ADR-0128) so a re-proving manual fire is required.
	if err := pauseChanged(d, id); err != nil {
		return nil, err
	}
	return &EditResult{RoutineID: id, PromptPath: promptPath, Opened: true}, nil
}
