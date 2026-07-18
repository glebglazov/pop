package queue

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// WorktreeBinding is the durable checkout record store.Binding IS — the sole
// Worktree-binding type in the codebase (ADR-0118). The alias keeps queue call
// sites and tests referring to it unchanged.
type WorktreeBinding = store.Binding

// bindingProvisioned reports whether the binding stored under key was
// provisioned by pop (safe to teardown) or adopted (must not delete).
func bindingProvisioned(d *tasks.Deps, key string) bool {
	return binding.Provisioned(d, key)
}

// bindingShouldTeardown reports whether the worktree at key should be torn
// down. It returns true when there is no binding (legacy/unknown — pop probably
// created it) or when the binding is explicitly provisioned. It returns false
// only for explicitly adopted bindings (Provisioned=false), which must never
// have their directories deleted.
func bindingShouldTeardown(d *tasks.Deps, key string) bool {
	return binding.ShouldTeardown(d, key)
}

// bindingForSet returns the shared-store binding for (repoKey, setID), read as a
// single keyed store row (ADR-0118).
func bindingForSet(d *tasks.Deps, repoKey, setID string) (WorktreeBinding, bool) {
	if d == nil {
		return WorktreeBinding{}, false
	}
	b, ok, err := binding.Lookup(d, setScopedKey(repoKey, setID))
	if err != nil {
		return WorktreeBinding{}, false
	}
	return b, ok
}

// repoIdentityKey returns the repository identity prefix used in set-scoped keys.
func repoIdentityKey(id *tasks.RepositoryIdentity) string {
	return binding.RepoKey(id)
}

// setScopedKey keys set-scoped state by repository identity plus set id.
func setScopedKey(repoKey, setID string) string {
	return binding.ScopedKey(repoKey, setID)
}

// repoIdentityFromWorktreePath extracts basename-shortHash from a queue worktree path.
func repoIdentityFromWorktreePath(path string) string {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(os.PathSeparator))
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "worktrees" {
			return parts[i+1]
		}
	}
	return ""
}
