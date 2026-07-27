package tasks

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ListPickerProjects expands configured project paths into picker-visible entries.
func ListPickerProjects(cfg *config.Config) ([]project.ExpandedProject, error) {
	return ListPickerProjectsWith(project.DefaultDeps(), cfg)
}

// ListPickerProjectsWith expands configured project paths using injected dependencies.
func ListPickerProjectsWith(pd *project.Deps, cfg *config.Config) ([]project.ExpandedProject, error) {
	paths, err := cfg.ExpandProjectsWith(config.DefaultDeps())
	if err != nil {
		return nil, err
	}
	expanded, _ := expandConfiguredPaths(pd, paths)
	project.DisambiguateNames(expanded, cfg.GetDisambiguationStrategy())
	return expanded, nil
}

func expandConfiguredPaths(pd *project.Deps, paths []config.ExpandedPath) ([]project.ExpandedProject, []string) {
	type expandResult struct {
		index    int
		path     string
		projects []project.ExpandedProject
		err      error
	}

	results := make(chan expandResult, len(paths))
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)
		go func(idx int, ep config.ExpandedPath) {
			defer wg.Done()

			var (
				projects  []project.ExpandedProject
				expandErr error
			)
			defer func() {
				if r := recover(); r != nil {
					expandErr = fmt.Errorf("panic expanding %s: %v", ep.Path, r)
				}
				results <- expandResult{index: idx, path: ep.Path, projects: projects, err: expandErr}
			}()

			displayName := lastNSegments(ep.Path, ep.DisplayDepth)
			projectName := filepath.Base(ep.Path)

			if project.HasWorktreesWith(pd, ep.Path) {
				worktrees, err := project.ListWorktreesForPathWith(pd, ep.Path)
				if err != nil {
					expandErr = err
					return
				}
				for _, wt := range worktrees {
					projects = append(projects, project.ExpandedProject{
						Name:         displayName + "/" + wt.Name,
						ProjectLabel: displayName,
						Path:         wt.Path,
						ProjectName:  projectName,
						IsWorktree:   true,
					})
				}
			} else {
				projects = append(projects, project.ExpandedProject{
					Name:         displayName,
					ProjectLabel: displayName,
					Path:         ep.Path,
					ProjectName:  projectName,
					IsWorktree:   false,
				})
			}
		}(i, p)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	resultsByIndex := make(map[int][]project.ExpandedProject, len(paths))
	var failedNames []string
	for r := range results {
		resultsByIndex[r.index] = r.projects
		if r.err != nil {
			failedNames = append(failedNames, filepath.Base(r.path))
		}
	}

	var expanded []project.ExpandedProject
	for i := range paths {
		expanded = append(expanded, resultsByIndex[i]...)
	}
	return expanded, failedNames
}

// lastNSegments returns the trailing n path segments joined with "/", the picker
// display-name helper. It is a pure filepath helper inlined here (rather than
// borrowed from ui) so the tasks package — and everything that consumes it,
// including the lipgloss-free work data core (ADR-0143) — imports no TUI layer.
func lastNSegments(path string, n int) string {
	if n <= 1 {
		return filepath.Base(path)
	}
	result := filepath.Base(path)
	dir := filepath.Dir(path)
	for i := 1; i < n; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		result = filepath.Base(dir) + "/" + result
		dir = parent
	}
	return result
}
