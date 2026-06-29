package implement

import (
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
	// PreSeedTopic pre-seeds the pane's Topic from each task's Title at drain
	// spawn (ADR-0058); forwarded verbatim to the task-set executor.
	PreSeedTopic func(taskTitle string)
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
		PreSeedTopic:    opts.PreSeedTopic,
	})
	if err != nil {
		return result, err
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
	// worktree forked from the current checkout's HEAD, bind it, and drain there
	// (ADR-0072). It runs before routing so the subsequent route resolves the
	// fresh binding.
	if inWorktree {
		return provisionInWorktree(d, in, resolved.ProjectPath, taskSetID)
	}

	cfg, _ := d.loadConfig(config.DefaultConfigPath())
	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              d.tasksDeps(),
		PD:              d.projectDeps(),
		Config:          cfg,
		Now:             d.now(),
		CurrentCheckout: resolved.ProjectPath,
		SetID:           taskSetID,
		Trigger:         binding.TriggerImplementForeground,
		RuntimeOverride: in.RuntimeOverride,
	})
	if err != nil {
		return in, err
	}
	// Foreground implement never reaches the Queue-only worktree directive
	// (ADR-0072), so ProvisionedManaged/AdoptedNamed are never set here: an existing
	// binding resumes at its bound checkout and an explicit override resolves to that
	// checkout — both resolved checkouts the executor must be pointed at. The
	// directive is ignored; the final step persists a default binding to the current
	// checkout (ADR-0062), but its RuntimePath is that same current checkout the
	// executor already resolves, so it needs no re-pointing here.
	if route.UsedExistingBinding || route.ProvisionedManaged || route.AdoptedNamed || strings.TrimSpace(in.RuntimeOverride) != "" {
		in.RuntimeOverride = route.RuntimePath
	}
	return in, nil
}

// provisionInWorktree implements `--in-worktree`: it forks a managed worktree
// from the current checkout's HEAD, records a provisioned Worktree binding for
// the set, and points the drain at the new checkout. An already bound set is
// rejected so the operator unbinds to retarget.
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

	b, err := binding.ProvisionWorktree(td, binding.ManagedWorktreesRoot(td), projectPath, setID, d.now())
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
