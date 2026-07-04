package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	MaxTries      int
	Timeout       time.Duration
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
	cfg, err := loadConfigIfPresent(loadConfig)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	baseAgentPresets := ResolveDefaultAgentPresets(opts.AgentPresets, opts.AgentPreset, opts.AgentExplicit, cfg)
	baseAgentPreset := baseAgentPresets[0]
	agentOutput := AgentOutputAuto
	if opts.AgentCmd == "" {
		var err error
		agentOutput, err = resolveAgentOutputMode(loadConfig, baseAgentPreset, opts.AgentOutput)
		if err != nil {
			return nil, exitErr(ExitSetup, "%v", err)
		}
	}
	if err := validateDirtyRuntimeStrategy(opts.AllowDirty); err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	strategy := resolveDirtyRuntimeStrategy(opts.AllowDirty)

	// Resolve commit-config overrides up front (the lazy validation point) so a
	// malformed [tasks.git] entry fails the drain hard before any commit —
	// including the per-task dirty-runtime checkpoint, which commits earliest.
	commitOverrides, err := resolveCommitConfigOverrides(loadConfig)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	agentQuotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	if _, err := cfg.EffortFor(baseAgentPreset); err != nil {
		return nil, exitErr(ExitSetup, "config: %v", err)
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

	// Start the Drain: insert a running row keyed by (repository, set) and
	// enforce mutual exclusion transactionally (ADR-0055), replacing the runtime
	// execution lock file and the cross-checkout backstop. The Runtime execution
	// lock is now held only during active execution (ADR-0067): this opening
	// BeginDrain is the backstop before BindCheckout, but reaching any gate menu
	// parks (finishes) it so the menu runs lock-free, and resuming AFK work
	// re-acquires a fresh Drain. `drain` is therefore mutable — nil while parked —
	// and the deferred finalize reads its latest value.
	drain, err := BeginDrain(d, runtimePath, taskSetID, confirmOut)
	if err != nil {
		return nil, err
	}

	// This process owns the running Drain: record its exit-reason terminal on
	// every exit path below (including an error bubbling up), so a reader can see
	// how it ended without parsing human output. A declined run never executed,
	// so its Drain row is cancelled rather than terminated (ADR-0056). Registered
	// before BindCheckout so a bind failure still finalizes the row. When the run
	// exits parked at a gate (drain == nil) this is a no-op — the park already
	// recorded the segment's terminal.
	defer func() {
		var (
			declined     bool
			quotaPaused  bool
			verifyFailed bool
			preset       string
			pinned       bool
			resetAt      time.Time
		)
		if result != nil {
			declined = result.Declined
			quotaPaused = result.QuotaPaused
			verifyFailed = result.TaskSetVerifyFailed
			preset = result.PausePreset
			pinned = result.PausePinnedAgent
			resetAt = result.PauseResetAt
		}
		finalizeDrain(drain, declined, quotaPaused, verifyFailed, preset, pinned, resetAt, err)
	}()

	// parkDrain releases the Runtime execution lock at a human-wait gate: it
	// finishes the held Drain with the same clean terminal the --yes path records
	// at that point (ADR-0056/0067) — the set's blocked/awaiting_approval/failed
	// disposition stays manifest-derived — and drops the live lock so the gate
	// menu, assist session, and runtime shell all run lock-free. A no-op when no
	// Drain is held (already parked).
	parkDrain := func() {
		if drain == nil {
			return
		}
		_ = drain.Finish(DrainOutcomeFinished, "", false, time.Time{})
		drain = nil
	}

	// ensureDrain re-acquires the Runtime execution lock before a contiguous run
	// of AFK attempts resumes after a gate park (ADR-0067). It is a no-op while a
	// Drain is already held (the opening BeginDrain, or an unparked segment). A
	// collision with a concurrent drain on the same checkout refuses cleanly with
	// the existing "already in progress" error; the gate decision was already
	// persisted to the manifest, so nothing is lost.
	ensureDrain := func() error {
		if drain != nil {
			return nil
		}
		handle, err := BeginDrain(d, runtimePath, taskSetID, confirmOut)
		if err != nil {
			return err
		}
		drain = handle
		return nil
	}

	// Adopt this checkout into the binding model before draining (ADR-0036): a
	// worktree-locus run records a never-delete adopted binding so the set is
	// integrateable even if the drain later fails; a trunk-locus run records
	// nothing. Done before the first task runs so a failed run is not
	// re-provisioned and orphaned by a later `queue run`.
	if opts.BindCheckout != nil {
		if err := opts.BindCheckout(taskSetID, resolved.ProjectPath, runtimePath); err != nil {
			return nil, exitErr(ExitOperational, "bind checkout: %v", err)
		}
	}

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}

	displayRows := cloneRows(refresh.Rows)
	MarkNextPick(displayRows)
	MarkRunTarget(displayRows, taskSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	if hitlFallback {
		outputFor(out).line(ansiYellow, "No runnable AFK work")
	}

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	if m := displayRefresh.Manifests[taskSetID]; m != nil {
		RenderTaskList(out, taskSetID, m)
	}

	if dirty {
		if err := reportDirtyRuntime(d, confirmOut, runtimePath, strategy); err != nil {
			return nil, exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	initialGate := selectedTaskSetStartsAtHITLGate(refresh, taskSetID) ||
		selectedTaskSetStartsAtFailedGate(refresh, taskSetID)
	var sharedPromptReader *bufio.Reader
	if initialGate {
		sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
	}

	maxTries := opts.MaxTries
	if maxTries <= 0 {
		maxTries = DefaultMaxTries
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}

	result = &RunTaskSetResult{TaskSetID: taskSetID, RuntimePath: runtimePath, ProjectPath: resolved.ProjectPath}
	dirtyStrategyApplied := false

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

			// Pre-approval Verifier phase (ADR-0086): with Agent verification
			// enabled and the set not opted out, a drain that has exhausted the
			// set's AFK work must clear a SHA-gated Verify verdict before its
			// terminal status stands. The verdict is cache-first — a re-drain at an
			// unchanged work SHA reuses the stored verdict and never re-invokes the
			// Verifier, so a parked set does not loop. A PASS lets DONE / AWAITING-
			// APPROVAL stand and falls through to the switch. A FIXABLE verdict
			// makes the Verifier a task producer: while the set is under its
			// remediation depth cap it spawns a new AFK Remediation task whose body
			// is the findings and keeps draining — the Drain picks the task up, its
			// completion moves the work SHA, the cached verdict goes stale, and the
			// Verifier re-fires, closing the loop. At or over the cap (or on a
			// NEEDS-HUMAN verdict) the set parks as VERIFY-FAILED and the drain
			// records the verify_failed terminal. The Runtime execution lock is
			// still held here, so the Verifier and the remediation write run under it.
			if m := currentRefresh.Manifests[taskSetID]; verifyEnabled(cfg) && !m.VerifyOptedOut() &&
				(row.Status == StatusDone || row.Status == StatusAwaitingApproval) {
				repo := ""
				if id, idErr := ResolveRepositoryIdentity(d, runtimePath); idErr == nil {
					repo = id.CommonDir
				}
				effective, verdict, verr := drainVerifyPhase(d, cfg, verifyCoreOptions{
					Repo:        repo,
					RuntimePath: runtimePath,
					SetID:       taskSetID,
					Agents:      opts.VerifyAgents,
					Effort:      opts.VerifyEffort,
					Timeout:     timeout,
					Output:      out,
					runVerifier: opts.verifyRunner,
				}, m, row.Status)
				if verr != nil {
					return result, verr
				}
				if effective == StatusVerifyFailed {
					// A FIXABLE verdict under the cap spawns a Remediation task and
					// keeps draining; only a NEEDS-HUMAN verdict or an exhausted cap
					// actually parks the set.
					if verdict != nil && Verdict(verdict.Verdict) == VerdictFixable {
						spawned, remID, rerr := spawnRemediationIfUnderCap(d, m, verdict.WorkSHA, verdict.Findings, maxRemediationDepth(cfg))
						if rerr != nil {
							return result, rerr
						}
						if spawned {
							outputFor(out).line(ansiBold+ansiCyan, "━━ Spawned remediation task %s — resuming the drain", remID)
							continue
						}
					}
					result.TaskSetVerifyFailed = true
					if verdict != nil {
						result.VerifyFindings = verdict.Findings
					}
					result.VerifyRerunCmd = FormatVerifyCommand(taskSetID, opts.VerifyAgents, opts.VerifyEffort)
					// Overlay the verdict-derived disposition on the display row so the
					// rendered table reads VERIFY-FAILED, matching `pop tasks status`.
					row.Status = StatusVerifyFailed
					row.Progress = BuildProgress(m, StatusVerifyFailed)
					row.VerifyFindings = result.VerifyFindings
					if !opts.Yes {
						fmt.Fprintln(out)
						Render(out, currentRefresh)
					} else {
						printTaskSetSummary(out, result)
					}
					return result, exitErr(ExitNoRunnable, "Task set %q verification failed — a human must review it\n%s", taskSetID, result.VerifyRerunCmd)
				}
			}

			switch row.Status {
			case StatusDone:
				result.TaskSetDone = true
				finishRunTaskSet(out, opts.Yes, result)
				return result, nil
			case StatusDeferred:
				result.TaskSetDeferred = true
				result.SkippedTasks = SkippedTaskIDs(currentRefresh.Manifests[taskSetID])
				finishRunTaskSet(out, opts.Yes, result)
				return result, nil
			case StatusBlocked, StatusAwaitingApproval:
				result.BlockedReason = row.BlockedReason
				if row.Status == StatusAwaitingApproval {
					result.TaskSetAwaitingApproval = true
				}
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				} else {
					printTaskSetSummary(out, result)
				}
				if hitl := BlockingHITLTask(currentRefresh.Manifests[taskSetID]); hitl != nil {
					// Park the Runtime execution lock before the HITL gate menu so it
					// runs lock-free (ADR-0067); only when the menu will actually prompt,
					// so a non-prompting fall-through keeps the normal terminal.
					if gateWillPrompt(opts.ConfirmIn, opts.Yes, currentRefresh.Manifests[taskSetID], hitl) {
						parkDrain()
					}
					rv := &reverifyGateContext{
						cfg:         cfg,
						agents:      opts.VerifyAgents,
						effort:      opts.VerifyEffort,
						timeout:     timeout,
						runVerifier: opts.verifyRunner,
					}
					handled, err := handleInteractiveHITLGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, currentRefresh.Manifests[taskSetID], hitl, rv)
					if err != nil {
						return nil, err
					}
					if handled {
						continue
					}
					if result.TaskSetAwaitingApproval {
						printTerminalHITLAdvice(d, out, taskSetID, currentRefresh.Manifests[taskSetID].Dir, hitl)
					} else {
						printHITLGateAdvice(d, out, taskSetID, currentRefresh.Manifests[taskSetID].Dir, hitl)
					}
				}
				if result.BlockedReason != "" {
					if result.TaskSetAwaitingApproval {
						return result, exitErr(ExitNoRunnable, "Task set %q agents done — awaiting approval: %s", taskSetID, result.BlockedReason)
					}
					return nil, exitErr(ExitNoRunnable, "Task set %q blocked: %s", taskSetID, result.BlockedReason)
				}
				return nil, exitErr(ExitNoRunnable, "Task set %q has no eligible AFK task", taskSetID)
			case StatusFailed:
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				}
				m := currentRefresh.Manifests[taskSetID]
				sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
				// Park the Runtime execution lock before the Failed gate menu so it
				// runs lock-free (ADR-0067).
				if gateWillPrompt(opts.ConfirmIn, opts.Yes, m, FailedTask(m)) {
					parkDrain()
				}
				handled, err := handleInteractiveFailedGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, m, FailedTask(m))
				if err != nil {
					return nil, err
				}
				if handled {
					continue
				}
				printFailedStopAdvice(out, taskSetID, m)
				return nil, exitErr(ExitOperational, "Task set %q has failed tasks", taskSetID)
			default:
				return nil, selErr
			}
		}

		// An eligible AFK task is about to run: (re-)acquire the Runtime execution
		// lock for the contiguous run of attempts that starts here (ADR-0067).
		// First iteration is a no-op (the opening BeginDrain still holds); after a
		// gate park this is a fresh BeginDrain, and a collision refuses cleanly
		// without touching manifest state.
		if err := ensureDrain(); err != nil {
			return result, err
		}

		if dirty && !dirtyStrategyApplied {
			if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, commitOverrides, confirmOut); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
			}
			dirtyStrategyApplied = true
		}

		// Pre-seed the pane's Topic from this task's Title before its first agent
		// prompt (ADR-0058); the hook guards on the existing @pop_topic, so the
		// first task in the set wins and the derive hook no-ops thereafter.
		if opts.PreSeedTopic != nil {
			opts.PreSeedTopic(sel.Task.Title)
		}

		basePrompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
		buildForAgent := func(agentSpec string) (func(string) (*AgentInvocation, error), error) {
			attemptOutput := agentOutput
			if agentSpec != baseAgentPreset {
				var err error
				attemptOutput, err = resolveAgentOutputMode(loadConfig, agentSpec, opts.AgentOutput)
				if err != nil {
					return nil, err
				}
			}
			return func(prompt string) (*AgentInvocation, error) {
				return ResolveAgentInvocationWithMode(agentSpec, opts.AgentCmd, prompt, runtimePath, attemptOutput)
			}, nil
		}

		agentSpecs := resolveTaskAgentSpecs(baseAgentPresets, opts.AgentCmd, sel.Task.Effort, sel.Task.EffortExplicit, cfg)
		taskResult, execErr := executeTaskAttemptsWithAgentFallback(d, sel, runtimePath, out, confirmOut, basePrompt, agentSpecs, buildForAgent, maxTries, timeout, commitOverrides, agentQuotaRetryAfter)
		if execErr != nil {
			afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
			if refreshErr == nil {
				result.Refresh = afterRefresh
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, afterRefresh)
				}
				m := afterRefresh.Manifests[taskSetID]
				sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
				// Park the Runtime execution lock before the post-failure Failed gate
				// menu so it runs lock-free (ADR-0067). An interrupt never reaches the
				// menu (the task is not marked failed), so its `interrupted` terminal is
				// preserved by the normal finalize.
				if gateWillPrompt(opts.ConfirmIn, opts.Yes, m, FailedTask(m)) {
					parkDrain()
				}
				handled, gateErr := handleInteractiveFailedGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, m, FailedTask(m))
				if gateErr != nil {
					return result, gateErr
				}
				if handled {
					continue
				}
				printFailedStopAdvice(out, taskSetID, m)
			}
			return result, execErr
		}
		if taskResult.QuotaPaused {
			result.QuotaPaused = true
			result.PauseReason = taskResult.PauseReason
			result.PausePreset = taskResult.PausePreset
			result.PauseResetAt = taskResult.PauseResetAt
			result.Refresh = currentRefresh
			printTaskSetSummary(out, result)
			return result, nil
		}

		result.Completed = append(result.Completed, taskResult)
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

// ensurePromptReader returns a single bufio.Reader reused across every gate
// prompt in one run. Reusing one reader matters: a fresh bufio.Reader buffers
// ahead on its first read, so making a new one per gate would swallow the input
// queued for later gates. Returns nil — and the caller falls back to static
// advice — when prompting is impossible (--yes or a non-interactive input).
func ensurePromptReader(existing *bufio.Reader, in io.Reader, yes bool) *bufio.Reader {
	if existing != nil {
		return existing
	}
	if yes || !canPrompt(in) {
		return nil
	}
	if in == nil {
		in = os.Stdin
	}
	return bufio.NewReader(in)
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

type hitlGateAction int

const (
	hitlGateExit hitlGateAction = iota
	hitlGateComplete
	hitlGateAssist
	hitlGateDefer
	hitlGateShell
	hitlGateReverify
)

func handleInteractiveHITLGate(d *Deps, out io.Writer, in io.Reader, reader *bufio.Reader, yes bool, agentPreset, agentCmd, cwd, runtimePath, definitionPath, statePath, taskSetID string, m *Manifest, hitl *Task, rv *reverifyGateContext) (bool, error) {
	if yes || !canPrompt(in) || m == nil || hitl == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	prompt := BuildHITLAssistancePrompt(d, taskSetID, m, *hitl, runtimePath)
	body := gateTaskBody(d, m, hitl)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}

	for {
		// The gate offers Re-verify only when Agent verification is enabled for
		// this set (ADR-0086/ADR-0012); the option force-re-runs the Verifier so a
		// human who edited the work inline can re-check it without a fresh drain.
		showReverify := gateReverifyEnabled(rv, m)
		action, err := promptHITLGateAction(out, reader, taskSetID, hitl, body, invocation, showReverify)
		if err != nil {
			return true, err
		}
		switch action {
		case hitlGateReverify:
			repo := ""
			if id, idErr := ResolveRepositoryIdentity(d, runtimePath); idErr == nil {
				repo = id.CommonDir
			}
			if rerr := reverifyAtGate(d, rv, out, repo, runtimePath, taskSetID, m); rerr != nil {
				fmt.Fprintf(outputFor(out), "Could not re-verify: %v\n", rerr)
				continue
			}
			// Refresh the set and overlay the fresh verdict so the rendered table
			// reflects the new state/label (PASS → still AWAITING-APPROVAL, a
			// non-PASS verdict → VERIFY-FAILED), then return to the gate menu.
			afterRefresh, err := RefreshWith(d, definitionPath, statePath)
			if err != nil {
				return true, exitErr(ExitOperational, "refresh after re-verify: %v", err)
			}
			ApplyVerifyVerdicts(d, afterRefresh, rv.cfg, runtimePath)
			fmt.Fprintln(out)
			Render(out, afterRefresh)
			afterManifest := afterRefresh.Manifests[taskSetID]
			if BlockingHITLTask(afterManifest) == nil {
				return true, nil
			}
			m = afterManifest
			hitl = BlockingHITLTask(m)
			body = gateTaskBody(d, m, hitl)
			prompt = BuildHITLAssistancePrompt(d, taskSetID, m, *hitl, runtimePath)
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
			if err != nil {
				return true, exitErr(ExitSetup, "%v", err)
			}
		case hitlGateComplete:
			result, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, hitl.File)})
			if err != nil {
				return true, err
			}
			RenderTaskComplete(out, result.TaskSetID, result.TaskID)
			return true, nil
		case hitlGateAssist:
			fmt.Fprintf(outputFor(out), "Starting HITL assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start HITL assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "HITL assistance exited with status %d; refreshing Task set.\n", exitCode)
			}
			afterRefresh, err := RefreshWith(d, definitionPath, statePath)
			if err != nil {
				return true, exitErr(ExitOperational, "refresh after HITL assistance: %v", err)
			}
			afterManifest := afterRefresh.Manifests[taskSetID]
			if BlockingHITLTask(afterManifest) == nil {
				return true, nil
			}
			m = afterManifest
			prompt = BuildHITLAssistancePrompt(d, taskSetID, m, *BlockingHITLTask(m), runtimePath)
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
			if err != nil {
				return true, exitErr(ExitSetup, "%v", err)
			}
			hitl = BlockingHITLTask(m)
			body = gateTaskBody(d, m, hitl)
		case hitlGateDefer:
			result, err := SkipTaskWith(d, nil, nil, SkipTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, hitl.File)})
			if err != nil {
				return true, err
			}
			RenderTaskSkip(out, result.TaskSetID, result.TaskID)
			return true, nil
		case hitlGateShell:
			if err := spawnRuntimeShell(d, in, runtimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Could not start shell: %v\n", err)
			}
			// No state change, no refresh — loop back to the gate menu unchanged.
		case hitlGateExit:
			return false, nil
		}
	}
}

// runAttendedAssistanceCommand runs the attended assistance agent. stdin must be
// the raw input source (the *os.File terminal), NOT the bufio.Reader used for
// gate prompts: os/exec only inherits a child's controlling terminal when
// cmd.Stdin is an *os.File. Handing it any other io.Reader makes exec splice a
// pipe instead, so a TTY-requiring agent (e.g. codex) fails immediately with
// "stdin is not a terminal".
func runAttendedAssistanceCommand(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, invocation *AgentAssistanceInvocation) (int, error) {
	if attended, ok := d.Runner.(AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), runtimePath, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return d.Runner.Run(context.Background(), runtimePath, out, out, invocation.Command.Name, invocation.Command.Args...)
}

// spawnRuntimeShell spawns $SHELL (falling back to /bin/sh) in the runtime
// checkout as an attended subshell. It is a pure side-trip: no task state is
// changed and no refresh occurs; callers re-show their gate menu after it exits.
func spawnRuntimeShell(d *Deps, stdin io.Reader, runtimePath string, out io.Writer) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if attended, ok := d.Runner.(AttendedCommandRunner); ok {
		_, err := attended.RunAttended(context.Background(), runtimePath, stdin, out, out, shell)
		return err
	}
	_, err := d.Runner.Run(context.Background(), runtimePath, out, out, shell)
	return err
}

// gateWillPrompt reports whether an interactive gate handler will enter its
// menu loop (a real human-wait) rather than no-op. It mirrors the guard at the
// top of handleInteractiveHITLGate / handleInteractiveFailedGate, so the caller
// can park the Runtime execution lock exactly when the menu is about to run
// lock-free (ADR-0067). When the gate will not prompt — under --yes, a
// non-interactive input, or with no gating task (e.g. an interrupted attempt) —
// the lock is left held and the normal finalize records the right terminal.
func gateWillPrompt(in io.Reader, yes bool, m *Manifest, gateTask *Task) bool {
	return !yes && canPrompt(in) && m != nil && gateTask != nil
}

func canPrompt(in io.Reader) bool {
	if _, ok := in.(NonInteractiveReader); ok {
		return false
	}
	if in == nil {
		return isInteractive(os.Stdin)
	}
	return in != os.Stdin || isInteractive(in)
}

// gateTaskBody returns the raw task file body for inline display at a gate, or
// "" when it cannot be read. The agent prompt carries the body regardless; this
// is the copy the human reads before electing to act on the task by hand.
func gateTaskBody(d *Deps, m *Manifest, task *Task) string {
	if d == nil || m == nil || task == nil {
		return ""
	}
	fs := d.FS
	if fs == nil {
		fs = DefaultDeps().FS
	}
	data, err := fs.ReadFile(filepath.Join(m.Dir, task.File))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

// renderGateTaskBody prints the blocking task's full body above a gate menu so a
// human electing to finish by hand can see every action point in place.
func renderGateTaskBody(display *output, taskFile, body string) {
	if body == "" {
		return
	}
	heading := fmt.Sprintf("--- %s ---", taskFile)
	fmt.Fprintln(display)
	fmt.Fprintln(display, heading)
	fmt.Fprintln(display, body)
	fmt.Fprintln(display, strings.Repeat("-", len(heading)))
}

// gateReverifyEnabled reports whether the HITL gate should offer the Re-verify
// option for the current set: only when a Verifier context is present, Agent
// verification is enabled in config, and the set has not opted out (ADR-0086).
func gateReverifyEnabled(rv *reverifyGateContext, m *Manifest) bool {
	return rv != nil && verifyEnabled(rv.cfg) && m != nil && !m.VerifyOptedOut()
}

func promptHITLGateAction(out io.Writer, reader *bufio.Reader, taskSetID string, hitl *Task, body string, invocation *AgentAssistanceInvocation, showReverify bool) (hitlGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue.", taskSetID, hitl.ID)
	renderGateTaskBody(display, hitl.File, body)
	fmt.Fprintln(display, "  1. Get agent assistance (default)")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  2. Complete task")
	fmt.Fprintln(display, "  3. Defer task")
	fmt.Fprintln(display, "  4. Open a shell in the checkout")
	if showReverify {
		fmt.Fprintln(display, "  5. Re-verify (re-run the Verifier against the current work)")
	}
	fmt.Fprintln(display, "  0. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, "0")
	if err != nil {
		return hitlGateExit, err
	}
	choice := strings.ToLower(strings.TrimSpace(answer))
	if choice == "5" && showReverify {
		return hitlGateReverify, nil
	}
	switch choice {
	case "", "1":
		return hitlGateAssist, nil
	case "2":
		return hitlGateComplete, nil
	case "3":
		return hitlGateDefer, nil
	case "4":
		return hitlGateShell, nil
	case "0", "q", "quit", "exit":
		return hitlGateExit, nil
	default:
		if showReverify {
			fmt.Fprintln(display, "Choose 1, 2, 3, 4, 5, or 0.")
		} else {
			fmt.Fprintln(display, "Choose 1, 2, 3, 4, or 0.")
		}
		return promptHITLGateAction(out, reader, taskSetID, hitl, body, invocation, showReverify)
	}
}

// readPromptLine reads one menu selection. eofDefault is returned when the
// input source closes with nothing pending, so a closed pipe resolves to a
// definite choice (each gate passes the number of its Exit option) instead of
// looping forever on empty reads.
func readPromptLine(reader *bufio.Reader, eofDefault string) (string, error) {
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", exitErr(ExitOperational, "read gate selection: %v", err)
	}
	if err == io.EOF && answer == "" {
		return eofDefault, nil
	}
	return strings.TrimRight(answer, "\r\n"), nil
}

type failedGateAction int

const (
	failedGateExit failedGateAction = iota
	failedGateRerun
	failedGateAssist
	failedGateComplete
	failedGateShell
)

// handleInteractiveFailedGate is the interactive counterpart to
// printFailedStopAdvice: it offers the same recovery paths as a numbered menu
// at both points where draining stops on a failed task. Returns (true, nil)
// when the caller should keep draining in-process — Re-run reset the task to
// open, Finish-by-hand marked it done — and (false, nil) when it should fall
// back to the static advice and exit with operational failure (Exit chosen, or
// the prompt cannot run under --yes / a non-interactive input).
func handleInteractiveFailedGate(d *Deps, out io.Writer, in io.Reader, reader *bufio.Reader, yes bool, agentPreset, agentCmd, cwd, runtimePath, definitionPath, statePath, taskSetID string, m *Manifest, failed *Task) (bool, error) {
	if yes || !canPrompt(in) || m == nil || failed == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	prompt := BuildFailedAssistancePrompt(d, taskSetID, m, *failed, runtimePath)
	body := gateTaskBody(d, m, failed)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}

	for {
		action, err := promptFailedGateAction(out, reader, taskSetID, failed, body, invocation)
		if err != nil {
			return true, err
		}
		switch action {
		case failedGateRerun:
			result, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, failed.File)})
			if err != nil {
				return true, err
			}
			RenderTaskReset(out, result.TaskSetID, result.TaskID)
			return true, nil
		case failedGateAssist:
			fmt.Fprintf(outputFor(out), "Starting Failed assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Failed assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "Failed assistance exited with status %d; refreshing Task set.\n", exitCode)
			}
			afterRefresh, err := RefreshWith(d, definitionPath, statePath)
			if err != nil {
				return true, exitErr(ExitOperational, "refresh after Failed assistance: %v", err)
			}
			afterManifest := afterRefresh.Manifests[taskSetID]
			// The assist agent does not change task state on its own, so the task
			// is still failed: refresh, then re-show the Failed gate. If the human
			// did override state during the session, fall through to normal
			// draining.
			if FailedTask(afterManifest) == nil {
				return true, nil
			}
			m = afterManifest
			failed = FailedTask(m)
			prompt = BuildFailedAssistancePrompt(d, taskSetID, m, *failed, runtimePath)
			body = gateTaskBody(d, m, failed)
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
			if err != nil {
				return true, exitErr(ExitSetup, "%v", err)
			}
		case failedGateComplete:
			result, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, failed.File)})
			if err != nil {
				return true, err
			}
			RenderTaskComplete(out, result.TaskSetID, result.TaskID)
			return true, nil
		case failedGateShell:
			if err := spawnRuntimeShell(d, in, runtimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Could not start shell: %v\n", err)
			}
		case failedGateExit:
			return false, nil
		}
	}
}

func promptFailedGateAction(out io.Writer, reader *bufio.Reader, taskSetID string, failed *Task, body string, invocation *AgentAssistanceInvocation) (failedGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiRed, "Failed: %s/%s failed before the set could continue.", taskSetID, failed.ID)
	renderGateTaskBody(display, failed.File, body)
	fmt.Fprintln(display, "  1. Re-run (default)")
	fmt.Fprintln(display, "  2. Agent assistance")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  3. Finish by hand")
	fmt.Fprintln(display, "  4. Open a shell in the checkout")
	fmt.Fprintln(display, "  0. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, "0")
	if err != nil {
		return failedGateExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return failedGateRerun, nil
	case "2":
		return failedGateAssist, nil
	case "3":
		return failedGateComplete, nil
	case "4":
		return failedGateShell, nil
	case "0", "q", "quit", "exit":
		return failedGateExit, nil
	default:
		fmt.Fprintln(display, "Choose 1, 2, 3, 4, or 0.")
		return promptFailedGateAction(out, reader, taskSetID, failed, body, invocation)
	}
}
