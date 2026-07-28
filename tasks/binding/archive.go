package binding

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// ErrArchiveCancelled reports that archive was aborted before mutating state,
// typically because the operator declined managed-worktree deletion.
var ErrArchiveCancelled = errors.New("archive cancelled")

// ArchiveConfirmOptions controls managed-worktree teardown confirmation during
// archive. Zero values mean non-interactive decline for managed bindings.
type ArchiveConfirmOptions struct {
	Yes bool
	In  io.Reader
	Out io.Writer
}

type archiveCheckoutGroup struct {
	runtimePath string
	binding     Binding
	keys        []string
}

// PrepareManagedWorktreesForArchive confirms (unless opts.Yes) and tears down
// every managed checkout among setIDs whose reference count hits zero before
// archive metadata is written. Declining any prompt aborts the whole batch with
// ErrArchiveCancelled.
func PrepareManagedWorktreesForArchive(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setIDs []string, opts ArchiveConfirmOptions) error {
	if len(setIDs) == 0 {
		return nil
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	excludeKeys := make(map[string]bool)
	groups := make(map[string]*archiveCheckoutGroup)
	for _, setID := range setIDs {
		key, b, ok, err := FindBySetID(td, setID)
		if err != nil {
			return err
		}
		if !ok || strings.TrimSpace(b.RuntimePath) == "" {
			continue
		}
		excludeKeys[key] = true
		canon, err := canonicalCheckoutPath(td, b.RuntimePath)
		if err != nil {
			return err
		}
		g, ok := groups[canon]
		if !ok {
			g = &archiveCheckoutGroup{
				runtimePath: b.RuntimePath,
				binding:     b,
			}
			groups[canon] = g
		}
		g.keys = append(g.keys, key)
	}

	hooks := LifecycleHooks{}
	for _, g := range groups {
		should, err := shouldOfferManagedCheckoutTeardown(td, g.runtimePath, excludeKeys)
		if err != nil {
			return err
		}
		if !should {
			continue
		}
		confirmed, err := ConfirmManagedWorktreeDelete(opts.In, out, opts.Yes, g.runtimePath)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, "Archive cancelled")
			return ErrArchiveCancelled
		}
		if err := TeardownManagedWorktree(td, pd, cfg, g.binding, hooks); err != nil {
			return err
		}
		for _, key := range g.keys {
			if err := Delete(td, key); err != nil {
				return err
			}
		}
	}
	return nil
}
