package tasks

import (
	"fmt"
	"io"
	"strings"
)

const (
	DirtyRuntimeContinue          DirtyRuntimeStrategy = "continue"
	DirtyRuntimeCommitAndContinue DirtyRuntimeStrategy = "commit-and-continue"
	DirtyRuntimeStashAndContinue  DirtyRuntimeStrategy = "stash-and-continue"
)

// DirtyRuntimeStrategy controls how a dirty runtime checkout is prepared for execution.
type DirtyRuntimeStrategy string

// Set validates and assigns a dirty-runtime strategy for Cobra flag parsing.
func (s *DirtyRuntimeStrategy) Set(value string) error {
	switch DirtyRuntimeStrategy(value) {
	case DirtyRuntimeContinue, DirtyRuntimeCommitAndContinue, DirtyRuntimeStashAndContinue:
		*s = DirtyRuntimeStrategy(value)
		return nil
	default:
		return fmt.Errorf("invalid dirty-runtime strategy %q; valid candidates: %s", value, strings.Join(ValidDirtyRuntimeStrategies(), ", "))
	}
}

func (s DirtyRuntimeStrategy) String() string { return string(s) }

func (s DirtyRuntimeStrategy) Type() string { return "dirty-runtime-strategy" }

// ValidDirtyRuntimeStrategies returns the accepted --allow-dirty values.
func ValidDirtyRuntimeStrategies() []string {
	return []string{
		string(DirtyRuntimeContinue),
		string(DirtyRuntimeCommitAndContinue),
		string(DirtyRuntimeStashAndContinue),
	}
}

func runtimeIsDirty(d *Deps, runtimePath string) (bool, error) {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// resolveDirtyRuntimeStrategy treats an unset strategy as the continue default.
func resolveDirtyRuntimeStrategy(strategy DirtyRuntimeStrategy) DirtyRuntimeStrategy {
	if strategy == "" {
		return DirtyRuntimeContinue
	}
	return strategy
}

func validateDirtyRuntimeStrategy(strategy DirtyRuntimeStrategy) error {
	if strategy == "" {
		return nil
	}
	var parsed DirtyRuntimeStrategy
	return parsed.Set(string(strategy))
}

// dirtyStrategyEffect describes, in one sentence, what the strategy will do to a
// dirty runtime checkout. Surfaced in the dirty-runtime confirmation.
func dirtyStrategyEffect(strategy DirtyRuntimeStrategy) string {
	switch strategy {
	case DirtyRuntimeCommitAndContinue:
		return "Strategy commit-and-continue: a checkpoint commit capturing this dirty state will be created before execution."
	case DirtyRuntimeStashAndContinue:
		return "Strategy stash-and-continue: tracked and untracked changes will be stashed before execution; restore the stash manually when ready."
	default:
		return "Strategy continue: execution proceeds without modifying these changes."
	}
}

// reportDirtyRuntime prints git status for the dirty runtime checkout followed by
// the chosen strategy's effect, so the operator can confirm with full context.
func reportDirtyRuntime(d *Deps, w io.Writer, runtimePath string, strategy DirtyRuntimeStrategy) error {
	status, err := d.Git.CommandInDir(runtimePath, "status")
	if err != nil {
		return err
	}
	out := outputFor(w)
	fmt.Fprintln(out)
	out.line(ansiYellow, "Runtime checkout has uncommitted changes:")
	fmt.Fprintln(out)
	fmt.Fprint(out, status)
	if !strings.HasSuffix(status, "\n") {
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out)
	out.line(ansiYellow, "%s", dirtyStrategyEffect(strategy))
	return nil
}

func applyDirtyRuntimeStrategy(d *Deps, runtimePath, taskSetID, taskID string, strategy DirtyRuntimeStrategy, commitOverrides []string, out io.Writer) error {
	switch strategy {
	case DirtyRuntimeContinue:
		return nil
	case DirtyRuntimeCommitAndContinue:
		return checkpointDirtyRuntime(d, runtimePath, taskSetID, taskID, commitOverrides)
	case DirtyRuntimeStashAndContinue:
		return stashDirtyRuntime(d, runtimePath, out)
	default:
		return validateDirtyRuntimeStrategy(strategy)
	}
}

// commitGitArgs prepends `-c key=value` pairs (one per configured commit-config
// override) before a git subcommand's arguments. With no overrides it returns
// args unchanged, so unconfigured commits are byte-for-byte identical to today.
func commitGitArgs(overrides []string, args ...string) []string {
	if len(overrides) == 0 {
		return args
	}
	out := make([]string, 0, len(overrides)*2+len(args))
	for _, kv := range overrides {
		out = append(out, "-c", kv)
	}
	return append(out, args...)
}

func checkpointDirtyRuntime(d *Deps, runtimePath, taskSetID, taskID string, commitOverrides []string) error {
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return err
	}
	staged, err := d.Git.CommandInDir(runtimePath, "diff", "--cached", "--name-only")
	if err != nil {
		_, _ = d.Git.CommandInDir(runtimePath, "reset")
		return err
	}
	if strings.TrimSpace(staged) == "" {
		return nil
	}
	subject := DirtyCheckpointSubject(taskSetID, taskID)
	if _, err := d.Git.CommandInDir(runtimePath, commitGitArgs(commitOverrides, "commit", "-m", subject)...); err != nil {
		_, _ = d.Git.CommandInDir(runtimePath, "reset")
		return err
	}
	return nil
}

func stashDirtyRuntime(d *Deps, runtimePath string, out io.Writer) error {
	before, _ := d.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "refs/stash")
	if _, err := d.Git.CommandInDir(runtimePath, "stash", "push", "--include-untracked"); err != nil {
		return err
	}
	after, err := d.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "refs/stash")
	if err != nil || strings.TrimSpace(after) == strings.TrimSpace(before) {
		return nil
	}
	outputFor(out).line(ansiYellow, "Created stash: stash@{0}. Restore it manually when ready.")
	return nil
}
