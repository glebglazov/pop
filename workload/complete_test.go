package workload

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func setupCustomIssueFixture(t *testing.T, issues []Issue) *execFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issuesDir := storageIssuesDir(t, root)
	setupManifest(t, issuesDir, "demo", issues)
	if _, err := RefreshWith(DefaultDeps(), issuesDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &execFixture{root: root, issuesDir: issuesDir}
}

func TestCompleteIssueOpenToDone(t *testing.T) {
	env := setupExecutorFixture(t, false)

	result, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IssueSetID != "demo" || result.IssueID != "01-a" {
		t.Fatalf("complete target = %s/%s", result.IssueSetID, result.IssueID)
	}
	assertIssueDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was open")
}

func TestCompleteIssueHITLOpenToDone(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was open")
}

func TestCompleteIssueFailedToDone(t *testing.T) {
	env := setupFailedIssueFixture(t)

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was failed")
}

func TestCompleteIssueSkippedToDone(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "skipped"},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was skipped")
}

func TestCompleteIssueSkippedBlockedByUndoneRejected(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "skipped", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "02-b.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "blocked by 01-a") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteIssueAlreadyDoneRejected(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already done") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteIssueBlockedByUndoneRejected(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "02-b.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "blocked by 01-a") {
		t.Fatalf("err = %v", err)
	}
	assertIssueOpen(t, env, "02-b")
}

func TestCompleteIssueBlockedByDoneAllowed(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "02-b.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueDone(t, env, "02-b")
}

func TestCompleteIssueRejectsBareIdentifier(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "01-a",
	})
	assertExitCode(t, err, ExitSetup)
}

func TestCompleteIssueRejectsAbsolutePath(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    filepath.Join(env.demoDir(), "01-a.md"),
	})
	assertExitCode(t, err, ExitSetup)
}

func TestCompleteIssueAcceptsBareFilename(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.demoDir()},
		IssuePath:    "01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIssueDone(t, env, "01-a")
}

func TestCompleteIssueDoesNotStageChanges(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "impl.txt"), "human work\n")

	_, err := CompleteIssueWith(env.deps(), nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}

	staged, err := realGitInDir(env.root, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(staged) != "" {
		t.Fatalf("command staged changes: %q", staged)
	}
}

func TestCompleteIssueProgressBeforeManifest(t *testing.T) {
	env := setupExecutorFixture(t, false)
	order := &writeOrderTracker{}
	fs := &atomicBlockingFS{
		FileSystem: deps.NewRealFileSystem(),
		tracker:    order,
	}
	d := env.deps()
	d.FS = fs

	_, err := CompleteIssueWith(d, nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.last != "manifest" || len(order.events) < 2 || order.events[0] != "progress" {
		t.Fatalf("write order = %v last=%q", order.events, order.last)
	}
}

func TestCompleteIssueManifestFailureManualRepair(t *testing.T) {
	env := setupExecutorFixture(t, false)
	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
	d.FS = fs

	_, err := CompleteIssueWith(d, nil, nil, CompleteIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRel(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
}
