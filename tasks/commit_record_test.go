package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// rootCommitOverrides give a bare `git init` repo an identity to commit with,
// through the same `-c key=value` channel the executor's configured overrides
// take.
var rootCommitOverrides = []string{"user.email=test@test", "user.name=test", "commit.gpgsign=false"}

// drainTaskWithCommit runs one task's completion the way a successful attempt
// does — stage, commit, record, mark done — after writing a file that makes the
// runtime dirty. It re-refreshes first, so each task sees the manifest as it was
// persisted by the previous one rather than an in-memory carry-over.
func drainTaskWithCommit(t *testing.T, env *execFixture, taskID, file string) *RunTaskResult {
	t.Helper()
	d := env.deps()
	if err := os.WriteFile(filepath.Join(env.root, file), []byte(taskID+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	m := refresh.Manifests["demo"]
	if m == nil {
		t.Fatal("refresh missing demo manifest")
	}
	sel := &Selection{TaskSetID: "demo", TaskID: taskID, Manifest: m}
	result, err := completeSuccessfulTask(d, sel, env.root, "summary for "+taskID, nil)
	if err != nil {
		t.Fatalf("completeSuccessfulTask(%s): %v", taskID, err)
	}
	return result
}

// loadDemoManifest reads the fixture set's manifest back off disk, so every
// assertion is about what was persisted rather than what was held in memory.
func loadDemoManifest(t *testing.T, env *execFixture) *Manifest {
	t.Helper()
	m := LoadManifest(env.deps(), "demo", env.demoManifest())
	if !m.Valid {
		t.Fatalf("manifest invalid after drain: %v", m.Errors)
	}
	return m
}

func taskByID(t *testing.T, m *Manifest, id string) Task {
	t.Helper()
	for _, task := range m.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("manifest has no task %q", id)
	return Task{}
}

// TestImplementationCommitsRecordBaseOnceAndCommitPerTask drives two tasks of a
// set through the executor's success path and asserts the manifest can rebuild
// the set's commit range afterwards: the base is the parent of the *first*
// commit and does not follow the second, and every task carries the SHA and
// subject of its own commit. The set is authored with neither field and with an
// unrelated set-level key, so it also pins that legacy sets drain unchanged and
// unknown keys survive the rewrites.
func TestImplementationCommitsRecordBaseOnceAndCommitPerTask(t *testing.T) {
	env := setupExecutorFixtureIsolated(t)
	writeTaskMD(t, env.demoDir(), "02-b.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, env.demoDir(), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	}, map[string]any{"unrelated_key": "keep-me"})

	preDrainHEAD, err := realGitInDir(env.root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	first := drainTaskWithCommit(t, env, "01-a", "a.txt")
	second := drainTaskWithCommit(t, env, "02-b", "b.txt")
	if first.CommitSHA == "" || second.CommitSHA == "" || first.CommitSHA == second.CommitSHA {
		t.Fatalf("expected two distinct commits, got %q and %q", first.CommitSHA, second.CommitSHA)
	}

	m := loadDemoManifest(t, env)
	if !m.BaseCommitRecorded {
		t.Fatal("set recorded no base commit")
	}
	if m.BaseCommit != preDrainHEAD {
		t.Fatalf("base commit = %q, want the parent of the first implementation commit %q", m.BaseCommit, preDrainHEAD)
	}
	for _, task := range []struct{ id, sha string }{{"01-a", first.CommitSHA}, {"02-b", second.CommitSHA}} {
		got := taskByID(t, m, task.id)
		if got.Commit == nil {
			t.Fatalf("task %s recorded no commit", task.id)
		}
		if got.Commit.SHA != task.sha {
			t.Fatalf("task %s commit SHA = %q, want %q", task.id, got.Commit.SHA, task.sha)
		}
		wantSubject, err := realGitInDir(env.root, "log", "-1", "--format=%s", task.sha)
		if err != nil {
			t.Fatalf("log %s: %v", task.sha, err)
		}
		if got.Commit.Subject != wantSubject {
			t.Fatalf("task %s recorded subject %q, want the committed %q", task.id, got.Commit.Subject, wantSubject)
		}
	}

	// The recorded base plus the recorded SHAs are enough to name the range, with
	// nothing read out of subject formats.
	rangeOut, err := realGitInDir(env.root, "rev-list", m.BaseCommit+"..HEAD")
	if err != nil {
		t.Fatalf("rev-list base..HEAD: %v", err)
	}
	if got := len(strings.Fields(rangeOut)); got != 2 {
		t.Fatalf("base..HEAD holds %d commits, want the set's 2", got)
	}

	var raw map[string]json.RawMessage
	data, err := os.ReadFile(env.demoManifest())
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if string(raw["unrelated_key"]) != `"keep-me"` {
		t.Fatalf("unknown set key not preserved: %s", raw["unrelated_key"])
	}
}

// TestRootImplementationCommitRecordsNullBase covers the edge where the set's
// first implementation commit is the repository's own first commit: it has no
// parent, so the base is persisted as an explicit null — recorded, but starting
// at the root of history — rather than a fabricated SHA.
func TestRootImplementationCommitRecordsNullBase(t *testing.T) {
	env := setupExecutorFixtureIsolated(t)
	repo := t.TempDir()
	if _, err := realGitInDir(repo, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "first.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write first.txt: %v", err)
	}

	d := &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
	commit, err := createImplementationCommit(d, repo, "demo", "01-a", "summary", rootCommitOverrides)
	if err != nil {
		t.Fatalf("createImplementationCommit: %v", err)
	}
	if commit == nil || commit.SHA == "" {
		t.Fatalf("expected a commit, got %+v", commit)
	}
	if commit.Parent != "" {
		t.Fatalf("root commit reported parent %q, want none", commit.Parent)
	}

	m := loadDemoManifest(t, env)
	recordCommit(m, 0, commit)
	if !m.BaseCommitRecorded || m.BaseCommit != "" {
		t.Fatalf("root-commit base = %q (recorded=%v), want recorded and empty", m.BaseCommit, m.BaseCommitRecorded)
	}
	if err := WriteManifestAtomic(env.deps(), m); err != nil {
		t.Fatalf("WriteManifestAtomic: %v", err)
	}

	data, err := os.ReadFile(env.demoManifest())
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if string(raw["base_commit"]) != "null" {
		t.Fatalf("base_commit = %s, want null", raw["base_commit"])
	}

	// Reloaded, a null base still reads as recorded — the distinction a range
	// resolver needs between "starts at the root" and "no base at all".
	reloaded := loadDemoManifest(t, env)
	if !reloaded.BaseCommitRecorded || reloaded.BaseCommit != "" {
		t.Fatalf("reloaded base = %q (recorded=%v), want recorded and empty", reloaded.BaseCommit, reloaded.BaseCommitRecorded)
	}
	if got := taskByID(t, reloaded, "01-a"); got.Commit == nil || got.Commit.SHA != commit.SHA || got.Commit.Subject != CommitSubject("demo", "01-a") {
		t.Fatalf("task commit round-trip = %+v, want %+v", got.Commit, commit)
	}
}
