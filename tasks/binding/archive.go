package binding

import (
	"errors"
	"fmt"
	"io"

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

// PrepareManagedWorktreesForArchive confirms (unless opts.Yes) and tears down
// every managed binding among setIDs before archive metadata is written.
// Declining any prompt aborts the whole batch with ErrArchiveCancelled.
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
	hooks := LifecycleHooks{}
	for _, setID := range setIDs {
		key, b, ok, err := FindBySetID(td, setID)
		if err != nil {
			return err
		}
		if !ok || !b.Provisioned {
			continue
		}
		confirmed, err := ConfirmManagedWorktreeDelete(opts.In, out, opts.Yes, b.RuntimePath)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, "Archive cancelled")
			return ErrArchiveCancelled
		}
		if err := TeardownAndReleaseManagedBinding(td, pd, cfg, key, b, hooks); err != nil {
			return err
		}
	}
	return nil
}
