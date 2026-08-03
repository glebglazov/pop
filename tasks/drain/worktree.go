package drain

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// prepareWorktreeDrain routes an actionable drain to its checkout (ADR-0070/0072).
// Routing runs unconditionally: a bound set resumes at its bound worktree, and an
// unbound set carrying a managed intent provisions a worktree forked from the
// Trunk worktree and binds it — the one place routing provisions. Unbound sets
// with no intent never reach here: the queue-drainable check faults them before
// dispatch, so routing has no in-place fallback to invent a checkout.
//
// It provisions, so it belongs to the dispatch phase and not the candidate read.
// A refusal comes back as a non-actionable Decision plus the line explaining it,
// for the caller to report — routing never prints on its own.
func prepareWorktreeDrain(d *Deps, dec Decision) (Decision, string) {
	if !dec.Actionable() {
		return dec, ""
	}
	var cfg *config.Config
	if d.LoadConfig != nil {
		cfg, _ = d.LoadConfig(config.DefaultConfigPath())
	}
	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              d.Tasks,
		PD:              d.Project,
		Config:          cfg,
		Now:             d.now(),
		CurrentCheckout: dec.scan.ProjectPath,
		SetID:           dec.TaskSetID,
		Trigger:         binding.TriggerQueueSpawn,
	})
	if err != nil {
		setID := dec.TaskSetID
		dec.TaskSetID = ""
		if errors.Is(err, binding.ErrBoundWorktreeInvalid) {
			dec.Reason = "bound worktree invalid"
			return dec, fmt.Sprintf("work: %s: bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", dec.Project, setID, err)
		}
		dec.Reason = "route"
		return dec, fmt.Sprintf("work: %s: route drain for %s: %v", dec.Project, setID, err)
	}
	dec.scan.ProjectPath = route.RuntimePath
	dec.scan.RuntimePath = route.RuntimePath
	dec.pinRuntimePath = true
	return dec, ""
}

func validateBoundWorktree(d *Deps, projectPath string, b WorktreeBinding) error {
	return binding.ValidateBoundWorktree(d.Tasks, projectPath, b)
}

func worktreeRegistered(d *Deps, projectPath, checkoutPath string) (bool, error) {
	out, err := d.Tasks.Git.CommandInDir(projectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("list worktrees: %w", err)
	}
	canonCheckout, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		return false, fmt.Errorf("canonicalize checkout: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		wtPath := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		canonWT, err := canonicalCheckoutPath(d.Tasks, wtPath)
		if err != nil {
			continue
		}
		if canonWT == canonCheckout {
			return true, nil
		}
	}
	return false, nil
}

func canonicalCheckoutPath(d *tasks.Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return d.FS.EvalSymlinks(abs)
}

// splitLines splits tmux output into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
