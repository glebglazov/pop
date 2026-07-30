package tasks

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
)

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
}

// HandleFoldConflict runs when rebasing the set branch onto trunk left a
// conflict with the rebase in progress. It offers attended agent assistance on
// a TTY; otherwise it refuses without aborting the rebase. Returns nil when the
// rebase completed (conflicts resolved and continued); the caller may continue
// the fold fast-forward.
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

	agentPreset := strings.TrimSpace(opts.AgentPreset)
	if agentPreset == "" {
		agentPreset = ResolveDefaultInteractiveAgentPreset(cfg)
	}

	conflicted, err := listConflictedPaths(d, ctx.RuntimePath)
	if err != nil {
		return fmt.Errorf("fold refused: list conflicted paths: %w", err)
	}

	prompt := BuildFoldConflictPrompt(d, ctx, conflicted)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, opts.AgentCmd, prompt, ctx.RuntimePath)
	if err != nil {
		return fmt.Errorf("fold refused: %w", err)
	}

	reader := bufio.NewReader(in)
	for {
		action, err := promptFoldConflictAction(out, reader, ctx.SetID, invocation)
		if err != nil {
			return err
		}
		switch action {
		case foldConflictAgent:
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
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, opts.AgentCmd, prompt, ctx.RuntimePath)
			if err != nil {
				return fmt.Errorf("fold refused: %w", err)
			}
			if err := foldRebaseCompleted(d, ctx.RuntimePath, ctx.TrunkBranch); err != nil {
				return err
			}
			return nil
		case foldConflictExit:
			return foldConflictRefusal(d, ctx.RuntimePath)
		}
	}
}

type foldConflictAction int

const (
	foldConflictAgent foldConflictAction = iota
	foldConflictExit
)

func promptFoldConflictAction(out io.Writer, reader *bufio.Reader, setID string, invocation *AgentAssistanceInvocation) (foldConflictAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiCyan, "Fold conflict: %s needs its branch rebased onto trunk.", setID)
	fmt.Fprintln(display, "  1. Agent assistance (default)")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  0. Exit without resolving")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, "0")
	if err != nil {
		return foldConflictExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return foldConflictAgent, nil
	case "0", "q", "quit", "exit":
		return foldConflictExit, nil
	default:
		fmt.Fprintln(display, "Choose 1 or 0.")
		return promptFoldConflictAction(out, reader, setID, invocation)
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
