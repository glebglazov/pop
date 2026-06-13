package tasks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ArchiveResult is the outcome of toggling one Task set's archived flag.
type ArchiveResult struct {
	Refresh   *RefreshResult
	TaskSetID string
	Archived  bool
}

// ArchivedTaskSetError reports that a deliberately archived Task set was
// explicitly targeted by a verb that schedules or mutates tasks.
type ArchivedTaskSetError struct {
	TaskSetID string
}

func (e ArchivedTaskSetError) Error() string {
	return fmt.Sprintf("task set %q is archived; run `pop tasks unarchive %s` first", e.TaskSetID, e.TaskSetID)
}

// RejectArchivedTaskSet returns a setup error when taskSetID is archived in
// the repository-local Task state. Unknown/unregistered sets are left to the
// caller's normal target validation.
func RejectArchivedTaskSet(d *Deps, statePath, defPath, taskSetID string) error {
	if taskSetID == "" {
		return nil
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return exitErr(ExitSetup, "%v", err)
	}
	if taskSetArchived(state, defPath, taskSetID) {
		return exitErr(ExitSetup, "%v", ArchivedTaskSetError{TaskSetID: taskSetID})
	}
	return nil
}

func taskSetArchived(state *GlobalState, defPath, taskSetID string) bool {
	if state == nil {
		return false
	}
	entry := state.Tasks[defPath]
	if entry == nil {
		return false
	}
	for _, set := range entry.TaskSets {
		if set.ID == taskSetID {
			return set.Archived
		}
	}
	return false
}

// ArchiveTaskSet marks one registered Task set as archived.
func ArchiveTaskSet(input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return ArchiveTaskSetWith(defaultDeps, projectDefaultDeps(), config.Load, input, taskSetID)
}

// ArchiveTaskSetWith marks one registered Task set as archived using injected dependencies.
func ArchiveTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return setTaskSetArchivedWith(d, pd, loadConfig, input, taskSetID, true)
}

// UnarchiveTaskSet clears one registered Task set's archived flag.
func UnarchiveTaskSet(input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return UnarchiveTaskSetWith(defaultDeps, projectDefaultDeps(), config.Load, input, taskSetID)
}

// UnarchiveTaskSetWith clears one registered Task set's archived flag using injected dependencies.
func UnarchiveTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return setTaskSetArchivedWith(d, pd, loadConfig, input, taskSetID, false)
}

func setTaskSetArchivedWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string, archived bool) (*ArchiveResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	if _, err := RefreshWith(d, resolved.DefinitionPath, statePath); err != nil {
		return nil, err
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}
	resolvedTaskSetID, err := resolveRegisteredTaskSetTarget(state, resolved.DefinitionPath, taskSetID)
	if err != nil {
		return nil, err
	}

	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[resolved.DefinitionPath]
		idx, _, err := findRegisteredTaskSet(entry, resolvedTaskSetID)
		if err != nil {
			return err
		}
		entry.TaskSets[idx].Archived = archived
		return nil
	})
	if err != nil {
		return nil, err
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	return &ArchiveResult{
		Refresh:   refresh,
		TaskSetID: resolvedTaskSetID,
		Archived:  archived,
	}, nil
}

func resolveRegisteredTaskSetTarget(state *GlobalState, defPath, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("expected a bare task set identifier")
	}
	if filepath.IsAbs(raw) || raw == "~" || strings.HasPrefix(filepath.ToSlash(raw), "~/") {
		return "", fmt.Errorf("%s", invalidRegisteredTargetMessage(state, defPath, raw, "absolute paths are not task target references"))
	}
	if raw == "." || raw == ".." || strings.HasPrefix(filepath.ToSlash(raw), "./") || strings.HasPrefix(filepath.ToSlash(raw), "../") {
		return "", fmt.Errorf("%s", invalidRegisteredTargetMessage(state, defPath, raw, "relative paths are not task target references"))
	}
	slash := strings.TrimSuffix(filepath.ToSlash(raw), "/")
	if strings.Contains(slash, "/") || strings.HasSuffix(slash, ".md") {
		return "", fmt.Errorf("%s", invalidRegisteredTargetMessage(state, defPath, raw, "expected a bare task set identifier"))
	}

	entry := state.Tasks[defPath]
	if _, _, err := findRegisteredTaskSet(entry, slash); err != nil {
		return "", err
	}
	return slash, nil
}

func invalidRegisteredTargetMessage(state *GlobalState, defPath, raw, reason string) string {
	ids := registeredIdentifierList(state, defPath)
	if len(ids) == 0 {
		return fmt.Sprintf("invalid target %q: %s", raw, reason)
	}
	return fmt.Sprintf("invalid target %q: %s; valid: %s", raw, reason, strings.Join(ids, ", "))
}
