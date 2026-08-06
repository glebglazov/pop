package tasks

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
)

// resolveRepoTurnCap returns the Turn cap the repository owning checkoutPath
// declares — how many Turns one implementation attempt there may spend — and 0
// when it declares none, which is every repository until someone bounds one.
//
// The number lives only in a central [repo."<path>"] block and is matched by
// repository identity, so a monorepo's six worktrees read the one bound and
// nothing has to be committed into the repository to set it (ADR-0191). A
// repo-config problem is not worth refusing to run an attempt over: it degrades
// to uncapped with a debug log, exactly as the rest of repo scope degrades.
func resolveRepoTurnCap(d *Deps, cfg *config.Config, checkoutPath string) int {
	if cfg == nil || checkoutPath == "" {
		return 0
	}
	cd := config.DefaultDeps()
	if d != nil && d.FS != nil {
		cd = &config.Deps{FS: d.FS}
	}
	repoCfg, err := cfg.ResolveRepoConfig(cd, checkoutPath)
	if err != nil {
		debug.Error("tasks: resolve turn cap for %s: %v", checkoutPath, err)
		return 0
	}
	return repoCfg.TurnCap
}
