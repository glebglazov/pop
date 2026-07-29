package binding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// scratchNameMaxProbes bounds the per-day disambiguation scan so a
// pathological managed-worktree root cannot spin the loop forever.
const scratchNameMaxProbes = 1000

// NextScratchSetID returns the identifier a managed worktree created ahead of
// any Task set carries (ADR-0152): scratch-<YYYYMMDD>-<n>, with the day in
// local time and n the lowest positive integer whose directory does not yet
// exist under the managed-worktree root for checkoutPath's repository. The
// identifier is a label, never a key — nothing later reconciles it against the
// Task set that binds the worktree.
func NextScratchSetID(d *tasks.Deps, checkoutPath string, now time.Time) (string, error) {
	if d == nil {
		return "", fmt.Errorf("missing task dependencies")
	}
	id, err := tasks.ResolveRepositoryIdentity(d, checkoutPath)
	if err != nil {
		return "", err
	}
	day := now.Format("20060102")
	repoDir := filepath.Join(ManagedWorktreesRoot(d), RepoKey(id))
	for n := 1; n <= scratchNameMaxProbes; n++ {
		name := fmt.Sprintf("scratch-%s-%d", day, n)
		_, err := d.FS.Stat(filepath.Join(repoDir, name))
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free scratch name for %s under %s", day, repoDir)
}

// ProvisionScratchWorktree forks a pop-managed worktree ahead of any Task set
// (ADR-0152): the directory is the generated scratch name under the
// managed-worktree root and the branch matches the managed shape
// (pop/<name>/<stamp>). startPoint is the picked base ref the fork starts
// from. No binding row is recorded — the worktree is an Unbound managed
// worktree until a set registers from inside it, and the reference-counted
// teardown predicate is satisfied from birth.
func ProvisionScratchWorktree(d *tasks.Deps, projectPath, startPoint string, now time.Time) (Binding, error) {
	if strings.TrimSpace(startPoint) == "" {
		return Binding{}, fmt.Errorf("start point is required")
	}
	name, err := NextScratchSetID(d, projectPath, now)
	if err != nil {
		return Binding{}, err
	}
	return ProvisionWorktree(d, ManagedWorktreesRoot(d), projectPath, name, startPoint, now)
}
