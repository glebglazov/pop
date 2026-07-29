package implement

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// WholeSetOptions configures whole-set Implement orchestration: drain routing
// and the task-set drain. Integration was removed (ADR-0070): a Done drain in a
// worktree is the human's own concern, so there is no merge epilogue.
type WholeSetOptions struct {
	ResolveInput    tasks.ResolveInput
	TaskSetOverride string
	InWorktree      bool
	ForceRebind     bool
	AgentPreset     string
	AgentPresets    []string
	AgentExplicit   bool
	AgentCmd        string
	AgentOutput     tasks.AgentOutputMode
	AllowDirty      tasks.DirtyRuntimeStrategy
	MaxTries         int
	MaxTriesExplicit bool
	Timeout          time.Duration
	// VerifyAgents and VerifyEffort steer the in-drain pre-approval Verifier
	// independently of the implementing agents (`--verify-agent`, repeatable, and
	// `--verify-effort`), forwarded verbatim to the task-set executor.
	VerifyAgents []string
	VerifyEffort string
	Yes          bool
	ConfirmIn    io.Reader
	ConfirmOut   io.Writer
	Output       io.Writer
	// PreSeedTopic pre-seeds the pane's Topic from each task's Title at drain
	// spawn (ADR-0058); forwarded verbatim to the task-set executor.
	PreSeedTopic func(taskTitle string)
}

// RuntimeConfirmOptions carries confirmation inputs and the report writer for
// foreground drain routing.
type RuntimeConfirmOptions struct {
	Yes        bool
	ConfirmIn  io.Reader
	ConfirmOut io.Writer
	// Output receives the drain-at-binding location report. When nil, the report
	// is discarded (tests that only assert ResolveInput may omit it).
	Output io.Writer
}

// RunWholeSet orchestrates whole-set Implement: route → drain.
func RunWholeSet(opts WholeSetOptions) (*tasks.RunTaskSetResult, error) {
	return RunWholeSetWith(DefaultDeps(), opts)
}

// RunWholeSetWith drains one task set using injected dependencies.
func RunWholeSetWith(d *Deps, opts WholeSetOptions) (*tasks.RunTaskSetResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	resolveInput, err := resolveTaskSetRuntime(d, opts.ResolveInput, opts.TaskSetOverride, opts.InWorktree, opts.ForceRebind, RuntimeConfirmOptions{
		Yes:        opts.Yes,
		ConfirmIn:  opts.ConfirmIn,
		ConfirmOut: opts.ConfirmOut,
		Output:     opts.Output,
	})
	if err != nil {
		return nil, err
	}
	result, err := tasks.RunTaskSetWith(d.tasksDeps(), d.projectDeps(), d.loadConfig, tasks.RunTaskSetOptions{
		ResolveInput:    resolveInput,
		TaskSetOverride: opts.TaskSetOverride,
		AgentPreset:     opts.AgentPreset,
		AgentPresets:    opts.AgentPresets,
		AgentExplicit:   opts.AgentExplicit,
		AgentCmd:        opts.AgentCmd,
		AgentOutput:     opts.AgentOutput,
		AllowDirty:      opts.AllowDirty,
		MaxTries:         opts.MaxTries,
		MaxTriesExplicit: opts.MaxTriesExplicit,
		Timeout:          opts.Timeout,
		VerifyAgents:    opts.VerifyAgents,
		VerifyEffort:    opts.VerifyEffort,
		Yes:             opts.Yes,
		ConfirmIn:       opts.ConfirmIn,
		ConfirmOut:      opts.ConfirmOut,
		Output:          opts.Output,
		BindCheckout:           bindCheckout(d),
		RefreshManagedCheckout: refreshManagedCheckout(d),
		PreSeedTopic:    opts.PreSeedTopic,
	})
	if err != nil {
		return result, err
	}
	return result, tasks.QuotaPausedExit(result != nil && result.QuotaPaused)
}

// ResolveTaskSetRuntime applies drain checkout routing for tests and returns
// the ResolveInput the executor should use.
func ResolveTaskSetRuntime(d *Deps, in tasks.ResolveInput, taskSetPath string, inWorktree bool) (tasks.ResolveInput, error) {
	return resolveTaskSetRuntime(d, in, taskSetPath, inWorktree, false, RuntimeConfirmOptions{})
}

// ResolveTaskSetRuntimeWith applies drain checkout routing with confirmation and
// report options — the domain-contract seam behaviour tests exercise.
func ResolveTaskSetRuntimeWith(d *Deps, in tasks.ResolveInput, taskSetPath string, inWorktree bool, confirm RuntimeConfirmOptions) (tasks.ResolveInput, error) {
	return resolveTaskSetRuntime(d, in, taskSetPath, inWorktree, false, confirm)
}

func resolveTaskSetRuntime(d *Deps, in tasks.ResolveInput, taskSetPath string, inWorktree bool, forceRebind bool, confirm RuntimeConfirmOptions) (tasks.ResolveInput, error) {
	resolved, err := tasks.ResolvePathsWith(d.tasksDeps(), d.projectDeps(), d.loadConfig, in)
	if err != nil {
		return in, err
	}
	refresh, err := tasks.RefreshWith(d.tasksDeps(), resolved.DefinitionPath, tasks.StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return in, err
	}
	taskSetOverride, err := tasks.ResolveTaskSetTarget(refresh, taskSetPath)
	if err != nil {
		return in, err
	}
	if taskSetOverride != "" {
		if err := tasks.RejectArchivedTaskSet(d.tasksDeps(), tasks.StatePathFor(resolved.DefinitionPath), resolved.DefinitionPath, taskSetOverride); err != nil {
			return in, err
		}
	}
	taskSetID, _, err := tasks.SelectTaskSet(refresh, taskSetOverride)
	if err != nil {
		return in, err
	}

	started, progress := taskSetRowInfo(refresh, taskSetID)
	cfg, _ := d.loadConfig(config.DefaultConfigPath())
	td := d.tasksDeps()
	hooks := implementLifecycleHooks(td)

	key, existing, bound, err := binding.GetForSet(td, resolved.ProjectPath, taskSetID)
	if err != nil {
		return in, err
	}

	confirmOut := confirm.ConfirmOut
	if confirmOut == nil {
		confirmOut = io.Discard
	}

	// --in-worktree is the explicit opt-in to isolation: provision a managed
	// worktree forked from the current checkout's HEAD, bind it, and drain there
	// (ADR-0072). On an already-bound set it is refused without --force-rebind;
	// with it, retarget passes through the same leave-binding authorization as a
	// plain rebind (ADR-0151).
	if inWorktree {
		if bound {
			if !forceRebind {
				return in, fmt.Errorf("tasks implement: task set %s is already bound; pass --force-rebind to retarget --in-worktree", taskSetID)
			}
			confirmed, err := binding.AuthorizeLeavingBinding(td, d.projectDeps(), cfg, taskSetID, existing, key, started, progress, confirm.Yes, confirm.ConfirmIn, confirmOut, hooks)
			if err != nil {
				return in, err
			}
			if !confirmed {
				inWorktree = false
				forceRebind = false
			} else {
				if err := binding.Delete(td, key); err != nil {
					return in, err
				}
				return provisionInWorktree(d, in, resolved.ProjectPath, taskSetID)
			}
		} else {
			return provisionInWorktree(d, in, resolved.ProjectPath, taskSetID)
		}
	}

	if bound && forceRebind {
		differs, err := binding.CheckoutPathsDiffer(td, existing.RuntimePath, resolved.ProjectPath)
		if err != nil {
			return in, err
		}
		if differs {
			confirmed, err := binding.AuthorizeLeavingBinding(td, d.projectDeps(), cfg, taskSetID, existing, key, started, progress, confirm.Yes, confirm.ConfirmIn, confirmOut, hooks)
			if err != nil {
				return in, err
			}
			if !confirmed {
				forceRebind = false
			}
		} else {
			forceRebind = false
		}
	}

	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              td,
		PD:              d.projectDeps(),
		Config:          cfg,
		Now:             d.now(),
		CurrentCheckout: resolved.ProjectPath,
		SetID:           taskSetID,
		Trigger:         binding.TriggerImplementForeground,
		RuntimeOverride: in.RuntimeOverride,
		ForceRebind:     forceRebind,
	})
	if err != nil {
		if errors.Is(err, binding.ErrBoundWorktreeInvalid) {
			return in, fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", taskSetID, err)
		}
		return in, err
	}

	currentID, err := tasks.ResolveRepositoryIdentity(td, resolved.ProjectPath)
	if err != nil {
		return in, err
	}
	runtimeID, err := tasks.ResolveRepositoryIdentity(td, route.RuntimePath)
	if err != nil {
		return in, err
	}
	if currentID.CommonDir != runtimeID.CommonDir {
		return in, fmt.Errorf("task set %q belongs to a different repository than the current checkout (%s vs %s); run implement from a checkout of the set's repository",
			taskSetID, currentID.CommonDir, runtimeID.CommonDir)
	}

	// Foreground implement never reaches the Queue-only worktree directive
	// (ADR-0072), so ProvisionedManaged/AdoptedNamed are never set here. An
	// existing binding wins regardless of cwd (ADR-0151): point the executor at
	// it and, when invoked from elsewhere, report where the drain actually runs.
	// --force-rebind re-points to the current checkout (Rebound). An unbound set's
	// BoundDefault persists a binding to the current checkout — RuntimePath is that
	// same checkout the executor already resolves, so it needs no re-pointing here.
	if route.Rebound || route.UsedExistingBinding || route.ProvisionedManaged || route.AdoptedNamed || strings.TrimSpace(in.RuntimeOverride) != "" {
		in.RuntimeOverride = route.RuntimePath
	}
	if route.UsedExistingBinding && !route.Rebound {
		currentRuntime, err := tasks.ResolveRuntimePathWith(td, resolved.ProjectPath, "")
		if err != nil {
			return in, err
		}
		boundRuntime, err := tasks.ResolveRuntimePathWith(td, route.RuntimePath, "")
		if err != nil {
			return in, err
		}
		if boundRuntime != currentRuntime {
			out := confirm.Output
			if out == nil {
				out = io.Discard
			}
			fmt.Fprintf(out, "draining at bound checkout %s\n", route.RuntimePath)
		}
	}
	return in, nil
}

// provisionInWorktree implements `--in-worktree`: it forks a managed worktree
// from the current checkout's HEAD, records a provisioned Worktree binding for
// the set, and points the drain at the new checkout. Callers retargeting an
// already-bound set must authorize leaving the binding and delete the old row
// before calling this helper.
func provisionInWorktree(d *Deps, in tasks.ResolveInput, projectPath, setID string) (tasks.ResolveInput, error) {
	td := d.tasksDeps()
	cfg, _ := d.loadConfig(config.DefaultConfigPath())

	key, _, bound, err := binding.GetForSet(td, projectPath, setID)
	if err != nil {
		return in, err
	}
	if bound {
		return in, fmt.Errorf("tasks implement: task set %s is already bound", setID)
	}

	b, err := binding.ProvisionWorktree(td, binding.ManagedWorktreesRoot(td), projectPath, setID, "HEAD", d.now())
	if err != nil {
		return in, err
	}
	if id, err := tasks.ResolveRepositoryIdentity(td, projectPath); err == nil {
		b.Project = binding.DetectProject(d.projectDeps(), td, cfg, id)
	}
	if err := binding.Put(td, key, b); err != nil {
		return in, err
	}

	in.RuntimeOverride = b.RuntimePath
	return in, nil
}

// bindCheckout returns the binding hook whole-set implement passes to the
// executor. It adopts the run's current checkout into the binding model
// (ADR-0036): a worktree-locus run records a never-delete adopted binding via
// the shared module, while a trunk-locus run records nothing. Implement never
// provisions a worktree — auto-provisioning stays the Queue's path.
func bindCheckout(d *Deps) func(setID, projectPath, runtimePath string) error {
	return func(setID, projectPath, runtimePath string) error {
		cfg, _ := d.loadConfig(config.DefaultConfigPath())
		_, err := binding.AdoptCurrentCheckout(d.tasksDeps(), d.projectDeps(), cfg, projectPath, runtimePath, setID)
		return err
	}
}

// refreshManagedCheckout fast-forwards a commitless provisioned managed
// worktree onto current trunk before the drain's first task (ADR-0147).
func refreshManagedCheckout(d *Deps) func(setID, projectPath, runtimePath string) error {
	return func(setID, projectPath, runtimePath string) error {
		td := d.tasksDeps()
		cfg, _ := d.loadConfig(config.DefaultConfigPath())
		_, b, ok, err := binding.GetForSet(td, projectPath, setID)
		if err != nil || !ok {
			return err
		}
		_ = binding.RefreshCommitlessManagedBranch(td, cfg, projectPath, b)
		return nil
	}
}

func taskSetRowInfo(refresh *tasks.RefreshResult, setID string) (started bool, progress string) {
	if refresh == nil {
		return false, ""
	}
	for _, row := range refresh.Rows {
		if row.ID == setID {
			return row.Started, row.Progress
		}
	}
	return false, ""
}

func implementLifecycleHooks(td *tasks.Deps) binding.LifecycleHooks {
	return binding.LifecycleHooks{
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return tasks.ReadRuntimeLockStatus(td, runtimePath)
		},
	}
}
