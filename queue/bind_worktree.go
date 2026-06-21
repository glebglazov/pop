package queue

import (
	"io"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// BindWorktreeOptions controls bind-worktree behaviour.
type BindWorktreeOptions = binding.BindWorktreeOptions

// BindWorktreeResult describes the outcome of adopting an existing checkout.
type BindWorktreeResult = binding.BindWorktreeResult

// BindWorktree creates an adopted binding and records a queue journal entry.
func BindWorktree(d *Deps, cfg *config.Config, setID, checkoutPath string, opts BindWorktreeOptions, out io.Writer) (BindWorktreeResult, error) {
	d = ensureQueueDeps(d)
	got, err := binding.BindWorktree(d.Tasks, d.Project, cfg, setID, checkoutPath, opts, binding.LifecycleHooks{
		ReadLock: d.readLock,
	}, out)
	if err != nil {
		return got, err
	}
	proj := ""
	if id, idErr := tasks.ResolveRepositoryIdentity(d.Tasks, got.RuntimePath); idErr == nil {
		proj = binding.DetectProject(d.Project, d.Tasks, cfg, id)
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventBound,
		Project:     proj,
		SetID:       got.SetID,
		RuntimePath: got.RuntimePath,
		SourceRef:   got.Branch,
		Source:      "human",
	}); err != nil {
		return got, err
	}
	return got, nil
}

// detectBindingProject finds the configured project name whose repo identity
// matches id. Returns empty string when zero or multiple projects match.
func detectBindingProject(d *Deps, cfg *config.Config, id *tasks.RepositoryIdentity) string {
	if d == nil {
		return ""
	}
	return binding.DetectProject(d.Project, d.Tasks, cfg, id)
}

func ensureQueueDeps(d *Deps) *Deps {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	return d
}
