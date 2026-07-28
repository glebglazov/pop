package binding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// seedDoneTaskSet writes a one-task DONE set under repo's Task storage and
// registers it. Returns the definition path.
func seedDoneTaskSet(t *testing.T, td *tasks.Deps, repo, setID string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	taskDir := filepath.Join(defPath, setID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(taskDir, "01-done.md")
	if err := os.WriteFile(md, []byte("## Acceptance criteria\n\n- [x] done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"tasks": []map[string]any{{
			"id": "01-done", "file": "01-done.md", "title": "Done", "type": "AFK", "status": "done",
		}},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.RegisterWith(td, defPath, tasks.StatePathFor(defPath)); err != nil {
		t.Fatalf("register: %v", err)
	}
	defPath, err = tasks.CanonicalDefinitionPathWith(td, defPath)
	if err != nil {
		t.Fatalf("re-canon: %v", err)
	}
	return defPath
}

func seedOpenTaskSet(t *testing.T, td *tasks.Deps, repo, setID string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	taskDir := filepath.Join(defPath, setID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-open.md"), []byte("## Acceptance criteria\n\n- [ ] open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"tasks": []map[string]any{{
			"id": "01-open", "file": "01-open.md", "title": "Open", "type": "AFK", "status": "open",
		}},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.RegisterWith(td, defPath, tasks.StatePathFor(defPath)); err != nil {
		t.Fatalf("register: %v", err)
	}
	return defPath
}

func writeFileCommit(t *testing.T, dir, name, contents, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, dir, "add", name)
	adoptRunGit(t, dir, "commit", "-m", msg)
}

func TestFoldMergesTrunkIntoSetThenFastForwardsTrunk(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-fold")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-fold",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	got, err := Fold(td, nil, cfg, "set-fold", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("fold: %v\n%s", err, out.String())
	}
	if got.TornDown != true {
		t.Fatalf("TornDown = %v, want true for last managed referent", got.TornDown)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("trunk must carry set work: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "trunk.txt")); err != nil {
		t.Fatalf("trunk must keep its own work: %v", err)
	}
	// Confirm trunk tip contains the set commit message / is ahead of the pre-fold base.
	log := runGitOutput(t, repo, "log", "--oneline", "-5")
	if !strings.Contains(log, "set work") {
		t.Fatalf("trunk history missing set work:\n%s", log)
	}
	if _, _, ok, err := FindBySetID(td, "set-fold"); err != nil || ok {
		t.Fatalf("binding should be released: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be torn down, stat=%v", err)
	}
}

func TestFoldConflictAbortsWithTrunkUnchanged(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-conflict")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-conflict",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
	trunkBefore := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-conflict", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
	trunkAfter := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if trunkAfter != trunkBefore {
		t.Fatalf("trunk moved: before=%s after=%s", trunkBefore, trunkAfter)
	}
	if mergeInProgress(td, repo) {
		t.Fatal("trunk must not be mid-merge")
	}
	if !mergeInProgress(td, b.RuntimePath) {
		t.Fatal("set worktree must be left mid-merge for the human to resolve")
	}
	if _, _, ok, _ := FindBySetID(td, "set-conflict"); !ok {
		t.Fatal("binding must remain after refused fold")
	}
}

// foldConflictResolverRunner completes an in-progress merge when fold conflict
// assistance runs the attended agent hook.
type foldConflictResolverRunner struct {
	setPath string
}

func (r *foldConflictResolverRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *foldConflictResolverRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	clash := filepath.Join(r.setPath, "clash.txt")
	if err := os.WriteFile(clash, []byte("resolved\n"), 0o644); err != nil {
		return 1, err
	}
	if err := exec.Command("git", "-C", r.setPath, "add", "clash.txt").Run(); err != nil {
		return 1, err
	}
	if err := exec.Command("git", "-C", r.setPath, "commit", "--no-edit").Run(); err != nil {
		return 1, err
	}
	return 0, nil
}

func (r *foldConflictResolverRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return tasks.RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

func TestFoldConflictOffersAssistanceAndCompletesOnResolve(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-resolve")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-resolve",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	td.Runner = &foldConflictResolverRunner{setPath: b.RuntimePath}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	got, err := Fold(td, nil, cfg, "set-resolve", FoldOptions{
		Yes: true,
		In:  strings.NewReader("\n"),
	}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("fold after assistance: %v\n%s", err, out.String())
	}
	if !got.TornDown {
		t.Fatalf("TornDown = %v, want true", got.TornDown)
	}
	if !strings.Contains(out.String(), "Fold conflict") {
		t.Fatalf("output should offer fold conflict assistance:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(repo, "clash.txt")); err != nil {
		t.Fatalf("trunk must carry merged file: %v", err)
	}
	if _, _, ok, _ := FindBySetID(td, "set-resolve"); ok {
		t.Fatal("binding should be released after successful fold")
	}
}

func TestFoldConflictInteractiveDeclineLeavesMergeInProgress(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-decline")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-decline",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
	trunkBefore := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-decline", FoldOptions{In: strings.NewReader("0\n")}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
	trunkAfter := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if trunkAfter != trunkBefore {
		t.Fatalf("trunk moved: before=%s after=%s", trunkBefore, trunkAfter)
	}
	if !mergeInProgress(td, b.RuntimePath) {
		t.Fatal("set worktree must remain mid-merge after declining assistance")
	}
}

func TestFoldTrunkMovingMidFoldRedoesOnceThenRefuses(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-race")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-race",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set\n", "set work")

	var ffAttempts atomic.Int32
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "merge" && args[1] == "--ff-only" {
				n := ffAttempts.Add(1)
				// Advance trunk so fold observes movement, then force the ff to fail.
				writeFileCommit(t, repo, fmt.Sprintf("race-%d.txt", n), "moved\n", fmt.Sprintf("race %d", n))
				return "", fmt.Errorf("not possible to fast-forward")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-race", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "moved during fold") {
		t.Fatalf("err = %v, want trunk-moved refusal", err)
	}
	if got := ffAttempts.Load(); got != 2 {
		t.Fatalf("ff attempts = %d, want 2 (one redo)", got)
	}
	if mergeInProgress(td, repo) {
		t.Fatal("trunk must not be mid-merge")
	}
}

func TestFoldRefusesPreconditions(t *testing.T) {
	t.Parallel()

	t.Run("not DONE", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedOpenTaskSet(t, td, repo, "set-open")
		b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-open",
		})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err = Fold(td, nil, cfg, "set-open", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || (!strings.Contains(err.Error(), "READY") && !strings.Contains(err.Error(), "must be DONE")) {
			t.Fatalf("err = %v, want not-DONE refusal", err)
		}
		_ = b
	})

	t.Run("NEEDS-VERIFY", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedDoneTaskSet(t, td, repo, "set-nv")
		if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-nv",
		}); err != nil {
			t.Fatalf("provision: %v", err)
		}
		cfg := &config.Config{
			Projects: []config.ProjectEntry{{Path: repo}},
			Task:     &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}},
		}
		_, err := Fold(td, nil, cfg, "set-nv", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "NEEDS-VERIFY") {
			t.Fatalf("err = %v, want NEEDS-VERIFY refusal", err)
		}
	})

	t.Run("dirty set worktree", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedDoneTaskSet(t, td, repo, "set-dirty")
		b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-dirty",
		})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		if err := os.WriteFile(filepath.Join(b.RuntimePath, "dirt.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err = Fold(td, nil, cfg, "set-dirty", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "set worktree is dirty") {
			t.Fatalf("err = %v, want dirty set refusal", err)
		}
	})

	t.Run("dirty trunk", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedDoneTaskSet(t, td, repo, "set-tdirty")
		if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-tdirty",
		}); err != nil {
			t.Fatalf("provision: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repo, "trunk-dirt.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err := Fold(td, nil, cfg, "set-tdirty", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "Trunk worktree is dirty") {
			t.Fatalf("err = %v, want dirty trunk refusal", err)
		}
	})

	t.Run("bound to trunk", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedDoneTaskSet(t, td, repo, "set-trunk")
		seedLifecycleBinding(t, td, repo, "set-trunk", Binding{
			RuntimePath: repo,
			Branch:      CurrentBranch(td, repo),
			Project:     filepath.Base(repo),
			Provisioned: false,
		})
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err := Fold(td, nil, cfg, "set-trunk", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "Trunk worktree itself") {
			t.Fatalf("err = %v, want trunk-bound refusal", err)
		}
	})

	t.Run("live claim on set", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedDoneTaskSet(t, td, repo, "set-claim")
		b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-claim",
		})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		h, err := tasks.BeginDrain(td, b.RuntimePath, "set-claim", io.Discard)
		if err != nil {
			t.Fatalf("BeginDrain: %v", err)
		}
		defer func() { _ = h.Finish("finished", "", false, time.Time{}) }()

		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err = Fold(td, nil, cfg, "set-claim", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "live claim") {
			t.Fatalf("err = %v, want live-claim refusal", err)
		}
	})
}

func TestFoldReleasesBindingAndConfirmGatesTeardown(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-confirm")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-confirm",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	got, err := Fold(td, nil, cfg, "set-confirm", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("fold decline teardown: %v", err)
	}
	if got.TornDown {
		t.Fatal("TornDown should be false when declined")
	}
	if _, _, ok, _ := FindBySetID(td, "set-confirm"); ok {
		t.Fatal("binding must be released even when teardown declined")
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("managed worktree must remain after decline: %v", err)
	}
	if !strings.Contains(out.String(), "delete managed worktree") && !strings.Contains(out.String(), "Kept managed worktree") {
		t.Fatalf("output should mention teardown prompt/keep:\n%s", out.String())
	}
}

func TestFoldYesSkipsTeardownConfirmation(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-yes")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-yes",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	got, err := Fold(td, nil, cfg, "set-yes", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold --yes: %v", err)
	}
	if !got.TornDown {
		t.Fatal("TornDown want true with --yes")
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed")
	}
}

func TestFoldAdoptedReleasesButNeverDeletes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "adopted-fold")
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-adopt")
	seedLifecycleBinding(t, td, repo, "set-adopt", Binding{
		RuntimePath: wt,
		Branch:      "adopted-fold",
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	writeFileCommit(t, wt, "feature.txt", "adopted\n", "adopted work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rec := &recordingGit{inner: td.Git}
	td.Git = rec
	got, err := Fold(td, nil, cfg, "set-adopt", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold adopted: %v", err)
	}
	if got.TornDown {
		t.Fatal("adopted checkout must not be torn down")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted worktree must remain: %v", err)
	}
	if _, _, ok, _ := FindBySetID(td, "set-adopt"); ok {
		t.Fatal("binding must be released")
	}
	if rec.ran("push") {
		t.Fatal("fold must never push")
	}
	if rec.ran("worktree", "remove") {
		t.Fatal("fold must not remove an adopted worktree")
	}
	// Trunk should carry the adopted work.
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("trunk must include folded work: %v", err)
	}
}

func TestFoldNeverArchives(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	defPath := seedDoneTaskSet(t, td, repo, "set-keep")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-keep",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := Fold(td, nil, cfg, "set-keep", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("fold: %v", err)
	}
	refresh, err := tasks.RefreshWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		t.Fatal(err)
	}
	row := tasks.FindRow(refresh, "set-keep")
	if row == nil {
		t.Fatal("set registration must remain after fold")
	}
	archived, err := tasks.RefreshArchivedWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		t.Fatal(err)
	}
	if tasks.FindRow(archived, "set-keep") != nil {
		t.Fatal("fold must not archive the set")
	}
}

func TestFoldNeverPushes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-nopush")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-nopush",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	rec := &recordingGit{inner: td.Git}
	td.Git = rec
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := Fold(td, nil, cfg, "set-nopush", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if rec.ran("push") {
		t.Fatal("fold must never push")
	}
	if rec.ran("merge-tree") {
		t.Fatal("fold must not compute a mergeability verdict")
	}
}
