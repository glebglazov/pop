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

// ArchiveSetSelectionRow is one Task set offered in the cross-set archive
// selection. Done sets are initially checked; every non-archived registered set
// remains toggleable so the operator can widen or narrow the batch.
type ArchiveSetSelectionRow struct {
	TaskSetID string
	Status    TaskSetStatus
	Checked   bool
}

// ArchiveSetSelectionContext carries the resolved cross-set archive selection.
type ArchiveSetSelectionContext struct {
	Rows []ArchiveSetSelectionRow
}

// ArchiveTaskSetsOptions configures an atomic archive update across multiple
// registered Task sets.
type ArchiveTaskSetsOptions struct {
	ResolveInput
	TaskSetIDs []string
}

// UnarchiveTaskSetsOptions configures an atomic restore update across multiple
// archived registered Task sets.
type UnarchiveTaskSetsOptions struct {
	ResolveInput
	TaskSetIDs []string
}

// ArchiveTaskSetsResult is the outcome of archiving zero or more Task sets.
type ArchiveTaskSetsResult struct {
	TaskSetIDs []string
	Refresh    *RefreshResult
}

// UnarchiveTaskSetsResult is the outcome of restoring zero or more Task sets.
type UnarchiveTaskSetsResult struct {
	TaskSetIDs []string
	Refresh    *RefreshResult
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

// LoadArchiveSetSelectionWith lists every non-archived registered Task set and
// marks only Done sets as initially checked.
func LoadArchiveSetSelectionWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput) (*ArchiveSetSelectionContext, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}
	refresh, err := RefreshWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, err
	}
	return &ArchiveSetSelectionContext{Rows: BuildArchiveSetSelection(refresh)}, nil
}

// LoadUnarchiveSetSelectionWith lists every archived registered Task set with
// no initially checked rows.
func LoadUnarchiveSetSelectionWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput) (*ArchiveSetSelectionContext, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}
	refresh, err := RefreshArchivedWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, err
	}
	return &ArchiveSetSelectionContext{Rows: BuildUnarchiveSetSelection(refresh)}, nil
}

// BuildArchiveSetSelection shapes status rows into a cross-set checkbox list.
func BuildArchiveSetSelection(refresh *RefreshResult) []ArchiveSetSelectionRow {
	if refresh == nil {
		return nil
	}
	rows := make([]ArchiveSetSelectionRow, 0, len(refresh.Rows))
	for _, row := range refresh.Rows {
		rows = append(rows, ArchiveSetSelectionRow{
			TaskSetID: row.ID,
			Status:    row.Status,
			Checked:   row.Status == StatusDone,
		})
	}
	return rows
}

// BuildUnarchiveSetSelection shapes archived status rows into a cross-set
// checkbox list. Restore starts intentionally empty, so every row is unchecked.
func BuildUnarchiveSetSelection(refresh *RefreshResult) []ArchiveSetSelectionRow {
	if refresh == nil {
		return nil
	}
	rows := make([]ArchiveSetSelectionRow, 0, len(refresh.Rows))
	for _, row := range refresh.Rows {
		rows = append(rows, ArchiveSetSelectionRow{
			TaskSetID: row.ID,
			Status:    row.Status,
			Checked:   false,
		})
	}
	return rows
}

// DoneArchiveSetIDs returns the ids that the no-argument archive picker should
// pre-check: exactly the non-archived Done Task sets in displayed row order.
func DoneArchiveSetIDs(rows []ArchiveSetSelectionRow) []string {
	var ids []string
	for _, row := range rows {
		if row.Checked {
			ids = append(ids, row.TaskSetID)
		}
	}
	return ids
}

// ArchiveTaskSetsWith marks several registered Task sets archived in one state
// update. An empty selection is a clean no-op and writes nothing.
func ArchiveTaskSetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ArchiveTaskSetsOptions) (*ArchiveTaskSetsResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, err
	}
	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}
	if len(opts.TaskSetIDs) == 0 {
		return &ArchiveTaskSetsResult{Refresh: refresh}, nil
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]bool, len(refresh.Rows))
	for _, row := range refresh.Rows {
		allowed[row.ID] = true
	}
	seen := make(map[string]bool, len(opts.TaskSetIDs))
	selected := make([]string, 0, len(opts.TaskSetIDs))
	for _, raw := range opts.TaskSetIDs {
		id, err := resolveRegisteredTaskSetTarget(state, resolved.DefinitionPath, raw)
		if err != nil {
			return nil, err
		}
		if !allowed[id] {
			return nil, fmt.Errorf("task set %q is already archived", id)
		}
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}

	if err := SetTaskSetArchived(d, resolved.DefinitionPath, selected, true); err != nil {
		return nil, err
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}
	return &ArchiveTaskSetsResult{TaskSetIDs: selected, Refresh: afterRefresh}, nil
}

// UnarchiveTaskSet clears one registered Task set's archived flag.
func UnarchiveTaskSet(input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return UnarchiveTaskSetWith(defaultDeps, projectDefaultDeps(), config.Load, input, taskSetID)
}

// UnarchiveTaskSetWith clears one registered Task set's archived flag using injected dependencies.
func UnarchiveTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string) (*ArchiveResult, error) {
	return setTaskSetArchivedWith(d, pd, loadConfig, input, taskSetID, false)
}

// UnarchiveTaskSetsWith clears several archived registered Task-set flags in
// one state update. An empty selection is a clean no-op and writes nothing.
func UnarchiveTaskSetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts UnarchiveTaskSetsOptions) (*UnarchiveTaskSetsResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, err
	}
	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshArchivedWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}
	if len(opts.TaskSetIDs) == 0 {
		return &UnarchiveTaskSetsResult{Refresh: refresh}, nil
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]bool, len(refresh.Rows))
	for _, row := range refresh.Rows {
		allowed[row.ID] = true
	}
	seen := make(map[string]bool, len(opts.TaskSetIDs))
	selected := make([]string, 0, len(opts.TaskSetIDs))
	for _, raw := range opts.TaskSetIDs {
		id, err := resolveRegisteredTaskSetTarget(state, resolved.DefinitionPath, raw)
		if err != nil {
			return nil, err
		}
		if !allowed[id] {
			return nil, fmt.Errorf("task set %q is not archived", id)
		}
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}

	if err := SetTaskSetArchived(d, resolved.DefinitionPath, selected, false); err != nil {
		return nil, err
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}
	return &UnarchiveTaskSetsResult{TaskSetIDs: selected, Refresh: afterRefresh}, nil
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

	if err := SetTaskSetArchived(d, resolved.DefinitionPath, []string{resolvedTaskSetID}, archived); err != nil {
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
