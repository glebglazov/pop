package tasks

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const (
	DefaultMaxTries       = config.DefaultTaskMaxTries
	DefaultAttemptTimeout = 1 * time.Hour
)

// RunTaskOptions configures a single-task execution.
type RunTaskOptions struct {
	ResolveInput
	TaskPathOverride string
	AgentPreset      string
	AgentPresets     []string
	// AgentExplicit reports the --agent flag was explicitly passed
	// (Flags().Changed).
	AgentExplicit bool
	AgentCmd      string
	AgentOutput   AgentOutputMode
	AllowDirty    DirtyRuntimeStrategy
	MaxTries         int
	MaxTriesExplicit bool
	Timeout          time.Duration
	Yes           bool
	ConfirmIn     io.Reader
	ConfirmOut    io.Writer
	Output        io.Writer
	// BindCheckout mirrors RunTaskSetOptions.BindCheckout for the single-task
	// path: it adopts the current checkout into the binding model (ADR-0036)
	// once the run has committed to its set and runtime checkout.
	BindCheckout func(setID, projectPath, runtimePath string) error
	// PreSeedTopic, when set, pre-seeds the pane's Topic from the task Title at
	// drain spawn (ADR-0058): the caller slugifies the Title into the canonical
	// Topic format and writes @pop_topic, so the agent's `set-topic --derive`
	// hook no-ops on the first prompt — a drained pane gets an accurate Topic
	// with no model call. Invoked once, after the task is selected and the run
	// is confirmed, before the agent's first prompt.
	PreSeedTopic func(taskTitle string)
}

// RunTaskResult is the outcome of a successful or declined run-task.
type RunTaskResult struct {
	Selection   *Selection
	Refresh     *RefreshResult
	Declined    bool
	NoOp        bool
	QuotaPaused bool
	PauseReason string
	// PausePreset names the agent preset whose quota ran out, when QuotaPaused.
	PausePreset  string
	PauseResetAt time.Time
	CommitSHA    string
	AgentSummary string
}

// RunTask executes one task task through an agent.
func RunTask(opts RunTaskOptions) (*RunTaskResult, error) {
	return RunTaskWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunTaskWith executes one task using injected dependencies.
func RunTaskWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskOptions) (result *RunTaskResult, err error) {
	if d.Runner == nil {
		d.Runner = RealCommandRunner{}
	}
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
	cfg := plan.cfg
	baseAgentPresets := plan.baseAgentPresets
	baseAgentPreset := plan.baseAgentPreset
	agentOutput := plan.agentOutput
	strategy := plan.strategy
	commitOverrides := plan.commitOverrides
	agentQuotaRetryAfter := plan.agentQuotaRetryAfter

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

	taskSetID, taskID, err := ResolveTaskTarget(refresh, opts.TaskPathOverride)
	if err != nil {
		return nil, err
	}
	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetID); err != nil {
		return nil, err
	}

	sel, err := SelectTask(refresh, taskSetID, taskID)
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

	// A ready HITL target routes to that task's HITL gate instead of an agent
	// run: it never claims a Drain, runs no agent, and asks no "Run task?"
	// confirmation, so this branch precedes all execution front matter. The gate
	// itself is the whole-set drain's HITL menu, reused verbatim.
	if sel.HITLGate {
		timeout := resolveAttemptTimeout(opts.Timeout)
		return runTargetedHITLGate(d, targetedHITLGateOptions{
			out:            out,
			in:             opts.ConfirmIn,
			yes:            opts.Yes,
			agentPreset:    opts.AgentPreset,
			agentCmd:       opts.AgentCmd,
			cwd:            opts.CWD,
			runtimePath:    runtimePath,
			definitionPath: resolved.DefinitionPath,
			statePath:      statePath,
			cfg:            cfg,
			timeout:        timeout,
			refresh:        refresh,
			sel:            sel,
		})
	}

	// Lock-free front matter (ADR-0067): the set render, the dirty-runtime
	// report, and the pre-run confirmation all run before any Drain is claimed,
	// so the Runtime execution lock is not held while a human sits at the prompt.
	// A declined confirmation returns here having claimed no Drain and held no
	// lock.
	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}

	displayRows := cloneRows(refresh.Rows)
	MarkNextPick(displayRows)
	MarkRunTarget(displayRows, sel.TaskSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	if dirty {
		if err := reportDirtyRuntime(d, confirmOut, runtimePath, strategy); err != nil {
			return nil, exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, taskConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunTaskResult{Selection: sel, Refresh: refresh, Declined: true}, nil
	}

	// Start the Drain only now, around the actual attempt execution (ADR-0067):
	// insert a running row keyed by (repository, set) and enforce mutual
	// exclusion transactionally (ADR-0055), replacing the runtime execution lock
	// file and the cross-checkout backstop. The single-task path claims a Drain
	// too so it interlocks with whole-set drains of the same set. The running
	// Drain row exists only while an attempt is actively executing, matching the
	// whole-set lifecycle.
	drain, err := BeginDrain(d, runtimePath, sel.TaskSetID, confirmOut)
	if err != nil {
		return nil, err
	}
	defer func() {
		var (
			declined    bool
			quotaPaused bool
			preset      string
			resetAt     time.Time
		)
		if result != nil {
			declined = result.Declined
			quotaPaused = result.QuotaPaused
			preset = result.PausePreset
			resetAt = result.PauseResetAt
		}
		finalizeDrain(drain, declined, quotaPaused, false, preset, false, resetAt, err)
	}()

	// Adopt this checkout into the binding model (ADR-0036): worktree-locus runs
	// record a never-delete adopted binding; trunk-locus runs record nothing.
	// This runs before the attempt on every non-declined path, regardless of the
	// drain timing.
	if opts.BindCheckout != nil {
		if err := opts.BindCheckout(sel.TaskSetID, resolved.ProjectPath, runtimePath); err != nil {
			return nil, exitErr(ExitOperational, "bind checkout: %v", err)
		}
	}

	if dirty {
		if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, commitOverrides, confirmOut); err != nil {
			return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
		}
	}

	// Pre-seed the pane's Topic from the task Title before the agent's first
	// prompt (ADR-0058), so the derive hook no-ops and the drained pane carries
	// an accurate Topic with no model call.
	if opts.PreSeedTopic != nil {
		opts.PreSeedTopic(sel.Task.Title)
	}

	basePrompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
	buildForAgent := buildAgentInvocationFactory(loadConfig, runtimePath, baseAgentPreset, opts.AgentCmd, agentOutput, opts.AgentOutput)

	maxTries, err := plan.maxTries(opts.MaxTriesExplicit, opts.MaxTries)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	retryDelays, err := plan.retryDelays()
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	timeout := resolveAttemptTimeout(opts.Timeout)

	agentSpecs := resolveTaskAgentSpecs(baseAgentPresets, opts.AgentCmd, sel.Task.Effort, sel.Task.EffortExplicit, cfg)
	result, execErr := executeTaskAttemptsWithAgentFallback(d, sel, runtimePath, out, confirmOut, basePrompt, agentSpecs, buildForAgent, maxTries, timeout, commitOverrides, agentQuotaRetryAfter, retryDelays)
	if execErr != nil {
		afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
		if refreshErr == nil && !opts.Yes {
			fmt.Fprintln(out)
			Render(out, afterRefresh)
		}
		return result, execErr
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, taskExitErr(sel, ExitOperational, "refresh after completion: %v", err)
	}
	result.Refresh = afterRefresh

	if !opts.Yes {
		fmt.Fprintln(out)
		Render(out, afterRefresh)
	}

	return result, nil
}

func cloneRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	return out
}

const taskConfirmPrompt = "Run task? [y/N]: "

// NonInteractiveReader marks explicit non-interactive confirmation input (for tests and automation).
type NonInteractiveReader struct{}

func (NonInteractiveReader) Read([]byte) (int, error) { return 0, io.EOF }

func confirmExecution(in io.Reader, out io.Writer, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if _, ok := in.(NonInteractiveReader); ok {
		return false, exitErr(ExitOperational, "non-interactive execution requires --yes or -y")
	}
	if in == nil {
		in = os.Stdin
	}
	interactive := in != os.Stdin || isInteractive(in)
	if !interactive {
		return false, exitErr(ExitOperational, "non-interactive execution requires --yes or -y")
	}
	if prompt == "" {
		prompt = taskConfirmPrompt
	}
	display := outputFor(out)
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, prompt))
	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil && err != io.EOF {
		return false, exitErr(ExitOperational, "read confirmation: %v", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
