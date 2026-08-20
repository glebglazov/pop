package tasks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/prompt"
	"github.com/glebglazov/pop/ui"
)

// ErrFoldRetry signals that the operator chose "Retry fold from scratch" at the
// Fold conflict prompt: the in-flight rebase was aborted and Fold should restart
// from preflight (status, dirty, claim, trunk HEAD re-read). Fold itself still
// never fetches.
var ErrFoldRetry = errors.New("fold: retry from preflight")

// ErrFoldAbandon signals that the operator chose "Abandon fold" at the Fold
// conflict prompt: the in-flight rebase was aborted and the fold must be rolled
// back to the state it found — nothing landed, and nothing is left parked. It is
// deliberately not "Exit": exit stops for now and leaves the rebase in progress
// for a later fold to resume, and walking away from a fold is a different
// intention from putting it down (ADR-0229).
var ErrFoldAbandon = errors.New("fold abandoned: rebase aborted, trunk unchanged and nothing rewritten")

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
// asked to restart Fold from preflight, and ErrFoldAbandon when they abandoned
// the fold outright.
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
		case foldConflictAbandon:
			if _, err := d.Git.CommandInDir(ctx.RuntimePath, "rebase", "--abort"); err != nil {
				return fmt.Errorf("fold refused: abort rebase to abandon the fold: %w", err)
			}
			fmt.Fprintln(outputFor(out), "Aborted the in-flight rebase and abandoned the fold; nothing landed.")
			return ErrFoldAbandon
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
	foldConflictAbandon
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
			{Key: "5", Label: "Abandon fold", Details: []string{"abort the rebase, restore the branch, delete the fold scratch branch"}},
			{Key: "0", Label: "Exit", Details: []string{"leave the rebase parked for a later fold to resume"}},
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
	case "5":
		return foldConflictAbandon, nil
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
	view := foldConflictPromptView{
		SetID:              ctx.SetID,
		RuntimePath:        ctx.RuntimePath,
		SetBranch:          ctx.SetBranch,
		TrunkBranch:        ctx.TrunkBranch,
		TrunkPath:          ctx.TrunkPath,
		ConflictedPaths:    conflicted,
		HasConflictedPaths: len(conflicted) > 0,
		NoConflictedPaths:  len(conflicted) == 0,
	}
	if ctx.Manifest != nil {
		view.TaskSetPathKnown = true
		view.TaskSetPath = ctx.Manifest.Dir
	}
	// The task context describes what the conflicting work was meant to do, so
	// it is only offered when the manifest can be trusted to say.
	if ctx.Manifest != nil && ctx.Manifest.Valid {
		view.HasTaskContext = true
		view.Tasks = foldConflictTaskRows(ctx.Manifest)
		view.Bodies = taskWhatToBuildRows(d, ctx.Manifest)
	}
	return prompt.MustRender(promptTemplates, "fold-conflict.tmpl.md", view)
}

// foldConflictPromptView is what the fold-conflict template renders against.
type foldConflictPromptView struct {
	SetID string
	// The task-set path is known only when a manifest came with the context, so
	// the template picks the line rather than rendering an empty one.
	TaskSetPathKnown bool
	TaskSetPath      string
	RuntimePath      string
	SetBranch        string
	TrunkBranch      string
	TrunkPath        string
	// The conflicted-path states are named booleans so the template picks the
	// listing or the sentence that stands in for it.
	ConflictedPaths    []string
	HasConflictedPaths bool
	NoConflictedPaths  bool
	HasTaskContext     bool
	Tasks              []taskRow
	Bodies             []taskWhatToBuildRow
}

// foldConflictTaskRows builds the manifest listing for the shared "task-listing"
// partial. The fold-conflict prompt names no blockers and no effort, so those
// clauses stay empty — the partial ranges over the same row type either way.
func foldConflictTaskRows(m *Manifest) []taskRow {
	rows := make([]taskRow, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		row := taskRow{
			ID:     task.ID,
			Type:   task.Type,
			Status: task.Status,
			Path:   filepath.Join(m.Dir, task.File),
		}
		if task.Title != "" {
			row.TitleClause = " " + task.Title
		}
		rows = append(rows, row)
	}
	return rows
}

// taskWhatToBuildRow is one task's body as the fold-conflict prompt stanzas it:
// headed by the task's file name, not by its full path.
type taskWhatToBuildRow struct {
	File string
	Body string
}

// taskWhatToBuildRows reads each task body the fold-conflict prompt inlines.
// A task whose file cannot be read, or whose body is blank, is left out
// entirely: the agent is here to resolve a rebase, and an empty stanza tells it
// nothing about what the work meant.
func taskWhatToBuildRows(d *Deps, m *Manifest) []taskWhatToBuildRow {
	if d == nil || m == nil {
		return nil
	}
	fs := d.FS
	if fs == nil {
		fs = DefaultDeps().FS
	}
	var rows []taskWhatToBuildRow
	for _, task := range m.Tasks {
		data, err := fs.ReadFile(filepath.Join(m.Dir, task.File))
		if err != nil {
			continue
		}
		body := strings.TrimRight(string(data), "\n")
		if body == "" {
			continue
		}
		rows = append(rows, taskWhatToBuildRow{File: task.File, Body: body})
	}
	return rows
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
