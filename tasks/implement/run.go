package implement

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks"
)

// WholeSetOptions configures whole-set Implement orchestration: drain routing,
// task-set drain, and integration epilogue.
type WholeSetOptions struct {
	ResolveInput    tasks.ResolveInput
	TaskSetOverride string
	InWorktree      bool
	AgentPreset     string
	AgentPresets    []string
	AgentExplicit   bool
	AgentCmd        string
	AgentOutput     tasks.AgentOutputMode
	AllowDirty      tasks.DirtyRuntimeStrategy
	MaxTries        int
	Timeout         time.Duration
	Yes             bool
	ConfirmIn       io.Reader
	ConfirmOut      io.Writer
	Output          io.Writer
}

// RunWholeSet orchestrates whole-set Implement: route → drain → epilogue.
func RunWholeSet(opts WholeSetOptions) (*tasks.RunTaskSetResult, error) {
	return RunWholeSetWith(DefaultDeps(), opts)
}

// RunWholeSetWith drains one task set using injected dependencies.
func RunWholeSetWith(d *Deps, opts WholeSetOptions) (*tasks.RunTaskSetResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	resolveInput, err := resolveTaskSetRuntime(d, opts.ResolveInput, opts.TaskSetOverride, opts.InWorktree)
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
		MaxTries:        opts.MaxTries,
		Timeout:         opts.Timeout,
		Yes:             opts.Yes,
		ConfirmIn:       opts.ConfirmIn,
		ConfirmOut:      opts.ConfirmOut,
		Output:          opts.Output,
		BindCheckout:    bindCheckout(d),
	})
	if err != nil {
		return result, err
	}
	if result != nil && result.TaskSetDone {
		recordMergeabilityOnDone(d, result, opts.ConfirmOut)
		OfferIntegration(d, result, opts)
	}
	if result != nil && result.QuotaPaused {
		return result, &tasks.ExitError{Code: tasks.ExitQuotaPaused}
	}
	return result, nil
}

// ResolveTaskSetRuntime applies drain checkout routing for tests and returns
// the ResolveInput the executor should use.
func ResolveTaskSetRuntime(d *Deps, in tasks.ResolveInput, taskSetPath string, inWorktree bool) (tasks.ResolveInput, error) {
	return resolveTaskSetRuntime(d, in, taskSetPath, inWorktree)
}

func resolveTaskSetRuntime(d *Deps, in tasks.ResolveInput, taskSetPath string, inWorktree bool) (tasks.ResolveInput, error) {
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

	// --in-worktree is the explicit opt-in to isolation: provision a managed
	// worktree forked from the trunk, bind it, and drain there (ADR-0052). It
	// runs before routing so the subsequent route resolves the fresh binding.
	if inWorktree {
		return provisionInWorktree(d, in, resolved.ProjectPath, taskSetID)
	}

	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              d.tasksDeps(),
		CurrentCheckout: resolved.ProjectPath,
		SetID:           taskSetID,
		Trigger:         binding.TriggerImplementForeground,
		RuntimeOverride: in.RuntimeOverride,
	})
	if err != nil {
		return in, err
	}
	// An existing binding resumes at its bound checkout; an explicit override
	// resolves to that checkout. Otherwise the drain stays in the current
	// checkout, which the executor already resolves, so leave the override empty.
	if route.UsedExistingBinding || strings.TrimSpace(in.RuntimeOverride) != "" {
		in.RuntimeOverride = route.RuntimePath
	}
	return in, nil
}

// provisionInWorktree implements `--in-worktree`: it forks a managed worktree
// from the Trunk worktree's HEAD, records a provisioned Worktree binding for the
// set, and points the drain at the new checkout. A binding wins — an already
// bound set is rejected so the operator unbinds to retarget — and a repo with no
// resolvable trunk refuses, since there is nothing to fork from.
func provisionInWorktree(d *Deps, in tasks.ResolveInput, projectPath, setID string) (tasks.ResolveInput, error) {
	td := d.tasksDeps()
	cfg, _ := d.loadConfig(config.DefaultConfigPath())

	key, _, bound, err := binding.GetForSet(td, projectPath, setID)
	if err != nil {
		return in, err
	}
	if bound {
		return in, fmt.Errorf("tasks implement: task set %s is already bound; run `pop tasks unbind-worktree %s` to retarget --in-worktree", setID, setID)
	}

	trunkPath, bare, err := binding.ResolveTrunkPath(td, cfg, projectPath)
	if err != nil {
		return in, err
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return in, fmt.Errorf("tasks implement: --in-worktree: no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
	}

	b, err := binding.ProvisionWorktree(td, binding.ManagedWorktreesRoot(td), trunkPath, setID, d.now())
	if err != nil {
		return in, err
	}
	if id, err := tasks.ResolveRepositoryIdentity(td, trunkPath); err == nil {
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
