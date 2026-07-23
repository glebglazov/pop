package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// RunTaskSetOptions configures sequential Task-set draining.
type RunTaskSetOptions struct {
	ResolveInput
	TaskSetOverride string
	AgentPreset     string
	AgentPresets    []string
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
	// BindCheckout, when set, is invoked once the drain has committed to its
	// Task set and runtime checkout (after the running Drain is started, before
	// any task runs). It lets the caller record the
	// set↔checkout association in the shared binding store — `pop tasks
	// implement` adopts its current checkout this way (ADR-0036). It receives the
	// resolved set id, project path, and runtime checkout path; a non-nil error
	// aborts the drain.
	BindCheckout func(setID, projectPath, runtimePath string) error
	// PreSeedTopic, when set, pre-seeds the pane's Topic from each task's Title
	// at drain spawn (ADR-0058). It is invoked once per task before that task's
	// first agent prompt, but guards on the existing @pop_topic, so the first
	// task in the set wins and the agent's `set-topic --derive` hook no-ops for
	// the whole drain — the pane carries an accurate Topic with no model call.
	PreSeedTopic func(taskTitle string)
	// VerifyAgents and VerifyEffort steer the in-drain pre-approval Verifier
	// independently of the implementing agents (`--verify-agent`, repeatable, and
	// `--verify-effort`): they are the highest-precedence Verifier overrides.
	// Empty ⇒ resolution falls through to the per-set override, then
	// [tasks.verify], then [tasks.implement].agents / DefaultVerifyEffort.
	VerifyAgents []string
	VerifyEffort string
	// verifyRunner overrides the pre-approval Verifier's agent spawn, mirroring
	// verifyCoreOptions.runVerifier. Unexported and test-only: production always
	// resolves the real Verifier agent. Nil ⇒ the configured Verifier runs.
	verifyRunner func(prompt string) (string, error)
}

// RunTaskSetResult is the outcome of a run-tasks invocation.
type RunTaskSetResult struct {
	TaskSetID               string
	Completed               []*RunTaskResult
	Refresh                 *RefreshResult
	Declined                bool
	TaskSetDone             bool
	TaskSetDeferred         bool
	TaskSetAwaitingApproval bool
	// TaskSetVerifyFailed reports the pre-approval Verifier could not clear the
	// set at its current work SHA (ADR-0086): a FIXABLE or NEEDS-HUMAN verdict
	// parked the drain. VerifyFindings carries the Verifier's human-facing reasons.
	TaskSetVerifyFailed bool
	VerifyFindings      string
	// VerifyRerunCmd is a copy-pasteable `pop tasks verify …` when verification failed.
	VerifyRerunCmd string
	SkippedTasks        []string
	BlockedReason       string
	QuotaPaused         bool
	PauseReason         string
	// PausePreset names the agent preset whose quota ran out, when QuotaPaused.
	PausePreset      string
	PauseResetAt     time.Time
	PausePinnedAgent bool
	// RuntimePath and ProjectPath are set once the drain has committed to its
	// runtime checkout, making them available to the caller even on Done.
	RuntimePath string
	ProjectPath string
}

// RunTaskSet drains one Task set sequentially through eligible AFK tasks.
func RunTaskSet(opts RunTaskSetOptions) (*RunTaskSetResult, error) {
	return RunTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunTaskSetWith drains one Task set using injected dependencies.
func RunTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskSetOptions) (result *RunTaskSetResult, err error) {
	if d.Runner == nil {
		d.Runner = RealCommandRunner{}
	}
	run, err := newImplementRun(d, pd, loadConfig, opts)
	if err != nil {
		return nil, err
	}
	// This process owns the running Drain: record its exit-reason terminal on
	// every exit path below — a bubbled setup error, a normal terminal, or the
	// loop returning — reading the run's *latest* Drain handle (nil while parked
	// at a gate). Registered immediately after the opening BeginDrain, before
	// BindCheckout, so a later-setup failure still finalizes the row. A declined
	// run never executed, so its Drain is cancelled rather than terminated
	// (ADR-0056); exiting parked (drain == nil) is a no-op — the park already
	// recorded the segment's terminal.
	defer run.finalize(&err)
	if err = run.setup(); err != nil {
		return run.result, err
	}
	return run.loop()
}

// loop drains the resolved Task set sequentially through eligible AFK tasks. It
// runs after setup and reads as pure orchestration: each iteration re-Refreshes,
// then dispatches to one of three methods — the pre-approval Verifier phase, the
// terminal-status switch, or the task-execution branch — each of which owns its
// choreography and hands back a continue/return directive. The methods mutate the
// run's Drain / prompt-reader / result state through the receiver so the deferred
// finalize sees the latest values.
func (r *implementRun) loop() (*RunTaskSetResult, error) {
	d := r.d
	resolved := r.resolved
	statePath := r.statePath
	taskSetID := r.taskSetID
	result := r.result

	for {
		currentRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
		if err != nil {
			return nil, exitErr(ExitOperational, "refresh before task selection: %v", err)
		}

		sel, selErr := SelectTaskInSet(currentRefresh, taskSetID)
		if selErr != nil {
			row := findRow(currentRefresh, taskSetID)
			if row == nil {
				return nil, selErr
			}
			result.Refresh = currentRefresh

			// Pre-approval Verifier phase (ADR-0086): when the set has exhausted its
			// AFK work at DONE / AWAITING-APPROVAL, clear a SHA-gated Verify verdict
			// before the terminal status stands. verifyPhase owns the whole choreography
			// (quota-pause park/wait, FIXABLE remediation spawn, the verify-fail gate)
			// and returns a directive: keep draining, return the run's result, or fall
			// through to the terminal-status switch.
			switch directive, verifyErr := r.verifyPhase(currentRefresh, row); directive {
			case verifyContinue:
				continue
			case verifyReturn:
				return result, verifyErr
			}

			// Terminal-status dispatch (DONE / DEFERRED / BLOCKED /
			// AWAITING-APPROVAL / FAILED, incl. the gate choreography): the method
			// owns the whole switch and hands back a directive — keep draining a
			// handled gate, or return the exact (result, err) it produced.
			directive, res, tErr := r.terminalStatus(currentRefresh, row, selErr)
			if directive == terminalContinue {
				continue
			}
			return res, tErr
		}

		// An eligible AFK task was selected: the task-execution branch re-acquires
		// the Drain, applies the one-time dirty strategy, runs the attempts, and
		// owns the post-failure Failed gate and the quota-pause park-and-wait. It
		// hands back a directive — keep draining, or return the exact (result, err).
		directive, res, runErr := r.runSelectedTask(currentRefresh, sel)
		if directive == runTaskContinue {
			continue
		}
		return res, runErr
	}
}

func finishRunTaskSet(out io.Writer, yes bool, result *RunTaskSetResult) {
	if yes {
		printTaskSetSummary(out, result)
		return
	}
	fmt.Fprintln(out)
	Render(out, result.Refresh)
	if result.TaskSetDeferred {
		fmt.Fprintln(out, deferralMessage(result))
	}
}

func selectedTaskSetStartsAtHITLGate(refresh *RefreshResult, taskSetID string) bool {
	row := findRow(refresh, taskSetID)
	return row != nil && (row.Status == StatusBlocked || row.Status == StatusAwaitingApproval) && BlockingHITLTask(refresh.Manifests[taskSetID]) != nil
}

// selectedTaskSetStartsAtFailedGate reports whether draining re-enters an
// already-Failed set, so the run goes straight to the Failed gate instead of
// asking for AFK consent first (the set has no immediately runnable task).
func selectedTaskSetStartsAtFailedGate(refresh *RefreshResult, taskSetID string) bool {
	row := findRow(refresh, taskSetID)
	return row != nil && row.Status == StatusFailed
}

func deferralMessage(result *RunTaskSetResult) string {
	if len(result.SkippedTasks) > 0 {
		return fmt.Sprintf("Task set %s deferred: skipped %s", result.TaskSetID, strings.Join(result.SkippedTasks, ", "))
	}
	return fmt.Sprintf("Task set %s deferred", result.TaskSetID)
}

func printTaskSetSummary(w io.Writer, result *RunTaskSetResult) {
	out := outputFor(w)
	if result.TaskSetDone {
		out.line(ansiGreen, "✓ Completed task set %s (%d task(s))", result.TaskSetID, len(result.Completed))
		return
	}
	if result.TaskSetDeferred {
		out.line(ansiYellow, "%s", deferralMessage(result))
		return
	}
	if result.TaskSetVerifyFailed {
		out.line(ansiRed, "Verification failed: task set %s needs a human review", result.TaskSetID)
		if reason := firstFindingsLine(result.VerifyFindings); reason != "" {
			out.line(ansiDim, "  %s", reason)
		}
		if result.VerifyRerunCmd != "" {
			out.line(ansiDim, "  Re-run: %s", result.VerifyRerunCmd)
		}
		return
	}
	if result.TaskSetAwaitingApproval {
		out.line(ansiCyan, "Agents done — awaiting approval: task set %s is ready for sign-off: %s", result.TaskSetID, result.BlockedReason)
		return
	}
	if result.BlockedReason != "" {
		out.line(ansiYellow, "Task set %s blocked: %s", result.TaskSetID, result.BlockedReason)
		return
	}
	if result.QuotaPaused {
		out.line(ansiYellow, "Task set %s paused after %d completed task(s): agent quota exhausted", result.TaskSetID, len(result.Completed))
		return
	}
	fmt.Fprintf(out, "Task set %s stopped after %d task(s)\n", result.TaskSetID, len(result.Completed))
}

// targetedHITLGateOptions carries the context runTargetedHITLGate needs to open
// one explicitly targeted HITL task's gate. It is populated by the single-task
// executor when `pop tasks implement <set>/<hitl>.md` names a ready HITL task.
type targetedHITLGateOptions struct {
	out            io.Writer
	in             io.Reader
	yes            bool
	agentPreset    string
	agentCmd       string
	cwd            string
	runtimePath    string
	definitionPath string
	statePath      string
	cfg            *config.Config
	timeout        time.Duration
	refresh        *RefreshResult
	sel            *Selection
}

// runTargetedHITLGate opens the HITL gate for one explicitly targeted HITL task
// (ADR-0102): it renders the set, then runs the same interactive HITL menu the
// whole-set drain reaches via BlockingHITLTask — except the gate targets the
// named task, so a set holding several attendable HITL gates can be disposed of
// one at a time rather than only the single blocking one the scheduler auto-picks.
// It runs no agent and claims no Drain; the human's choice (complete / defer /
// assist / re-verify) mutates task state directly. A handled choice returns a
// clean result; an exit or a non-promptable run (--yes / non-interactive) falls
// back to the static gate advice and a no-runnable exit, mirroring the drain.
func runTargetedHITLGate(d *Deps, opts targetedHITLGateOptions) (*RunTaskResult, error) {
	taskSetID := opts.sel.TaskSetID
	m := opts.refresh.Manifests[taskSetID]
	// Re-resolve the target against the refreshed manifest so gate mutations
	// operate on a live *Task, not the selection snapshot.
	hitl := findTaskInManifest(m, opts.sel.TaskID)
	if m == nil || hitl == nil {
		return nil, exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, opts.sel.TaskID))
	}

	out := opts.out
	result := &RunTaskResult{Selection: opts.sel, Refresh: opts.refresh}

	displayRows := cloneRows(opts.refresh.Rows)
	MarkRunTarget(displayRows, taskSetID)
	displayRefresh := *opts.refresh
	displayRefresh.Rows = displayRows
	fmt.Fprintln(out)
	Render(out, &displayRefresh)
	RenderTaskList(out, taskSetID, m)

	// Register a checkout gate hold only while the menu will actually prompt, so a
	// concurrent quota-recovery drain on the same checkout cannot resume mid-gate
	// (ADR-0100); a non-promptable run leaves no hold and falls straight to advice.
	willPrompt := gateWillPrompt(opts.in, opts.yes, m, hitl)
	if willPrompt {
		_ = RegisterCheckoutGateHold(d, taskSetID, opts.runtimePath, false)
	}
	rv := &reverifyGateContext{cfg: opts.cfg, timeout: opts.timeout}
	// The targeted single-task HITL path reuses the whole-set drain's HITL menu,
	// so it builds its own gateEnv rather than a run — no throwaway implementRun
	// (decision 6). No shared prompt reader: this is a one-shot gate.
	env := gateEnv{
		d:              d,
		out:            out,
		in:             opts.in,
		yes:            opts.yes,
		agentPreset:    opts.agentPreset,
		agentCmd:       opts.agentCmd,
		cwd:            opts.cwd,
		runtimePath:    opts.runtimePath,
		definitionPath: opts.definitionPath,
		statePath:      opts.statePath,
		taskSetID:      taskSetID,
	}
	handled, err := handleInteractiveHITLGate(env, m, hitl, rv)
	if willPrompt {
		_ = ReleaseCheckoutGateHold(d, taskSetID, opts.runtimePath)
	}
	if err != nil {
		return nil, err
	}
	if handled {
		if afterRefresh, refreshErr := RefreshWith(d, opts.definitionPath, opts.statePath); refreshErr == nil {
			result.Refresh = afterRefresh
			if !opts.yes {
				fmt.Fprintln(out)
				Render(out, afterRefresh)
			}
		}
		return result, nil
	}

	// Not handled: the human exited or the run cannot prompt. Print the same
	// static advice the drain shows and exit no-runnable so callers see the set
	// is still Human-blocked.
	if row := findRow(opts.refresh, taskSetID); row != nil && row.Status == StatusAwaitingApproval {
		printTerminalHITLAdvice(d, out, taskSetID, m.Dir, hitl)
		return result, exitErr(ExitNoRunnable, "Task set %q agents done — awaiting approval: HITL: %s", taskSetID, hitl.ID)
	}
	printHITLGateAdvice(d, out, taskSetID, m.Dir, hitl)
	return result, exitErr(ExitNoRunnable, "Task set %q is Human-blocked: HITL: %s", taskSetID, hitl.ID)
}

// findTaskInManifest returns a pointer to the task with the given ID in the
// manifest, or nil when none matches.
func findTaskInManifest(m *Manifest, taskID string) *Task {
	if m == nil {
		return nil
	}
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			return &m.Tasks[i]
		}
	}
	return nil
}
