package integration

import (
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds integration dependencies and test seams.
type Deps struct {
	Tasks   *tasks.Deps
	Project *project.Deps

	ComputeMergeability func(workingPath, runtimePath string) (Record, error)
	AcquireRuntimeLock  func(runtimePath string) (RuntimeLock, error)
}

// RuntimeLock serializes integration with runtime execution.
type RuntimeLock interface {
	Release() error
}

// DefaultDeps returns integration dependencies backed by real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		Tasks:   tasks.DefaultDeps(),
		Project: project.DefaultDeps(),
	}
}

func (d *Deps) tasksDeps() *tasks.Deps {
	if d != nil && d.Tasks != nil {
		return d.Tasks
	}
	return tasks.DefaultDeps()
}

func (d *Deps) projectDeps() *project.Deps {
	if d != nil && d.Project != nil {
		return d.Project
	}
	return project.DefaultDeps()
}

func (d *Deps) computeMergeability(workingPath, runtimePath string) (Record, error) {
	if d != nil && d.ComputeMergeability != nil {
		return d.ComputeMergeability(workingPath, runtimePath)
	}
	return Compute(d.tasksDeps(), workingPath, runtimePath)
}

func (d *Deps) acquireRuntimeLock(runtimePath string) (RuntimeLock, error) {
	if d != nil && d.AcquireRuntimeLock != nil {
		return d.AcquireRuntimeLock(runtimePath)
	}
	return tasks.AcquireRuntimeLock(d.tasksDeps(), runtimePath, nil)
}

// ScopedKeyForPaths keys set-scoped mergeability by repository identity plus set id.
func ScopedKeyForPaths(td *tasks.Deps, projectPath, runtimePath, setID string) (string, error) {
	repoKey := repoIdentityFromWorktreePath(runtimePath)
	if repoKey == "" {
		id, err := tasks.ResolveRepositoryIdentity(td, projectPath)
		if err != nil {
			return "", err
		}
		repoKey = bindingRepoKey(id)
	}
	return binding.ScopedKey(repoKey, setID), nil
}

func bindingRepoKey(id *tasks.RepositoryIdentity) string {
	return id.Basename + "-" + id.ShortHash
}
