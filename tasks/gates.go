package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/clipboard"
	"github.com/glebglazov/pop/ui"
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
	d           *Deps
	out         io.Writer
	in          io.Reader
	reader      *promptReader
	yes         bool
	// agentOverride is the attended agent a human named for this session, empty
	// when they named none. A drain's --agent does not reach here: an attended
	// session launches from [work.attended].agents, never from the list the drain
	// beside it walks (ADR-0195).
	agentOverride string
	agentCmd      string
	// cfg is the loaded config the attended gates resolve their attended entry
	// from. Nil is legal: the built-in default agent applies.
	cfg            *config.Config
	cwd            string
	runtimePath    string
	definitionPath string
	statePath      string
	taskSetID      string
	fold           AssistFold
	// treeStable lends the checkout to one mutating menu verb — Accept,
	// Remediate, Fold — for the length of that verb alone. It is set by the
	// Assist session, which holds nothing itself; a drain's own gates leave it
	// nil, because the drain parked at the gate already holds the checkout its
	// verb would ask for, and a command cannot wait on itself.
	treeStable treeStableSeam
}

// treeStableSeam takes the Checkout claim and hands back the release. A verb
// calls it when the human chooses it, not when the menu opens: the tree only has
// to hold still while the verb runs.
type treeStableSeam func(out io.Writer) (release func(), err error)

// holdTreeStill takes the checkout for one menu verb. With no seam — the drain's
// own gates — the release is a no-op, so one verb body serves both callers.
func (e gateEnv) holdTreeStill(out io.Writer) (func(), error) {
	if e.treeStable == nil {
		return func() {}, nil
	}
	return e.treeStable(out)
}

// reportTreeStableRefusal prints why a verb could not take the checkout, and
// leaves the caller on the menu. An interrupt during the wait is the human
// changing their mind about waiting, not about the session: it says so and goes
// back to the menu rather than ending the session under them.
func reportTreeStableRefusal(out io.Writer, verb string, err error) {
	if isInterrupted(err) {
		fmt.Fprintf(outputFor(out), "%s cancelled — back to the menu.\n", verb)
		return
	}
	fmt.Fprintf(outputFor(out), "Could not take the checkout for %s: %v\n", verb, err)
}

// ensurePromptReader returns a single prompt reader reused across every gate
// prompt in one run. Reusing one reader matters: a fresh bufio.Reader buffers
// ahead on its first read, so making a new one per gate would swallow the input
// queued for later gates. Returns nil — and the caller falls back to static
// advice — when prompting is impossible (--yes or a non-interactive input).
func ensurePromptReader(existing *promptReader, in io.Reader, yes bool) *promptReader {
	if existing != nil {
		return existing
	}
	if yes || !canPrompt(in) {
		return nil
	}
	return newPromptReader(in)
}

type hitlGateAction int

const (
	hitlGateExit hitlGateAction = iota
	hitlGateComplete
	hitlGateAssist
	hitlGateDefer
	hitlGateShell
	hitlGateReverify
	hitlGateReadRefine
	hitlGateReadVerify
)

func handleInteractiveHITLGate(env gateEnv, m *Manifest, hitl *Task, rv *reverifyGateContext) (bool, error) {
	d := env.d
	out := env.out
	in := env.in
	reader := env.reader
	agentOverride := env.agentOverride
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
		reader = newPromptReader(in)
	}

	prompt := BuildHITLAssistancePrompt(d, taskSetID, m, *hitl, runtimePath)
	body := gateTaskBody(d, m, hitl)
	// The agent is resolved when assistance is chosen, never on the way in: the
	// walk that picks one refuses when every attended entry is cooling or
	// missing, and that refusal belongs in the menu, not in the door.
	var invocation *AgentAssistanceInvocation

	for {
		// The gate offers Re-verify only when Agent verification is enabled for
		// this set (ADR-0086/ADR-0012); the option force-re-runs the Verifier so a
		// human who edited the work inline can re-check it without a fresh drain.
		showReverify := gateReverifyEnabled(rv, m)
		// Re-resolved each time round the menu: a Re-verify may land a Remediation
		// task and a report written since the gate opened is still the one to point at.
		refine := resolveGateRefineState(d, env.cfg, m)
		verify, hasVerify := latestVerifyPointer(d, m)
		action, err := promptHITLGateAction(out, in, d, env.cfg, runtimePath, reader, taskSetID, m, hitl, body, invocation, showReverify, refine, verify, hasVerify)
		if err != nil {
			return true, err
		}
		switch action {
		case hitlGateReadRefine:
			pageReportDocument(d, in, runtimePath, out, refine.Pointer)
			// A read changes nothing — loop back to the menu with the set as it was.
		case hitlGateReadVerify:
			pageReportDocument(d, in, runtimePath, out, verify)
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
		case hitlGateComplete:
			result, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, hitl.File)})
			if err != nil {
				return true, err
			}
			RenderTaskComplete(out, result.TaskSetID, result.TaskID)
			return true, nil
		case hitlGateAssist:
			invocation, err = ResolveAgentAssistanceInvocation(d, env.cfg, agentOverride, agentCmd, prompt, runtimePath)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start HITL assistance: %v\n", err)
				continue
			}
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
	deliverClipboardBriefing(d, out, invocation.ClipboardPrompt)
	if attended, ok := d.Runner.(AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), runtimePath, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return d.Runner.Run(context.Background(), runtimePath, out, out, invocation.Command.Name, invocation.Command.Args...)
}

// deliverClipboardBriefing places an attended assistance briefing on the
// clipboard before launch, for a preset whose interactive binary takes no
// positional prompt (kimi) — the only way the briefing reaches the human is
// via paste (ADR-0164). A no-op when the invocation carries no such briefing.
// Clipboard failure degrades to printing the briefing text in full; it never
// blocks the launch.
func deliverClipboardBriefing(d *Deps, out io.Writer, prompt string) {
	if prompt == "" {
		return
	}
	copyFn := clipboard.Copy
	if d != nil && d.ClipboardCopy != nil {
		copyFn = d.ClipboardCopy
	}
	if err := copyFn(prompt); err != nil {
		fmt.Fprintf(outputFor(out), "Could not copy briefing to clipboard (%v); paste this into the session:\n%s\n", err, prompt)
		return
	}
	fmt.Fprintln(outputFor(out), "Briefing copied to clipboard — paste it into the session.")
}

// spawnRuntimeShell spawns $SHELL (falling back to /bin/sh) in the runtime
// checkout as an attended subshell. It is a pure side-trip: no task state is
// changed and no refresh occurs; callers re-show their gate menu after it exits.
func spawnRuntimeShell(d *Deps, stdin io.Reader, runtimePath string, out io.Writer) error {
	// The banner is the honest half of leaving the shell unlocked: pop cannot
	// enforce what a human types at a prompt, and claiming the checkout for a tab
	// that may stay open all afternoon would stall every command queued behind it.
	fmt.Fprintf(outputFor(out), "Shell in %s — the checkout is not claimed while it is open, so another command can be admitted to it.\n", runtimePath)
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

// pageReportDocument shows one pass's report — refine's or verification's — to
// the human at the gate. It spawns no agent: the document is already written,
// and this entry is a read of it, not a second opinion (ADR-0252, ADR-0245). The
// human's pager runs attended in the runtime checkout; when none will run, the
// document is printed instead, because the whole point of the entry is that the
// human sees it.
func pageReportDocument(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, p ReportPointer) {
	if p.Path == "" {
		return
	}
	name, args := pagerCommand(p.Path)
	var err error
	if attended, ok := d.Runner.(AttendedCommandRunner); ok {
		_, err = attended.RunAttended(context.Background(), runtimePath, stdin, out, out, name, args...)
	} else {
		_, err = d.Runner.Run(context.Background(), runtimePath, out, out, name, args...)
	}
	if err == nil {
		return
	}
	fmt.Fprintf(outputFor(out), "Could not page %s (%v); printing it instead.\n", p.Path, err)
	fs := d.FS
	if fs == nil {
		fs = DefaultDeps().FS
	}
	data, readErr := fs.ReadFile(p.Path)
	if readErr != nil {
		fmt.Fprintf(outputFor(out), "Could not read the report: %v\n", readErr)
		return
	}
	fmt.Fprintln(outputFor(out), strings.TrimRight(string(data), "\n"))
}

// pagerCommand is $PAGER as the human set it, words and flags alike, falling
// back to less in the raw-control mode a Markdown document reads best in.
func pagerCommand(path string) (string, []string) {
	fields := strings.Fields(os.Getenv("PAGER"))
	if len(fields) == 0 {
		fields = []string{"less", "-R"}
	}
	return fields[0], append(fields[1:], path)
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

// gateReverifyEnabled reports whether the HITL gate should offer the Re-verify
// option for the current set: only when a Verifier context is present, Agent
// verification is enabled in config, and the set has not opted out (ADR-0086).
func gateReverifyEnabled(rv *reverifyGateContext, m *Manifest) bool {
	return rv != nil && verifyEnabled(rv.cfg) && m != nil && !m.VerifyOptedOut()
}

func promptHITLGateAction(out io.Writer, in io.Reader, d *Deps, cfg *config.Config, runtimePath string, reader *promptReader, taskSetID string, m *Manifest, hitl *Task, body string, invocation *AgentAssistanceInvocation, showReverify bool, refine gateRefineState, verify ReportPointer, hasVerify bool) (hitlGateAction, error) {
	items := []ui.GateMenuItem{
		{Key: "1", Label: "Get agent assistance (default)", Details: gateInvocationDetails(invocation), Default: true, Assists: true},
		{Key: "2", Label: "Complete task"},
		{Key: "3", Label: "Defer task"},
		{Key: "4", Label: "Open a shell in the checkout"},
	}
	// The two conditional entries take the next free number each, so the menu
	// stays contiguous whichever of them the set earns; the keys are read back
	// through this map rather than by position.
	keys := map[string]hitlGateAction{"1": hitlGateAssist, "2": hitlGateComplete, "3": hitlGateDefer, "4": hitlGateShell}
	add := func(action hitlGateAction, label string, details ...string) {
		key := strconv.Itoa(len(items) + 1)
		keys[key] = action
		items = append(items, ui.GateMenuItem{Key: key, Label: label, Details: details})
	}
	if showReverify {
		add(hitlGateReverify, "Re-verify (re-run the Verifier against the current work)")
	}
	if refine.HasReport {
		add(hitlGateReadRefine, "Read the refine report (no agent runs)", gateRefineEntryDetails(refine)...)
	}
	if hasVerify {
		add(hitlGateReadVerify, "Read the verify report (no agent runs)", verify.Path)
	}
	items = append(items, ui.GateMenuItem{Key: "0", Label: "Exit"})

	spec := ui.GateMenuSpec{
		Headline: fmt.Sprintf("Human-blocked: %s/%s needs human work before the set can continue.", taskSetID, hitl.ID),
		Tone:     ui.GateMenuToneWarn,
		Preamble: joinPreamble(
			gateWaiterPreamble(d, runtimePath),
			gateTaskBodyPreamble(hitl.File, body),
			gateRemediationPreamble(d, taskSetID, m),
			gateRefinePreamble(refine),
			gateVerifyPreamble(verify, hasVerify),
		),
		Items: items,
	}
	choice, _, err := promptGateMenu(out, in, reader, spec, nil, cfg)
	if err != nil {
		return hitlGateExit, err
	}
	if action, ok := keys[choice]; ok {
		return action, nil
	}
	return hitlGateExit, nil
}

// readPromptLine reads one menu selection. eofDefault is returned when the
// input source closes with nothing pending, so a closed pipe resolves to a
// definite choice (each gate passes the number of its Exit option) instead of
// looping forever on empty reads. out is where the reader reports a terminal it
// had to wrestle the foreground away from, or could not.
func readPromptLine(reader *promptReader, out io.Writer, eofDefault string) (string, error) {
	answer, err := reader.ReadLine(promptWarner(out))
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
	agentOverride := env.agentOverride
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
		reader = newPromptReader(in)
	}

	prompt := BuildFailedAssistancePrompt(d, taskSetID, m, *failed, runtimePath)
	body := gateTaskBody(d, m, failed)
	// Resolved when assistance is chosen — see handleInteractiveHITLGate.
	var invocation *AgentAssistanceInvocation

	for {
		action, err := promptFailedGateAction(out, in, d, env.cfg, runtimePath, reader, taskSetID, failed, body, invocation)
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
			invocation, err = ResolveAgentAssistanceInvocation(d, env.cfg, agentOverride, agentCmd, prompt, runtimePath)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Failed assistance: %v\n", err)
				continue
			}
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

func promptFailedGateAction(out io.Writer, in io.Reader, d *Deps, cfg *config.Config, runtimePath string, reader *promptReader, taskSetID string, failed *Task, body string, invocation *AgentAssistanceInvocation) (failedGateAction, error) {
	spec := ui.GateMenuSpec{
		Headline: fmt.Sprintf("Failed: %s/%s failed before the set could continue.", taskSetID, failed.ID),
		Tone:     ui.GateMenuToneError,
		Preamble: joinPreamble(
			gateWaiterPreamble(d, runtimePath),
			gateTaskBodyPreamble(failed.File, body),
		),
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Re-run (default)", Default: true},
			{Key: "2", Label: "Agent assistance", Details: gateInvocationDetails(invocation), Assists: true},
			{Key: "3", Label: "Finish by hand"},
			{Key: "4", Label: "Open a shell in the checkout"},
			{Key: "0", Label: "Exit"},
		},
	}
	choice, _, err := promptGateMenu(out, in, reader, spec, nil, cfg)
	if err != nil {
		return failedGateExit, err
	}
	switch choice {
	case "1":
		return failedGateRerun, nil
	case "2":
		return failedGateAssist, nil
	case "3":
		return failedGateComplete, nil
	case "4":
		return failedGateShell, nil
	default:
		return failedGateExit, nil
	}
}

type verifyFailedGateAction int

const (
	verifyFailedGateExit verifyFailedGateAction = iota
	verifyFailedGateAccept
	verifyFailedGateRemediate
	verifyFailedGateAssist
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
	agentOverride := env.agentOverride
	agentCmd := env.agentCmd
	runtimePath := env.runtimePath
	taskSetID := env.taskSetID
	if env.yes || !canPrompt(in) || m == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = newPromptReader(in)
	}

	prompt := BuildVerifyFailedAssistancePrompt(d, taskSetID, m, workSHA, findings, runtimePath)
	// Resolved when assistance is chosen — see handleInteractiveHITLGate.
	var invocation *AgentAssistanceInvocation

	for {
		action, err := promptVerifyFailedGateAction(out, in, d, env.cfg, runtimePath, reader, taskSetID, m, findings, invocation)
		if err != nil {
			return true, err
		}
		switch action {
		case verifyFailedGateAccept:
			note, err := readGateNote(out, reader, "Accept note (why this is acceptable, optional): ")
			if err != nil {
				return true, err
			}
			release, holdErr := env.holdTreeStill(out)
			if holdErr != nil {
				reportTreeStableRefusal(out, "Accept", holdErr)
				continue
			}
			if _, err := acceptResolvedSet(d, verifyCoreOptions{
				Repo:        repo,
				RuntimePath: runtimePath,
				SetID:       taskSetID,
				Output:      out,
				Accept:      true,
				AcceptNote:  note,
			}, m, workSHA); err != nil {
				release()
				return true, err
			}
			release()
			return true, nil
		case verifyFailedGateRemediate:
			note, err := readGateNote(out, reader, "Remediation note (what to fix, optional): ")
			if err != nil {
				return true, err
			}
			release, holdErr := env.holdTreeStill(out)
			if holdErr != nil {
				reportTreeStableRefusal(out, "Remediate", holdErr)
				continue
			}
			if _, err := remediateResolvedSet(d, verifyCoreOptions{
				Repo:          repo,
				RuntimePath:   runtimePath,
				SetID:         taskSetID,
				Output:        out,
				Remediate:     true,
				RemediateNote: note,
			}, m, workSHA); err != nil {
				release()
				return true, err
			}
			release()
			return true, nil
		case verifyFailedGateAssist:
			invocation, err = ResolveAgentAssistanceInvocation(d, env.cfg, agentOverride, agentCmd, prompt, runtimePath)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Verify-failed assistance: %v\n", err)
				continue
			}
			fmt.Fprintf(outputFor(out), "Starting Verify-failed assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Verify-failed assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "Verify-failed assistance exited with status %d.\n", exitCode)
			}
			// Advisory only: no verdict or manifest change — loop back to the gate menu.
			prompt = BuildVerifyFailedAssistancePrompt(d, taskSetID, m, workSHA, findings, runtimePath)
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

func promptVerifyFailedGateAction(out io.Writer, in io.Reader, d *Deps, cfg *config.Config, runtimePath string, reader *promptReader, taskSetID string, m *Manifest, findings string, invocation *AgentAssistanceInvocation) (verifyFailedGateAction, error) {
	spec := ui.GateMenuSpec{
		Headline: fmt.Sprintf("Verify-failed: %s did not clear the Verifier and needs a human decision.", taskSetID),
		Tone:     ui.GateMenuToneError,
		Preamble: joinPreamble(
			gateWaiterPreamble(d, runtimePath),
			gateFindingsPreamble(findings),
			gateRemediationPreamble(d, taskSetID, m),
		),
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Accept (record a human-authored PASS)"},
			{Key: "2", Label: "Remediate (spawn a fix task)"},
			{Key: "3", Label: "Agent assistance", Details: gateInvocationDetails(invocation), Assists: true},
			{Key: "4", Label: "Open a shell in the checkout"},
			{Key: "0", Label: "Exit", Default: true},
		},
	}
	choice, _, err := promptGateMenu(out, in, reader, spec, nil, cfg)
	if err != nil {
		return verifyFailedGateExit, err
	}
	switch choice {
	case "1":
		return verifyFailedGateAccept, nil
	case "2":
		return verifyFailedGateRemediate, nil
	case "3":
		return verifyFailedGateAssist, nil
	case "4":
		return verifyFailedGateShell, nil
	default:
		return verifyFailedGateExit, nil
	}
}

// readGateNote prompts for a single-line note at a gate. It returns "" on an
// empty answer or a closed input, so Accept / Remediate remain usable without a
// note (both trim and tolerate an empty rationale).
func readGateNote(out io.Writer, reader *promptReader, label string) (string, error) {
	display := outputFor(out)
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, label))
	answer, err := readPromptLine(reader, out, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
