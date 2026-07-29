package binding

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// ProvisionManagedBindingRequest carries inputs for eager managed-worktree
// provisioning at register or bind-worktree time (ADR-0147).
type ProvisionManagedBindingRequest struct {
	TD           *tasks.Deps
	PD           *project.Deps
	ConfigDeps   *config.Deps
	Config       *config.Config
	TrunkPath    string // when empty, resolved from CheckoutPath + Config
	CheckoutPath string
	SetID        string
	Now          time.Time
}

// ProvisionManagedBinding forks a managed worktree from the repository's Trunk
// worktree's HEAD for setID, records a provisioned Worktree binding, and
// returns it. It is the shared seam for `register --managed` and
// `bind-worktree --managed` (ADR-0147). When TrunkPath is empty the trunk is
// resolved from CheckoutPath; a repo with no resolvable trunk yields
// ErrNoResolvableTrunk.
func ProvisionManagedBinding(req ProvisionManagedBindingRequest) (Binding, error) {
	if req.TD == nil {
		return Binding{}, fmt.Errorf("missing task dependencies")
	}
	setID := strings.TrimSpace(req.SetID)
	checkoutPath := strings.TrimSpace(req.CheckoutPath)
	if setID == "" {
		return Binding{}, fmt.Errorf("set id is required")
	}
	if checkoutPath == "" {
		return Binding{}, fmt.Errorf("checkout path is required")
	}

	repoID, err := tasks.ResolveRepositoryIdentity(req.TD, checkoutPath)
	if err != nil {
		return Binding{}, fmt.Errorf("resolve repository identity: %w", err)
	}
	key := Key(repoID, setID)

	trunkPath := strings.TrimSpace(req.TrunkPath)
	if trunkPath == "" {
		cd := req.ConfigDeps
		if cd == nil {
			cd = config.DefaultDeps()
		}
		var bare bool
		trunkPath, bare, err = ResolveTrunkPathWith(cd, req.TD, req.Config, checkoutPath)
		if err != nil {
			return Binding{}, err
		}
		if bare || trunkPath == "" {
			return Binding{}, fmt.Errorf("%w for %s", ErrNoResolvableTrunk, setID)
		}
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	b, err := ProvisionWorktree(req.TD, ManagedWorktreesRoot(req.TD), trunkPath, setID, "HEAD", now)
	if err != nil {
		return Binding{}, err
	}
	pd := req.PD
	if pd == nil {
		pd = project.DefaultDeps()
	}
	if req.Config != nil {
		b.Project = DetectProject(pd, req.TD, req.Config, repoID)
	}
	if err := Put(req.TD, key, b); err != nil {
		_ = TeardownWorktree(req.TD, trunkPath, b.RuntimePath, b.Branch, true)
		return Binding{}, err
	}
	return b, nil
}

// ManagedProvision records one provisioned binding and its store key for rollback.
type ManagedProvision struct {
	Key     string
	Binding Binding
}

// RollbackManagedProvisions tears down worktrees and forgets bindings recorded
// by ProvisionManagedBinding. It is used when a multi-set managed register must
// stay atomic.
func RollbackManagedProvisions(td *tasks.Deps, trunkPath string, provisions []ManagedProvision) {
	trunkPath = strings.TrimSpace(trunkPath)
	for i := len(provisions) - 1; i >= 0; i-- {
		p := provisions[i]
		_ = Delete(td, p.Key)
		_ = TeardownWorktree(td, trunkPath, p.Binding.RuntimePath, p.Binding.Branch, true)
	}
}
