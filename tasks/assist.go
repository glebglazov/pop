package tasks

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
)

// AssistOptions configures a `pop tasks assist <set>` session.
type AssistOptions struct {
	ResolveInput ResolveInput
	// TaskSetID is the bare Task-set identifier to open.
	TaskSetID string
	// AgentPreset / AgentCmd select the attended assistance adapter.
	AgentPreset string
	AgentCmd    string
	// Output receives menu text and attended-session streams.
	Output io.Writer
	// Input is the interactive TTY (or test reader). Non-interactive input refuses.
	Input io.Reader
	// Fold performs the menu's fold action. This package cannot call
	// binding.Fold itself — tasks/binding imports tasks — so the cmd layer
	// injects it, keeping the refusal error in this process where the menu can
	// print it. A nil seam hides the fold action.
	Fold AssistFold
}

// AssistFold folds a set's branch onto Trunk and releases its checkout,
// streaming its own progress and fold-conflict prompts through in/out.
type AssistFold func(setID string, in io.Reader, out io.Writer) error

// AssistTaskSet opens an Assist session using default dependencies.
func AssistTaskSet(opts AssistOptions) error {
	return AssistTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// AssistTaskSetWith opens a human-in-the-loop Assist session on an arbitrary Task
// set at its current derived status, without draining or re-running the Verifier.
// It presents the gate menu that status calls for (or a generic assistance menu),
// re-derives after status-changing dispositions, and exits on `0`.
//
// Guard rails: Binding-first runtime resolution, TTY required (headless equivalents
// named), refuse live drain / Missing / Archived / repository mismatch, and a
// non-claiming Checkout gate hold for the session's duration (released on every
// exit path including interrupt).
func AssistTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts AssistOptions) error {
	if d == nil {
		d = defaultDeps
	}
	setID := strings.TrimSpace(opts.TaskSetID)
	if setID == "" {
		return exitErr(ExitSetup, "a task set identifier is required")
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	in := opts.Input
	if in == nil {
		in = os.Stdin
	}

	// Assist is a human session: refuse headless rather than degrading.
	if !canPrompt(in) {
		return exitErr(ExitSetup, "assisting a task set needs an interactive terminal; headless equivalents: `pop tasks verify %s --accept/--remediate`, `pop tasks complete`/`skip`/`open`, or `pop tasks implement`", setID)
	}

	runtimePath, manifestPath, err := ValidateAssistLaunch(d, pd, loadConfig, opts)
	if err != nil {
		return err
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return err
	}
	statePath := StatePathFor(resolved.DefinitionPath)
	runtimeID, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return err
	}

	cfg, _ := loadConfig(config.DefaultConfigPath())
	// `pop tasks assist --agent` is the human naming their own attended agent for
	// this session; empty resolves to the attended group (ADR-0195).
	agentOverride := strings.TrimSpace(opts.AgentPreset)

	if err := RegisterCheckoutGateHold(d, setID, runtimePath, false); err != nil {
		return err
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = ReleaseCheckoutGateHold(d, setID, runtimePath)
	}
	defer release()

	// Interrupt must release the hold on every exit path.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	interruptErr := make(chan error, 1)
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-sigCh:
			release()
			select {
			case interruptErr <- exitErr(ExitInterrupted, "assist session interrupted"):
			default:
			}
		case <-stopWatch:
		}
	}()

	reader := newPromptReader(in)
	env := gateEnv{
		d:              d,
		out:            out,
		in:             in,
		reader:         reader,
		agentOverride:  agentOverride,
		agentCmd:       opts.AgentCmd,
		cwd:            resolved.ProjectPath,
		runtimePath:    runtimePath,
		definitionPath: resolved.DefinitionPath,
		statePath:      statePath,
		taskSetID:      setID,
		cfg:            cfg,
		fold:           opts.Fold,
	}

	for {
		select {
		case err := <-interruptErr:
			return err
		default:
		}

		m := LoadManifest(d, setID, manifestPath)
		if !m.Valid {
			return exitErr(ExitSetup, "task set %q is malformed: %s", setID, MalformedSummary(m))
		}
		status, findings, workSHA := assistDerivedStatus(d, cfg, m, setID, runtimePath, runtimeID.CommonDir)

		fmt.Fprintln(out)
		fmt.Fprintf(out, "Assist session: %s [%s]\n", setID, status)
		fmt.Fprintf(out, "Runtime: %s\n", runtimePath)

		var handled bool
		var gateErr error
		switch status {
		case StatusBlocked, StatusAwaitingApproval:
			hitl := BlockingHITLTask(m)
			if hitl == nil {
				// Status says HITL but no open HITL task — fall through to generic.
				handled, gateErr = handleGenericAssistMenu(env, m, status, findings)
			} else {
				// No re-verify: Assist never invokes the Verifier.
				handled, gateErr = handleInteractiveHITLGate(env, m, hitl, nil)
			}
		case StatusVerifyFailed:
			handled, gateErr = handleInteractiveVerifyFailedGate(env, runtimeID.CommonDir, m, workSHA, findings)
		case StatusFailed:
			failed := FailedTask(m)
			if failed == nil {
				handled, gateErr = handleGenericAssistMenu(env, m, status, findings)
			} else {
				handled, gateErr = handleInteractiveFailedGate(env, m, failed)
			}
		default:
			handled, gateErr = handleGenericAssistMenu(env, m, status, findings)
		}
		if gateErr != nil {
			return gateErr
		}
		if !handled {
			return nil
		}
		// Status-changing disposition: re-derive and re-enter the appropriate menu.
		// Re-check live drain in case a concurrent drain started mid-session.
		if err := refuseAssistWhileDrainLive(d, setID); err != nil {
			return err
		}
	}
}

// ValidateAssistLaunch checks preconditions for opening an Assist session without
// requiring an interactive terminal. It returns the binding-first runtime checkout
// and manifest path the session would use. It applies the same refusals as
// AssistTaskSetWith except the interactive-terminal requirement.
func ValidateAssistLaunch(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts AssistOptions) (runtimePath, manifestPath string, err error) {
	if d == nil {
		d = defaultDeps
	}
	setID := strings.TrimSpace(opts.TaskSetID)
	if setID == "" {
		return "", "", exitErr(ExitSetup, "a task set identifier is required")
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return "", "", err
	}
	statePath := StatePathFor(resolved.DefinitionPath)

	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, setID); err != nil {
		return "", "", err
	}

	// Callers pin Binding-first resolution via ResolveInput.RuntimeOverride
	// (ADR-0146) — the same seam `pop tasks verify` uses — so this package
	// never imports tasks/binding.
	runtimePath, err = ResolveRuntimePathWith(d, resolved.ProjectPath, opts.ResolveInput.RuntimeOverride)
	if err != nil {
		return "", "", err
	}

	currentID, err := ResolveRepositoryIdentity(d, resolved.ProjectPath)
	if err != nil {
		return "", "", err
	}
	runtimeID, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return "", "", err
	}
	if currentID.CommonDir != runtimeID.CommonDir {
		return "", "", exitErr(ExitSetup, "task set %q belongs to a different repository than the current checkout (%s vs %s); run assist from a checkout of the set's repository",
			setID, currentID.CommonDir, runtimeID.CommonDir)
	}

	disc, err := DiscoverWith(d, resolved.DefinitionPath)
	if err != nil {
		return "", "", exitErr(ExitOperational, "discover task sets: %v", err)
	}
	manifestPath, ok := disc.Manifests[setID]
	if !ok {
		// Registered-but-gone sets surface as MISSING on refresh.
		if refresh, rerr := RefreshWith(d, resolved.DefinitionPath, statePath); rerr == nil {
			if row := FindRow(refresh, setID); row != nil && row.Status == StatusMissing {
				return "", "", exitErr(ExitSetup, "task set %q is missing (registered but not on disk)", setID)
			}
		}
		return "", "", exitErr(ExitSetup, "unknown task set %q", setID)
	}

	if err := refuseAssistWhileDrainLive(d, setID); err != nil {
		return "", "", err
	}
	return runtimePath, manifestPath, nil
}

// refuseAssistWhileDrainLive refuses when the named set has a live running Drain.
func refuseAssistWhileDrainLive(d *Deps, setID string) error {
	drains, err := LiveRunningDrains(d)
	if err != nil {
		return exitErr(ExitOperational, "check live drains: %v", err)
	}
	for _, dr := range drains {
		if dr.SetID == setID {
			return exitErr(ExitSetup, "task set %q has a live drain (pid %d on %s); wait for it to finish or interrupt it before assisting",
				setID, dr.PID, dr.RuntimePath)
		}
	}
	return nil
}

// assistDerivedStatus computes the set's status from the manifest plus any cached
// Verify verdict at the runtime HEAD — never invoking the Verifier.
func assistDerivedStatus(d *Deps, cfg *config.Config, m *Manifest, setID, runtimePath, repo string) (TaskSetStatus, string, string) {
	status := DeriveStatus(m)
	workSHA := verifyWorkSHA(d, runtimePath)
	findings := ""
	if !verifyEnabled(cfg) || (status != StatusDone && status != StatusAwaitingApproval) {
		return status, findings, workSHA
	}
	var current, latestPass *store.VerifyVerdict
	if s, ok, err := openDrainStoreIfExists(d); err == nil && ok && repo != "" && workSHA != "" {
		if v, err := s.GetVerifyVerdict(repo, setID, workSHA); err == nil {
			current = v
		}
		if v, err := s.GetLatestPassVerifyVerdict(repo, setID); err == nil {
			latestPass = v
		}
	}
	row := Row{ID: setID, Status: status}
	decorateRowWithVerdict(&row, m, workSHA, current, latestPass)
	// Findings key on the mark, not the status: on a human-completed set the
	// verdict is only a mark, and the assistance prompt must still carry what the
	// Verifier found.
	if row.VerifyMark == VerifyMarkFailed {
		findings = row.VerifyFindings
	}
	return row.Status, findings, workSHA
}

type genericAssistAction int

const (
	genericAssistExit genericAssistAction = iota
	genericAssistAgent
	genericAssistShell
	genericAssistFold
)

// handleGenericAssistMenu is the Assist session menu for Ready / Done / Deferred
// (and other non-gate statuses): agent assistance, a shell in the checkout, or
// exit. No drain entry and no re-verify entry.
func handleGenericAssistMenu(env gateEnv, m *Manifest, status TaskSetStatus, findings string) (bool, error) {
	d := env.d
	out := env.out
	in := env.in
	reader := env.reader
	runtimePath := env.runtimePath
	taskSetID := env.taskSetID
	if !canPrompt(in) || m == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = newPromptReader(in)
	}

	prompt := BuildAssistPrompt(d, taskSetID, m, status, runtimePath, findings)
	invocation, err := ResolveAgentAssistanceInvocation(d, env.cfg, env.agentOverride, env.agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}
	offerFold := env.fold != nil && assistFoldEligible(d, taskSetID, status)

	for {
		action, err := promptGenericAssistAction(out, reader, taskSetID, status, invocation, offerFold)
		if err != nil {
			return false, err
		}
		switch action {
		case genericAssistAgent:
			fmt.Fprintf(outputFor(out), "Starting Assist session assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Assist assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "Assist assistance exited with status %d.\n", exitCode)
			}
			// Advisory side-trip: refresh the set-wide prompt and re-show this menu.
			after, rerr := RefreshWith(d, env.definitionPath, env.statePath)
			if rerr == nil {
				if refreshed := after.Manifests[taskSetID]; refreshed != nil {
					m = refreshed
				}
			}
			prompt = BuildAssistPrompt(d, taskSetID, m, status, runtimePath, findings)
			invocation, err = ResolveAgentAssistanceInvocation(d, env.cfg, env.agentOverride, env.agentCmd, prompt, runtimePath)
			if err != nil {
				return false, exitErr(ExitSetup, "%v", err)
			}
		case genericAssistShell:
			if err := spawnRuntimeShell(d, in, runtimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Could not start shell: %v\n", err)
			}
		case genericAssistFold:
			if err := env.fold(taskSetID, in, out); err != nil {
				fmt.Fprintf(outputFor(out), "Fold failed: %v\n", err)
				continue
			}
			return false, nil
		case genericAssistExit:
			return false, nil
		}
	}
}

func promptGenericAssistAction(out io.Writer, reader *promptReader, taskSetID string, status TaskSetStatus, invocation *AgentAssistanceInvocation, offerFold bool) (genericAssistAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiCyan, "Assist: %s is %s.", taskSetID, status)
	fmt.Fprintln(display, "  1. Agent assistance (default)")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  2. Open a shell in the checkout")
	if offerFold {
		fmt.Fprintln(display, "  3. Fold branch back into Trunk and release checkout")
	}
	fmt.Fprintln(display, "  0. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, out, "0")
	if err != nil {
		return genericAssistExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return genericAssistAgent, nil
	case "2":
		return genericAssistShell, nil
	case "3":
		if !offerFold {
			fmt.Fprintln(display, "Choose 1, 2, or 0.")
			return promptGenericAssistAction(out, reader, taskSetID, status, invocation, offerFold)
		}
		return genericAssistFold, nil
	case "0", "q", "quit", "exit":
		return genericAssistExit, nil
	default:
		if offerFold {
			fmt.Fprintln(display, "Choose 1, 2, 3, or 0.")
		} else {
			fmt.Fprintln(display, "Choose 1, 2, or 0.")
		}
		return promptGenericAssistAction(out, reader, taskSetID, status, invocation, offerFold)
	}
}

func assistFoldEligible(d *Deps, setID string, status TaskSetStatus) bool {
	return FoldEligibleStatus(status) && stillHasWorktreeBinding(d, setID)
}

func stillHasWorktreeBinding(d *Deps, setID string) bool {
	if d == nil {
		return false
	}
	s, ok, err := d.Store(false)
	if err != nil || !ok {
		return false
	}
	all, err := s.AllBindings()
	if err != nil {
		return false
	}
	for key, b := range all {
		parts := strings.Split(key, "\x00")
		if len(parts) != 2 || parts[1] != setID {
			continue
		}
		if strings.TrimSpace(b.RuntimePath) != "" {
			return true
		}
	}
	return false
}
