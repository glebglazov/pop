package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

const managedRegisterNoTrunkMsg = "no Trunk worktree configured; re-run with --trunk <path> to name one"

// resolveManagedTrunk resolves the Trunk worktree for a managed register or
// bind-worktree. When trunkFlag is non-empty it is normalized, persisted to
// config.runtime.toml as trunk = true on the checkout path (ADR-0150), and
// returned. When trunkFlag is empty the trunk is resolved from cfg and
// checkoutPath; a bare repo with no configured trunk refuses with an error
// naming --trunk.
func resolveManagedTrunk(td *tasks.Deps, cfg *config.Config, checkoutPath, trunkFlag string) (trunkPath string, err error) {
	cd := cmdLayerDeps().configDeps()
	trunkFlag = strings.TrimSpace(trunkFlag)
	if trunkFlag != "" {
		trunkPath, err = tasks.NormalizeProjectPathWith(td, trunkFlag)
		if err != nil {
			return "", fmt.Errorf("normalize --trunk path: %w", err)
		}
		same, err := sameRepositoryCheckout(td, checkoutPath, trunkPath)
		if err != nil {
			return "", err
		}
		if !same {
			return "", fmt.Errorf("--trunk %q is not a checkout of this repository", trunkPath)
		}
		if err := config.SetRuntimeRepoTrunkWith(cd, trunkPath); err != nil {
			return "", fmt.Errorf("persist trunk to runtime config: %w", err)
		}
		return trunkPath, nil
	}

	path, bare, err := binding.ResolveTrunkPathWith(cd, td, cfg, checkoutPath)
	if err != nil {
		return "", err
	}
	if bare || strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s", managedRegisterNoTrunkMsg)
	}
	return path, nil
}

func sameRepositoryCheckout(td *tasks.Deps, a, b string) (bool, error) {
	idA, err := tasks.ResolveRepositoryIdentity(td, a)
	if err != nil {
		return false, err
	}
	idB, err := tasks.ResolveRepositoryIdentity(td, b)
	if err != nil {
		return false, err
	}
	return binding.RepoKey(idA) == binding.RepoKey(idB), nil
}

// eagerProvisionManagedNewRegistrations forks a managed worktree from trunkPath
// for each newly registered set and records a provisioned binding (ADR-0147).
// A failure rolls back every binding and worktree created in this call.
func eagerProvisionManagedNewRegistrations(td *tasks.Deps, pd *project.Deps, cfg *config.Config, trunkPath, checkoutPath string, newSetIDs []string) error {
	if len(newSetIDs) == 0 {
		return nil
	}
	cd := cmdLayerDeps().configDeps()
	now := time.Now()
	var done []binding.ManagedProvision
	for _, setID := range newSetIDs {
		b, err := binding.ProvisionManagedBinding(binding.ProvisionManagedBindingRequest{
			TD:           td,
			PD:           pd,
			ConfigDeps:   cd,
			Config:       cfg,
			TrunkPath:    trunkPath,
			CheckoutPath: checkoutPath,
			SetID:        setID,
			Now:          now,
		})
		if err != nil {
			binding.RollbackManagedProvisions(td, trunkPath, done)
			return err
		}
		repoID, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
		if err != nil {
			binding.RollbackManagedProvisions(td, trunkPath, append(done, binding.ManagedProvision{Binding: b}))
			return err
		}
		done = append(done, binding.ManagedProvision{Key: binding.Key(repoID, setID), Binding: b})
	}
	return nil
}
