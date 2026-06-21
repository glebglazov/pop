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

// ProvisionFailureMode selects behavior when managed worktree provisioning fails.
type ProvisionFailureMode int

const (
	ProvisionFail ProvisionFailureMode = iota
	ProvisionFallbackInline
)

// RouteDrainCheckoutRequest carries inputs for drain checkout resolution.
// Precedence: existing Worktree binding → runtime-path override → managed
// worktree from Execution base (worktree-ready, not inline) → current checkout.
type RouteDrainCheckoutRequest struct {
	TD                 *tasks.Deps
	PD                 *project.Deps
	Cfg                *config.Config
	CurrentCheckout    string
	SetID              string
	Trigger            DrainTrigger
	Inline             bool
	RuntimeOverride    string
	WorktreeReady      bool
	OnProvisionFailure ProvisionFailureMode
	ProjectName        string
	WorktreesRoot      string
	Now                time.Time
}

// RouteDrainCheckoutResult describes the resolved drain checkout.
type RouteDrainCheckoutResult struct {
	RuntimePath         string
	ExecutionBase       string
	UsedExistingBinding bool
	ProvisionedNew      bool
	Binding             Binding
}

var (
	ErrInlineWhenBound       = errors.New("inline conflicts with worktree binding")
	ErrRuntimeOverrideConflict = errors.New("runtime override conflicts with bound worktree")
	ErrNeedsExecutionBase    = errors.New("needs execution_base")
	ErrBoundWorktreeInvalid  = errors.New("bound worktree invalid")
)

// RouteDrainCheckout resolves which checkout a set drain runs in.
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
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
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

	if existing, ok := store.Get(key); ok && strings.TrimSpace(existing.RuntimePath) != "" {
		if req.Inline {
			return RouteDrainCheckoutResult{}, fmt.Errorf("%w: task set %s; run `pop tasks unbind-worktree %s` before --inline", ErrInlineWhenBound, setID, setID)
		}
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

	if override := strings.TrimSpace(req.RuntimeOverride); override != "" {
		runtimePath, err := tasks.ResolveRuntimePathWith(req.TD, checkout, override)
		if err != nil {
			return RouteDrainCheckoutResult{}, err
		}
		if req.Inline {
			linked, err := IsLinkedWorktree(req.TD, runtimePath)
			if err != nil {
				return RouteDrainCheckoutResult{}, err
			}
			if linked {
				return RouteDrainCheckoutResult{}, fmt.Errorf("tasks implement: --inline conflicts with linked --task-runtime-path %s", runtimePath)
			}
		}
		return RouteDrainCheckoutResult{RuntimePath: runtimePath}, nil
	}

	if req.Inline {
		return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
	}

	linked, err := IsLinkedWorktree(req.TD, currentRuntime)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	if linked {
		return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
	}

	if !req.WorktreeReady {
		return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
	}

	basePath, bare, err := ResolveExecutionBasePath(req.TD, req.Cfg, checkout)
	if err != nil {
		return RouteDrainCheckoutResult{}, err
	}
	if bare || strings.TrimSpace(basePath) == "" {
		if req.Trigger == TriggerImplementForeground {
			return RouteDrainCheckoutResult{}, fmt.Errorf("tasks implement: %w; skipped", ErrNeedsExecutionBase)
		}
		if req.OnProvisionFailure == ProvisionFallbackInline {
			return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
		}
		return RouteDrainCheckoutResult{}, ErrNeedsExecutionBase
	}

	managed, err := provisionManagedBinding(req, store, key, basePath, repoID)
	if err != nil {
		if req.OnProvisionFailure == ProvisionFallbackInline {
			return RouteDrainCheckoutResult{RuntimePath: currentRuntime}, nil
		}
		return RouteDrainCheckoutResult{}, err
	}
	return RouteDrainCheckoutResult{
		RuntimePath:    managed.RuntimePath,
		ExecutionBase:  basePath,
		ProvisionedNew: true,
		Binding:        managed,
	}, nil
}

func provisionManagedBinding(req RouteDrainCheckoutRequest, store *Store, key, basePath string, repoID *tasks.RepositoryIdentity) (Binding, error) {
	if existing, ok := store.Get(key); ok && strings.TrimSpace(existing.RuntimePath) != "" {
		return existing, nil
	}
	if strings.TrimSpace(req.WorktreesRoot) == "" {
		return Binding{}, fmt.Errorf("worktrees root is required to provision")
	}
	managed, err := ProvisionWorktree(req.TD, req.WorktreesRoot, basePath, req.SetID, req.Now)
	if err != nil {
		return Binding{}, err
	}
	if req.ProjectName != "" {
		managed.Project = req.ProjectName
	} else {
		managed.Project = DetectProject(req.PD, req.TD, req.Cfg, repoID)
	}
	store.Put(key, managed)
	if err := Save(req.TD, store); err != nil {
		return Binding{}, err
	}
	return managed, nil
}

// ResolveExecutionBasePath resolves the repository checkout used as Execution
// base: explicit execution_base override, then the git main worktree for
// non-bare repositories.
func ResolveExecutionBasePath(td *tasks.Deps, cfg *config.Config, checkoutPath string) (path string, bare bool, err error) {
	if td == nil {
		return "", false, fmt.Errorf("missing task dependencies")
	}
	repoKey, err := repoKeyFromCheckout(td, checkoutPath)
	if err != nil {
		return "", false, err
	}
	if cfg != nil {
		for rawKey, block := range cfg.Repo {
			if block.ExecutionBase == nil || !*block.ExecutionBase {
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
