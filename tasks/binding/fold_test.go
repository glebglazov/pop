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
	"github.com/glebglazov/pop/store"
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

func seedAwaitingApprovalTaskSet(t *testing.T, td *tasks.Deps, repo, setID string, hitl []map[string]any) string {
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
	if err := os.WriteFile(filepath.Join(taskDir, "01-done.md"), []byte("## Acceptance criteria\n\n- [x] done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskList := []map[string]any{{
		"id": "01-done", "file": "01-done.md", "title": "Done", "type": "AFK", "status": "done",
	}}
	for _, h := range hitl {
		file := h["id"].(string) + ".md"
		if err := os.WriteFile(filepath.Join(taskDir, file), []byte("## Acceptance criteria\n\n- [ ] gate\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		h["file"] = file
		h["type"] = "HITL"
		if _, ok := h["status"]; !ok {
			h["status"] = "open"
		}
		taskList = append(taskList, h)
	}
	payload := map[string]any{"tasks": taskList}
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

func manifestStatusAt(t *testing.T, td *tasks.Deps, repo, setID string) tasks.TaskSetStatus {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	refresh, err := tasks.RefreshWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m := refresh.Manifests[setID]
	if m == nil {
		t.Fatalf("manifest for %s missing", setID)
	}
	return tasks.DeriveStatus(m)
}

func hitlTaskStatus(t *testing.T, td *tasks.Deps, repo, setID, taskID string) tasks.TaskStatus {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	refresh, err := tasks.RefreshWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m := refresh.Manifests[setID]
	if m == nil {
		t.Fatalf("manifest for %s missing", setID)
	}
	for _, task := range m.Tasks {
		if task.ID == taskID {
			return task.Status
		}
	}
	t.Fatalf("task %s not found in %s", taskID, setID)
	return ""
}

func writeFileCommit(t *testing.T, dir, name, contents, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, dir, "add", name)
	adoptRunGit(t, dir, "commit", "-m", msg)
}

func TestFoldRebasesSetOntoTrunkThenFastForwardsTrunk(t *testing.T) {
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
	if strings.Contains(log, "pop fold:") {
		t.Fatalf("trunk must not gain a pop fold merge commit:\n%s", log)
	}
	merges := strings.TrimSpace(runGitOutput(t, repo, "log", "--merges", "--oneline"))
	if merges != "" {
		t.Fatalf("trunk history must stay linear after fold, got merges:\n%s", merges)
	}
	if _, _, ok, err := FindBySetID(td, "set-fold"); err != nil || ok {
		t.Fatalf("binding should be released: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be torn down, stat=%v", err)
	}
}

func TestFoldFlattensMergeCommitOnSetBranch(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-flatten")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-flatten",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	// Create a merge commit on the set branch (noise that plain rebase should flatten).
	side := filepath.Join(t.TempDir(), "side")
	adoptRunGit(t, repo, "worktree", "add", "-b", "side-merge", side, "HEAD")
	writeFileCommit(t, side, "side.txt", "side\n", "side work")
	adoptRunGit(t, b.RuntimePath, "merge", "--no-ff", "-m", "merge side into set", "side-merge")
	if !strings.Contains(runGitOutput(t, b.RuntimePath, "log", "--merges", "--oneline", "-1"), "merge side into set") {
		t.Fatal("setup: set branch should contain a merge commit")
	}
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := Fold(td, nil, cfg, "set-flatten", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("trunk must carry set work: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "side.txt")); err != nil {
		t.Fatalf("trunk must carry flattened side work: %v", err)
	}
	merges := strings.TrimSpace(runGitOutput(t, repo, "log", "--merges", "--oneline"))
	if merges != "" {
		t.Fatalf("plain rebase must flatten set-branch merge commits; trunk has:\n%s", merges)
	}
	if strings.Contains(runGitOutput(t, repo, "log", "--oneline"), "pop fold:") {
		t.Fatal("trunk must not gain a pop fold merge commit")
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
	if !strings.Contains(err.Error(), "rebase") {
		t.Fatalf("err = %v, want rebase wording", err)
	}
	trunkAfter := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if trunkAfter != trunkBefore {
		t.Fatalf("trunk moved: before=%s after=%s", trunkBefore, trunkAfter)
	}
	if rebaseInProgress(td, repo) {
		t.Fatal("trunk must not be mid-rebase")
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("set worktree must be left mid-rebase for the human to resolve")
	}
	if _, _, ok, _ := FindBySetID(td, "set-conflict"); !ok {
		t.Fatal("binding must remain after refused fold")
	}
}

// foldConflictResolverRunner completes an in-progress rebase when fold conflict
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
	cmd := exec.Command("git", "-C", r.setPath, "-c", "core.editor=true", "rebase", "--continue")
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true", "EDITOR=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return 1, fmt.Errorf("rebase --continue: %w\n%s", err, out)
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
		In:  strings.NewReader("\n"), // agent assistance; post-resolve verify declines on EOF→n
	}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("fold after assistance: %v\n%s", err, out.String())
	}
	if !got.TornDown {
		t.Fatalf("TornDown = %v, want true", got.TornDown)
	}
	gotOut := out.String()
	for _, want := range []string{
		"Fold conflict",
		"1. Agent assistance (default)",
		"2. Resume fold",
		"3. Retry fold from scratch",
		"4. Verify set",
		"0. Exit",
		"Verify set? [y/N]:",
	} {
		if !strings.Contains(gotOut, want) {
			t.Fatalf("output missing %q:\n%s", want, gotOut)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "clash.txt")); err != nil {
		t.Fatalf("trunk must carry merged file: %v", err)
	}
	if _, _, ok, _ := FindBySetID(td, "set-resolve"); ok {
		t.Fatal("binding should be released after successful fold")
	}
}

// foldConflictNoopRunner leaves the rebase unresolved so the conflict prompt loops.
type foldConflictNoopRunner struct{}

func (r *foldConflictNoopRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *foldConflictNoopRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *foldConflictNoopRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return tasks.RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

func TestFoldConflictAssistanceUnresolvedRePrompts(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-reprompt")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-reprompt",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	td.Runner = &foldConflictNoopRunner{}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	// Agent (noop), then Exit — must re-show the prompt after the noop, not refuse.
	_, err = Fold(td, nil, cfg, "set-reprompt", FoldOptions{
		In: strings.NewReader("1\n0\n"),
	}, LifecycleHooks{}, &out)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal after exit", err)
	}
	gotOut := out.String()
	if strings.Count(gotOut, "Fold conflict:") < 2 {
		t.Fatalf("expected conflict prompt twice after unresolved assistance:\n%s", gotOut)
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("rebase must remain in progress after exit")
	}
}

func TestFoldConflictResumeContinuesWithoutPreflight(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-resume")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-resume",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-resume", FoldOptions{In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want initial conflict", err)
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("expected parked rebase")
	}

	// Resolve conflict files, then Resume (2) — must not die on dirty preflight.
	clash := filepath.Join(b.RuntimePath, "clash.txt")
	if err := os.WriteFile(clash, []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", b.RuntimePath, "add", "clash.txt").Run(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	got, err := Fold(td, nil, cfg, "set-resume", FoldOptions{
		Yes: true,
		In:  strings.NewReader("2\nn\n"), // resume, decline post-resolve verify
	}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("fold resume: %v\n%s", err, out.String())
	}
	if !got.TornDown {
		t.Fatalf("TornDown = %v, want true", got.TornDown)
	}
	if !strings.Contains(out.String(), "2. Resume fold") {
		t.Fatalf("missing resume option:\n%s", out.String())
	}
	if rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("rebase should be finished after resume")
	}
}

func TestFoldConflictRetryAbortsAndRestartsFromPreflight(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-retry")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-retry",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-retry", FoldOptions{In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want initial conflict", err)
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("expected parked rebase")
	}

	// Human refreshes trunk by hand (no fetch inside fold), then Retry.
	writeFileCommit(t, repo, "trunk-refresh.txt", "refreshed\n", "refresh trunk")
	trunkAfterRefresh := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	var fetchCalls atomic.Int32
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "fetch" {
				fetchCalls.Add(1)
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	// Retry aborts; next rebase still conflicts; Exit.
	var out bytes.Buffer
	_, err = Fold(td, nil, cfg, "set-retry", FoldOptions{
		In: strings.NewReader("3\n0\n"),
	}, LifecycleHooks{}, &out)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict after retry", err)
	}
	if fetchCalls.Load() != 0 {
		t.Fatalf("fold retry must not fetch; fetchCalls=%d", fetchCalls.Load())
	}
	if !strings.Contains(out.String(), "retrying fold from preflight") {
		t.Fatalf("missing retry notice:\n%s", out.String())
	}
	// After abort+retry the second conflict leaves rebase in progress again.
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("expected rebase in progress after retry conflict")
	}
	trunkNow := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if trunkNow != trunkAfterRefresh {
		t.Fatalf("trunk must stay at refreshed HEAD; want %s got %s", trunkAfterRefresh, trunkNow)
	}
}

func TestFoldParkedRebaseReentersConflictPrompt(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-parked")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-parked",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-parked", FoldOptions{In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want initial conflict", err)
	}

	if err := PreflightFold(td, cfg, "set-parked"); err != nil {
		t.Fatalf("PreflightFold on parked rebase: %v", err)
	}

	var out bytes.Buffer
	_, err = Fold(td, nil, cfg, "set-parked", FoldOptions{
		In: strings.NewReader("0\n"),
	}, LifecycleHooks{}, &out)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal on exit", err)
	}
	if strings.Contains(err.Error(), "dirty") {
		t.Fatalf("parked rebase must not refuse as dirty: %v", err)
	}
	if !strings.Contains(out.String(), "Fold conflict") {
		t.Fatalf("expected conflict prompt on re-entry:\n%s", out.String())
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("exit must leave rebase parked")
	}
}

func TestFoldConflictVerifyFailStopsFold(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-vfail")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-vfail",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-vfail", FoldOptions{In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil {
		t.Fatal("want conflict")
	}

	// Drive HandleFoldConflict directly with a failing verifier at the parked set.
	trunkBranch := CurrentBranch(td, repo)
	err = tasks.HandleFoldConflict(td, cfg, tasks.FoldConflictContext{
		SetID:       "set-vfail",
		RuntimePath: b.RuntimePath,
		SetBranch:   b.Branch,
		TrunkBranch: trunkBranch,
		TrunkPath:   repo,
	}, tasks.FoldConflictAssistanceOptions{
		In:  strings.NewReader("4\n"),
		Out: io.Discard,
		RunVerifier: func(string) (string, error) {
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: nope\n", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("err = %v, want verify refusal", err)
	}
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("verify FAIL must leave rebase parked")
	}
	trunkBefore := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	_ = trunkBefore
}

func TestFoldConflictInteractiveDeclineLeavesRebaseInProgress(t *testing.T) {
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
	if !rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("set worktree must remain mid-rebase after declining assistance")
	}
}

func TestFoldNonConflictRebaseFailureLeavesTrunkUnchanged(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-fail")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-fail",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set\n", "set work")
	trunkBefore := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "rebase" {
				return "", fmt.Errorf("simulated non-conflict rebase failure")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-fail", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "rebase set branch onto trunk failed") {
		t.Fatalf("err = %v, want non-conflict rebase refusal", err)
	}
	trunkAfter := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if trunkAfter != trunkBefore {
		t.Fatalf("trunk moved: before=%s after=%s", trunkBefore, trunkAfter)
	}
	if rebaseInProgress(td, repo) || rebaseInProgress(td, b.RuntimePath) {
		t.Fatal("neither checkout should be mid-rebase after plain failure")
	}
	if _, _, ok, _ := FindBySetID(td, "set-fail"); !ok {
		t.Fatal("binding must remain after refused fold")
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
	if rebaseInProgress(td, repo) {
		t.Fatal("trunk must not be mid-rebase")
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

func TestFoldNeverPushesOrFetches(t *testing.T) {
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
	if rec.ran("fetch") {
		t.Fatal("fold must never fetch")
	}
	if rec.ran("merge-tree") {
		t.Fatal("fold must not compute a mergeability verdict")
	}
	if !rec.ran("rebase") {
		t.Fatal("fold must rebase the set branch onto trunk")
	}
}

// TestFoldAdoptedManagedRootCheckoutReachesTeardown asserts that adopting a
// checkout under the managed-worktree root records a provisioned binding and
// that fold reaches the confirm-gated teardown path instead of silently leaving
// the directory (ADR-0152).
func TestFoldAdoptedManagedRootCheckoutReachesTeardown(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-adopt-managed")

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	path := filepath.Join(ManagedWorktreesRoot(td), RepoKey(id), "adopted-managed")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir managed parent: %v", err)
	}
	adoptRunGit(t, repo, "worktree", "add", "-b", "adopted-managed", path, "HEAD")
	writeFileCommit(t, path, "feature.txt", "adopted managed\n", "adopted managed work")

	adopted, err := AdoptCurrentCheckout(td, nil, nil, repo, path, "set-adopt-managed")
	if err != nil {
		t.Fatalf("adopt current checkout: %v", err)
	}
	if !adopted {
		t.Fatal("expected binding to be recorded")
	}

	_, b, ok, err := FindBySetID(td, "set-adopt-managed")
	if err != nil {
		t.Fatalf("lookup binding: %v", err)
	}
	if !ok {
		t.Fatal("expected a binding for set-adopt-managed")
	}
	if !b.Provisioned {
		t.Fatalf("managed-root adoption must be recorded as provisioned, got %+v", b)
	}
	if b.RuntimePath != path {
		t.Fatalf("RuntimePath = %q, want %q", b.RuntimePath, path)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	got, err := Fold(td, nil, cfg, "set-adopt-managed", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !got.TornDown {
		t.Fatal("TornDown = false, want true for managed-root adoption")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be torn down, stat err = %v", err)
	}
}

func TestFoldAwaitingApprovalListsHITLInConfirmation(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedAwaitingApprovalTaskSet(t, td, repo, "set-confirm-signoff", []map[string]any{
		{"id": "09-review", "title": "Review"},
		{"id": "10-signoff", "title": "Sign off"},
	})
	if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-confirm-signoff",
	}); err != nil {
		t.Fatalf("provision: %v", err)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	_, err := Fold(td, nil, cfg, "set-confirm-signoff", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, &out)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v, want cancelled fold", err)
	}
	if !strings.Contains(out.String(), "fold will complete: 09-review, 10-signoff") {
		t.Fatalf("confirmation missing HITL list:\n%s", out.String())
	}
}

func TestFoldAwaitingApprovalCompletesHITLAndReachesDone(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedAwaitingApprovalTaskSet(t, td, repo, "set-signoff", []map[string]any{
		{"id": "09-review", "title": "Review"},
		{"id": "10-signoff", "title": "Sign off"},
	})
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-signoff",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-signoff", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if got := manifestStatusAt(t, td, repo, "set-signoff"); got != tasks.StatusDone {
		t.Fatalf("set status = %q, want DONE", got)
	}
	for _, id := range []string{"09-review", "10-signoff"} {
		if got := hitlTaskStatus(t, td, repo, "set-signoff", id); got != tasks.TaskDone {
			t.Fatalf("task %s status = %q, want done", id, got)
		}
	}
}

func TestFoldAwaitingApprovalConflictLeavesHITLUntouched(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedAwaitingApprovalTaskSet(t, td, repo, "set-conflict-signoff", []map[string]any{
		{"id": "09-review", "title": "Review"},
	})
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-conflict-signoff",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "clash.txt", "from-set\n", "set clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-conflict-signoff", FoldOptions{
		Yes: true,
		In:  tasks.NonInteractiveReader{},
	}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
	if got := hitlTaskStatus(t, td, repo, "set-conflict-signoff", "09-review"); got != tasks.TaskOpen {
		t.Fatalf("HITL task status = %q, want open after failed fold", got)
	}
	if got := manifestStatusAt(t, td, repo, "set-conflict-signoff"); got != tasks.StatusAwaitingApproval {
		t.Fatalf("set status = %q, want AWAITING-APPROVAL", got)
	}
}

func TestFoldAwaitingApprovalNonInteractiveRefusesWithoutYes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedAwaitingApprovalTaskSet(t, td, repo, "set-ni", []map[string]any{
		{"id": "09-review", "title": "Review"},
	})
	if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-ni",
	}); err != nil {
		t.Fatalf("provision: %v", err)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err := Fold(td, nil, cfg, "set-ni", FoldOptions{In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "AWAITING-APPROVAL set requires --yes") {
		t.Fatalf("err = %v, want non-interactive sign-off refusal", err)
	}
}

func TestFoldAwaitingApprovalNonInteractiveProceedsWithYes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedAwaitingApprovalTaskSet(t, td, repo, "set-ni-yes", []map[string]any{
		{"id": "09-review", "title": "Review"},
	})
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-ni-yes",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-ni-yes", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold --yes: %v", err)
	}
	if got := manifestStatusAt(t, td, repo, "set-ni-yes"); got != tasks.StatusDone {
		t.Fatalf("set status = %q, want DONE", got)
	}
	_ = b
}

func TestFoldDoneNonInteractiveWithoutSignOffYesUnchanged(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-done-ni")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-done-ni",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err = Fold(td, nil, cfg, "set-done-ni", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("DONE fold without sign-off prompt should still work with --yes for teardown: %v", err)
	}
	_ = b
}

func TestFoldRefusesAwaitingApprovalStatuses(t *testing.T) {
	t.Parallel()

	t.Run("VERIFY-FAILED", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedAwaitingApprovalTaskSet(t, td, repo, "set-vf", []map[string]any{
			{"id": "09-review", "title": "Review"},
		})
		b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-vf",
		})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		id, err := tasks.ResolveRepositoryIdentity(td, b.RuntimePath)
		if err != nil {
			t.Fatalf("identity: %v", err)
		}
		workSHA := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD"))
		s, _, err := td.Store(true)
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		if err := s.PutVerifyVerdict(store.VerifyVerdict{
			Repo: id.CommonDir, SetID: "set-vf", WorkSHA: workSHA, Verdict: "NEEDS-HUMAN",
		}); err != nil {
			t.Fatalf("PutVerifyVerdict: %v", err)
		}
		cfg := &config.Config{
			Projects: []config.ProjectEntry{{Path: repo}},
			Task:     &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}},
		}
		_, err = Fold(td, nil, cfg, "set-vf", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "VERIFY-FAILED") {
			t.Fatalf("err = %v, want VERIFY-FAILED refusal", err)
		}
	})

	t.Run("BLOCKED", func(t *testing.T) {
		t.Parallel()
		repo := initAdoptRepo(t)
		td := lifecycleTestDeps(t)
		seedAwaitingApprovalTaskSet(t, td, repo, "set-blocked", []map[string]any{
			{"id": "01-gate", "title": "Gate"},
		})
		defPath := filepath.Join(tasksDirForBindingRepo(t, td, repo), "set-blocked")
		manifestPath := filepath.Join(defPath, "index.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		tasksList := payload["tasks"].([]any)
		tasksList = append(tasksList, map[string]any{
			"id": "02-a", "file": "02-a.md", "title": "A", "type": "AFK", "status": "open",
			"blocked_by": []string{"01-gate"},
		})
		if err := os.WriteFile(filepath.Join(defPath, "02-a.md"), []byte("## Acceptance criteria\n\n- [ ] a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		payload["tasks"] = tasksList
		rewritten, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, rewritten, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
			TD: td, CheckoutPath: repo, SetID: "set-blocked",
		}); err != nil {
			t.Fatalf("provision: %v", err)
		}
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		_, err = Fold(td, nil, cfg, "set-blocked", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "BLOCKED") {
			t.Fatalf("err = %v, want BLOCKED refusal", err)
		}
	})
}

func tasksDirForBindingRepo(t *testing.T, td *tasks.Deps, repo string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatal(err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	return defPath
}
