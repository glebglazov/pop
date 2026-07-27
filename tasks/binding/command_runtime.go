package binding

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// ResolveCommandRuntime resolves the Runtime path for a set-scoped command under
// Binding-first runtime resolution (ADR-0146): an explicit override wins; else
// the set's Worktree binding when bound; else the current checkout. It is the
// single named seam `pop tasks verify` (accept / remediate / re-run) and
// `pop tasks status` route through so every surface reads and writes at the
// same checkout's HEAD.
func ResolveCommandRuntime(td *tasks.Deps, currentCheckout, setID, override string) (string, error) {
	if td == nil {
		return "", fmt.Errorf("missing task dependencies")
	}
	if o := strings.TrimSpace(override); o != "" {
		return tasks.ResolveRuntimePathWith(td, currentCheckout, o)
	}
	current, err := tasks.ResolveRuntimePathWith(td, currentCheckout, "")
	if err != nil {
		return "", err
	}
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return current, nil
	}
	_, b, ok, err := GetForSet(td, current, setID)
	if err != nil {
		return "", err
	}
	if ok && strings.TrimSpace(b.RuntimePath) != "" {
		return tasks.ResolveRuntimePathWith(td, b.RuntimePath, "")
	}
	return current, nil
}

// CommandRuntimeResolver returns a Binding-first per-set runtime resolver
// (ADR-0146) for status surfaces that share one current checkout as the unbound
// fallback. The normalized current checkout is also returned for overview
// runtime-lock and checkout badges.
func CommandRuntimeResolver(td *tasks.Deps, currentCheckout string) (func(setID string) string, string, error) {
	if td == nil {
		return nil, "", fmt.Errorf("missing task dependencies")
	}
	current, err := tasks.ResolveRuntimePathWith(td, currentCheckout, "")
	if err != nil {
		return nil, "", err
	}
	id, err := tasks.ResolveRepositoryIdentity(td, current)
	if err != nil {
		return nil, "", err
	}
	repoKey := RepoKey(id)
	bindings, err := AllBindings(td)
	if err != nil {
		return nil, "", err
	}
	return func(setID string) string {
		path := RuntimeForSet(bindings, repoKey, setID, current)
		if path == current {
			return current
		}
		normalized, err := tasks.ResolveRuntimePathWith(td, path, "")
		if err != nil {
			return path
		}
		return normalized
	}, current, nil
}
