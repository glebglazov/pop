package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// gateEnv is the shared context the three interactive gate menus (HITL, Failed,
// Verify-failed) run against — the output/input/prompt reader/yes flag, the agent
// preset/cmd/cwd, and the runtime/definition/state paths and set id. The
// whole-set drain builds one from its implementRun (newGateEnv); the targeted
// single-task HITL path (runTargetedHITLGate) constructs its own without a run,
// since it reuses the exact same menu code (decision 6). The handlers are free
// functions over it — deliberately not implementRun methods — so both callers
// share them.
type gateEnv struct {
	d              *Deps
	out            io.Writer
	in             io.Reader
	reader         *bufio.Reader
	yes            bool
	agentPreset    string
	agentCmd       string
	cwd            string
	runtimePath    string
	definitionPath string
	statePath      string
	taskSetID      string
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

type hitlGateAction int

const (
	hitlGateExit hitlGateAction = iota
	hitlGateComplete
	hitlGateAssist
	hitlGateDefer
	hitlGateShell
	hitlGateReverify
)

func handleInteractiveHITLGate(env gateEnv, m *Manifest, hitl *Task, rv *reverifyGateContext) (bool, error) {
	d := env.d
	out := env.out
	in := env.in
	reader := env.reader
	agentPreset := env.agentPreset
	agentCmd := env.agentCmd
	cwd := env.cwd
	runtimePath := env.runtimePath
	definitionPath := env.definitionPath
	statePath := env.statePath
	taskSetID := env.taskSetID
	if env.yes || !canPrompt(in) || m == nil || hitl == nil {
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
func handleInteractiveFailedGate(env gateEnv, m *Manifest, failed *Task) (bool, error) {
	d := env.d
	out := env.out
	in := env.in
	reader := env.reader
	agentPreset := env.agentPreset
	agentCmd := env.agentCmd
	cwd := env.cwd
	runtimePath := env.runtimePath
	definitionPath := env.definitionPath
	statePath := env.statePath
	taskSetID := env.taskSetID
	if env.yes || !canPrompt(in) || m == nil || failed == nil {
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

type verifyFailedGateAction int

const (
	verifyFailedGateExit verifyFailedGateAction = iota
	verifyFailedGateAccept
	verifyFailedGateRemediate
	verifyFailedGateShell
)

// handleInteractiveVerifyFailedGate is the interactive counterpart to the
// VERIFY-FAILED park (ADR-0103): when a drain lands on a Verify-failed set on a
// TTY it presents the findings and lets a human disposition the set — Accept
// (record a human-authored PASS with a note), Remediate (spawn a Remediation
// task with a note), open a shell in the checkout, or exit. Accept and Remediate
// invoke the exact store/spawn behavior behind the `--accept` / `--remediate`
// CLI flags. Re-verify is deliberately not offered here — re-running the Verifier
// is a separate force action, not a finding response. Returns (true, nil) when
// the caller should keep draining in-process (Accept flipped the set to verified,
// Remediate spawned drainable work) and (false, nil) when it should fall back to
// the static advice and exit (Exit chosen, or the prompt cannot run under --yes /
// a non-interactive input).
func handleInteractiveVerifyFailedGate(env gateEnv, repo string, m *Manifest, workSHA, findings string) (bool, error) {
	d := env.d
	out := env.out
	in := env.in
	reader := env.reader
	runtimePath := env.runtimePath
	taskSetID := env.taskSetID
	if env.yes || !canPrompt(in) || m == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	for {
		action, err := promptVerifyFailedGateAction(out, reader, taskSetID, findings)
		if err != nil {
			return true, err
		}
		switch action {
		case verifyFailedGateAccept:
			note, err := readGateNote(out, reader, "Accept note (why this is acceptable, optional): ")
			if err != nil {
				return true, err
			}
			if _, err := acceptResolvedSet(d, verifyCoreOptions{
				Repo:        repo,
				RuntimePath: runtimePath,
				SetID:       taskSetID,
				Output:      out,
				Accept:      true,
				AcceptNote:  note,
			}, m, workSHA); err != nil {
				return true, err
			}
			return true, nil
		case verifyFailedGateRemediate:
			note, err := readGateNote(out, reader, "Remediation note (what to fix, optional): ")
			if err != nil {
				return true, err
			}
			if _, err := remediateResolvedSet(d, verifyCoreOptions{
				Repo:          repo,
				RuntimePath:   runtimePath,
				SetID:         taskSetID,
				Output:        out,
				Remediate:     true,
				RemediateNote: note,
			}, m, workSHA); err != nil {
				return true, err
			}
			return true, nil
		case verifyFailedGateShell:
			if err := spawnRuntimeShell(d, in, runtimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Could not start shell: %v\n", err)
			}
			// No state change, no refresh — loop back to the gate menu unchanged.
		case verifyFailedGateExit:
			return false, nil
		}
	}
}

func promptVerifyFailedGateAction(out io.Writer, reader *bufio.Reader, taskSetID, findings string) (verifyFailedGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiRed, "Verify-failed: %s did not clear the Verifier and needs a human decision.", taskSetID)
	renderVerifyGateFindings(display, findings)
	fmt.Fprintln(display, "  1. Accept (record a human-authored PASS)")
	fmt.Fprintln(display, "  2. Remediate (spawn a fix task)")
	fmt.Fprintln(display, "  3. Open a shell in the checkout")
	fmt.Fprintln(display, "  0. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [0]: "))

	answer, err := readPromptLine(reader, "0")
	if err != nil {
		return verifyFailedGateExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "1":
		return verifyFailedGateAccept, nil
	case "2":
		return verifyFailedGateRemediate, nil
	case "3":
		return verifyFailedGateShell, nil
	case "", "0", "q", "quit", "exit":
		return verifyFailedGateExit, nil
	default:
		fmt.Fprintln(display, "Choose 1, 2, 3, or 0.")
		return promptVerifyFailedGateAction(out, reader, taskSetID, findings)
	}
}

// renderVerifyGateFindings prints the recorded Verifier findings above the gate
// menu so the human reads what failed before choosing a disposition.
func renderVerifyGateFindings(display *output, findings string) {
	if strings.TrimSpace(findings) == "" {
		return
	}
	display.line(ansiBold, "  Findings:")
	for _, line := range strings.Split(strings.TrimRight(findings, "\n"), "\n") {
		fmt.Fprintf(display, "    %s\n", line)
	}
}

// readGateNote prompts for a single-line note at a gate. It returns "" on an
// empty answer or a closed input, so Accept / Remediate remain usable without a
// note (both trim and tolerate an empty rationale).
func readGateNote(out io.Writer, reader *bufio.Reader, label string) (string, error) {
	display := outputFor(out)
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, label))
	answer, err := readPromptLine(reader, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
