package tasks

import (
	"os"
	"sort"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
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
	defPath, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || defPath == "" {
		return nil, err
	}
	if refresh == nil {
		return nil, nil
	}

	ids := make([]string, 0, len(refresh.Manifests))
	for id := range refresh.Manifests {
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
	return taskTargetIdentifierCompletions(refresh, toComplete), nil
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

	refresh := &RefreshResult{Manifests: make(map[string]*Manifest, len(disc.Manifests))}
	for stem, manifestPath := range disc.Manifests {
		refresh.Manifests[stem] = LoadManifest(d, stem, manifestPath)
	}
	return defPath, refresh, nil
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
