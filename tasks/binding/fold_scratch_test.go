package binding

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func TestFoldScratchBranchIsDeterministicAndFlattened(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ branch, want string }{
		{"feat/thing", "pop/fold/feat-thing"},
		{"pop/set-x/20260820-120000", "pop/fold/pop-set-x-20260820-120000"},
		{"human-work", "pop/fold/human-work"},
	} {
		if got := foldScratchBranch(tc.branch); got != tc.want {
			t.Errorf("foldScratchBranch(%q) = %q, want %q", tc.branch, got, tc.want)
		}
		if again := foldScratchBranch(tc.branch); again != tc.want {
			t.Errorf("foldScratchBranch(%q) is not deterministic: %q then %q", tc.branch, tc.want, again)
		}
	}
}

func currentBranchAt(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runGitOutput(t, dir, "branch", "--show-current"))
}

func refAt(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(runGitOutput(t, dir, "rev-parse", ref))
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	return strings.TrimSpace(runGitOutput(t, dir, "branch", "--list", branch)) != ""
}

// The whole point of the scratch branch: the branch the fold was asked to land is
// still standing at its pre-fold tip while the rebase rewrites and while trunk is
// about to move, and it changes exactly once — after the work is already in trunk.
func TestFoldRebasesScratchBranchAndMovesTheRealBranchOnlyAfterTrunkHasTheWork(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-scratch")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-scratch",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch(b.Branch)
	tipBefore := refAt(t, b.RuntimePath, b.Branch)

	var atRebase, atFastForward struct {
		branch string
		tip    string
	}
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "rebase" {
				atRebase.branch = currentBranchAt(t, dir)
				atRebase.tip = refAt(t, dir, b.Branch)
			}
			if len(args) >= 2 && args[0] == "merge" && args[1] == "--ff-only" {
				atFastForward.branch = currentBranchAt(t, b.RuntimePath)
				atFastForward.tip = refAt(t, b.RuntimePath, b.Branch)
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	// Teardown declined so the folded checkout survives for inspection.
	if _, err := Fold(td, nil, cfg, "set-scratch", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("fold: %v", err)
	}

	if atRebase.branch != scratch {
		t.Fatalf("rebase ran on branch %q, want the fold scratch branch %q", atRebase.branch, scratch)
	}
	if atRebase.tip != tipBefore {
		t.Fatalf("%s moved before the rebase: %s -> %s", b.Branch, tipBefore, atRebase.tip)
	}
	if atFastForward.branch != scratch {
		t.Fatalf("at the fast-forward the checkout stood on %q, want %q — a checked-out branch cannot be forced", atFastForward.branch, scratch)
	}
	if atFastForward.tip != tipBefore {
		t.Fatalf("%s moved before trunk had the work: %s -> %s", b.Branch, tipBefore, atFastForward.tip)
	}

	trunkTip := refAt(t, repo, "HEAD")
	if got := refAt(t, b.RuntimePath, b.Branch); got != trunkTip {
		t.Fatalf("%s = %s after fold, want the landed trunk tip %s", b.Branch, got, trunkTip)
	}
	if got := currentBranchAt(t, b.RuntimePath); got != b.Branch {
		t.Fatalf("folded checkout left on %q, want its own branch %q", got, b.Branch)
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("fold scratch branch %s survived a successful fold", scratch)
	}
	if dirty, err := worktreeIsDirty(td, b.RuntimePath); err != nil || dirty {
		t.Fatalf("folded checkout should be clean at the landed tip: dirty=%v err=%v", dirty, err)
	}
}

// The fast-forward is the fold's one irreversible act, so it comes first: a fold
// that cannot land leaves the real branch exactly where it was, not rewritten for
// nothing.
func TestFoldFailedFastForwardLeavesTheRealBranchUnmoved(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-ff-fail")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-ff-fail",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	tipBefore := refAt(t, b.RuntimePath, b.Branch)
	trunkBefore := refAt(t, repo, "HEAD")

	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			// Trunk stays put, so this is a fast-forward that simply would not go —
			// not the trunk-moved race.
			if len(args) >= 2 && args[0] == "merge" && args[1] == "--ff-only" {
				return "", fmt.Errorf("simulated fast-forward failure")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-ff-fail", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "could not fast-forward trunk") {
		t.Fatalf("err = %v, want a fast-forward refusal", err)
	}
	if got := refAt(t, b.RuntimePath, b.Branch); got != tipBefore {
		t.Fatalf("%s was rewritten by a fold that never landed: %s -> %s", b.Branch, tipBefore, got)
	}
	if got := refAt(t, repo, "HEAD"); got != trunkBefore {
		t.Fatalf("trunk moved: %s -> %s", trunkBefore, got)
	}
	if got := currentBranchAt(t, b.RuntimePath); got != b.Branch {
		t.Fatalf("refused fold left the checkout on %q, want %q", got, b.Branch)
	}
	if branchExists(t, repo, foldScratchBranch(b.Branch)) {
		t.Fatalf("a fold that stopped before landing must leave no scratch branch")
	}
	if _, _, ok, _ := FindBySetID(td, "set-ff-fail"); !ok {
		t.Fatal("binding must remain after a refused fold")
	}
}

// A branch trunk already carries would fold to nothing at all, so it is refused by
// name rather than reported as a successful fold.
func TestFoldRefusesBranchAlreadyContainedInTrunk(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-contained")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-contained",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	rec := &recordingGit{inner: td.Git}
	td.Git = rec
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-contained", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "already contained in trunk") {
		t.Fatalf("err = %v, want the already-contained refusal", err)
	}
	if !strings.Contains(err.Error(), b.Branch) {
		t.Fatalf("err = %v, want it to name %s", err, b.Branch)
	}
	if rec.ran("rebase") {
		t.Fatal("a contained branch must be refused in preflight, before any rebase")
	}
	if _, _, ok, _ := FindBySetID(td, "set-contained"); !ok {
		t.Fatal("binding must remain after a refused fold")
	}
}

// Conflict resolution can run for an hour after preflight looked at trunk, so trunk
// is asked again on the edge of the fast-forward — in preflight's own words.
func TestFoldRechecksTrunkImmediatelyBeforeTheFastForward(t *testing.T) {
	t.Parallel()

	// Each case dirties or claims trunk once the rebase has run, which is the window
	// preflight cannot see.
	cases := []struct {
		name    string
		disturb func(t *testing.T, td *tasks.Deps, trunkPath string)
		want    string
	}{
		{
			name: "dirty",
			disturb: func(t *testing.T, _ *tasks.Deps, trunkPath string) {
				if err := os.WriteFile(filepath.Join(trunkPath, "trunk-dirt.txt"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "Trunk worktree is dirty",
		},
		{
			name: "live claim",
			disturb: func(t *testing.T, td *tasks.Deps, trunkPath string) {
				h, err := tasks.BeginDrain(td, trunkPath, "trunk-holder", io.Discard)
				if err != nil {
					t.Fatalf("BeginDrain on trunk: %v", err)
				}
				t.Cleanup(func() { _ = h.Finish(store.DrainEnding{State: store.StateFinished}) })
			},
			want: "live claim",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := initAdoptRepo(t)
			td := lifecycleTestDeps(t)
			setID := "set-recheck-" + strings.ReplaceAll(tc.name, " ", "-")
			seedDoneTaskSet(t, td, repo, setID)
			b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
				TD: td, CheckoutPath: repo, SetID: setID,
			})
			if err != nil {
				t.Fatalf("provision: %v", err)
			}
			writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
			writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
			tipBefore := refAt(t, b.RuntimePath, b.Branch)
			trunkBefore := refAt(t, repo, "HEAD")
			trunkPath, _, err := ResolveTrunkPath(td, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, b.RuntimePath)
			if err != nil {
				t.Fatalf("resolve trunk: %v", err)
			}

			var ffAttempts atomic.Int32
			inner := td.Git
			td.Git = &interceptGit{
				inner: inner,
				onCommandInDir: func(dir string, args ...string) (string, error) {
					if len(args) >= 2 && args[0] == "merge" && args[1] == "--ff-only" {
						ffAttempts.Add(1)
					}
					out, err := inner.CommandInDir(dir, args...)
					if len(args) >= 1 && args[0] == "rebase" {
						tc.disturb(t, td, trunkPath)
					}
					return out, err
				},
			}

			cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
			_, err = Fold(td, nil, cfg, setID, FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal saying %q", err, tc.want)
			}
			if got := ffAttempts.Load(); got != 0 {
				t.Fatalf("fast-forward attempts = %d, want none once trunk stopped being fit to land", got)
			}
			if got := refAt(t, repo, "HEAD"); got != trunkBefore {
				t.Fatalf("trunk moved: %s -> %s", trunkBefore, got)
			}
			if got := refAt(t, b.RuntimePath, b.Branch); got != tipBefore {
				t.Fatalf("%s was rewritten by a fold that never landed: %s -> %s", b.Branch, tipBefore, got)
			}
		})
	}
}

// Trunk moving under a fold costs the rebase, not the work: the scratch branch is
// reset to the recorded tip and rebased again, so what lands is one copy of the
// branch replayed onto the trunk that actually exists.
func TestFoldTrunkMovedAfterRebaseResetsScratchToTheRecordedTipAndRedoesOnce(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-redo")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-redo",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch(b.Branch)
	tipBefore := refAt(t, b.RuntimePath, b.Branch)

	var (
		mu       sync.Mutex
		startsAt []string
		rebases  int
	)
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 3 && args[0] == "checkout" && args[1] == "-B" && args[2] == scratch {
				mu.Lock()
				startsAt = append(startsAt, args[3])
				mu.Unlock()
			}
			out, err := inner.CommandInDir(dir, args...)
			if len(args) >= 1 && args[0] == "rebase" {
				mu.Lock()
				rebases++
				first := rebases == 1
				mu.Unlock()
				// Trunk moves once, in exactly the window the fold cannot hold shut.
				if first {
					writeFileCommit(t, repo, "race.txt", "moved\n", "trunk moved mid-fold")
				}
			}
			return out, err
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := Fold(td, nil, cfg, "set-redo", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("fold after one trunk move: %v", err)
	}

	if rebases != 2 {
		t.Fatalf("rebases = %d, want 2 (the redo)", rebases)
	}
	if len(startsAt) != 2 || startsAt[0] != tipBefore || startsAt[1] != tipBefore {
		t.Fatalf("scratch branch start points = %v, want it created and reset at the recorded tip %s", startsAt, tipBefore)
	}
	log := runGitOutput(t, repo, "log", "--oneline")
	if got := strings.Count(log, "set work"); got != 1 {
		t.Fatalf("trunk carries the set commit %d times, want exactly one copy:\n%s", got, log)
	}
	if !strings.Contains(log, "trunk moved mid-fold") {
		t.Fatalf("trunk must keep the commit that landed mid-fold:\n%s", log)
	}
	if merges := strings.TrimSpace(runGitOutput(t, repo, "log", "--merges", "--oneline")); merges != "" {
		t.Fatalf("trunk history must stay linear:\n%s", merges)
	}
	if got := refAt(t, b.RuntimePath, b.Branch); got != refAt(t, repo, "HEAD") {
		t.Fatalf("%s = %s, want the landed trunk tip", b.Branch, got)
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("fold scratch branch %s survived a successful fold", scratch)
	}
}
