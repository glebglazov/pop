package binding

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// Abandon is the exit that says "this fold never happened": the rebase is aborted,
// the checkout goes back to its own branch and the scratch ref goes, leaving every
// ref where the fold found it.
func TestFoldConflictAbandonRollsBackToThePreFoldState(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-abandon")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-abandon",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
	scratch := foldScratchBranch(b.Branch)
	tipBefore := refAt(t, b.RuntimePath, b.Branch)
	trunkBefore := refAt(t, repo, "HEAD")
	branchesBefore := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list"))

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out strings.Builder
	_, err = Fold(td, nil, cfg, "set-abandon", FoldOptions{In: strings.NewReader("5\n")}, LifecycleHooks{}, &out)
	if !errors.Is(err, tasks.ErrFoldAbandon) {
		t.Fatalf("err = %v, want ErrFoldAbandon", err)
	}
	if !strings.Contains(out.String(), "5. Abandon fold") {
		t.Fatalf("conflict prompt must offer abandon:\n%s", out.String())
	}
	if rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("abandon must abort the rebase, not park it")
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("abandon must delete the fold scratch branch %s", scratch)
	}
	if got := refAt(t, b.RuntimePath, b.Branch); got != tipBefore {
		t.Fatalf("%s moved: %s -> %s", b.Branch, tipBefore, got)
	}
	if got := refAt(t, repo, "HEAD"); got != trunkBefore {
		t.Fatalf("trunk moved: %s -> %s", trunkBefore, got)
	}
	if got := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list")); got != branchesBefore {
		t.Fatalf("branch list changed\nbefore:\n%s\nafter:\n%s", branchesBefore, got)
	}
	if got := currentBranchAt(t, b.RuntimePath); got != b.Branch {
		t.Fatalf("abandoned fold left the checkout on %q, want %q", got, b.Branch)
	}
	if dirty, err := worktreeIsDirty(td, b.RuntimePath); err != nil || dirty {
		t.Fatalf("abandoned fold must leave a clean checkout: dirty=%v err=%v", dirty, err)
	}
	if _, _, ok, _ := FindBySetID(td, "set-abandon"); !ok {
		t.Fatal("binding must remain after an abandoned fold")
	}
}

// Exit is the other intention: put the fold down as it stands. The rebase stays in
// progress and the next fold picks the resolution up rather than refusing on the
// dirtiness that conflict created.
func TestFoldConflictExitParksAndALaterFoldResumesIt(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-park")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-park",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
	scratch := foldScratchBranch(b.Branch)
	tipBefore := refAt(t, b.RuntimePath, b.Branch)
	trunkBefore := refAt(t, repo, "HEAD")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-park", FoldOptions{In: strings.NewReader("0\n")}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "rebase still in progress") {
		t.Fatalf("err = %v, want a refusal naming the parked rebase", err)
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("exit must park the rebase")
	}
	if got := rebasingBranch(td, b.RuntimePath); got != scratch {
		t.Fatalf("parked rebase is rewriting %q, want the fold scratch branch %q", got, scratch)
	}
	if !branchExists(t, repo, scratch) {
		t.Fatalf("a parked fold keeps its scratch branch %s", scratch)
	}
	if got := refAt(t, b.RuntimePath, b.Branch); got != tipBefore {
		t.Fatalf("%s moved while parked: %s -> %s", b.Branch, tipBefore, got)
	}
	if got := refAt(t, repo, "HEAD"); got != trunkBefore {
		t.Fatalf("trunk moved while parked: %s -> %s", trunkBefore, got)
	}

	// The human resolves the conflict and folds again: resume, not a fresh start.
	if err := os.WriteFile(filepath.Join(b.RuntimePath, "clash.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", b.RuntimePath, "add", "clash.txt").Run(); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := Fold(td, nil, cfg, "set-park", FoldOptions{
		Yes: true,
		In:  strings.NewReader("2\nn\n"), // resume, decline the post-resolve verify
	}, LifecycleHooks{}, &out); err != nil {
		t.Fatalf("fold resuming a parked rebase: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "dirty") {
		t.Fatalf("a parked fold must not be refused as dirty:\n%s", out.String())
	}
	if rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("resume must finish the rebase")
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("fold scratch branch %s survived a completed fold", scratch)
	}
	if got := refAt(t, repo, "HEAD"); got == trunkBefore {
		t.Fatal("trunk must carry the resolved work after the resumed fold")
	}
}

// A scratch branch at preflight is normal. Which of the three things it means is
// read from git alone, and each reading has its own outcome.
func TestPreflightClassifiesAnExistingFoldScratchBranch(t *testing.T) {
	t.Parallel()

	t.Run("parked routes into the conflict prompt", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		wt := addLinkedWorktree(t, repo, "human-work")
		writeFileCommit(t, wt, "clash.txt", "from-branch\n", "branch clash")
		writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
		scratch := foldScratchBranch("human-work")
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

		if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err == nil {
			t.Fatal("want the conflict refusal that parks the rebase")
		}
		parked, ok := readParkedFold(td, wt)
		if !ok || parked.scratch != scratch {
			t.Fatalf("readParkedFold = %+v, %v; want the parked fold on %s", parked, ok, scratch)
		}
		// A parked rebase detaches HEAD, so the branch being folded is read back from
		// the scratch ref git is rewriting.
		if parked.branch != "human-work" {
			t.Fatalf("parked branch = %q, want human-work", parked.branch)
		}
		plan := foldCheckoutPlan{path: wt, trunkPath: repo, branch: "human-work", trunkBranch: CurrentBranch(td, repo)}
		if got := classifyFoldScratch(td, ok, plan, scratch); got != foldScratchParked {
			t.Fatalf("classification = %v, want parked", got)
		}

		var out strings.Builder
		_, err := FoldCheckout(td, cfg, wt, FoldOptions{In: strings.NewReader("0\n")}, &out)
		if err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("err = %v, want the conflict refusal on exit", err)
		}
		for _, unwanted := range []string{"dirty", "detached"} {
			if strings.Contains(err.Error(), unwanted) {
				t.Fatalf("a parked fold must not be refused as %s: %v", unwanted, err)
			}
		}
		if !strings.Contains(out.String(), "Fold conflict") {
			t.Fatalf("a parked fold re-enters the conflict prompt:\n%s", out.String())
		}
	})

	t.Run("residue is deleted and the fold proceeds", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		wt := addLinkedWorktree(t, repo, "human-work")
		writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
		writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
		scratch := foldScratchBranch("human-work")
		// A fold that died on its way back from the branch move: the branch already
		// reaches the scratch ref, so the ref holds nothing of its own.
		runGitOutput(t, wt, "branch", scratch, "human-work")
		plan := foldCheckoutPlan{path: wt, trunkPath: repo, branch: "human-work", trunkBranch: CurrentBranch(td, repo)}
		if got := classifyFoldScratch(td, false, plan, scratch); got != foldScratchResidue {
			t.Fatalf("classification = %v, want residue", got)
		}

		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err != nil {
			t.Fatalf("fold over residue: %v", err)
		}
		if branchExists(t, repo, scratch) {
			t.Fatalf("residue %s must be deleted", scratch)
		}
		if got := refAt(t, wt, "human-work"); got != refAt(t, repo, "HEAD") {
			t.Fatalf("human-work = %s, want the landed trunk tip %s", got, refAt(t, repo, "HEAD"))
		}
	})

	t.Run("ambiguous is refused by name", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		wt := addLinkedWorktree(t, repo, "human-work")
		writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
		writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
		scratch := foldScratchBranch("human-work")
		// A scratch-named ref carrying a commit nothing else holds: pop cannot say what
		// it is, so it must not touch it.
		tree := strings.TrimSpace(runGitOutput(t, wt, "rev-parse", "HEAD^{tree}"))
		stray := strings.TrimSpace(runGitOutput(t, wt, "commit-tree", tree, "-p", "HEAD", "-m", "stray"))
		runGitOutput(t, wt, "branch", scratch, stray)
		plan := foldCheckoutPlan{path: wt, trunkPath: repo, branch: "human-work", trunkBranch: CurrentBranch(td, repo)}
		if got := classifyFoldScratch(td, false, plan, scratch); got != foldScratchAmbiguous {
			t.Fatalf("classification = %v, want ambiguous", got)
		}

		tipBefore := refAt(t, wt, "human-work")
		trunkBefore := refAt(t, repo, "HEAD")
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		rec := &recordingGit{inner: td.Git}
		td.Git = rec
		_, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), scratch) {
			t.Fatalf("err = %v, want a refusal naming %s", err, scratch)
		}
		if rec.ran("rebase") {
			t.Fatal("an ambiguous scratch ref must be refused before any rebase")
		}
		if got := refAt(t, wt, scratch); got != stray {
			t.Fatalf("ambiguous ref %s moved: %s -> %s", scratch, stray, got)
		}
		if got := refAt(t, wt, "human-work"); got != tipBefore {
			t.Fatalf("human-work moved: %s -> %s", tipBefore, got)
		}
		if got := refAt(t, repo, "HEAD"); got != trunkBefore {
			t.Fatalf("trunk moved: %s -> %s", trunkBefore, got)
		}
	})
}

// After the fast-forward the work is landed, so the remaining ref updates are worth
// retrying; when they still fail, the report says what holds the work and what does
// not — and trunk keeps it either way.
func TestFoldPostLandingFailureRetriesThenReportsWithTrunkIntact(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch("human-work")
	tipBefore := refAt(t, wt, "human-work")

	var attempts atomic.Int32
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			// The branch move is the first step past the fast-forward; a held ref lock is
			// what makes it fail for reasons that may pass.
			if len(args) >= 2 && args[0] == "branch" && args[1] == "-f" {
				attempts.Add(1)
				return "", fmt.Errorf("simulated ref lock")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard)
	if err == nil {
		t.Fatal("want a post-landing failure report")
	}
	for _, want := range []string{"landed in trunk", "trunk holds the work", "human-work does not"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to say %q", err, want)
		}
	}
	if got := attempts.Load(); got != int32(foldPostLandingAttempts) {
		t.Fatalf("branch move attempts = %d, want the bounded %d", got, foldPostLandingAttempts)
	}
	trunkTip := refAt(t, repo, "HEAD")
	if trunkTip != refAt(t, wt, scratch) {
		t.Fatalf("trunk = %s, want the folded tip %s — trunk is never unwound", trunkTip, refAt(t, wt, scratch))
	}
	log := runGitOutput(t, repo, "log", "--oneline")
	if got := strings.Count(log, "branch work"); got != 1 {
		t.Fatalf("trunk carries the folded commit %d times, want one:\n%s", got, log)
	}
	if got := refAt(t, wt, "human-work"); got != tipBefore {
		t.Fatalf("human-work = %s, want its pre-fold tip %s — the report says it did not move", got, tipBefore)
	}
}

// A failure while reading trunk after the scratch rebase is still before Fold's
// irreversible boundary. The checkout and scratch ref must therefore roll back.
func TestFoldTrunkReadFailureAfterRebaseRollsBackBeforeLanding(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	branchTip := refAt(t, wt, "human-work")
	trunkTip := refAt(t, repo, "HEAD")
	scratch := foldScratchBranch("human-work")

	inner := td.Git
	var rebaseFinished atomic.Bool
	var readFailed atomic.Bool
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "rebase" {
				out, err := inner.CommandInDir(dir, args...)
				if err == nil {
					rebaseFinished.Store(true)
				}
				return out, err
			}
			if len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD" && rebaseFinished.Load() && readFailed.CompareAndSwap(false, true) {
				return "", fmt.Errorf("simulated trunk read failure")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err == nil || !strings.Contains(err.Error(), "read trunk HEAD") {
		t.Fatalf("err = %v, want the trunk read refusal", err)
	}
	if got := currentBranchAt(t, wt); got != "human-work" {
		t.Fatalf("checkout branch = %q, want human-work", got)
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("pre-landing failure left scratch branch %s", scratch)
	}
	if got := refAt(t, wt, "human-work"); got != branchTip {
		t.Fatalf("human-work moved: %s -> %s", branchTip, got)
	}
	if got := refAt(t, repo, "HEAD"); got != trunkTip {
		t.Fatalf("trunk moved: %s -> %s", trunkTip, got)
	}
}

// Git can move trunk and then report failure, for example from a post-merge hook.
// Fold must read the refs, enter post-landing recovery, and complete the local tail.
func TestFoldFailedFastForwardThatLandedConverges(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch("human-work")

	inner := td.Git
	var fastForwardFailed atomic.Bool
	var readFailed atomic.Bool
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 3 && args[0] == "merge" && args[1] == "--ff-only" && args[2] == scratch {
				if _, err := inner.CommandInDir(dir, args...); err != nil {
					return "", err
				}
				fastForwardFailed.Store(true)
				return "", fmt.Errorf("simulated failure after trunk moved")
			}
			if len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD" && fastForwardFailed.Load() && readFailed.CompareAndSwap(false, true) {
				return "", fmt.Errorf("simulated first recovery read failure")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err != nil {
		t.Fatalf("fold after failed fast-forward that landed: %v", err)
	}
	if !readFailed.Load() {
		t.Fatal("test did not exercise the failed recovery read")
	}
	if got := refAt(t, wt, "human-work"); got != refAt(t, repo, "HEAD") {
		t.Fatalf("human-work = %s, want landed trunk tip %s", got, refAt(t, repo, "HEAD"))
	}
	if got := currentBranchAt(t, wt); got != "human-work" {
		t.Fatalf("checkout branch = %q, want human-work", got)
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("converged fold left scratch branch %s", scratch)
	}
}

// Once the real branch has moved, a checkout or scratch-delete failure is past
// Fold's irreversible boundary. A later Task-set Fold must recognize that landing,
// finish the local cleanup, and continue through binding release and teardown.
func TestTaskSetFoldRerunAfterRealBranchMovedCompletesItsTail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fails func(args []string, branch, scratch string) bool
	}{
		{
			name: "checkout failure",
			fails: func(args []string, branch, _ string) bool {
				return len(args) >= 2 && args[0] == "checkout" && args[1] == branch
			},
		},
		{
			name: "scratch delete failure",
			fails: func(args []string, _, scratch string) bool {
				return len(args) >= 3 && args[0] == "branch" && args[1] == "-d" && args[2] == scratch
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := initAdoptRepo(t)
			td := lifecycleTestDeps(t)
			setID := "set-tail"
			seedDoneTaskSet(t, td, repo, setID)
			b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
				TD: td, CheckoutPath: repo, SetID: setID,
			})
			if err != nil {
				t.Fatalf("provision: %v", err)
			}
			writeFileCommit(t, b.RuntimePath, "feature.txt", "branch work\n", "branch work")
			writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
			scratch := foldScratchBranch(b.Branch)
			cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

			inner := td.Git
			var attempts atomic.Int32
			td.Git = &interceptGit{
				inner: inner,
				onCommandInDir: func(dir string, args ...string) (string, error) {
					if tc.fails(args, b.Branch, scratch) {
						attempts.Add(1)
						return "", fmt.Errorf("simulated post-landing failure")
					}
					return inner.CommandInDir(dir, args...)
				},
			}
			if _, err := Fold(td, nil, cfg, setID, FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard); err == nil {
				t.Fatal("want the first Fold to stop after landing")
			}
			if got := attempts.Load(); got != int32(foldPostLandingAttempts) {
				t.Fatalf("post-landing attempts = %d, want %d", got, foldPostLandingAttempts)
			}
			landed := refAt(t, repo, "HEAD")
			if got := refAt(t, repo, b.Branch); got != landed {
				t.Fatalf("real branch = %s, want landed tip %s", got, landed)
			}

			// The dashboard/CLI eligibility probe must not erase the only signal that
			// tells the subsequent Fold this is a completed landing.
			td.Git = inner
			if err := PreflightFold(td, cfg, setID); err != nil {
				t.Fatalf("preflight completed landing: %v", err)
			}
			if !branchExists(t, repo, scratch) {
				t.Fatalf("read-only preflight deleted completed-landing signal %s", scratch)
			}

			got, err := Fold(td, nil, cfg, setID, FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
			if err != nil {
				t.Fatalf("rerun after real branch moved: %v", err)
			}
			if !got.TornDown {
				t.Fatal("rerun did not complete reference-counted teardown")
			}
			if _, _, ok, err := FindBySetID(td, setID); err != nil || ok {
				t.Fatalf("binding after rerun: present=%v err=%v", ok, err)
			}
			if branchExists(t, repo, scratch) {
				t.Fatalf("rerun left scratch ref %s", scratch)
			}
			if got := refAt(t, repo, "HEAD"); got != landed {
				t.Fatalf("rerun moved trunk: %s -> %s", landed, got)
			}
			if _, err := os.Stat(b.RuntimePath); !os.IsNotExist(err) {
				t.Fatalf("managed worktree survived teardown: %v", err)
			}
		})
	}
}

// filesOutsideGit lists every file under roots, skipping what git keeps for itself,
// so a test can prove a fold wrote no record of its own.
func filesOutsideGit(t *testing.T, roots ...string) []string {
	t.Helper()
	var found []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Name() == ".git" {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				found = append(found, path)
			}
			return nil
		})
	}
	sort.Strings(found)
	return found
}

// A fold killed after the fast-forward is finished by running it again: the scratch
// branch is rebuilt at the recorded tip, every commit drops as already-upstream, the
// fast-forward is a no-op and the branch lands where it was going. Nothing outside
// git says any of this happened — pop's whole data directory is deleted between the
// two runs and the second one still converges.
func TestFoldRerunAfterPostLandingCrashConvergesWithoutAJournal(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "feature.txt", "branch work\n", "branch work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch("human-work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	dataDir := td.FS.Getenv("XDG_DATA_HOME")

	// Warm up everything pop opens for itself, so the snapshot below measures the
	// fold and not pop starting up.
	_ = PreflightFold(td, cfg, "no-such-set")
	dataFilesBefore := filesOutsideGit(t, dataDir)

	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			// Stops the fold exactly where its work is landed and its branch is not.
			if len(args) >= 2 && args[0] == "branch" && args[1] == "-f" {
				return "", fmt.Errorf("simulated crash after landing")
			}
			return inner.CommandInDir(dir, args...)
		},
	}
	if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err == nil {
		t.Fatal("want the post-landing failure that leaves the fold half-done")
	}
	landed := refAt(t, repo, "HEAD")

	// Two places a progress record could hide: pop's own data directory, and the
	// checkouts. Neither holds one — the data directory is as the fold found it, and
	// every file in both checkouts is one git accounts for.
	if got := filesOutsideGit(t, dataDir); !sameStrings(got, dataFilesBefore) {
		t.Fatalf("a stopped fold wrote into pop's data directory\nbefore:\n%s\nafter:\n%s",
			strings.Join(dataFilesBefore, "\n"), strings.Join(got, "\n"))
	}
	for _, path := range []string{repo, wt} {
		if status := strings.TrimSpace(runGitOutput(t, path, "status", "--porcelain")); status != "" {
			t.Fatalf("%s holds files git does not account for:\n%s", path, status)
		}
	}

	// Nothing pop stores survives into the re-run: where the fold got to is in git.
	td.Git = inner
	if err := td.CloseStore(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("wipe pop data dir: %v", err)
	}

	if _, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard); err != nil {
		t.Fatalf("re-run after a post-landing crash: %v", err)
	}
	if got := refAt(t, repo, "HEAD"); got != landed {
		t.Fatalf("trunk moved on the converging re-run: %s -> %s", landed, got)
	}
	if got := refAt(t, wt, "human-work"); got != landed {
		t.Fatalf("human-work = %s, want the landed tip %s", got, landed)
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("the converged fold left the scratch ref %s behind", scratch)
	}
	if got := currentBranchAt(t, wt); got != "human-work" {
		t.Fatalf("checkout left on %q, want human-work", got)
	}
	log := runGitOutput(t, repo, "log", "--oneline")
	if got := strings.Count(log, "branch work"); got != 1 {
		t.Fatalf("trunk carries the folded commit %d times, want one:\n%s", got, log)
	}
	if merges := strings.TrimSpace(runGitOutput(t, repo, "log", "--merges", "--oneline")); merges != "" {
		t.Fatalf("trunk history must stay linear:\n%s", merges)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
