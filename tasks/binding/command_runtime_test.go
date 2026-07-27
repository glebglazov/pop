package binding

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func TestResolveCommandRuntimeBoundUsesBinding(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	seedBinding(t, td, repo, "demo", Adopt(wt, "bound-branch", ""))

	got, err := ResolveCommandRuntime(td, repo, "demo", "")
	if err != nil {
		t.Fatalf("ResolveCommandRuntime: %v", err)
	}
	want, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatalf("want runtime: %v", err)
	}
	if got != want {
		t.Fatalf("runtime = %q, want binding %q (invoking checkout %q must not win)", got, want, repo)
	}
}

func TestResolveCommandRuntimeUnboundUsesCurrentCheckout(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)

	got, err := ResolveCommandRuntime(td, repo, "unbound-set", "")
	if err != nil {
		t.Fatalf("ResolveCommandRuntime: %v", err)
	}
	want, err := tasks.ResolveRuntimePathWith(td, repo, "")
	if err != nil {
		t.Fatalf("want runtime: %v", err)
	}
	if got != want {
		t.Fatalf("runtime = %q, want current checkout %q", got, want)
	}
}

func TestResolveCommandRuntimeOverrideWins(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	other := addLinkedWorktree(t, repo, "override-branch")
	seedBinding(t, td, repo, "demo", Adopt(wt, "bound-branch", ""))

	got, err := ResolveCommandRuntime(td, repo, "demo", other)
	if err != nil {
		t.Fatalf("ResolveCommandRuntime: %v", err)
	}
	want, err := tasks.ResolveRuntimePathWith(td, other, "")
	if err != nil {
		t.Fatalf("want runtime: %v", err)
	}
	if got != want {
		t.Fatalf("runtime = %q, want explicit override %q", got, want)
	}
}

func TestCommandRuntimeResolverPerSet(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	seedBinding(t, td, repo, "bound", Adopt(wt, "bound-branch", ""))

	resolver, current, err := CommandRuntimeResolver(td, repo)
	if err != nil {
		t.Fatalf("CommandRuntimeResolver: %v", err)
	}
	wantCurrent, err := tasks.ResolveRuntimePathWith(td, repo, "")
	if err != nil {
		t.Fatalf("want current: %v", err)
	}
	if current != wantCurrent {
		t.Fatalf("current = %q, want %q", current, wantCurrent)
	}
	wantBound, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatalf("want bound: %v", err)
	}
	if got := resolver("bound"); got != wantBound {
		t.Fatalf("bound set runtime = %q, want %q", got, wantBound)
	}
	if got := resolver("unbound"); got != wantCurrent {
		t.Fatalf("unbound set runtime = %q, want current %q", got, wantCurrent)
	}
}

// TestAcceptFromUnboundCwdUsesBindingWorkSHAAndRepo is the ADR-0146 regression:
// accepting a bound set from an unbound cwd must write the PASS at the binding's
// HEAD and repository identity — not the invoking checkout's HEAD.
func TestAcceptFromUnboundCwdUsesBindingWorkSHAAndRepo(t *testing.T) {
	t.Parallel()
	td := isolatedTasksDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "verify-bind")

	// Advance the binding's HEAD past trunk so a cwd-based accept would store the
	// wrong SHA and leave the dashboard reading VERIFY-FAILED at the binding.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("bound work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, wt, "add", "work.txt")
	adoptRunGit(t, wt, "commit", "-m", "bound advance")

	trunkSHA := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	boundSHA := strings.TrimSpace(runGitOutput(t, wt, "rev-parse", "HEAD"))
	if trunkSHA == "" || boundSHA == "" || trunkSHA == boundSHA {
		t.Fatalf("need distinct HEADs: trunk=%q bound=%q", trunkSHA, boundSHA)
	}

	seedBinding(t, td, repo, "demo", Adopt(wt, "verify-bind", ""))

	boundID, err := tasks.ResolveRepositoryIdentity(td, wt)
	if err != nil {
		t.Fatalf("binding identity: %v", err)
	}
	writeAcceptFixtureSet(t, boundID.TasksDir, "demo")

	// Seam resolves from the unbound trunk cwd against the bound set.
	runtime, err := ResolveCommandRuntime(td, repo, "demo", "")
	if err != nil {
		t.Fatalf("ResolveCommandRuntime: %v", err)
	}
	wantRuntime, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatalf("want runtime: %v", err)
	}
	if runtime != wantRuntime {
		t.Fatalf("resolved runtime = %q, want binding %q", runtime, wantRuntime)
	}

	var out bytes.Buffer
	res, err := tasks.VerifyTaskSetWith(td, &project.Deps{Git: td.Git, FS: td.FS}, func(string) (*config.Config, error) {
		return nil, nil
	}, tasks.VerifyOptions{
		ResolveInput: tasks.ResolveInput{CWD: repo, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Accept:       true,
		Note:         "accepted from trunk cwd",
	})
	if err != nil {
		t.Fatalf("VerifyTaskSetWith accept: %v", err)
	}
	if res.WorkSHA != boundSHA {
		t.Fatalf("accepted WorkSHA = %q, want binding HEAD %q (trunk was %q)", res.WorkSHA, boundSHA, trunkSHA)
	}
	if res.WorkSHA == trunkSHA {
		t.Fatalf("accepted WorkSHA matched trunk HEAD %q — cwd won over the binding", trunkSHA)
	}

	stored := readAcceptVerdict(t, td, boundID.CommonDir, "demo", boundSHA)
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored {
		t.Fatalf("stored verdict = %+v, want human-authored PASS at binding HEAD", stored)
	}
	if stored.Repo != boundID.CommonDir {
		t.Fatalf("stored repo = %q, want binding repository identity %q", stored.Repo, boundID.CommonDir)
	}
	if stored.WorkSHA != boundSHA {
		t.Fatalf("stored WorkSHA = %q, want binding HEAD %q", stored.WorkSHA, boundSHA)
	}
	// Guarding the defect: nothing must be keyed at trunk's HEAD.
	if v := readAcceptVerdictOptional(t, td, boundID.CommonDir, "demo", trunkSHA); v != nil {
		t.Fatalf("unexpected verdict at trunk HEAD %+v — accept must not key to the invoking checkout", v)
	}
}

func writeAcceptFixtureSet(t *testing.T, tasksDir, setID string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, setID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "## Acceptance criteria\n\n- [ ] ok\n"
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "tasks": [
    {"id": "01-a", "file": "01-a.md", "title": "A", "type": "AFK", "status": "done"}
  ]
}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAcceptVerdict(t *testing.T, d *tasks.Deps, repo, setID, sha string) *store.VerifyVerdict {
	t.Helper()
	v := readAcceptVerdictOptional(t, d, repo, setID, sha)
	if v == nil {
		t.Fatalf("GetVerifyVerdict(%s, %s, %s): missing", repo, setID, sha)
	}
	return v
}

func readAcceptVerdictOptional(t *testing.T, d *tasks.Deps, repo, setID, sha string) *store.VerifyVerdict {
	t.Helper()
	s, ok, err := d.Store(false)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if !ok {
		return nil
	}
	v, err := s.GetVerifyVerdict(repo, setID, sha)
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	return v
}
