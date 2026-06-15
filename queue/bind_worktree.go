package queue

import (
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// BindWorktreeOptions controls bind-worktree behaviour.
type BindWorktreeOptions struct {
	Force bool
}

// BindWorktreeResult describes the outcome of adopting an existing checkout.
type BindWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	Replaced    bool
}

// BindWorktree creates an adopted (Provisioned=false) binding for (repo
// identity, setID) pointing to checkoutPath. Run from inside the checkout;
// pass os.Getwd() as checkoutPath. It refuses to re-point a set already bound
// elsewhere unless opts.Force is true, and always refuses while the set holds
// a live Runtime execution lock.
func BindWorktree(d *Deps, cfg *config.Config, setID, checkoutPath string, opts BindWorktreeOptions, out io.Writer) (BindWorktreeResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return BindWorktreeResult{}, fmt.Errorf("set id is required")
	}
	checkoutPath = strings.TrimSpace(checkoutPath)
	if checkoutPath == "" {
		return BindWorktreeResult{}, fmt.Errorf("checkout path is required")
	}
	if out == nil {
		out = io.Discard
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}

	branch, err := resolveSetBranch(d, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve branch in %s: %w", checkoutPath, err)
	}

	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve repository identity: %w", err)
	}
	repoKey := repoIdentityKey(id)
	key := setScopedKey(repoKey, setID)

	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return BindWorktreeResult{}, err
	}

	var replaced bool
	if existing, ok := state.WorktreeBindings[key]; ok {
		lock := d.readLock(existing.RuntimePath)
		if lock != nil && lock.Locked {
			if lock.Metadata != nil && lock.Metadata.SetID != "" && lock.Metadata.SetID != setID {
				return BindWorktreeResult{}, fmt.Errorf("queue: %s runtime checkout is locked for another set (%s); refusing bind-worktree", setID, lock.Metadata.SetID)
			}
			return BindWorktreeResult{}, fmt.Errorf("queue: %s is currently executing; refusing bind-worktree", setID)
		}
		existingCanon, _ := canonicalCheckoutPath(d.Tasks, existing.RuntimePath)
		newCanon, _ := canonicalCheckoutPath(d.Tasks, checkoutPath)
		if existingCanon != newCanon {
			if !opts.Force {
				return BindWorktreeResult{}, fmt.Errorf("queue: %s is already bound to %s; use --force to re-point", setID, existing.RuntimePath)
			}
			replaced = true
		}
	}

	proj := detectBindingProject(d, cfg, id)

	if state.WorktreeBindings == nil {
		state.WorktreeBindings = map[string]WorktreeBinding{}
	}
	state.WorktreeBindings[key] = binding.Adopt(checkoutPath, branch, proj)
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		return BindWorktreeResult{}, err
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventBound,
		Project:     proj,
		SetID:       setID,
		RuntimePath: checkoutPath,
		SourceRef:   branch,
		Source:      "human",
	}); err != nil {
		return BindWorktreeResult{}, err
	}
	if replaced {
		fmt.Fprintf(out, "queue: re-bound %s → %s (branch %s)\n", setID, checkoutPath, branch)
	} else {
		fmt.Fprintf(out, "queue: bound %s → %s (branch %s)\n", setID, checkoutPath, branch)
	}
	return BindWorktreeResult{SetID: setID, RuntimePath: checkoutPath, Branch: branch, Replaced: replaced}, nil
}

// detectBindingProject finds the configured project name whose repo identity
// matches id. Returns empty string when zero or multiple projects match.
func detectBindingProject(d *Deps, cfg *config.Config, id *tasks.RepositoryIdentity) string {
	if cfg == nil || d == nil || d.Project == nil {
		return ""
	}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return ""
	}
	var matches []string
	for _, p := range projects {
		pid, err := tasks.ResolveRepositoryIdentity(d.Tasks, p.Path)
		if err != nil {
			continue
		}
		if pid.ShortHash == id.ShortHash && pid.Basename == id.Basename {
			matches = append(matches, p.Name)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
