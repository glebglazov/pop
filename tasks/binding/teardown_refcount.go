package binding

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// shouldOfferManagedCheckoutTeardown reports whether a confirm-gated managed
// checkout teardown should run for runtimePath: the path must live under the
// managed-worktree root and, after excluding excludeKeys, zero non-archived
// Task sets still bind that checkout (ADR-0116).
func shouldOfferManagedCheckoutTeardown(td *tasks.Deps, runtimePath string, excludeKeys map[string]bool) (bool, error) {
	under, err := checkoutUnderManagedRoot(td, runtimePath)
	if err != nil {
		return false, err
	}
	if !under {
		return false, nil
	}
	n, err := liveReferentCount(td, runtimePath, excludeKeys)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// WorktreeMarkerState is the Worktree picker's three-way classification of a
// checkout for the Unbound managed worktree marker (ADR-0152).
type WorktreeMarkerState int

const (
	// OrdinaryWorktree is a checkout pop did not create.
	OrdinaryWorktree WorktreeMarkerState = iota
	// ManagedBound is a pop-managed checkout with at least one live,
	// non-archived Task set referent.
	ManagedBound
	// ManagedUnbound is a pop-managed checkout with zero live referents —
	// the state the Worktree picker marks (ADR-0152).
	ManagedUnbound
)

// ClassifyManagedWorktree reports runtimePath's marker state for the Worktree
// picker. It delegates to shouldOfferManagedCheckoutTeardown rather than
// re-deriving the rule: "location stays the teardown marker" (ADR-0152) means
// the marker and the teardown offer describe the same fact and must never
// disagree.
func ClassifyManagedWorktree(td *tasks.Deps, runtimePath string) (WorktreeMarkerState, error) {
	under, err := checkoutUnderManagedRoot(td, runtimePath)
	if err != nil {
		return OrdinaryWorktree, err
	}
	if !under {
		return OrdinaryWorktree, nil
	}
	unbound, err := shouldOfferManagedCheckoutTeardown(td, runtimePath, nil)
	if err != nil {
		return OrdinaryWorktree, err
	}
	if unbound {
		return ManagedUnbound, nil
	}
	return ManagedBound, nil
}

// IsManagedCheckout reports whether runtimePath is a checkout pop provisioned —
// the same location-derived question every teardown and marker predicate here
// asks. It exists so a read-only reporter (`pop tasks checkout --json`) can
// state the fact without reaching into the store or re-deriving the roots.
func IsManagedCheckout(td *tasks.Deps, runtimePath string) (bool, error) {
	return checkoutUnderManagedRoot(td, runtimePath)
}

// checkoutUnderManagedRoot reports whether runtimePath is a checkout pop created
// — the Provisioned bit every adoption derives (ADR-0152). It asks the same
// question of every managed root, not just the current one, so a worktree still
// waiting for the gated root move stays managed (and therefore torn down on
// integration) instead of silently reading as adopted for the length of the wait.
func checkoutUnderManagedRoot(td *tasks.Deps, runtimePath string) (bool, error) {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	canonPath, err := canonicalCheckoutPath(td, runtimePath)
	if err != nil {
		return false, err
	}
	for _, root := range ManagedWorktreeRoots(td) {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return false, err
		}
		canonRoot := rootAbs
		if _, err := td.FS.Stat(rootAbs); err == nil {
			canonRoot, err = td.FS.EvalSymlinks(rootAbs)
			if err != nil {
				return false, err
			}
		}
		if canonPath == canonRoot {
			continue
		}
		if strings.HasPrefix(canonPath, canonRoot+string(os.PathSeparator)) {
			return true, nil
		}
	}
	return false, nil
}

func liveReferentCount(td *tasks.Deps, checkoutPath string, excludeKeys map[string]bool) (int, error) {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	targetCanon, err := canonicalCheckoutPath(td, checkoutPath)
	if err != nil {
		return 0, err
	}
	bindings, err := AllBindings(td)
	if err != nil {
		return 0, err
	}
	if len(bindings) == 0 {
		return 0, nil
	}
	state, err := tasks.LoadGlobalStateWith(td, tasks.DefaultStatePathWith(td))
	if err != nil {
		return 0, err
	}
	count := 0
	for key, b := range bindings {
		if excludeKeys != nil && excludeKeys[key] {
			continue
		}
		path := strings.TrimSpace(b.RuntimePath)
		if path == "" {
			continue
		}
		bCanon, err := canonicalCheckoutPath(td, path)
		if err != nil || bCanon != targetCanon {
			continue
		}
		setID := SetIDFromKey(key)
		if setID == "" {
			count++
			continue
		}
		id, err := tasks.ResolveRepositoryIdentity(td, path)
		if err != nil {
			count++
			continue
		}
		defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
		if err != nil {
			count++
			continue
		}
		if taskSetArchived(state, defPath, setID) {
			continue
		}
		count++
	}
	return count, nil
}

func taskSetArchived(state *tasks.GlobalState, defPath, setID string) bool {
	if state == nil {
		return false
	}
	entry := state.Entry(defPath)
	if entry == nil {
		return false
	}
	for _, reg := range entry.TaskSets {
		if reg.ID == setID {
			return reg.Archived
		}
	}
	return false
}

func maybeTeardownReboundCheckout(td *tasks.Deps, pd *project.Deps, cfg *config.Config, old Binding, leavingKey string, yes bool, in io.Reader, out io.Writer, nonInteractiveErr string, hooks LifecycleHooks) error {
	exclude := map[string]bool{leavingKey: true}
	should, err := shouldOfferManagedCheckoutTeardown(td, old.RuntimePath, exclude)
	if err != nil {
		return err
	}
	if !should {
		return nil
	}
	if out == nil {
		out = io.Discard
	}
	confirmed, err := confirmManagedWorktreeDelete(in, out, yes, old.RuntimePath, nonInteractiveErr)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	return TeardownManagedWorktree(td, pd, cfg, old, hooks)
}
