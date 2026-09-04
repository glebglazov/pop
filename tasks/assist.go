package tasks

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/ui"
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
// The session itself contends for nothing: it reads Task storage and holds no
// claim, so it opens on a set another drain is running, on a set parked at a
// gate, on an archived set and on a set whose manifest will not parse — a broken
// manifest is precisely what a human opens Assist to look at. Exclusivity lives
// on the verbs inside the menu instead: Accept, Remediate and Fold each take the
// Checkout claim for the length of their own act (see gateEnv.holdTreeStill).
//
// What is left to refuse is only what cannot work at all: no interactive
// terminal, a set that is not on disk, and no checkout to open a session in.
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

	target, err := resolveAssistTarget(d, pd, loadConfig, opts)
	if err != nil {
		return err
	}

	cfg, _ := loadConfig(config.DefaultConfigPath())
	// `pop tasks assist --agent` is the human naming their own attended agent for
	// this session; empty resolves to the attended group (ADR-0195).
	agentOverride := strings.TrimSpace(opts.AgentPreset)

	env := gateEnv{
		d:              d,
		out:            out,
		in:             in,
		reader:         newPromptReader(in),
		agentOverride:  agentOverride,
		agentCmd:       opts.AgentCmd,
		cwd:            target.projectPath,
		runtimePath:    target.runtimePath,
		definitionPath: target.definitionPath,
		statePath:      target.statePath,
		taskSetID:      setID,
		cfg:            cfg,
		fold:           opts.Fold,
		treeStable:     assistTreeStable(d, target.runtimePath, setID),
	}

	for {
		m := LoadManifest(d, setID, target.manifestPath)
		status, findings, workSHA := assistDerivedStatus(d, cfg, m, setID, target.runtimePath, target.repo)

		fmt.Fprintln(out)
		fmt.Fprintf(out, "Assist session: %s [%s]\n", setID, status)
		fmt.Fprintf(out, "Runtime: %s\n", target.runtimePath)
		printAssistDiagnostics(d, out, target, setID, m)

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
			handled, gateErr = handleInteractiveVerifyFailedGate(env, target.repo, m, workSHA, findings)
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
	}
}

// assistTarget is the address of the set the session works on: the checkout it
// acts in, and the Task storage it reads. Every field is derived from the
// checkout the set is bound to, never from where the human was standing, so a
// session opened from a sibling checkout of another repository addresses the
// same set the Work dashboard would.
type assistTarget struct {
	runtimePath    string
	projectPath    string
	definitionPath string
	statePath      string
	manifestPath   string
	// repo is the runtime checkout's git common directory — the repository key
	// every Verify verdict is stored under.
	repo string
}

// resolveAssistTarget resolves the set's checkout and its Task storage, and
// refuses only when one of them cannot exist: no checkout to open a session in,
// or a set that is unknown or registered but gone from disk.
func resolveAssistTarget(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts AssistOptions) (assistTarget, error) {
	setID := strings.TrimSpace(opts.TaskSetID)
	if setID == "" {
		return assistTarget{}, exitErr(ExitSetup, "a task set identifier is required")
	}

	// Callers pin Binding-first resolution via ResolveInput.RuntimeOverride
	// (ADR-0146) — the same seam `pop tasks verify` uses — so this package never
	// imports tasks/binding. Where no checkout was named, the current one stands
	// in for it.
	runtimePath := strings.TrimSpace(opts.ResolveInput.RuntimeOverride)
	if runtimePath == "" {
		resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
		if err != nil {
			return assistTarget{}, err
		}
		runtimePath = resolved.ProjectPath
	}
	runtimePath, err := NormalizeRuntimePathWith(d, runtimePath)
	if err != nil {
		return assistTarget{}, err
	}

	// Task storage is read from the set's own checkout: the definition directory
	// follows the repository the checkout belongs to, so a session opened from
	// elsewhere reads the set's manifest rather than looking for it in the
	// repository the human happened to be in.
	definitionPath, err := resolveDefinitionPath(d, runtimePath, opts.ResolveInput.DefinitionOverride)
	if err != nil {
		return assistTarget{}, err
	}
	statePath := StatePathFor(definitionPath)
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return assistTarget{}, err
	}

	disc, err := DiscoverWith(d, definitionPath)
	if err != nil {
		return assistTarget{}, exitErr(ExitOperational, "discover task sets: %v", err)
	}
	manifestPath, ok := disc.Manifests[setID]
	if !ok {
		// Registered-but-gone sets surface as MISSING on refresh.
		if refresh, rerr := RefreshWith(d, definitionPath, statePath); rerr == nil {
			if row := FindRow(refresh, setID); row != nil && row.Status == StatusMissing {
				return assistTarget{}, exitErr(ExitSetup, "task set %q is missing (registered but not on disk)", setID)
			}
		}
		return assistTarget{}, exitErr(ExitSetup, "unknown task set %q", setID)
	}

	return assistTarget{
		runtimePath:    runtimePath,
		projectPath:    runtimePath,
		definitionPath: definitionPath,
		statePath:      statePath,
		manifestPath:   manifestPath,
		repo:           id.CommonDir,
	}, nil
}

// ValidateAssistLaunch resolves the checkout and manifest an Assist session would
// use, applying every refusal the session itself applies except the
// interactive-terminal requirement. The Work dashboard calls it before spawning a
// pane, so a session that cannot open says why in the dashboard instead of dying
// inside a pane the operator then has to go and read.
func ValidateAssistLaunch(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts AssistOptions) (runtimePath, manifestPath string, err error) {
	if d == nil {
		d = defaultDeps
	}
	target, err := resolveAssistTarget(d, pd, loadConfig, opts)
	if err != nil {
		return "", "", err
	}
	return target.runtimePath, target.manifestPath, nil
}

// printAssistDiagnostics states what is wrong with the set the session just
// opened on, in place of the refusals that used to stand where the session now
// stands. A malformed manifest is named error by error: Assist is where a human
// goes to look at one, so the session shows the parse failures and opens the
// menu anyway.
func printAssistDiagnostics(d *Deps, out io.Writer, target assistTarget, setID string, m *Manifest) {
	if assistSetArchived(d, target, setID) {
		fmt.Fprintf(out, "Archived: this set is out of the Work queue.\n")
	}
	if m == nil || m.Valid {
		return
	}
	fmt.Fprintf(out, "Manifest is malformed: %s\n", MalformedSummary(m))
	for _, e := range m.Errors {
		fmt.Fprintf(out, "  - %s\n", e)
	}
}

func assistSetArchived(d *Deps, target assistTarget, setID string) bool {
	state, err := LoadGlobalStateWith(d, target.statePath)
	if err != nil {
		return false
	}
	return taskSetArchived(state, target.definitionPath, setID)
}

// assistTreeStable is how an Assist session lends the checkout to one menu verb.
// Accept, Remediate and Fold move the tree, so each takes the Checkout claim
// when it is chosen and gives it back when it is done — waiting in the Admission
// queue when someone else holds it, which is the wait a human at a terminal
// wants over a refusal they must re-issue by hand (ADR-0239).
func assistTreeStable(d *Deps, runtimePath, setID string) treeStableSeam {
	return func(out io.Writer) (func(), error) {
		hold, err := AcquireTreeStable(d, runtimePath, setID, out, AdmissionWait)
		if err != nil {
			return nil, err
		}
		return func() { _ = hold.Release() }, nil
	}
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

	prompt := BuildAssistPrompt(d, env.cfg, taskSetID, m, status, runtimePath, findings)
	// The agent is resolved when assistance is chosen, never on the way in: a
	// session must open — and show the set — even when every attended agent is
	// cooling or missing, and the walk's refusal belongs in the menu.
	var invocation *AgentAssistanceInvocation
	offerFold := env.fold != nil && assistFoldEligible(d, taskSetID, status)

	for {
		action, err := promptGenericAssistAction(out, in, reader, d, env.cfg, taskSetID, status, invocation, offerFold)
		if err != nil {
			return false, err
		}
		switch action {
		case genericAssistAgent:
			invocation, err = ResolveAgentAssistanceInvocation(d, env.cfg, env.agentOverride, env.agentCmd, prompt, runtimePath)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Assist assistance: %v\n", err)
				continue
			}
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
			prompt = BuildAssistPrompt(d, env.cfg, taskSetID, m, status, runtimePath, findings)
		case genericAssistShell:
			if err := spawnRuntimeShell(d, in, runtimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Could not start shell: %v\n", err)
			}
		case genericAssistFold:
			release, holdErr := env.holdTreeStill(out)
			if holdErr != nil {
				reportTreeStableRefusal(out, "Fold", holdErr)
				continue
			}
			err := env.fold(taskSetID, in, out)
			release()
			if err != nil {
				fmt.Fprintf(outputFor(out), "Fold failed: %v\n", err)
				continue
			}
			return false, nil
		case genericAssistExit:
			return false, nil
		}
	}
}

func promptGenericAssistAction(out io.Writer, in io.Reader, reader *promptReader, d *Deps, cfg *config.Config, taskSetID string, status TaskSetStatus, invocation *AgentAssistanceInvocation, offerFold bool) (genericAssistAction, error) {
	items := []ui.GateMenuItem{
		{Key: "1", Label: "Agent assistance (default)", Details: gateInvocationDetails(invocation), Default: true, Assists: true},
		{Key: "2", Label: "Open a shell in the checkout"},
	}
	if offerFold {
		items = append(items, ui.GateMenuItem{Key: "3", Label: "Fold branch back into Trunk and release checkout"})
	}
	items = append(items, ui.GateMenuItem{Key: "0", Label: "Exit"})

	spec := ui.GateMenuSpec{
		Headline: fmt.Sprintf("Assist: %s is %s.", taskSetID, status),
		Tone:     ui.GateMenuToneDefault,
		Items:    items,
	}
	choice, _, err := promptGateMenu(out, in, reader, spec, nil, cfg)
	if err != nil {
		return genericAssistExit, err
	}
	switch choice {
	case "1":
		return genericAssistAgent, nil
	case "2":
		return genericAssistShell, nil
	case "3":
		if offerFold {
			return genericAssistFold, nil
		}
		return genericAssistExit, nil
	default:
		return genericAssistExit, nil
	}
}

func assistFoldEligible(d *Deps, setID string, status TaskSetStatus) bool {
	bound, provisioned := worktreeBindingFlags(d, setID)
	return Unfolded(bound, provisioned, status)
}

// worktreeBindingFlags reports whether setID holds a Worktree binding with a
// non-blank runtime path, and that binding's Provisioned bit. One AllBindings
// read, no git.
func worktreeBindingFlags(d *Deps, setID string) (bound, provisioned bool) {
	if d == nil {
		return false, false
	}
	s, ok, err := d.Store(false)
	if err != nil || !ok {
		return false, false
	}
	all, err := s.AllBindings()
	if err != nil {
		return false, false
	}
	for key, b := range all {
		parts := strings.Split(key, "\x00")
		if len(parts) != 2 || parts[1] != setID {
			continue
		}
		if strings.TrimSpace(b.RuntimePath) != "" {
			return true, b.Provisioned
		}
	}
	return false, false
}
