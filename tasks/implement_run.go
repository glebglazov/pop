package tasks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
)

// implementRun models one Implement run: a single invocation of a whole-set
// Implement (`pop tasks implement` / a `queue run` drain of one set). It holds at
// most one live Drain at a time — nil while parked at a human-wait gate or a
// quota-recovery wait — and may span several across those parks (ADR-0067). The
// name is deliberately neither "session" (a tmux term) nor "drain" (the store-row
// term): the run is the whole-set invocation, the Drain is the per-segment
// runtime execution lock it re-acquires.
//
// It carries the threaded state RunTaskSetWith used to hold as inline locals: the
// injected deps, the resolved run plan (shared config bundle from newRunPlan), the
// per-entry-point resolved paths/target, and the mutable pieces the drain loop and
// the deferred finalize share — the live Drain handle, the reused gate prompt
// reader, and the accumulating result.
type implementRun struct {
	d          *Deps
	loadConfig func(string) (*config.Config, error)
	opts       RunTaskSetOptions
	plan       *runPlan

	resolved     *ResolvedPaths
	runtimePath  string
	statePath    string
	taskSetID    string
	hitlFallback bool

	confirmOut io.Writer
	out        io.Writer

	// refresh is the opening Refresh snapshot, used only by setup for the initial
	// render and the start-at-gate checks; the loop re-Refreshes each iteration.
	refresh *RefreshResult

	dirty bool

	maxTries    int
	retryDelays []time.Duration
	timeout     time.Duration

	// drain is the live Drain handle — nil while parked at a gate or a
	// quota-recovery wait. parkDrain/ensureDrain mutate it and the deferred
	// finalize reads its latest value.
	drain *DrainHandle
	// sharedPromptReader is the single gate prompt reader reused across every gate
	// in one run (see ensurePromptReader). Seeded here when the run starts at a
	// gate; the loop continues to (re)seed it.
	sharedPromptReader *bufio.Reader
	result             *RunTaskSetResult
}

// newImplementRun resolves everything RunTaskSetWith needs before it can drain:
// the shared run plan, the per-entry-point paths/refresh/target, and the opening
// BeginDrain. It stops at the BeginDrain so the caller can register the deferred
// finalize at exactly the point the inline version did — immediately after the
// Drain exists, before BindCheckout — leaving the remaining setup to setup(),
// which then runs under that deferred. Failing before or at BeginDrain leaves no
// live Drain, so the caller returns the error directly with nothing to finalize.
func newImplementRun(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskSetOptions) (*implementRun, error) {
	plan, err := newRunPlan(loadConfig, runPlanInput{
		agentPresets:  opts.AgentPresets,
		agentPreset:   opts.AgentPreset,
		agentExplicit: opts.AgentExplicit,
		agentCmd:      opts.AgentCmd,
		agentOutput:   opts.AgentOutput,
		allowDirty:    opts.AllowDirty,
	})
	if err != nil {
		return nil, err
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	runtimePath, err := ResolveRuntimePathWith(d, resolved.ProjectPath, opts.RuntimeOverride)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetOverride, err := ResolveTaskSetTarget(refresh, opts.TaskSetOverride)
	if err != nil {
		return nil, err
	}
	if taskSetOverride != "" {
		if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetOverride); err != nil {
			return nil, err
		}
	}

	taskSetID, hitlFallback, err := SelectTaskSet(refresh, taskSetOverride)
	if err != nil {
		return nil, err
	}

	confirmOut := opts.ConfirmOut
	if confirmOut == nil {
		confirmOut = os.Stderr
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	// Start the Drain: insert a running row keyed by (repository, set) and enforce
	// mutual exclusion transactionally (ADR-0055), replacing the runtime execution
	// lock file and the cross-checkout backstop. The Runtime execution lock is now
	// held only during active execution (ADR-0067): this opening BeginDrain is the
	// backstop before BindCheckout, but reaching any gate menu parks (finishes) it
	// so the menu runs lock-free, and resuming AFK work re-acquires a fresh Drain.
	drain, err := BeginDrain(d, runtimePath, taskSetID, confirmOut)
	if err != nil {
		return nil, err
	}

	return &implementRun{
		d:            d,
		loadConfig:   loadConfig,
		opts:         opts,
		plan:         plan,
		resolved:     resolved,
		runtimePath:  runtimePath,
		statePath:    statePath,
		taskSetID:    taskSetID,
		hitlFallback: hitlFallback,
		confirmOut:   confirmOut,
		out:          out,
		refresh:      refresh,
		drain:        drain,
	}, nil
}

// finalize records the appropriate exit-reason terminal for the run's latest
// Drain (nil while parked ⇒ a no-op, since the park already recorded the
// segment's terminal), or cancels it when the run was declined and never executed
// (ADR-0056). It reads the result's disposition flags and the final error, so
// RunTaskSetWith defers it with &err. Registered before BindCheckout so a bind or
// later-setup failure still finalizes the row.
func (r *implementRun) finalize(errp *error) {
	var (
		declined     bool
		quotaPaused  bool
		verifyFailed bool
		preset       string
		pinned       bool
		resetAt      time.Time
	)
	if r.result != nil {
		declined = r.result.Declined
		quotaPaused = r.result.QuotaPaused
		verifyFailed = r.result.TaskSetVerifyFailed
		preset = r.result.PausePreset
		pinned = r.result.PausePinnedAgent
		resetAt = r.result.PauseResetAt
	}
	var err error
	if errp != nil {
		err = *errp
	}
	finalizeDrain(r.drain, declined, quotaPaused, verifyFailed, preset, pinned, resetAt, err)
}

// setup runs the remaining preparation after the opening BeginDrain: checkout
// binding (ADR-0036), the dirty-runtime check, the initial render, and the lazy
// max-tries / retry-delay / attempt-timeout resolution. It initializes the result
// as its last step, so any failure returns before result exists (leaving the
// deferred finalize to read a nil result ⇒ a plain finished terminal). It runs
// under RunTaskSetWith's deferred finalize, so a failure here still finalizes the
// live Drain.
func (r *implementRun) setup() error {
	d := r.d
	opts := r.opts

	// Adopt this checkout into the binding model before draining (ADR-0036): a
	// worktree-locus run records a never-delete adopted binding so the set is
	// integrateable even if the drain later fails; a trunk-locus run records
	// nothing. Done before the first task runs so a failed run is not
	// re-provisioned and orphaned by a later `queue run`.
	if opts.BindCheckout != nil {
		if err := opts.BindCheckout(r.taskSetID, r.resolved.ProjectPath, r.runtimePath); err != nil {
			return exitErr(ExitOperational, "bind checkout: %v", err)
		}
	}

	dirty, err := runtimeIsDirty(d, r.runtimePath)
	if err != nil {
		return exitErr(ExitSetup, "runtime git status: %v", err)
	}
	r.dirty = dirty

	displayRows := cloneRows(r.refresh.Rows)
	MarkNextPick(displayRows)
	MarkRunTarget(displayRows, r.taskSetID)
	displayRefresh := *r.refresh
	displayRefresh.Rows = displayRows

	if r.hitlFallback {
		outputFor(r.out).line(ansiYellow, "No runnable AFK work")
	}

	fmt.Fprintln(r.out)
	Render(r.out, &displayRefresh)

	if m := displayRefresh.Manifests[r.taskSetID]; m != nil {
		RenderTaskList(r.out, r.taskSetID, m)
	}

	if dirty {
		if err := reportDirtyRuntime(d, r.confirmOut, r.runtimePath, r.plan.strategy); err != nil {
			return exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	initialGate := selectedTaskSetStartsAtHITLGate(r.refresh, r.taskSetID) ||
		selectedTaskSetStartsAtFailedGate(r.refresh, r.taskSetID)
	if initialGate {
		r.sharedPromptReader = ensurePromptReader(r.sharedPromptReader, opts.ConfirmIn, opts.Yes)
	}

	maxTries, err := r.plan.maxTries(opts.MaxTriesExplicit, opts.MaxTries)
	if err != nil {
		return exitErr(ExitSetup, "%v", err)
	}
	retryDelays, err := r.plan.retryDelays()
	if err != nil {
		return exitErr(ExitSetup, "%v", err)
	}
	r.maxTries = maxTries
	r.retryDelays = retryDelays
	r.timeout = resolveAttemptTimeout(opts.Timeout)

	r.result = &RunTaskSetResult{TaskSetID: r.taskSetID, RuntimePath: r.runtimePath, ProjectPath: r.resolved.ProjectPath}
	return nil
}

// parkDrain releases the Runtime execution lock at a human-wait gate: it finishes
// the held Drain with the same clean terminal the --yes path records at that point
// (ADR-0056/0067) — the set's blocked/awaiting_approval/failed disposition stays
// manifest-derived — and drops the live lock so the gate menu, assist session, and
// runtime shell all run lock-free. A no-op when no Drain is held (already parked).
func (r *implementRun) parkDrain() {
	if r.drain == nil {
		return
	}
	_ = r.drain.Finish(store.StateFinished, "", false, time.Time{})
	r.drain = nil
}

// parkAtGate parks the drain and registers a checkout gate hold so quota recovery
// waiters on the same runtime path cannot resume until the gate session ends
// (ADR-0100). Only when the gate will actually prompt, so a non-prompting
// fall-through keeps the normal terminal.
func (r *implementRun) parkAtGate(m *Manifest, gateTask *Task) {
	if !gateWillPrompt(r.opts.ConfirmIn, r.opts.Yes, m, gateTask) {
		return
	}
	r.parkDrain()
	_ = RegisterCheckoutGateHold(r.d, r.taskSetID, r.runtimePath)
}

func (r *implementRun) releaseGateHold() {
	_ = ReleaseCheckoutGateHold(r.d, r.runtimePath)
}

// newGateEnv builds the shared gate context the three interactive menus run
// against from the run's threaded state. It reads r.sharedPromptReader at call
// time, so a caller that (re)seeds the reader before invoking a gate must do so
// first (the Failed gate does). The handlers are free functions over gateEnv,
// not methods on implementRun, so the targeted single-task HITL path can build
// its own env and share them (decision 6).
func (r *implementRun) newGateEnv() gateEnv {
	return gateEnv{
		d:              r.d,
		out:            r.out,
		in:             r.opts.ConfirmIn,
		reader:         r.sharedPromptReader,
		yes:            r.opts.Yes,
		agentPreset:    r.opts.AgentPreset,
		agentCmd:       r.opts.AgentCmd,
		cwd:            r.opts.CWD,
		runtimePath:    r.runtimePath,
		definitionPath: r.resolved.DefinitionPath,
		statePath:      r.statePath,
		taskSetID:      r.taskSetID,
	}
}

// ensureDrain re-acquires the Runtime execution lock before a contiguous run of
// AFK attempts resumes after a gate park (ADR-0067). It is a no-op while a Drain
// is already held (the opening BeginDrain, or an unparked segment). A collision
// with a concurrent drain on the same checkout refuses cleanly with the existing
// "already in progress" error; the gate decision was already persisted to the
// manifest, so nothing is lost.
func (r *implementRun) ensureDrain() error {
	if r.drain != nil {
		return nil
	}
	handle, err := BeginDrain(r.d, r.runtimePath, r.taskSetID, r.confirmOut)
	if err != nil {
		return err
	}
	r.drain = handle
	return nil
}
