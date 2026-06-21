package queue

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

const abandonConfirmPrompt = "Abandon worktree for %s? This removes the bound checkout and branch without integrating. Task statuses are unchanged. [y/N]: "
const abandonConfirmPromptAdopted = "Abandon binding for %s? This forgets the association; the checkout and branch are kept. Task statuses are unchanged. [y/N]: "

// AbandonResult describes the outcome of releasing a worktree binding.
type AbandonResult struct {
	SetID string
	Noop  bool
}

// AbandonOptions controls confirmation for abandon.
type AbandonOptions struct {
	Yes bool
	In  io.Reader
}

// Abandon releases a set's worktree binding without integrating.
func Abandon(d *Deps, cfg *config.Config, setID string, out io.Writer) (AbandonResult, error) {
	return AbandonWithOptions(d, cfg, setID, out, AbandonOptions{In: tasks.NonInteractiveReader{}})
}

// AbandonWithOptions releases a set's worktree binding without integrating.
func AbandonWithOptions(d *Deps, cfg *config.Config, setID string, out io.Writer, opts AbandonOptions) (AbandonResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return AbandonResult{}, fmt.Errorf("set id is required")
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
	if opts.In == nil {
		opts.In = os.Stdin
	}

	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return AbandonResult{}, err
	}
	key, binding, ok, err := findWorktreeBinding(state, setID)
	if err != nil {
		return AbandonResult{}, err
	}
	if !ok {
		fmt.Fprintf(out, "%s has no worktree binding to unbind\n", setID)
		return AbandonResult{SetID: setID, Noop: true}, nil
	}

	return abandonResolvedBinding(d, cfg, state, key, binding, setID, out, opts)
}

// AbandonBindingWithOptions releases the binding stored at bindingKey using
// the same implementation as AbandonWithOptions. It is for callers, such as the
// dashboard, that already resolved a highlighted row to a repository-scoped key.
func AbandonBindingWithOptions(d *Deps, cfg *config.Config, bindingKey, setID string, out io.Writer, opts AbandonOptions) (AbandonResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return AbandonResult{}, fmt.Errorf("set id is required")
	}
	bindingKey = strings.TrimSpace(bindingKey)
	if bindingKey == "" {
		return AbandonWithOptions(d, cfg, setID, out, opts)
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
	if opts.In == nil {
		opts.In = os.Stdin
	}

	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return AbandonResult{}, err
	}
	binding, ok := state.WorktreeBindings[bindingKey]
	if !ok {
		fmt.Fprintf(out, "%s has no worktree binding to unbind\n", setID)
		return AbandonResult{SetID: setID, Noop: true}, nil
	}
	return abandonResolvedBinding(d, cfg, state, bindingKey, binding, setID, out, opts)
}

func abandonResolvedBinding(d *Deps, cfg *config.Config, state *DaemonState, key string, binding WorktreeBinding, setID string, out io.Writer, opts AbandonOptions) (AbandonResult, error) {
	lock := d.readLock(binding.RuntimePath)
	if lock != nil && lock.Locked {
		if lock.Metadata != nil && lock.Metadata.SetID != "" && lock.Metadata.SetID != setID {
			return AbandonResult{}, fmt.Errorf("%s runtime checkout is locked for another set (%s); refusing unbind", setID, lock.Metadata.SetID)
		}
		return AbandonResult{}, fmt.Errorf("%s is currently executing; refusing unbind", setID)
	}

	needsConfirm, err := abandonNeedsConfirm(d, cfg, state, setID, binding)
	if err != nil {
		return AbandonResult{}, err
	}
	if needsConfirm {
		prompt := fmt.Sprintf(abandonConfirmPrompt, setID)
		if !binding.Provisioned {
			prompt = fmt.Sprintf(abandonConfirmPromptAdopted, setID)
		}
		confirmed, err := confirmAbandon(opts.In, out, opts.Yes, prompt)
		if err != nil {
			return AbandonResult{}, err
		}
		if !confirmed {
			fmt.Fprintf(out, "Unbind cancelled for %s\n", setID)
			return AbandonResult{SetID: setID, Noop: true}, nil
		}
	}

	var branch string
	if binding.Provisioned {
		scan, err := resolveBindingScan(d, cfg, binding)
		if err != nil {
			return AbandonResult{}, err
		}
		branch = strings.TrimSpace(binding.Branch)
		if branch == "" {
			branch, err = resolveSetBranch(d, binding.RuntimePath)
			if err != nil {
				return AbandonResult{}, err
			}
		}
		if err := teardownBoundWorktree(d, scan.RuntimePath, binding.RuntimePath, branch); err != nil {
			return AbandonResult{}, err
		}
	} else {
		branch = strings.TrimSpace(binding.Branch)
	}

	state, err = EnsureDaemonState(d.Tasks)
	if err != nil {
		return AbandonResult{}, err
	}
	delete(state.WorktreeBindings, key)
	if state.Mergeability != nil {
		delete(state.Mergeability, key)
	}
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		return AbandonResult{}, err
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventAbandoned,
		Project:     binding.Project,
		SetID:       setID,
		RuntimePath: binding.RuntimePath,
		SourceRef:   branch,
		Source:      "human",
	}); err != nil {
		return AbandonResult{}, err
	}
	if binding.Provisioned {
		fmt.Fprintf(out, "Unbound %s and removed worktree %s\n", setID, binding.RuntimePath)
	} else {
		fmt.Fprintf(out, "Unbound %s (adopted checkout retained at %s)\n", setID, binding.RuntimePath)
	}
	return AbandonResult{SetID: setID}, nil
}

func findWorktreeBinding(state *DaemonState, setID string) (string, WorktreeBinding, bool, error) {
	if state == nil || len(state.WorktreeBindings) == 0 {
		return "", WorktreeBinding{}, false, nil
	}
	var keys []string
	for key, binding := range state.WorktreeBindings {
		parts := strings.Split(key, "\x00")
		if len(parts) != 2 || parts[1] != setID {
			continue
		}
		keys = append(keys, key)
		_ = binding
	}
	switch len(keys) {
	case 0:
		return "", WorktreeBinding{}, false, nil
	case 1:
		return keys[0], state.WorktreeBindings[keys[0]], true, nil
	default:
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "queue: set %q is ambiguous; bound in:", setID)
		for _, key := range keys {
			binding := state.WorktreeBindings[key]
			fmt.Fprintf(&b, "\n  %s (%s)", binding.Project, binding.RuntimePath)
		}
		return "", WorktreeBinding{}, false, fmt.Errorf("%s", b.String())
	}
}

func abandonNeedsConfirm(d *Deps, cfg *config.Config, state *DaemonState, setID string, binding WorktreeBinding) (bool, error) {
	if _, _, ok, err := findIntegrationRecord(state, setID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	if binding.Project == "" {
		return false, nil
	}
	failed, err := setHasStatus(d, cfg, binding, setID, tasks.StatusFailed)
	if err != nil {
		return false, err
	}
	return failed, nil
}

func setHasStatus(d *Deps, cfg *config.Config, binding WorktreeBinding, setID string, status tasks.TaskSetStatus) (bool, error) {
	scan, err := resolveBindingScan(d, cfg, binding)
	if err != nil {
		return false, err
	}
	refresh, err := d.refresh(scan.DefinitionPath)
	if err != nil {
		return false, err
	}
	for _, row := range refresh.Rows {
		if row.ID == setID && row.Status == status {
			return true, nil
		}
	}
	return false, nil
}

func resolveBindingScan(d *Deps, cfg *config.Config, binding WorktreeBinding) (projectScan, error) {
	return resolveIntegrationScan(d, cfg, MergeabilityRecord{
		Project:     binding.Project,
		RuntimePath: binding.RuntimePath,
	})
}

func confirmAbandon(in io.Reader, out io.Writer, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if _, ok := in.(tasks.NonInteractiveReader); ok {
		return false, fmt.Errorf("non-interactive abandon requires --yes")
	}
	if in == nil {
		in = os.Stdin
	}
	if in == os.Stdin {
		if f, ok := in.(*os.File); ok {
			info, err := f.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				return false, fmt.Errorf("non-interactive abandon requires --yes")
			}
		}
	}
	if prompt == "" {
		prompt = abandonConfirmPrompt
	}
	fmt.Fprintf(out, "%s", prompt)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read abandon confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func teardownBoundWorktree(d *Deps, workingPath, runtimePath, branch string) error {
	return binding.TeardownWorktree(d.Tasks, workingPath, runtimePath, branch, true)
}
