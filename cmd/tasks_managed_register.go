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

// resolveManagedRegisterTrunk resolves the Trunk worktree for a managed register.
// When trunkFlag is non-empty it is normalized, persisted to global config as
// trunk = true on a [repo."<path>"] block, and returned; cfg is reloaded when
// persistence succeeds. When trunkFlag is empty the trunk is resolved from cfg
// and checkoutPath; a bare repo with no configured trunk refuses with an error
// naming --trunk.
func resolveManagedRegisterTrunk(td *tasks.Deps, cfg *config.Config, configPath, checkoutPath, trunkFlag string) (trunkPath string, outCfg *config.Config, err error) {
	trunkFlag = strings.TrimSpace(trunkFlag)
	if trunkFlag != "" {
		trunkPath, err = tasks.NormalizeProjectPathWith(td, trunkFlag)
		if err != nil {
			return "", cfg, fmt.Errorf("normalize --trunk path: %w", err)
		}
		same, err := sameRepositoryCheckout(td, checkoutPath, trunkPath)
		if err != nil {
			return "", cfg, err
		}
		if !same {
			return "", cfg, fmt.Errorf("--trunk %q is not a checkout of this repository", trunkPath)
		}
		if err := config.PersistRepoTrunkWith(cmdLayerDeps().configDeps(), configPath, trunkPath); err != nil {
			return "", cfg, fmt.Errorf("persist trunk to config: %w", err)
		}
		if cfg == nil {
			cfg = &config.Config{}
		}
		if cfg.Repo == nil {
			cfg.Repo = make(map[string]config.RepoOverrideConfig)
		}
		block := cfg.Repo[trunkPath]
		trunk := true
		block.Trunk = &trunk
		cfg.Repo[trunkPath] = block
		return trunkPath, cfg, nil
	}

	path, bare, err := binding.ResolveTrunkPath(td, cfg, checkoutPath)
	if err != nil {
		return "", cfg, err
	}
	if bare || strings.TrimSpace(path) == "" {
		return "", cfg, fmt.Errorf("%s", managedRegisterNoTrunkMsg)
	}
	return path, cfg, nil
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
	now := time.Now()
	var done []binding.ManagedProvision
	for _, setID := range newSetIDs {
		b, err := binding.ProvisionManagedBinding(binding.ProvisionManagedBindingRequest{
			TD:           td,
			PD:           pd,
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
