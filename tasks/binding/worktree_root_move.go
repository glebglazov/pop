package binding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// worktreeRootMoveLock names the cross-process lock the move holds, so two pop
// processes touching a binding at the same time cannot both start relocating the
// same directories.
const worktreeRootMoveLock = "worktree-root-move"

// WorktreeRootMove is the outcome of one look at the managed-worktree root move:
// the `pop queue` → `pop work` cut renamed every runtime surface, and the root
// pop-provisioned worktrees live under is the last one to follow.
//
// Exactly one of three things is true of a returned value. Nothing to do (the
// legacy root is gone): every field is empty. Refused: Refusals names each
// worktree that blocked the move and nothing was touched. Moved: Moved lists the
// new paths, Rewritten counts the binding rows repointed and Repaired names the
// repositories whose worktree administrative files were re-linked.
type WorktreeRootMove struct {
	// From and To are the legacy and current managed-worktree roots. They are
	// set whenever the legacy root still exists, including on a refusal, so a
	// report can name both ends of the move it is describing.
	From string
	To   string
	// Pending labels every managed worktree found under the legacy root, as
	// <repo key>/<name>. It is filled by both the inspection and the move, so a
	// refusal still says how much work is waiting.
	Pending []string
	// Moved lists the new absolute path of each relocated worktree. Empty on a
	// refusal or when there was nothing to move.
	Moved []string
	// Rewritten counts the bindings.runtime_path values repointed at To.
	Rewritten int
	// Repaired names each repository (by managed-root repo key) that
	// `git worktree repair` ran for after the move.
	Repaired []string
	// RepairWarnings holds a line per repository whose repair failed. The move
	// itself has already succeeded by then — the paths are recorded and the
	// worktrees resolve, only the repository's reverse pointers are stale — so a
	// failed repair is reported, never fatal.
	RepairWarnings []string
	// Refusals names each worktree that blocks the move, one line each. Non-empty
	// means nothing was written: no directory moved, no binding rewritten.
	Refusals []string
}

// Refused reports whether the gate turned the move into a no-op.
func (m WorktreeRootMove) Refused() bool { return len(m.Refusals) > 0 }

// Ran reports whether this call actually relocated anything.
func (m WorktreeRootMove) Ran() bool { return len(m.Moved) > 0 }

// managedWorktree is one directory under a managed-worktree root: the
// <repo key>/<name> two-level layout ProvisionWorktree creates.
type managedWorktree struct {
	RepoKey string
	Name    string
	From    string
	To      string
}

// label identifies a worktree in a refusal or a report the way a human reads the
// managed root: repository key then worktree name.
func (w managedWorktree) label() string { return w.RepoKey + "/" + w.Name }

// PendingWorktreeRootMove reports what the move would do without doing any of
// it: which worktrees are still under the legacy root and what, if anything,
// blocks relocating them. It exists so a read-only surface (`pop doctor`) can
// name the offenders a human has to resolve, since the move itself runs silently
// off the binding read path.
func PendingWorktreeRootMove(d *tasks.Deps) (WorktreeRootMove, error) {
	if d == nil {
		d = tasks.DefaultDeps()
	}
	from, to := LegacyManagedWorktreesRoot(d), ManagedWorktreesRoot(d)
	if !legacyRootPresent(d, from) {
		return WorktreeRootMove{}, nil
	}
	res := WorktreeRootMove{From: from, To: to}
	pending, err := managedWorktreesUnder(d, from, to)
	if err != nil {
		return res, err
	}
	for _, w := range pending {
		res.Pending = append(res.Pending, w.label())
	}
	res.Refusals = worktreeMoveRefusals(d, pending)
	return res, nil
}

// MoveManagedWorktreesRoot relocates every pop-provisioned worktree from the
// pre-cut root to the current one, repoints the recorded checkout of every
// affected binding, and re-links each repository's worktree administrative
// files. It is the read-path fold behind every binding accessor (see
// bindingStore), so no verb and no daemon owns it.
//
// It is the only migration in the `pop work` cut that can destroy uncommitted
// work if it half-completes, so it is gated rather than best-effort: a worktree
// with uncommitted changes or a live drain refuses the whole move, naming itself,
// and nothing is written. The human commits or finishes, and the next binding
// touch moves everything.
//
// The steady state — legacy root gone — costs one stat. Everything below only
// runs while a machine still has worktrees to move.
func MoveManagedWorktreesRoot(d *tasks.Deps) (WorktreeRootMove, error) {
	if d == nil {
		d = tasks.DefaultDeps()
	}
	from := LegacyManagedWorktreesRoot(d)
	if !legacyRootPresent(d, from) {
		return WorktreeRootMove{}, nil
	}
	var res WorktreeRootMove
	err := tasks.WithFileLock(d, tasks.LockPathWith(d, worktreeRootMoveLock), "managed worktree root move", func() error {
		// Re-check inside the lock: the process we waited for may have been the
		// one that did the move.
		if !legacyRootPresent(d, from) {
			return nil
		}
		var err error
		res, err = moveManagedWorktreesRoot(d, from, ManagedWorktreesRoot(d))
		return err
	})
	return res, err
}

// legacyRootPresent reports whether the pre-cut root is still a directory. A
// missing root is the steady state after the move, and the only cost the fold
// pays on an ordinary binding read.
func legacyRootPresent(d *tasks.Deps, from string) bool {
	info, err := d.FS.Stat(from)
	return err == nil && info.IsDir()
}

func moveManagedWorktreesRoot(d *tasks.Deps, from, to string) (WorktreeRootMove, error) {
	res := WorktreeRootMove{From: from, To: to}
	pending, err := managedWorktreesUnder(d, from, to)
	if err != nil {
		return res, err
	}
	for _, w := range pending {
		res.Pending = append(res.Pending, w.label())
	}
	if res.Refusals = worktreeMoveRefusals(d, pending); len(res.Refusals) > 0 {
		return res, nil
	}

	// Directories first, then the recorded paths. A crash between the two
	// self-heals: the worktrees already at the new root are no longer under the
	// legacy root, so the next run finds only the rest — and the prefix rewrite
	// is a blanket one, repointing their bindings along with everything else.
	moved := make([]managedWorktree, 0, len(pending))
	for _, w := range pending {
		if err := d.FS.MkdirAll(filepath.Dir(w.To), 0o755); err != nil {
			return res, moveFailure(d, moved, fmt.Errorf("create managed worktree parent %s: %w", filepath.Dir(w.To), err))
		}
		if err := d.FS.Rename(w.From, w.To); err != nil {
			return res, moveFailure(d, moved, fmt.Errorf("move managed worktree %s: %w", w.From, err))
		}
		moved = append(moved, w)
		res.Moved = append(res.Moved, w.To)
	}

	if s, ok, err := d.Store(false); err != nil {
		return res, moveFailure(d, moved, err)
	} else if ok {
		n, err := s.RewriteBindingRuntimePathPrefix(from+string(os.PathSeparator), to+string(os.PathSeparator))
		if err != nil {
			return res, moveFailure(d, moved, fmt.Errorf("repoint worktree bindings: %w", err))
		}
		res.Rewritten = n
	}

	res.Repaired, res.RepairWarnings = repairMovedWorktrees(d, moved)
	retireLegacyRoot(d, from)
	return res, nil
}

// moveFailure undoes the directory renames already done and returns cause, so a
// failed move leaves the filesystem as it found it. When a rename cannot be
// undone the returned error says so as well as naming the original cause: that
// combination is the one state a human has to repair by hand, so it must not be
// reported as the plain cause.
func moveFailure(d *tasks.Deps, moved []managedWorktree, cause error) error {
	var stranded []string
	for i := len(moved) - 1; i >= 0; i-- {
		if err := d.FS.Rename(moved[i].To, moved[i].From); err != nil {
			stranded = append(stranded, fmt.Sprintf("%s (%v)", moved[i].To, err))
		}
	}
	if len(stranded) == 0 {
		return cause
	}
	return fmt.Errorf("%w; could not move back: %s", cause, strings.Join(stranded, ", "))
}

// managedWorktreesUnder walks the two-level <repo key>/<name> layout under root
// and returns each worktree with the destination it would take under to. It is a
// filesystem-only walk — the same shape the Project picker's managed-worktree
// discovery uses — because a directory pop created is a managed worktree whether
// or not a binding still names it (ADR-0110, ADR-0152).
func managedWorktreesUnder(d *tasks.Deps, root, to string) ([]managedWorktree, error) {
	repoEntries, err := d.FS.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read managed worktree root %s: %w", root, err)
	}
	var out []managedWorktree
	for _, repoEntry := range repoEntries {
		if !repoEntry.IsDir() {
			continue
		}
		repoKey := repoEntry.Name()
		wtEntries, err := d.FS.ReadDir(filepath.Join(root, repoKey))
		if err != nil {
			return nil, fmt.Errorf("read managed worktrees of %s: %w", repoKey, err)
		}
		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}
			out = append(out, managedWorktree{
				RepoKey: repoKey,
				Name:    wtEntry.Name(),
				From:    filepath.Join(root, repoKey, wtEntry.Name()),
				To:      filepath.Join(to, repoKey, wtEntry.Name()),
			})
		}
	}
	return out, nil
}

// worktreeMoveRefusals returns one line per worktree that blocks the move. It
// reuses the two checks drain gating already makes — the working tree carries
// uncommitted changes, or a live (PID-alive) drain holds it — plus the one this
// move adds: something already occupies the destination. A worktree whose git
// status cannot be read refuses too; the point of the gate is that a directory
// pop cannot vouch for is never relocated.
func worktreeMoveRefusals(d *tasks.Deps, pending []managedWorktree) []string {
	if len(pending) == 0 {
		return nil
	}
	drains, err := tasks.LiveRunningDrains(d)
	if err != nil {
		return []string{fmt.Sprintf("cannot read live drains: %v", err)}
	}
	var out []string
	for _, w := range pending {
		if _, err := d.FS.Stat(w.To); err == nil {
			out = append(out, fmt.Sprintf("%s: %s already exists", w.label(), w.To))
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			out = append(out, fmt.Sprintf("%s: cannot read %s: %v", w.label(), w.To, err))
			continue
		}
		if dr, ok := liveDrainOn(drains, w.From); ok {
			out = append(out, fmt.Sprintf("%s: live drain of %s (pid %d) in %s", w.label(), dr.SetID, dr.PID, w.From))
			continue
		}
		dirty, err := tasks.RuntimeIsDirty(d, w.From)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: cannot read git status of %s: %v", w.label(), w.From, err))
			continue
		}
		if dirty {
			out = append(out, fmt.Sprintf("%s: uncommitted changes in %s", w.label(), w.From))
		}
	}
	return out
}

// liveDrainOn returns the live drain executing in path, or under it — a drain
// records the checkout root, but a nested runtime path must block the move just
// the same.
func liveDrainOn(drains []tasks.RunningDrain, path string) (tasks.RunningDrain, bool) {
	target := filepath.Clean(path)
	for _, dr := range drains {
		p := filepath.Clean(dr.RuntimePath)
		if p == target || strings.HasPrefix(p, target+string(os.PathSeparator)) {
			return dr, true
		}
	}
	return tasks.RunningDrain{}, false
}

// repairMovedWorktrees runs `git worktree repair` once per affected repository,
// passing every new path that repository owns. Moving a linked worktree by hand
// leaves the repository's worktrees/<name>/gitdir file pointing at the old
// directory; repair re-establishes the link. Run from inside one of the moved
// worktrees, which still resolves its repository because its own .git file holds
// an absolute path into the repository that the move did not touch.
func repairMovedWorktrees(d *tasks.Deps, moved []managedWorktree) (repaired, warnings []string) {
	byRepo := map[string][]string{}
	var order []string
	for _, w := range moved {
		if _, seen := byRepo[w.RepoKey]; !seen {
			order = append(order, w.RepoKey)
		}
		byRepo[w.RepoKey] = append(byRepo[w.RepoKey], w.To)
	}
	for _, repoKey := range order {
		paths := byRepo[repoKey]
		args := append([]string{"worktree", "repair"}, paths...)
		if _, err := d.Git.CommandInDir(paths[0], args...); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: git worktree repair: %v", repoKey, err))
			continue
		}
		repaired = append(repaired, repoKey)
	}
	return repaired, warnings
}

// retireLegacyRoot deletes the pre-cut root once the move emptied it, so the
// fold stops looking and the sign-off check ("no queue/worktrees remains") can be
// made by eye. It drops the now-childless per-repository directories first, since
// the move relocates worktrees and leaves their parents behind. Anything else the
// walk skipped — a stray file, a leftover a refusal left behind — keeps the
// directory alive on purpose.
func retireLegacyRoot(d *tasks.Deps, from string) {
	entries, err := d.FS.ReadDir(from)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoDir := filepath.Join(from, entry.Name())
		if children, err := d.FS.ReadDir(repoDir); err == nil && len(children) == 0 {
			_ = d.FS.RemoveAll(repoDir)
		}
	}
	if remaining, err := d.FS.ReadDir(from); err == nil && len(remaining) == 0 {
		_ = d.FS.RemoveAll(from)
	}
}
