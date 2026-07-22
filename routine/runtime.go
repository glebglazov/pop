package routine

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// RuntimeResult is the outcome of a direct runtime-config write (agents/effort).
type RuntimeResult struct {
	RoutineID string
	Agents    []string
	Effort    string
	// Paused reports the routine's pause state after the write. Editing runtime
	// config is a run-affecting change, so an edit pauses with reason `changed`.
	Paused bool
}

// UpdateRuntime edits a Routine's runtime agents/effort using default deps.
func UpdateRuntime(id string, agents []string, agentsSet bool, effort string, effortSet bool) (*RuntimeResult, error) {
	return UpdateRuntimeWith(defaultDeps, id, agents, agentsSet, effort, effortSet)
}

// UpdateRuntimeWith is the edit chokepoint: it validates the requested agents
// and effort, applies whichever were set to the manifest, and pauses the
// Routine with reason `changed` because a runtime-config edit is a run-affecting
// change (ADR-0128). Invalid preset names or effort values are rejected before
// anything is written.
func UpdateRuntimeWith(d *Deps, id string, agents []string, agentsSet bool, effort string, effortSet bool) (*RuntimeResult, error) {
	return writeRuntime(d, id, agents, agentsSet, effort, effortSet, true)
}

// ConfigureRuntime sets a Routine's runtime agents/effort at creation using
// default deps, without flipping its pause reason.
func ConfigureRuntime(id string, agents []string, agentsSet bool, effort string, effortSet bool) (*RuntimeResult, error) {
	return ConfigureRuntimeWith(defaultDeps, id, agents, agentsSet, effort, effortSet)
}

// ConfigureRuntimeWith writes runtime config as part of `pop routine new`. The
// routine is already paused-on-creation, so this leaves the pause reason
// untouched rather than marking it `changed`.
func ConfigureRuntimeWith(d *Deps, id string, agents []string, agentsSet bool, effort string, effortSet bool) (*RuntimeResult, error) {
	return writeRuntime(d, id, agents, agentsSet, effort, effortSet, false)
}

func writeRuntime(d *Deps, id string, agents []string, agentsSet bool, effort string, effortSet bool, markChanged bool) (*RuntimeResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	// An explicitly-empty set is a clear back to unset (config-resolved), not a
	// validation failure — the dashboard agent/effort modal submits empty fields
	// to reset. Only a non-empty list is validated against the known presets.
	clearAgents := agentsSet && len(nonEmptyAgentSpecs(agents)) == 0
	if agentsSet && !clearAgents {
		if err := validateAgentPresets(agents); err != nil {
			return nil, err
		}
	}
	trimmedEffort := strings.TrimSpace(effort)
	clearEffort := effortSet && trimmedEffort == ""
	if effortSet && !clearEffort {
		if err := validateEffort(trimmedEffort); err != nil {
			return nil, err
		}
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return nil, err
	}
	if agentsSet {
		r.Manifest.Agents = nonEmptyAgentSpecs(agents)
	}
	if effortSet {
		r.Manifest.Effort = trimmedEffort
	}
	if markChanged {
		r.Manifest.Paused = true
		r.Manifest.PauseReason = PauseReasonChanged
	}
	if err := writeManifest(d, id, r.Manifest); err != nil {
		return nil, err
	}
	return &RuntimeResult{
		RoutineID: id,
		Agents:    r.Manifest.Agents,
		Effort:    r.Manifest.Effort,
		Paused:    r.Manifest.Paused,
	}, nil
}

// validateAgentPresets rejects any spec whose preset is not a known agent.
func validateAgentPresets(agents []string) error {
	specs := nonEmptyAgentSpecs(agents)
	if len(specs) == 0 {
		return fmt.Errorf("at least one agent preset is required")
	}
	for _, spec := range specs {
		if _, err := tasks.ResolveAgentAdapter(spec); err != nil {
			return err
		}
	}
	return nil
}

// validateEffort rejects an effort value outside the accepted tiers.
func validateEffort(effort string) error {
	if !tasks.IsValidEffort(effort) {
		return fmt.Errorf("invalid effort %q; valid: %s", effort, strings.Join(tasks.ValidEfforts(), ", "))
	}
	return nil
}
