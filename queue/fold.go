package queue

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/binding"
)

// FoldOptions controls confirmation for managed-worktree teardown after fold.
type FoldOptions = binding.FoldOptions

// FoldResult describes a successful fold.
type FoldResult = binding.FoldResult

// FoldSet folds a task set through the same implementation as `pop tasks fold`.
func FoldSet(d *Deps, cfg *config.Config, ref SetRef, out io.Writer, opts FoldOptions) (FoldResult, error) {
	d = ensureQueueDeps(d)
	return binding.Fold(d.Tasks, d.Project, cfg, ref.SetID, opts, queueFoldHooks(d), out)
}

// PreflightFold applies fold precondition checks for a dashboard row without
// rebasing or releasing its binding. A fold rebase left in progress is allowed
// through so the dashboard can shell out to Fold and re-enter the conflict prompt.
func PreflightFold(d *Deps, cfg *config.Config, ref SetRef) error {
	d = ensureQueueDeps(d)
	return binding.PreflightFold(d.Tasks, cfg, ref.SetID)
}

func queueFoldHooks(d *Deps) binding.LifecycleHooks {
	return binding.LifecycleHooks{ReadLock: d.readLock}
}

// foldExecCommand builds `pop tasks fold <set>` for the dashboard ExecProcess
// path so teardown confirmation and fold-conflict assistance use a real TTY.
func foldExecCommand(row DashboardRow) *exec.Cmd {
	args := []string{"tasks", "fold", row.SetID}
	cmd := exec.Command("pop", args...)
	wd := strings.TrimSpace(row.ProjectPath)
	if wd == "" {
		wd = strings.TrimSpace(row.RuntimePath)
	}
	if wd != "" {
		cmd.Dir = wd
	}
	return cmd
}

func foldExecError(stderr string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(stderr)
	if msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}
