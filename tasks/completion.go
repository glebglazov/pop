package tasks

import (
	"os"
	"sort"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

type completionArchiveMode int

const (
	completionActiveOnly completionArchiveMode = iota
	completionArchivedOnly
)

// CompletionInput selects context for read-only shell completion candidates.
type CompletionInput struct {
	ProjectName        string
	Path               string
	DefinitionOverride string
	CWD                string
}

// CompleteProjectNames returns picker-visible project names for shell completion.
func CompleteProjectNames() ([]string, error) {
	return CompleteProjectNamesWith(defaultDeps, project.DefaultDeps(), config.Load)
}

// CompleteProjectNamesWith returns picker-visible project names using injected dependencies.
func CompleteProjectNamesWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error)) ([]string, error) {
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		if isConfigMissing(err) {
			return nil, nil
		}
		return nil, err
	}

	projects, err := ListPickerProjectsWith(pd, cfg)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	sort.Strings(names)
	return names, nil
}

// CompleteTaskSetIDs returns discovered Task-set identifiers for shell completion.
func CompleteTaskSetIDs(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteTaskSetIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteTaskSetIDsWith returns discovered Task-set identifiers using injected dependencies.
func CompleteTaskSetIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	return completeTaskSetIDsWithArchiveMode(d, pd, loadConfig, input, toComplete, completionActiveOnly)
}

// CompleteExportTaskSetIDs returns on-disk Task-set identifiers for
// `transfer export` completion. Alone among completion surfaces it orders
// newest-first (reverse identifier sort) and drops ids already on the command
// line, matching the recency-driven transfer workflow. Archived sets stay
// omitted like every surface except unarchive.
func CompleteExportTaskSetIDs(input CompletionInput, chosen []string, toComplete string) ([]string, error) {
	return CompleteExportTaskSetIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input, chosen, toComplete)
}

// CompleteExportTaskSetIDsWith returns export completion candidates using injected dependencies.
func CompleteExportTaskSetIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, chosen []string, toComplete string) ([]string, error) {
	ids, err := completeTaskSetIDsWithArchiveMode(d, pd, loadConfig, input, toComplete, completionActiveOnly)
	if err != nil {
		return nil, err
	}
	alreadyChosen := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		alreadyChosen[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if alreadyChosen[id] {
			continue
		}
		out = append(out, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// CompleteArchivedTaskSetIDs returns archived Task-set identifiers for shell completion.
func CompleteArchivedTaskSetIDs(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteArchivedTaskSetIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteArchivedTaskSetIDsWith returns archived Task-set identifiers using injected dependencies.
func CompleteArchivedTaskSetIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	return completeTaskSetIDsWithArchiveMode(d, pd, loadConfig, input, toComplete, completionArchivedOnly)
}

func completeTaskSetIDsWithArchiveMode(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string, mode completionArchiveMode) ([]string, error) {
	defPath, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || defPath == "" {
		return nil, err
	}
	if refresh == nil {
		return nil, nil
	}

	archived, err := completionArchivedIDs(d, defPath)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(refresh.Manifests))
	for id := range refresh.Manifests {
		isArchived := archived[id]
		if mode == completionActiveOnly && isArchived {
			continue
		}
		if mode == completionArchivedOnly && !isArchived {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// CompleteTaskTargets returns bare Task set identifiers and, after an
// identifier and slash, set-relative task files. It never offers filesystem
// path segments.
func CompleteTaskTargets(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteTaskTargetsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteTaskTargetsWith returns Run task target candidates using injected dependencies.
func CompleteTaskTargetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}
	if err := omitArchivedCompletionTargets(d, refresh); err != nil {
		return nil, err
	}
	return taskTargetIdentifierCompletions(refresh, toComplete), nil
}

// CompleteActionableTaskTargets is the filtered variant for implement and the
// override verbs: Done Task sets and done tasks are omitted because neither is
// actionable by those verbs. Timings completes the unfiltered list instead.
func CompleteActionableTaskTargets(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteActionableTaskTargetsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteActionableTaskTargetsWith returns actionable task target candidates using injected dependencies.
func CompleteActionableTaskTargetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}
	if err := omitArchivedCompletionTargets(d, refresh); err != nil {
		return nil, err
	}
	return actionableTaskTargetCompletions(refresh, toComplete), nil
}

func completionRefreshContext(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput) (string, *RefreshResult, error) {
	defPath, err := resolveCompletionDefinitionPath(d, pd, loadConfig, input)
	if err != nil || defPath == "" {
		return "", nil, err
	}

	disc, err := DiscoverWith(d, defPath)
	if err != nil {
		return "", nil, err
	}
	if disc.TaskDirErr != nil {
		return defPath, nil, nil
	}

	refresh := &RefreshResult{DefinitionPath: defPath, Manifests: make(map[string]*Manifest, len(disc.Manifests))}
	for stem, manifestPath := range disc.Manifests {
		refresh.Manifests[stem] = LoadManifest(d, stem, manifestPath)
	}
	return defPath, refresh, nil
}

func completionArchivedIDs(d *Deps, defPath string) (map[string]bool, error) {
	state, err := LoadGlobalStateWith(d, StatePathFor(defPath))
	if err != nil {
		return nil, err
	}
	archived := make(map[string]bool)
	entry := state.Tasks[defPath]
	if entry == nil {
		return archived, nil
	}
	for _, set := range entry.TaskSets {
		if set.Archived {
			archived[set.ID] = true
		}
	}
	return archived, nil
}

func omitArchivedCompletionTargets(d *Deps, refresh *RefreshResult) error {
	archived, err := completionArchivedIDs(d, refresh.DefinitionPath)
	if err != nil {
		return err
	}
	for id := range archived {
		delete(refresh.Manifests, id)
	}
	return nil
}

func resolveCompletionDefinitionPath(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput) (string, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, ResolveInput{
		ProjectName:        input.ProjectName,
		Path:               input.Path,
		DefinitionOverride: input.DefinitionOverride,
		CWD:                input.CWD,
	})
	if err != nil {
		return "", nil
	}
	return resolved.DefinitionPath, nil
}

func isConfigMissing(err error) bool {
	return os.IsNotExist(err)
}
