package binding

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// DrainTrigger identifies who is resolving drain checkout routing.
type DrainTrigger int

const (
	TriggerImplementForeground DrainTrigger = iota
	TriggerQueueSpawn
)

// RouteDrainCheckoutRequest carries inputs for drain checkout resolution.
// Precedence: existing Worktree binding → runtime-path override → registration
// worktree-intent → current checkout (ADR-0059). The directive step is the only
// place routing provisions a worktree, and only on the first unbound drain of a
// set whose registration carries one; every other path resolves an existing
// checkout. Config, PD, and Now are consulted only by that directive step (to
// resolve the Trunk worktree, label the binding, and stamp the provisioned
// branch); a request that never hits it may leave them zero.
type RouteDrainCheckoutRequest struct {
	TD              *tasks.Deps
	PD              *project.Deps
	Config          *config.Config
	Now             time.Time
	CurrentCheckout string
	SetID           string
	Trigger         DrainTrigger
	RuntimeOverride string
}

// RouteDrainCheckoutResult describes the resolved drain checkout.
type RouteDrainCheckoutResult struct {
	RuntimePath         string
	UsedExistingBinding bool
	// ProvisionedManaged is true when this route just forked and bound a managed
	// worktree from the registration directive (ADR-0059). Like UsedExistingBinding,
	// it tells the caller RuntimePath is a resolved checkout the executor must be
	// pointed at rather than the current checkout it resolves on its own.
	ProvisionedManaged bool
	Binding            Binding
}

var (
	ErrRuntimeOverrideConflict = errors.New("runtime override conflicts with bound worktree")
	ErrBoundWorktreeInvalid    = errors.New("bound worktree invalid")
	// ErrNoResolvableTrunk is returned when a `managed` worktree directive cannot
	// be satisfied because the repository has no resolvable Trunk worktree to fork
	// from. Routing refuses rather than silently falling back in place (ADR-0059);
	// surfacing it as a visible config-class error on the set is a later slice.
	ErrNoResolvableTrunk = errors.New("managed worktree directive: no resolvable Trunk worktree")
)

// RouteDrainCheckout resolves which checkout a set drain runs in, honoring the
// precedence binding → runtime-path override → registration worktree-intent →
// current checkout (ADR-0059). An existing Worktree binding resumes there; an
// explicit runtime-path override resolves to that checkout; an unbound set whose
// registration carries a `managed` worktree directive forks a managed worktree
// from the Trunk worktree, records a managed binding, and drains there; otherwise
// the drain runs in the current checkout. "Drain where you are" still follows for
// the no-directive case — a linked current checkout is adopted (a never-delete
// adopted binding recorded by the executor), the trunk drains inline with no
// binding. The directive is consulted only when unbound and unoverridden, so an
// operator's bind/override always wins and provisioning stays lazy and one-time:
// later drains resume via the binding recorded here.
func RouteDrainCheckout(req RouteDrainCheckoutRequest) (RouteDrainCheckoutResult, error) {
	if req.TD == nil {
		return RouteDrainCheckoutResult{}, fmt.Errorf("missing task dependencies")
	}
	setID := strings.TrimSpace(req.SetID)
	if setID == "" {
		return RouteDrainCheckoutResult{}, fmt.Errorf("set id is required")
	}
	checkout := strings.TrimSpace(req.CurrentCheckout)
	if checkout == "" {
		return RouteDrainCheckoutResult{}, fmt.Errorf("checkout path is required")
	}

	repoID, err := tasks.ResolveRepositoryIdentity(req.TD, checkout)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	key := Key(repoID, setID)

	store, err := Load(req.TD)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}

	currentRuntime, err := tasks.ResolveRuntimePathWith(req.TD, checkout, "")
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}

	// 1. An existing Worktree binding resumes there.
	if existing, ok := store.Get(key); ok && strings.TrimSpace(existing.RuntimePath) != "" {
		if override := strings.TrimSpace(req.RuntimeOverride); override != "" {
			overridePath, err := tasks.ResolveRuntimePathWith(req.TD, checkout, override)
			if err != nil {
				return RouteDrainCheckoutResult{}, err
			}
			if overridePath != existing.RuntimePath {
				return RouteDrainCheckoutResult{}, fmt.Errorf("%w: %s conflicts with %s for %s", ErrRuntimeOverrideConflict, overridePath, existing.RuntimePath, setID)
			}
		}
		if req.Trigger == TriggerQueueSpawn {
			if err := ValidateBoundWorktree(req.TD, checkout, existing); err != nil {
				return RouteDrainCheckoutResult{}, fmt.Errorf("%w: %v", ErrBoundWorktreeInvalid, err)
			}
		}
		return RouteDrainCheckoutResult{
			RuntimePath:         existing.RuntimePath,
			UsedExistingBinding: true,
			Binding:             existing,
		}, nil
	}

	// 2. An explicit runtime-path override resolves to that checkout.
	if override := strings.TrimSpace(req.RuntimeOverride); override != "" {
		runtimePath, err := tasks.ResolveRuntimePathWith(req.TD, checkout, override)
		if err != nil {
			return RouteDrainCheckoutResult{}, err
		}
		return RouteDrainCheckoutResult{RuntimePath: runtimePath}, nil
	}

	// 3. An unbound set whose registration carries a `managed` worktree directive
	// forks a managed worktree from the Trunk worktree and binds it (ADR-0059).
	// This is the only path routing provisions, and only here, lazily, once: the
	// binding above resumes later drains. The named-worktree arm is a later slice.
	defPath, err := tasks.CanonicalDefinitionPathWith(req.TD, repoID.TasksDir)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	intent, err := tasks.RegisteredWorktreeIntent(req.TD, defPath, setID)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	if intent != nil && intent.Managed {
		b, err := provisionManagedWorktree(req, checkout, setID, key, store)
		if err != nil {
			return RouteDrainCheckoutResult{}, err
		}
		return RouteDrainCheckoutResult{
			RuntimePath:        b.RuntimePath,
			ProvisionedManaged: true,
			Binding:            b,
		}, nil
	}

	// 4. Otherwise the drain runs in the current checkout — no provisioning.
	return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
}

// provisionManagedWorktree forks a managed worktree from the repository's Trunk
// worktree for setID, records a provisioned (pop-owned, torn-down-on-integrate)
// Worktree binding under key, and returns it. It is the lazy provisioner the
// `managed` registration directive triggers on the first unbound drain — the
// same fork-from-trunk act as `pop tasks implement --in-worktree`, routed so both
// foreground implement and the Queue reach it through RouteDrainCheckout. A repo
// with no resolvable trunk yields ErrNoResolvableTrunk; routing never falls back
// in place.
func provisionManagedWorktree(req RouteDrainCheckoutRequest, checkout, setID, key string, store *Store) (Binding, error) {
	trunkPath, bare, err := ResolveTrunkPath(req.TD, req.Config, checkout)
	if err != nil {
		return Binding{}, err
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return Binding{}, fmt.Errorf("%w for %s", ErrNoResolvableTrunk, setID)
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	b, err := ProvisionWorktree(req.TD, ManagedWorktreesRoot(req.TD), trunkPath, setID, now)
	if err != nil {
		return Binding{}, err
	}
	if id, err := tasks.ResolveRepositoryIdentity(req.TD, trunkPath); err == nil {
		b.Project = DetectProject(req.PD, req.TD, req.Config, id)
	}
	store.Put(key, b)
	if err := Save(req.TD, store); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// ResolveTrunkPath resolves the repository checkout used as the Trunk worktree:
// explicit trunk = true override, then the git main worktree for non-bare
// repositories. Returns (path, false, nil) on success; (_, true, nil) when the
// repo is bare with no trunk override (caller must refuse and name a trunk).
func ResolveTrunkPath(td *tasks.Deps, cfg *config.Config, checkoutPath string) (path string, bare bool, err error) {
	if td == nil {
		return "", false, fmt.Errorf("missing task dependencies")
	}
	repoKey, err := repoKeyFromCheckout(td, checkoutPath)
	if err != nil {
		return "", false, err
	}
	if cfg != nil {
		for rawKey, block := range cfg.Repo {
			if block.Trunk == nil || !*block.Trunk {
				continue
			}
			candidate, err := tasks.NormalizeProjectPathWith(td, rawKey)
			if err != nil {
				continue
			}
			candidateKey, err := repoKeyFromCheckout(td, candidate)
			if err != nil {
				continue
			}
			if candidateKey == repoKey {
				return candidate, false, nil
			}
		}
	}
	mainPath, bare, err := GitMainWorktree(td, checkoutPath)
	if err != nil || bare || mainPath == "" {
		return mainPath, bare, err
	}
	return mainPath, false, nil
}

func repoKeyFromCheckout(td *tasks.Deps, checkoutPath string) (string, error) {
	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return "", err
	}
	return RepoKey(id), nil
}

// GitMainWorktree returns the repository's primary working tree by parsing
// `git worktree list --porcelain`. A bare repo has no primary working tree.
func GitMainWorktree(td *tasks.Deps, fromCheckout string) (string, bool, error) {
	out, err := td.Git.CommandInDir(fromCheckout, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("list worktrees: %w", err)
	}
	mainPath, bare := ParseGitMainWorktree(out)
	return mainPath, bare, nil
}

// ParseGitMainWorktree extracts the primary working tree from porcelain output.
func ParseGitMainWorktree(porcelain string) (string, bool) {
	var firstPath string
	started := false
	for _, line := range strings.Split(porcelain, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if started {
				break
			}
			firstPath = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			started = true
			continue
		}
		if !started {
			continue
		}
		switch {
		case line == "bare":
			return "", true
		case strings.TrimSpace(line) == "":
			return firstPath, false
		}
	}
	return firstPath, false
}

// ValidateBoundWorktree checks that a bound checkout exists and is registered
// with git from projectPath.
func ValidateBoundWorktree(td *tasks.Deps, projectPath string, b Binding) error {
	if td == nil {
		return fmt.Errorf("missing task dependencies")
	}
	path := strings.TrimSpace(b.RuntimePath)
	if path == "" {
		return fmt.Errorf("binding has no runtime path")
	}
	if _, err := td.FS.Stat(path); err != nil {
		return fmt.Errorf("checkout missing: %w", err)
	}
	registered, err := worktreeRegistered(td, projectPath, path)
	if err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("checkout %s is not registered with git", path)
	}
	return nil
}

func worktreeRegistered(td *tasks.Deps, projectPath, checkoutPath string) (bool, error) {
	out, err := td.Git.CommandInDir(projectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("list worktrees: %w", err)
	}
	canonCheckout, err := canonicalCheckoutPath(td, checkoutPath)
	if err != nil {
		return false, fmt.Errorf("canonicalize checkout: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		wtPath := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		canonWT, err := canonicalCheckoutPath(td, wtPath)
		if err != nil {
			continue
		}
		if canonWT == canonCheckout {
			return true, nil
		}
	}
	return false, nil
}

func canonicalCheckoutPath(td *tasks.Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return td.FS.EvalSymlinks(abs)
}

// GetForSet returns the binding store key and record for (checkoutPath, setID).
func GetForSet(td *tasks.Deps, checkoutPath, setID string) (string, Binding, bool, error) {
	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return "", Binding{}, false, err
	}
	key := Key(id, setID)
	store, err := Load(td)
	if err != nil {
		return "", Binding{}, false, err
	}
	b, ok := store.Get(key)
	return key, b, ok, nil
}

// Put saves binding under key in the shared store.
func Put(td *tasks.Deps, key string, b Binding) error {
	store, err := Load(td)
	if err != nil {
		return err
	}
	store.Put(key, b)
	return Save(td, store)
}

// Delete removes key from the shared store.
func Delete(td *tasks.Deps, key string) error {
	store, err := Load(td)
	if err != nil {
		return err
	}
	store.Delete(key)
	return Save(td, store)
}

// FindBySetID finds a binding for setID when it is unambiguous across repos.
func FindBySetID(td *tasks.Deps, setID string) (string, Binding, bool, error) {
	store, err := Load(td)
	if err != nil {
		return "", Binding{}, false, err
	}
	if store == nil || len(store.Bindings) == 0 {
		return "", Binding{}, false, nil
	}
	var keys []string
	for key := range store.Bindings {
		parts := strings.Split(key, keySeparator)
		if len(parts) != 2 || parts[1] != setID {
			continue
		}
		keys = append(keys, key)
	}
	switch len(keys) {
	case 0:
		return "", Binding{}, false, nil
	case 1:
		b, _ := store.Get(keys[0])
		return keys[0], b, true, nil
	default:
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "queue: set %q is ambiguous; bound in:", setID)
		for _, key := range keys {
			rec, _ := store.Get(key)
			fmt.Fprintf(&b, "\n  %s (%s)", rec.Project, rec.RuntimePath)
		}
		return "", Binding{}, false, fmt.Errorf("%s", b.String())
	}
}

// AllBindings returns every binding in the shared store.
func AllBindings(td *tasks.Deps) (map[string]Binding, error) {
	store, err := Load(td)
	if err != nil {
		return nil, err
	}
	if store == nil || len(store.Bindings) == 0 {
		return nil, nil
	}
	out := make(map[string]Binding, len(store.Bindings))
	for k, v := range store.Bindings {
		out[k] = v
	}
	return out, nil
}

// Provisioned reports whether the binding under key was provisioned by pop.
func Provisioned(td *tasks.Deps, key string) bool {
	store, err := Load(td)
	if err != nil {
		return false
	}
	return store.Provisioned(key)
}

// ShouldTeardown reports whether the checkout under key may be removed.
func ShouldTeardown(td *tasks.Deps, key string) bool {
	store, err := Load(td)
	if err != nil {
		return true
	}
	return store.ShouldTeardown(key)
}
