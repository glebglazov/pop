package binding

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

const rebindProgressPrompt = "Rebind %s (%s) away from its binding? The drain will resume in a checkout that lacks that work, so it will most likely restart from the wrong task. [y/N]: "

const implementRebindNonInteractiveErr = "non-interactive implement requires --yes to rebind away from binding when the set has progress"

const implementRebindTeardownNonInteractiveErr = "non-interactive implement requires --yes to delete managed worktree when rebinding"

// CheckoutPathsDiffer reports whether two checkout paths resolve to different
// canonical locations after symlink evaluation.
func CheckoutPathsDiffer(td *tasks.Deps, a, b string) (bool, error) {
	if td == nil {
		return false, fmt.Errorf("missing task dependencies")
	}
	oldCanon, err := canonicalCheckoutPath(td, a)
	if err != nil {
		return false, err
	}
	newCanon, err := canonicalCheckoutPath(td, b)
	if err != nil {
		return false, err
	}
	return oldCanon != newCanon, nil
}

// AuthorizeLeavingBinding confirms moving a Task set off its Worktree binding.
// Both plain --force-rebind and --in-worktree retarget pass through this seam
// (ADR-0151): when started is true (at least one done task), the operator is
// warned that the drain will likely restart from the wrong task; declining
// returns (false, nil). On success it runs the reference-counted managed
// checkout teardown prompt for the vacated binding, separate from and after
// the progress prompt.
func AuthorizeLeavingBinding(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setID string, old Binding, leavingKey string, started bool, progress string, yes bool, in io.Reader, out io.Writer, hooks LifecycleHooks) (bool, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return false, fmt.Errorf("set id is required")
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}
	if out == nil {
		out = io.Discard
	}

	confirmIn := in
	if confirmIn != nil {
		if _, ok := confirmIn.(tasks.NonInteractiveReader); !ok {
			confirmIn = &lineReader{br: bufio.NewReader(confirmIn), orig: confirmIn}
		}
	}

	lock := readRuntimeLock(hooks, old.RuntimePath)
	if lock != nil && lock.Locked && !lockedByAnotherSet(lock, setID) {
		return false, fmt.Errorf("refusing implement: %s is currently executing", setID)
	}

	if started {
		progressLabel := strings.TrimSpace(progress)
		if progressLabel == "" {
			progressLabel = "in progress"
		}
		prompt := fmt.Sprintf(rebindProgressPrompt, setID, progressLabel)
		confirmed, err := confirmYesNo(confirmIn, out, yes, prompt, implementRebindNonInteractiveErr)
		if err != nil {
			return false, err
		}
		if !confirmed {
			return false, nil
		}
	}

	if err := maybeTeardownReboundCheckout(td, pd, cfg, old, leavingKey, yes, confirmIn, out, implementRebindTeardownNonInteractiveErr, hooks); err != nil {
		return false, err
	}
	return true, nil
}

func checkoutCanonOrMissing(td *tasks.Deps, path string) (string, error) {
	canon, err := canonicalCheckoutPath(td, path)
	if err != nil {
		if _, statErr := td.FS.Stat(path); statErr != nil {
			return "", nil
		}
		return "", err
	}
	return canon, nil
}

// RebindForegroundCheckout re-points an idle binding to checkoutPath after
// AuthorizeLeavingBinding. It is a no-op when old and new checkouts are
// already the same path. When adopt is false the binding row is not written —
// callers that provision a new managed worktree (--in-worktree retarget) delete
// the old row and bind the provisioned checkout themselves.
func RebindForegroundCheckout(td *tasks.Deps, pd *project.Deps, cfg *config.Config, key, checkoutPath, setID string, old Binding, adopt bool) (Binding, bool, error) {
	if td == nil {
		return Binding{}, false, fmt.Errorf("missing task dependencies")
	}
	checkoutPath = strings.TrimSpace(checkoutPath)
	if checkoutPath == "" {
		return Binding{}, false, fmt.Errorf("checkout path is required")
	}

	oldCanon, err := checkoutCanonOrMissing(td, old.RuntimePath)
	if err != nil {
		return Binding{}, false, err
	}
	newCanon, err := canonicalCheckoutPath(td, checkoutPath)
	if err != nil {
		return Binding{}, false, err
	}
	if oldCanon != "" && oldCanon == newCanon {
		return old, false, nil
	}

	if !adopt {
		return old, true, nil
	}

	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return Binding{}, false, err
	}
	runtimePath, err := tasks.ResolveRuntimePathWith(td, checkoutPath, "")
	if err != nil {
		return Binding{}, false, err
	}
	b := Adopt(td, runtimePath, CurrentBranch(td, runtimePath), DetectProject(pd, td, cfg, id))
	if err := Put(td, key, b); err != nil {
		return Binding{}, false, err
	}
	return b, true, nil
}
