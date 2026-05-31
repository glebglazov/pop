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
	PRD                string
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

// CompletePRDStems returns discovered Issue-set identifiers for shell completion.
func CompletePRDStems(input CompletionInput) ([]string, error) {
	return CompletePRDStemsWith(defaultDeps, project.DefaultDeps(), config.Load, input)
}

// CompletePRDStemsWith returns discovered Issue-set identifiers using injected dependencies.
func CompletePRDStemsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput) ([]string, error) {
	defPath, err := resolveCompletionDefinitionPath(d, pd, loadConfig, input)
	if err != nil || defPath == "" {
		return nil, err
	}

	disc, err := DiscoverWith(d, defPath)
	if err != nil {
		return nil, err
	}
	if disc.IssueDirErr != nil {
		return nil, nil
	}

	ids := make([]string, 0, len(disc.Manifests))
	for id := range disc.Manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// CompleteIssueIDs returns manifest issue IDs for the selected PRD.
func CompleteIssueIDs(input CompletionInput) ([]string, error) {
	return CompleteIssueIDsWith(defaultDeps, project.DefaultDeps(), config.Load, input)
}

// CompleteIssueIDsWith returns manifest issue IDs using injected dependencies.
func CompleteIssueIDsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input CompletionInput) ([]string, error) {
	if strings.TrimSpace(input.PRD) == "" {
		return nil, nil
	}

	defPath, err := resolveCompletionDefinitionPath(d, pd, loadConfig, input)
	if err != nil || defPath == "" {
		return nil, err
	}

	disc, err := DiscoverWith(d, defPath)
	if err != nil {
		return nil, err
	}
	if disc.IssueDirErr != nil {
		return nil, nil
	}

	manifestPath, ok := disc.Manifests[input.PRD]
	if !ok {
		return nil, nil
	}

	m := LoadManifest(d, input.PRD, manifestPath)
	ids := make([]string, len(m.Issues))
	for i, issue := range m.Issues {
		ids[i] = issue.ID
	}
	sort.Strings(ids)
	return ids, nil
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
