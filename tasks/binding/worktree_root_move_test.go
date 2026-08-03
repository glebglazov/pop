package binding

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// seedLegacyManagedWorktree provisions a managed worktree under the pre-cut root
// and records its binding, exactly as a pop build from before the `pop work` cut
// left the machine. The binding row goes in through the store rather than this
// package's Put, because Put is one of the accessors the move folds off — going
// through it would relocate the very state the test is trying to construct.
func seedLegacyManagedWorktree(t *testing.T, td *tasks.Deps, repo, setID string) Binding {
	t.Helper()
	b, err := ProvisionWorktree(td, LegacyManagedWorktreesRoot(td), repo, setID, "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision legacy managed worktree: %v", err)
	}
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	b.ScopedKey = Key(id, setID)
	if err := s.PutBinding(b); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	return b
}

func bindingRuntimePath(t *testing.T, td *tasks.Deps, repo, setID string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	b, ok, err := Lookup(td, Key(id, setID))
	if err != nil || !ok {
		t.Fatalf("lookup binding for %s: ok=%v err=%v", setID, ok, err)
	}
	return b.RuntimePath
}

// TestManagedWorktreesRootIsUnderTheWorkDataDir pins where new worktrees are
// provisioned: the `pop work` data dir, not the retired queue one.
func TestManagedWorktreesRootIsUnderTheWorkDataDir(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)

	b, err := ProvisionWorktree(td, ManagedWorktreesRoot(td), repo, "set-new", "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	wantRoot := filepath.Join(filepath.Dir(tasks.TaskStorageRoot(td)), "work", "worktrees")
	if ManagedWorktreesRoot(td) != wantRoot {
		t.Fatalf("managed root = %q, want %q", ManagedWorktreesRoot(td), wantRoot)
	}
	if !strings.HasPrefix(b.RuntimePath, wantRoot+string(os.PathSeparator)) {
		t.Fatalf("provisioned worktree %q is not under %q", b.RuntimePath, wantRoot)
	}
}

// TestMoveManagedWorktreesRootRelocatesRepointsAndRepairs drives the whole move:
// the directories land under the new root, every recorded checkout follows, the
// repository's worktree administrative files are re-linked (so `git worktree
// list` names the new path), and the emptied legacy root is retired.
func TestMoveManagedWorktreesRootRelocatesRepointsAndRepairs(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	first := seedLegacyManagedWorktree(t, td, repo, "set-one")
	second := seedLegacyManagedWorktree(t, td, repo, "set-two")
	writeFileCommit(t, first.RuntimePath, "one.txt", "one\n", "one")

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if move.Refused() {
		t.Fatalf("clean worktrees must not refuse: %v", move.Refusals)
	}
	if len(move.Moved) != 2 || move.Rewritten != 2 {
		t.Fatalf("move = %+v, want 2 moved and 2 rewritten", move)
	}

	newRoot := ManagedWorktreesRoot(td)
	for _, b := range []Binding{first, second} {
		rel, err := filepath.Rel(LegacyManagedWorktreesRoot(td), b.RuntimePath)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		want := filepath.Join(newRoot, rel)
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("moved worktree %q: %v", want, err)
		}
		if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy worktree %q still present, stat err = %v", b.RuntimePath, err)
		}
	}
	if got := bindingRuntimePath(t, td, repo, "set-one"); !strings.HasPrefix(got, newRoot+string(os.PathSeparator)) {
		t.Fatalf("binding runtime path = %q, want it under %q", got, newRoot)
	}

	// `git worktree repair` ran once for the repository, so the repo's reverse
	// pointers name the new directories and none of the old ones.
	if len(move.Repaired) != 1 {
		t.Fatalf("Repaired = %v, want one repository", move.Repaired)
	}
	if len(move.RepairWarnings) != 0 {
		t.Fatalf("RepairWarnings = %v, want none", move.RepairWarnings)
	}
	list, err := td.Git.CommandInDir(repo, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	if !strings.Contains(list, newRoot) {
		t.Fatalf("worktree list does not name the new root %q:\n%s", newRoot, list)
	}
	if strings.Contains(list, LegacyManagedWorktreesRoot(td)) {
		t.Fatalf("worktree list still names the legacy root:\n%s", list)
	}

	// The emptied legacy root is gone, so the fold stops looking.
	if _, err := os.Stat(LegacyManagedWorktreesRoot(td)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy root should be retired, stat err = %v", err)
	}
	// A second call is a no-op.
	again, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("second move: %v", err)
	}
	if again.Ran() || again.Refused() || again.From != "" {
		t.Fatalf("second move = %+v, want an empty no-op", again)
	}
}

// TestMoveManagedWorktreesRootRefusesDirtyWorktree: one worktree with
// uncommitted changes refuses the whole move — its clean sibling stays put too —
// and both the filesystem and the recorded paths are exactly as they were.
func TestMoveManagedWorktreesRootRefusesDirtyWorktree(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	dirty := seedLegacyManagedWorktree(t, td, repo, "set-dirty")
	clean := seedLegacyManagedWorktree(t, td, repo, "set-clean")
	if err := os.WriteFile(filepath.Join(dirty.RuntimePath, "scratch.txt"), []byte("in flight\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted file: %v", err)
	}

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !move.Refused() {
		t.Fatalf("a dirty managed worktree must refuse the move, got %+v", move)
	}
	if len(move.Refusals) != 1 || !strings.Contains(move.Refusals[0], "set-dirty") ||
		!strings.Contains(move.Refusals[0], "uncommitted changes") {
		t.Fatalf("Refusals = %v, want one line naming set-dirty's uncommitted changes", move.Refusals)
	}
	if move.Ran() || move.Rewritten != 0 {
		t.Fatalf("a refused move must write nothing, got %+v", move)
	}
	for _, b := range []Binding{dirty, clean} {
		if _, err := os.Stat(b.RuntimePath); err != nil {
			t.Fatalf("worktree %q must stay where it is: %v", b.RuntimePath, err)
		}
	}
	if _, err := os.Stat(ManagedWorktreesRoot(td)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused move must not create the new root, stat err = %v", err)
	}
	if got := bindingRuntimePath(t, td, repo, "set-clean"); got != clean.RuntimePath {
		t.Fatalf("binding runtime path = %q, want it unchanged at %q", got, clean.RuntimePath)
	}
}

// TestMoveManagedWorktreesRootRefusesLiveDrain: a checkout a live drain is
// executing in is never relocated under the running agent's feet, and the
// refusal names the set and the PID holding it.
func TestMoveManagedWorktreesRootRefusesLiveDrain(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	busy := seedLegacyManagedWorktree(t, td, repo, "set-busy")

	s, _, err := td.Store(true)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StartDrain(store.Drain{
		Repo:        repo,
		SetID:       "set-busy",
		RuntimePath: busy.RuntimePath,
		PID:         os.Getpid(),
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !move.Refused() {
		t.Fatalf("a live drain must refuse the move, got %+v", move)
	}
	if len(move.Refusals) != 1 || !strings.Contains(move.Refusals[0], "set-busy") ||
		!strings.Contains(move.Refusals[0], "live drain") {
		t.Fatalf("Refusals = %v, want one line naming set-busy's live drain", move.Refusals)
	}
	if _, err := os.Stat(busy.RuntimePath); err != nil {
		t.Fatalf("busy worktree must stay where it is: %v", err)
	}
	if got := bindingRuntimePath(t, td, repo, "set-busy"); got != busy.RuntimePath {
		t.Fatalf("binding runtime path = %q, want it unchanged at %q", got, busy.RuntimePath)
	}
}

// TestMoveManagedWorktreesRootRefusesOccupiedDestination: the move never writes
// over a directory that is already at the destination — it names it and leaves
// both ends alone for a human to sort out.
func TestMoveManagedWorktreesRootRefusesOccupiedDestination(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	legacy := seedLegacyManagedWorktree(t, td, repo, "set-collide")

	rel, err := filepath.Rel(LegacyManagedWorktreesRoot(td), legacy.RuntimePath)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	occupied := filepath.Join(ManagedWorktreesRoot(td), rel)
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatalf("occupy destination: %v", err)
	}

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !move.Refused() || !strings.Contains(move.Refusals[0], "already exists") {
		t.Fatalf("Refusals = %v, want an occupied-destination refusal", move.Refusals)
	}
	if _, err := os.Stat(legacy.RuntimePath); err != nil {
		t.Fatalf("worktree must stay where it is: %v", err)
	}
}

// TestMoveManagedWorktreesRootHealsAPartialMove: a crash between the directory
// renames and the recorded-path rewrite leaves worktrees at the new root with
// bindings still naming the old one. The next run rewrites the whole prefix, so
// those bindings are repointed along with the worktrees it moves itself.
func TestMoveManagedWorktreesRootHealsAPartialMove(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	crashed := seedLegacyManagedWorktree(t, td, repo, "set-crashed")
	remaining := seedLegacyManagedWorktree(t, td, repo, "set-remaining")

	// Replay the interrupted state: one directory already moved, its binding not.
	rel, err := filepath.Rel(LegacyManagedWorktreesRoot(td), crashed.RuntimePath)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	moved := filepath.Join(ManagedWorktreesRoot(td), rel)
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Rename(crashed.RuntimePath, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if move.Refused() {
		t.Fatalf("a partial move must not refuse: %v", move.Refusals)
	}
	if len(move.Moved) != 1 {
		t.Fatalf("Moved = %v, want only the worktree left behind", move.Moved)
	}
	if move.Rewritten != 2 {
		t.Fatalf("Rewritten = %d, want both bindings repointed", move.Rewritten)
	}
	if got := bindingRuntimePath(t, td, repo, "set-crashed"); got != moved {
		t.Fatalf("crashed binding = %q, want %q", got, moved)
	}
	if got := bindingRuntimePath(t, td, repo, "set-remaining"); strings.HasPrefix(got, LegacyManagedWorktreesRoot(td)) {
		t.Fatalf("remaining binding = %q, want it off the legacy root (was %q)", got, remaining.RuntimePath)
	}
}

// TestPendingWorktreeRootMoveNamesOffendersWithoutMoving: the read-only
// inspection `pop doctor` uses reports the same refusal the move would make, and
// touches nothing while doing it.
func TestPendingWorktreeRootMoveNamesOffendersWithoutMoving(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	dirty := seedLegacyManagedWorktree(t, td, repo, "set-inspect")
	if err := os.WriteFile(filepath.Join(dirty.RuntimePath, "scratch.txt"), []byte("in flight\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted file: %v", err)
	}

	pending, err := PendingWorktreeRootMove(td)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(pending.Pending) != 1 || !strings.Contains(pending.Pending[0], "set-inspect") {
		t.Fatalf("Pending = %v, want the one worktree named", pending.Pending)
	}
	if !pending.Refused() || !strings.Contains(pending.Refusals[0], "uncommitted changes") {
		t.Fatalf("Refusals = %v, want the dirty-tree refusal", pending.Refusals)
	}
	if pending.Ran() {
		t.Fatal("the inspection must never move anything")
	}
	if _, err := os.Stat(dirty.RuntimePath); err != nil {
		t.Fatalf("worktree must stay where it is: %v", err)
	}

	// Nothing pending once the legacy root is gone.
	if err := os.RemoveAll(LegacyManagedWorktreesRoot(td)); err != nil {
		t.Fatalf("remove legacy root: %v", err)
	}
	clean, err := PendingWorktreeRootMove(td)
	if err != nil {
		t.Fatalf("inspect after retirement: %v", err)
	}
	if clean.From != "" || len(clean.Pending) != 0 || clean.Refused() {
		t.Fatalf("inspection after retirement = %+v, want empty", clean)
	}
}

// TestFoldAfterWorktreeRootMoveTearsDownAtTheNewPath is the end of the road for a
// moved worktree: the set it is bound to still folds, and the checkout is still
// recognised as pop-provisioned, so integration tears it down at its new path.
func TestFoldAfterWorktreeRootMoveTearsDownAtTheNewPath(t *testing.T) {
	t.Parallel()
	td := lifecycleTestDeps(t)
	repo := initAdoptRepo(t)
	seedDoneTaskSet(t, td, repo, "set-moved-fold")
	b := seedLegacyManagedWorktree(t, td, repo, "set-moved-fold")
	writeFileCommit(t, b.RuntimePath, "feature.txt", "moved work\n", "moved work")

	move, err := MoveManagedWorktreesRoot(td)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !move.Ran() {
		t.Fatalf("move did not run: %+v", move)
	}
	newPath := move.Moved[0]

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	got, err := Fold(td, nil, cfg, "set-moved-fold", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold after move: %v", err)
	}
	if !got.TornDown {
		t.Fatal("TornDown = false: a moved managed worktree must still read as provisioned")
	}
	if got.RuntimePath != newPath {
		t.Fatalf("folded checkout = %q, want the moved path %q", got.RuntimePath, newPath)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moved worktree should be torn down, stat err = %v", err)
	}
	// The set's work reached the trunk.
	out, err := td.Git.CommandInDir(repo, "log", "--oneline")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(out, "moved work") {
		t.Fatalf("trunk log does not carry the folded commit:\n%s", out)
	}
}
