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
// worktree-intent → default binding to the chosen checkout (ADR-0059, ADR-0062).
// The directive step is the only place routing provisions a worktree, and only on
// the first unbound drain of a set whose registration carries one; every other
// path resolves an existing checkout. The final step no longer runs transiently:
// the first no-directive drain persists a default (adopted) binding to the chosen
// checkout, so later drains resume there. Config and PD are consulted by the
// directive step (to resolve the Trunk worktree and label/stamp the binding) and
// PD/Config also label the default binding; a request that resumes an existing
// binding may leave them zero.
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
	// AdoptedNamed is true when this route resolved a `name` worktree directive on
	// the first unbound drain: it matched the named worktree on this machine and
	// recorded an adopted (never-deleted) binding to it (ADR-0059). Like
	// ProvisionedManaged it marks RuntimePath as a resolved checkout the executor
	// must be pointed at; unlike it, pop never tears the checkout down.
	AdoptedNamed bool
	// BoundDefault is true when this route hit the no-directive final step and
	// persisted a default Worktree binding to the chosen checkout — the current
	// checkout for foreground, the integration target for the Queue (ADR-0062).
	// Like AdoptedNamed it records a never-deleted (adopted) binding; later drains
	// resume via step 1. RuntimePath is the checkout the executor already resolves
	// on its own, so unlike the other flags the caller need not re-point at it.
	BoundDefault bool
	Binding      Binding
}

var (
	ErrRuntimeOverrideConflict = errors.New("runtime override conflicts with bound worktree")
	ErrBoundWorktreeInvalid    = errors.New("bound worktree invalid")
	// ErrNoResolvableTrunk is returned when a `managed` worktree directive cannot
	// be satisfied because the repository has no resolvable Trunk worktree to fork
	// from. Routing refuses rather than silently falling back in place (ADR-0059);
	// surfacing it as a visible config-class error on the set is a later slice.
	ErrNoResolvableTrunk = errors.New("managed worktree directive: no resolvable Trunk worktree")
	// ErrNamedWorktreeNotFound is returned when a `name` worktree directive cannot
	// be satisfied because no worktree of that name exists on this machine. The
	// name is the portable identifier resolved per machine (ADR-0059); routing
	// refuses rather than silently draining in place. Surfacing it as a visible
	// config-class error on the set is a later slice.
	ErrNamedWorktreeNotFound = errors.New("named worktree directive: no worktree of that name on this machine")
)

// RouteDrainCheckout resolves which checkout a set drain runs in, honoring the
// precedence binding → runtime-path override → (Queue-only) registration
// worktree-intent → default binding to the chosen checkout (ADR-0059, ADR-0062,
// ADR-0072). An existing Worktree binding resumes there; an explicit runtime-path
// override resolves to that checkout. The worktree directive is Queue-only: on a
// Queue spawn an unbound set whose registration carries a `managed` directive
// forks a managed worktree from the Trunk worktree (records a managed binding) and
// a `name` directive adopts the named worktree, but a foreground implement ignores
// the directive entirely. Otherwise the first such drain persists a default
// (adopted) Worktree binding to the checkout it chose — the current checkout for a
// foreground implement, the integration target the Queue routes into for a
// headless spawn — and resumes there on later drains (ADR-0062). The directive and
// the default binding are reached only when unbound and unoverridden, so an
// operator's bind/override always wins and any provisioning stays lazy and
// one-time: later drains resume via the binding recorded here.
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

	// 3. The Worktree directive is Queue-only (ADR-0072). On a Queue spawn an unbound
	// set whose registration carries a directive resolves it once, lazily, and binds
	// the result (ADR-0059); the binding above resumes later drains. The `managed` arm
	// forks a managed worktree from the Trunk worktree — the only path routing
	// provisions. The `name` arm adopts the existing worktree of that name on this
	// machine, matched by its operator-facing name (never a path), recording a
	// never-delete adopted binding. A foreground implement skips this arm entirely:
	// it ignores the directive and falls through to step 4, binding the current
	// checkout (rebinding an already-bound set is a separate slice).
	if req.Trigger == TriggerQueueSpawn {
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
		if intent != nil && intent.Name != "" {
			b, err := adoptNamedWorktree(req, checkout, intent.Name, setID, key, store)
			if err != nil {
				return RouteDrainCheckoutResult{}, err
			}
			return RouteDrainCheckoutResult{
				RuntimePath:  b.RuntimePath,
				AdoptedNamed: true,
				Binding:      b,
			}, nil
		}
	}

	// 4. Otherwise the first no-directive drain persists a default Worktree binding
	// to the checkout it resolved and resumes there on later drains (ADR-0062),
	// rather than running transiently. Foreground binds to the current checkout;
	// the Queue feeds the repo's integration target as the current checkout, so
	// both bind to currentRuntime — the difference is which checkout the caller
	// chose, not what routing re-derives. The binding is adopted (Provisioned=false,
	// never torn down): routing chose the checkout but never created it, and the
	// integration target especially must never be removed. It records the branch
	// too, so the dashboard reads execution checkout and branch from the table.
	b := Adopt(currentRuntime, CurrentBranch(req.TD, currentRuntime), DetectProject(req.PD, req.TD, req.Config, repoID))
	store.Put(key, b)
	if err := Save(req.TD, store); err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	return RouteDrainCheckoutResult{
		RuntimePath:  b.RuntimePath,
		BoundDefault: true,
		Binding:      b,
	}, nil
}

// ProbeWorktreeDirective reports whether a set's registration worktree-intent is
// satisfiable in the current environment without provisioning anything — the
// read-only counterpart to RouteDrainCheckout's directive step (ADR-0059). It
// returns nil when there is no directive, the set is already bound (the directive
// was satisfied on a prior drain), or the directive resolves; it returns
// ErrNoResolvableTrunk (`managed` with no resolvable Trunk worktree) or
// ErrNamedWorktreeNotFound (`name` with no such worktree on this machine) when it
// cannot. `pop tasks status` and the Queue decision use it to surface an
// unsatisfiable directive as a config-class error on the set instead of
// dispatching a drain that could only fail — no provisioning, no crash-backoff,
// no silent in-place fallback. Incidental resolution errors (repo identity,
// state) are returned as-is; callers distinguish the two sentinels with
// errors.Is before treating a probe result as a config-class error.
func ProbeWorktreeDirective(td *tasks.Deps, pd *project.Deps, cfg *config.Config, checkout, setID string) error {
	if td == nil {
		return fmt.Errorf("missing task dependencies")
	}
	setID = strings.TrimSpace(setID)
	checkout = strings.TrimSpace(checkout)
	if setID == "" || checkout == "" {
		return nil
	}

	repoID, err := tasks.ResolveRepositoryIdentity(td, checkout)
	if err != nil {
		return err
	}

	// An existing Worktree binding means the directive was already satisfied on a
	// prior drain (or the operator bound the set explicitly); later drains resume
	// there, so the directive is not re-evaluated and cannot be unsatisfiable.
	store, err := Load(td)
	if err != nil {
		return err
	}
	if existing, ok := store.Get(Key(repoID, setID)); ok && strings.TrimSpace(existing.RuntimePath) != "" {
		return nil
	}

	defPath, err := tasks.CanonicalDefinitionPathWith(td, repoID.TasksDir)
	if err != nil {
		return err
	}
	intent, err := tasks.RegisteredWorktreeIntent(td, defPath, setID)
	if err != nil {
		return err
	}
	if intent == nil {
		return nil
	}
	if intent.Managed {
		trunkPath, bare, err := ResolveTrunkPath(td, cfg, checkout)
		if err != nil {
			return err
		}
		if bare || strings.TrimSpace(trunkPath) == "" {
			return ErrNoResolvableTrunk
		}
		return nil
	}
	if intent.Name != "" {
		if _, err := resolveNamedWorktree(td, checkout, intent.Name); err != nil {
			return err
		}
	}
	return nil
}

// provisionManagedWorktree forks a managed worktree from the repository's Trunk
// worktree for setID, records a provisioned (pop-owned, torn-down-on-integrate)
// Worktree binding under key, and returns it. It is the lazy provisioner the
// `managed` registration directive triggers on the first unbound Queue drain
// (ADR-0072). Foreground `pop tasks implement --in-worktree` forks from the
// current checkout instead; only the Queue has no "current" and uses trunk. A
// repo with no resolvable trunk yields ErrNoResolvableTrunk; routing never
// falls back in place.
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

// adoptNamedWorktree resolves the worktree named by a `name` registration
// directive on this machine, records an adopted (never-deleted) Worktree binding
// pointing at it under key, and returns it. Adopted semantics match
// `bind-worktree`: pop drains into the checkout but never tears it down
// (Provisioned=false). The worktree is matched by its operator-facing name — its
// checkout's basename, the label `pop worktree` shows — never by a path, since
// the manifest carries the portable name resolved per machine (ADR-0059). A name
// with no such worktree yields ErrNamedWorktreeNotFound; routing never falls back
// in place.
func adoptNamedWorktree(req RouteDrainCheckoutRequest, checkout, name, setID, key string, store *Store) (Binding, error) {
	wt, err := resolveNamedWorktree(req.TD, checkout, name)
	if err != nil {
		return Binding{}, err
	}
	b := Adopt(wt.Path, CurrentBranch(req.TD, wt.Path), "")
	if id, err := tasks.ResolveRepositoryIdentity(req.TD, wt.Path); err == nil {
		b.Project = DetectProject(req.PD, req.TD, req.Config, id)
	}
	store.Put(key, b)
	if err := Save(req.TD, store); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// resolveNamedWorktree returns the worktree of checkout's repository whose
// operator-facing name (its checkout basename) matches name. It lists the repo's
// worktrees with `git worktree list --porcelain` and matches on the name/label,
// never the branch or an absolute path. Absent on this machine yields
// ErrNamedWorktreeNotFound.
func resolveNamedWorktree(td *tasks.Deps, checkout, name string) (project.Worktree, error) {
	pd := &project.Deps{Git: td.Git, FS: td.FS}
	ctx, err := project.DetectRepoContextFromPathWith(pd, checkout)
	if err != nil {
		return project.Worktree{}, err
	}
	worktrees, err := project.ListWorktreesWith(pd, ctx)
	if err != nil {
		return project.Worktree{}, err
	}
	for _, wt := range worktrees {
		if wt.Name == name {
			return wt, nil
		}
	}
	return project.Worktree{}, fmt.Errorf("%w: %q", ErrNamedWorktreeNotFound, name)
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
