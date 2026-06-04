package workload

import (
	"os"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// CompletionInput selects context for read-only shell completion candidates.
type CompletionInput struct {
	ProjectName        string
	Path               string
	DefinitionOverride string
	CWD                string
	IssueSet           string
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

// CompleteIssueSetIDs returns discovered Issue-set identifiers for shell completion.
func CompleteIssueSetIDs(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssueSetIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssueSetIDsWith returns discovered Issue-set identifiers using injected dependencies.
func CompleteIssueSetIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
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

// CompleteIssueSetPaths returns CWD-relative discovered Issue-set paths for shell completion.
func CompleteIssueSetPaths(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssueSetPathsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssueSetPathsWith returns CWD-relative discovered Issue-set paths using injected dependencies.
func CompleteIssueSetPathsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}
	cwd, err := cwdOrDefault(d, input.CWD)
	if err != nil {
		return nil, err
	}
	return issueSetPathCompletionsFromCWD(refresh, cwd, toComplete), nil
}

// CompleteIssueSetTargets returns Issue set identifiers by default and CWD-relative Issue-set paths for path-like input.
func CompleteIssueSetTargets(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssueSetTargetsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssueSetTargetsWith returns Issue set target candidates using injected dependencies.
func CompleteIssueSetTargetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	if completionLooksPathLike(toComplete) {
		return CompleteIssueSetPathsWith(d, pd, loadConfig, input, toComplete)
	}
	return CompleteIssueSetIDsWith(d, pd, loadConfig, input, toComplete)
}

// CompleteIssuePaths returns CWD-relative discovered issue markdown paths for shell completion.
func CompleteIssuePaths(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssuePathsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssuePathsWith returns CWD-relative discovered issue markdown paths using injected dependencies.
func CompleteIssuePathsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}
	cwd, err := cwdOrDefault(d, input.CWD)
	if err != nil {
		return nil, err
	}
	return issuePathCompletionsFromCWD(refresh, cwd, toComplete), nil
}

// CompleteIssueTargets returns Issue set identifiers by default and CWD-relative Issue paths for path-like input.
func CompleteIssueTargets(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssueTargetsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssueTargetsWith returns Run issue target candidates using injected dependencies.
func CompleteIssueTargetsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}
	if completionLooksPathLike(toComplete) {
		cwd, err := cwdOrDefault(d, input.CWD)
		if err != nil {
			return nil, err
		}
		return issuePathCompletionsFromCWD(refresh, cwd, toComplete), nil
	}
	return issueTargetIdentifierCompletions(refresh, toComplete), nil
}

// CompleteIssueIDs returns manifest issue IDs for the selected Issue set.
func CompleteIssueIDs(input CompletionInput, toComplete string) ([]string, error) {
	return CompleteIssueIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input, toComplete)
}

// CompleteIssueIDsWith returns manifest issue IDs using injected dependencies.
func CompleteIssueIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput, toComplete string) ([]string, error) {
	if completionLooksPathLike(toComplete) {
		_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
		if err != nil || refresh == nil {
			return nil, err
		}
		return issuePathCompletions(refresh, input.IssueSet, input.CWD, toComplete), nil
	}

	if strings.TrimSpace(input.IssueSet) == "" {
		return nil, nil
	}

	_, refresh, err := completionRefreshContext(d, pd, loadConfig, input)
	if err != nil || refresh == nil {
		return nil, err
	}

	return issuePathCompletions(refresh, input.IssueSet, input.CWD, toComplete), nil
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
	if disc.IssueDirErr != nil {
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
