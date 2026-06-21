package implement

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

func offerIntegrationTestDeps(t *testing.T) *Deps {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	real := deps.NewRealFileSystem()
	td := tasks.DefaultDeps()
	td.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return xdg
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	d := DefaultDeps()
	d.Tasks = td
	d.StdinInteractive = func(io.Reader) bool { return true }
	return d
}

func offerIntegrationRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

func setupOfferIntegrationWorktree(t *testing.T, d *Deps, status string) (repo, wt, setID string) {
	t.Helper()
	td := d.tasksDeps()
	repo = t.TempDir()
	offerIntegrationRunGit(t, repo, "init")
	offerIntegrationRunGit(t, repo, "config", "user.email", "pop@example.test")
	offerIntegrationRunGit(t, repo, "config", "user.name", "Pop Test")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	offerIntegrationRunGit(t, repo, "add", "base.txt")
	offerIntegrationRunGit(t, repo, "commit", "-m", "base")

	setID = "test-set-1"
	wt = filepath.Join(t.TempDir(), "wt-feature")
	offerIntegrationRunGit(t, repo, "worktree", "add", "-b", "feature", wt, "HEAD")

	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}
	offerIntegrationRunGit(t, wt, "add", "feature.txt")
	offerIntegrationRunGit(t, wt, "commit", "-m", "feature work")

	bstore, _ := binding.Load(td)
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("repo identity: %v", err)
	}
	key := binding.Key(id, setID)
	bstore.Put(key, binding.Adopt(wt, "feature", ""))
	if err := binding.Save(td, bstore); err != nil {
		t.Fatalf("save binding: %v", err)
	}

	store := &integration.Store{Records: map[string]integration.Record{
		key: {
			RuntimePath: wt,
			SetID:       setID,
			Status:      status,
		},
	}}
	if err := integration.Save(td, store); err != nil {
		t.Fatalf("save mergeability: %v", err)
	}
	return repo, wt, setID
}

func offerOpts(yes bool, in io.Reader, out io.Writer) WholeSetOptions {
	return WholeSetOptions{
		Yes:       yes,
		ConfirmIn: in,
		Output:    out,
	}
}

func lookupMergeability(t *testing.T, d *Deps, setID string) (integration.Record, bool) {
	t.Helper()
	id := integration.DefaultDeps()
	id.Tasks = d.tasksDeps()
	rec, ok, err := integration.Lookup(id, setID)
	if err != nil {
		t.Fatalf("lookup mergeability: %v", err)
	}
	return rec, ok
}

func TestOfferIntegrationNonInteractiveSkips(t *testing.T) {
	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return false }

	result := &tasks.RunTaskSetResult{TaskSetID: "demo", TaskSetDone: true, RuntimePath: "/some/wt"}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("y\n"), &out))
	if out.Len() != 0 {
		t.Fatalf("expected no output for non-interactive stdin, got: %q", out.String())
	}
}

func TestOfferIntegrationYesFlagTrunkDrainSkips(t *testing.T) {
	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return true }

	result := &tasks.RunTaskSetResult{TaskSetID: "demo", TaskSetDone: true, RuntimePath: "/some/wt"}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(true, strings.NewReader("y\n"), &out))
	if out.Len() != 0 {
		t.Fatalf("expected no output when --yes is set on trunk drain, got: %q", out.String())
	}
}

func TestOfferIntegrationYesNoAutoMergeCleanCleanParks(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusClean)

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(true, strings.NewReader(""), &out))

	if out.Len() != 0 {
		t.Fatalf("expected no output (parked), got: %q", out.String())
	}
	if _, ok := lookupMergeability(t, d, setID); !ok {
		t.Fatal("mergeability record cleared: set must remain in backlog when auto_merge_clean is not set")
	}
}

func TestOfferIntegrationYesNoAutoMergeCleanConflictsParks(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusConflicts)

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(true, strings.NewReader(""), &out))

	if out.Len() != 0 {
		t.Fatalf("expected no output (parked), got: %q", out.String())
	}
	if _, ok := lookupMergeability(t, d, setID); !ok {
		t.Fatal("mergeability record cleared: conflicting set must remain in backlog")
	}
}

func TestOfferIntegrationYesAutoMergeCleanCleanIntegrates(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusClean)

	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatalf("write .pop.toml: %v", err)
	}
	d.LoadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, nil
	}

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(true, strings.NewReader(""), &out))

	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("feature.txt missing from main repo after auto-integration: %v", err)
	}
	if _, ok := lookupMergeability(t, d, setID); ok {
		t.Fatal("mergeability record not cleared: set must be removed from backlog after auto-integration")
	}
}

func TestOfferIntegrationYesAutoMergeCleanConflictsParks(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusConflicts)

	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatalf("write .pop.toml: %v", err)
	}
	d.LoadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, nil
	}

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(true, strings.NewReader(""), &out))

	if out.Len() != 0 {
		t.Fatalf("expected no output for conflicting set, got: %q", out.String())
	}
	rec, ok := lookupMergeability(t, d, setID)
	if !ok {
		t.Fatal("mergeability record cleared: conflicting set must remain in backlog even with auto_merge_clean")
	}
	if rec.Status != integration.StatusConflicts {
		t.Fatalf("mergeability status = %q, want conflicts", rec.Status)
	}
}

func TestOfferIntegrationTrunkDrainNoOffer(t *testing.T) {
	d := offerIntegrationTestDeps(t)

	repo := t.TempDir()
	offerIntegrationRunGit(t, repo, "init")
	offerIntegrationRunGit(t, repo, "config", "user.email", "pop@example.test")
	offerIntegrationRunGit(t, repo, "config", "user.name", "Pop Test")
	offerIntegrationRunGit(t, repo, "commit", "--allow-empty", "-m", "base")

	result := &tasks.RunTaskSetResult{
		TaskSetID:   "demo",
		TaskSetDone: true,
		RuntimePath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("y\n"), &out))
	if out.Len() != 0 {
		t.Fatalf("trunk drain must produce no integration offer, got: %q", out.String())
	}
}

func TestOfferIntegrationCleanPromptDeclined(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusClean)

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("n\n"), &out))

	outStr := out.String()
	if !strings.Contains(outStr, "Integrate") {
		t.Fatalf("expected Integrate prompt, got: %q", outStr)
	}
	if !strings.Contains(outStr, "merges clean") {
		t.Fatalf("expected 'merges clean' in prompt, got: %q", outStr)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should not be removed on decline: %v", err)
	}
}

func TestOfferIntegrationCleanPromptShowsBranch(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusClean)

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("n\n"), &out))

	outStr := out.String()
	if !strings.Contains(outStr, "Integrate "+setID+" into ") {
		t.Fatalf("expected branch name in integrate offer, got: %q", outStr)
	}
}

func TestOfferIntegrationConflictsInPrompt(t *testing.T) {
	d := offerIntegrationTestDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, d, integration.StatusConflicts)

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("n\n"), &out))

	outStr := out.String()
	if !strings.Contains(outStr, "has conflicts") {
		t.Fatalf("expected 'has conflicts' in prompt for conflicting merge, got: %q", outStr)
	}
}

func TestOfferIntegrationNotDoneSkips(t *testing.T) {
	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return true }

	result := &tasks.RunTaskSetResult{
		TaskSetID:   "demo",
		TaskSetDone: false,
		RuntimePath: "/some/wt",
	}
	var out bytes.Buffer
	OfferIntegration(d, result, offerOpts(false, strings.NewReader("y\n"), &out))
	if out.Len() != 0 {
		t.Fatalf("expected no output when set not Done, got: %q", out.String())
	}
}
