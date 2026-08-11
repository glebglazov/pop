package tasks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// ErrFoldRetry signals that the operator chose "Retry fold from scratch" at the
// Fold conflict prompt: the in-flight rebase was aborted and Fold should restart
// from preflight (status, dirty, claim, trunk HEAD re-read). Fold itself still
// never fetches.
var ErrFoldRetry = errors.New("fold: retry from preflight")

// FoldConflictContext carries the identity and git context for a fold rebase
// conflict inside the set's own checkout.
type FoldConflictContext struct {
	SetID       string
	Manifest    *Manifest
	RuntimePath string
	SetBranch   string
	TrunkBranch string
	TrunkPath   string
}

// FoldConflictAssistanceOptions configures an attended fold-conflict session.
type FoldConflictAssistanceOptions struct {
	AgentPreset string
	AgentCmd    string
	In          io.Reader
	Out         io.Writer
	// RunVerifier is a test seam for Verify set / post-resolve verify; nil uses
	// the real Verifier path behind verifyResolvedSet.
	RunVerifier func(prompt string) (string, error)
}

// HandleFoldConflict runs when rebasing the set branch onto trunk left a
// conflict with the rebase in progress — including when Fold re-enters a parked
// operation. It loops the Fold conflict prompt until the rebase completes, the
// operator retries from scratch, exits, or verify fails. Returns nil when the
// rebase completed (conflicts resolved and continued); the caller may continue
// the fold fast-forward. Returns ErrFoldRetry when the operator aborted and
// asked to restart Fold from preflight.
func HandleFoldConflict(d *Deps, cfg *config.Config, ctx FoldConflictContext, opts FoldConflictAssistanceOptions) error {
	if d == nil {
		d = defaultDeps
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	if !canPrompt(in) {
		return foldConflictRefusal(d, ctx.RuntimePath)
	}

	// An explicit --agent on the fold is the human naming their own attended
	// agent for this session; empty resolves to the attended group (ADR-0195).
	agentOverride := strings.TrimSpace(opts.AgentPreset)

	conflicted, err := listConflictedPaths(d, ctx.RuntimePath)
	if err != nil {
		return fmt.Errorf("fold refused: list conflicted paths: %w", err)
	}

	prompt := BuildFoldConflictPrompt(d, ctx, conflicted)
	invocation, err := ResolveAgentAssistanceInvocation(d, cfg, agentOverride, opts.AgentCmd, prompt, ctx.RuntimePath)
	if err != nil {
		return fmt.Errorf("fold refused: %w", err)
	}

	reader := newPromptReader(in)
	for {
		badge := foldConflictVerifiedBadge(d, cfg, ctx.SetID, ctx.RuntimePath)
		action, err := promptFoldConflictAction(out, in, reader, d, cfg, ctx.SetID, badge, invocation)
		if err != nil {
			return err
		}
		switch action {
		case foldConflictAgent:
			invocation, err = ResolveAgentAssistanceInvocation(d, cfg, agentOverride, opts.AgentCmd, prompt, ctx.RuntimePath)
			if err != nil {
				return fmt.Errorf("fold refused: %w", err)
			}
			fmt.Fprintf(outputFor(out), "Starting fold conflict assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, ctx.RuntimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start fold conflict assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "Fold conflict assistance exited with status %d.\n", exitCode)
			}
			conflicted, err = listConflictedPaths(d, ctx.RuntimePath)
			if err != nil {
				return fmt.Errorf("fold refused: list conflicted paths: %w", err)
			}
			prompt = BuildFoldConflictPrompt(d, ctx, conflicted)
			invocation, err = ResolveAgentAssistanceInvocation(d, cfg, agentOverride, opts.AgentCmd, prompt, ctx.RuntimePath)
			if err != nil {
				return fmt.Errorf("fold refused: %w", err)
			}
			if err := foldRebaseCompleted(d, ctx.RuntimePath, ctx.TrunkBranch); err != nil {
				// Still unresolved — re-prompt rather than refuse once.
				continue
			}
			return offerFoldPostResolveVerify(d, cfg, ctx, opts, out, reader)
		case foldConflictResume:
			if err := foldResumeRebase(d, ctx.RuntimePath, out); err != nil {
				fmt.Fprintf(outputFor(out), "Resume fold: %v\n", err)
			}
			if err := foldRebaseCompleted(d, ctx.RuntimePath, ctx.TrunkBranch); err != nil {
				continue
			}
			return offerFoldPostResolveVerify(d, cfg, ctx, opts, out, reader)
		case foldConflictRetry:
			if _, err := d.Git.CommandInDir(ctx.RuntimePath, "rebase", "--abort"); err != nil {
				return fmt.Errorf("fold refused: abort rebase for retry: %w", err)
			}
			fmt.Fprintln(outputFor(out), "Aborted in-flight rebase; retrying fold from preflight.")
			return ErrFoldRetry
		case foldConflictVerify:
			if err := runFoldSetVerify(d, cfg, ctx, opts, out); err != nil {
				return err
			}
			continue
		case foldConflictExit:
			return foldConflictRefusal(d, ctx.RuntimePath)
		}
	}
}

type foldConflictAction int

const (
	foldConflictAgent foldConflictAction = iota
	foldConflictResume
	foldConflictRetry
	foldConflictVerify
	foldConflictExit
)

func promptFoldConflictAction(out io.Writer, in io.Reader, reader *promptReader, d *Deps, cfg *config.Config, setID string, badge VerifiedAtBadge, invocation *AgentAssistanceInvocation) (foldConflictAction, error) {
	var preamble []string
	if text := VerifiedAtBadgeText(badge); text != "" {
		preamble = append(preamble, "  "+text)
	}
	spec := ui.GateMenuSpec{
		Headline: fmt.Sprintf("Fold conflict: %s needs its branch rebased onto trunk.", setID),
		Tone:     ui.GateMenuToneDefault,
		Preamble: preamble,
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Agent assistance (default)", Details: gateInvocationDetails(invocation), Default: true, Assists: true},
			{Key: "2", Label: "Resume fold"},
			{Key: "3", Label: "Retry fold from scratch"},
			{Key: "4", Label: "Verify set"},
			{Key: "0", Label: "Exit"},
		},
	}
	choice, _, err := promptGateMenu(out, in, reader, spec, nil, cfg)
	if err != nil {
		return foldConflictExit, err
	}
	switch choice {
	case "1":
		return foldConflictAgent, nil
	case "2":
		return foldConflictResume, nil
	case "3":
		return foldConflictRetry, nil
	case "4":
		return foldConflictVerify, nil
	default:
		return foldConflictExit, nil
	}
}

// BuildFoldConflictPrompt generates the attended-agent prompt for resolving a
// fold rebase conflict inside the set checkout only.
func BuildFoldConflictPrompt(d *Deps, ctx FoldConflictContext, conflicted []string) string {
	if d == nil {
		d = defaultDeps
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human resolving a Pop fold rebase conflict.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", ctx.SetID)
	if ctx.Manifest != nil {
		fmt.Fprintf(&b, "Task set path: %s\n", ctx.Manifest.Dir)
	}
	fmt.Fprintf(&b, "Set checkout (resolve here): %s\n", ctx.RuntimePath)
	fmt.Fprintf(&b, "Set branch: %s\n", ctx.SetBranch)
	fmt.Fprintf(&b, "Trunk branch rebasing onto: %s\n", ctx.TrunkBranch)
	fmt.Fprintf(&b, "Trunk worktree (read-only boundary): %s\n", ctx.TrunkPath)
	fmt.Fprintf(&b, "\n")

	if len(conflicted) == 0 {
		fmt.Fprintf(&b, "Conflicted paths: (none currently listed — rebase may still be in progress)\n\n")
	} else {
		fmt.Fprintf(&b, "Conflicted paths:\n")
		for _, p := range conflicted {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		fmt.Fprintf(&b, "\n")
	}

	if ctx.Manifest != nil && ctx.Manifest.Valid {
		fmt.Fprintf(&b, "Task context (what this work was meant to do):\n")
		for _, task := range ctx.Manifest.Tasks {
			fmt.Fprintf(&b, "- %s [%s %s]", task.ID, task.Type, task.Status)
			if task.Title != "" {
				fmt.Fprintf(&b, " %s", task.Title)
			}
			fmt.Fprintf(&b, " (%s)\n", filepath.Join(ctx.Manifest.Dir, task.File))
		}
		fmt.Fprintf(&b, "\n")
		appendTaskWhatToBuild(d, &b, ctx.Manifest)
	}

	fmt.Fprintf(&b, "Hard boundary: resolve inside the set checkout only. Never check out, edit, rebase, merge into, or commit on the Trunk worktree at %s.\n", ctx.TrunkPath)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Operations you may perform:\n")
	fmt.Fprintf(&b, "- Resolve conflict markers in the conflicted paths under the set checkout.\n")
	fmt.Fprintf(&b, "- Stage resolved paths and run `git rebase --continue` in this checkout to finish rebasing the set branch onto trunk.\n")
	fmt.Fprintf(&b, "- Never touch the Trunk worktree (%s).\n", ctx.TrunkPath)
	fmt.Fprintf(&b, "- Never push.\n")
	return b.String()
}

func appendTaskWhatToBuild(d *Deps, b *strings.Builder, m *Manifest) {
	if d == nil || m == nil {
		return
	}
	fs := d.FS
	if fs == nil {
		fs = DefaultDeps().FS
	}
	for _, task := range m.Tasks {
		data, err := fs.ReadFile(filepath.Join(m.Dir, task.File))
		if err != nil {
			continue
		}
		body := strings.TrimRight(string(data), "\n")
		if body == "" {
			continue
		}
		fmt.Fprintf(b, "--- %s ---\n%s\n\n", task.File, body)
	}
}

func listConflictedPaths(d *Deps, checkoutPath string) ([]string, error) {
	out, err := d.Git.CommandInDir(checkoutPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		// No unmerged paths is a normal outcome when conflicts were cleared.
		if strings.TrimSpace(out) == "" {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func foldResumeRebase(d *Deps, setPath string, out io.Writer) error {
	// core.editor=true accepts the replayed commit message without opening a TTY
	// editor — resume is a continue, not a re-author.
	_, err := d.Git.CommandInDir(setPath, "-c", "core.editor=true", "rebase", "--continue")
	if err != nil {
		return err
	}
	fmt.Fprintln(outputFor(out), "Resumed rebase.")
	return nil
}

func offerFoldPostResolveVerify(d *Deps, cfg *config.Config, ctx FoldConflictContext, opts FoldConflictAssistanceOptions, out io.Writer, reader *promptReader) error {
	display := outputFor(out)
	fmt.Fprintln(display)
	fmt.Fprintln(display, "Rebase resolved. Verify the set before fast-forwarding trunk?")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Verify set? [y/N]: "))
	answer, err := readPromptLine(reader, out, "n")
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return runFoldSetVerify(d, cfg, ctx, opts, out)
	default:
		return nil
	}
}

func runFoldSetVerify(d *Deps, cfg *config.Config, ctx FoldConflictContext, opts FoldConflictAssistanceOptions, out io.Writer) error {
	id, err := ResolveRepositoryIdentity(d, ctx.RuntimePath)
	if err != nil {
		return fmt.Errorf("fold refused: verify resolve repository: %w", err)
	}
	defPath, err := CanonicalDefinitionPathWith(d, id.TasksDir)
	if err != nil {
		return fmt.Errorf("fold refused: verify resolve task storage: %w", err)
	}
	m := ctx.Manifest
	if m == nil || !m.Valid {
		loaded, loadErr := loadVerifiableManifest(d, verifyCoreOptions{
			DefPath: defPath,
			SetID:   ctx.SetID,
		})
		if loadErr != nil {
			return fmt.Errorf("fold refused: verify load set: %w", loadErr)
		}
		m = loaded
	}
	res, err := verifyResolvedSet(d, cfg, verifyCoreOptions{
		Repo:        id.CommonDir,
		DefPath:     defPath,
		RuntimePath: ctx.RuntimePath,
		SetID:       ctx.SetID,
		Output:      out,
		runVerifier: opts.RunVerifier,
	})
	if err != nil {
		return fmt.Errorf("fold refused: verify failed: %w", err)
	}
	if res.Verdict != VerdictPass {
		return fmt.Errorf("fold refused: verify returned %s (trunk unchanged)", res.Verdict)
	}
	return nil
}

func foldConflictVerifiedBadge(d *Deps, cfg *config.Config, setID, runtimePath string) VerifiedAtBadge {
	if d == nil || strings.TrimSpace(setID) == "" || strings.TrimSpace(runtimePath) == "" {
		return VerifiedAtBadge{}
	}
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return VerifiedAtBadge{}
	}
	defPath, err := CanonicalDefinitionPathWith(d, id.TasksDir)
	if err != nil {
		return VerifiedAtBadge{}
	}
	refresh, err := RefreshWith(d, defPath, StatePathFor(defPath))
	if err != nil {
		return VerifiedAtBadge{}
	}
	ApplyVerifyVerdicts(d, refresh, cfg, runtimePath)
	row := FindRow(refresh, setID)
	if row == nil {
		return VerifiedAtBadge{}
	}
	return DeriveVerifiedAtBadge(*row)
}

func foldRebaseCompleted(d *Deps, setPath, trunkBranch string) error {
	if rebaseInProgressBinding(d, setPath) {
		return foldConflictRefusal(d, setPath)
	}
	if !trunkIsAncestorOfHEAD(d, setPath, trunkBranch) {
		return foldConflictRefusal(d, setPath)
	}
	return nil
}

func foldConflictRefusal(d *Deps, setPath string) error {
	if rebaseInProgressBinding(d, setPath) {
		return fmt.Errorf("fold refused: conflict rebasing the set's branch onto trunk (trunk unchanged); rebase still in progress in %s", setPath)
	}
	return fmt.Errorf("fold refused: conflict rebasing the set's branch onto trunk (trunk unchanged)")
}

func rebaseInProgressBinding(d *Deps, path string) bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := d.Git.CommandInDir(path, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(out)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func trunkIsAncestorOfHEAD(d *Deps, setPath, trunkBranch string) bool {
	_, err := d.Git.CommandInDir(setPath, "merge-base", "--is-ancestor", trunkBranch, "HEAD")
	return err == nil
}
