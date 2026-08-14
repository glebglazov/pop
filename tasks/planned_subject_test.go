package tasks

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// plannedSubject is the house-convention subject a planner would have written
// onto a task — no `tasks(...)` prefix anywhere in it, which is what makes the
// verify range in the test below prove it resolves without a subject search.
const plannedSubject = "feat(auth): add token refresh"

// TestPlannedSubjectCommittedVerbatimAndFallsBackToDefault drives a two-task set
// through the executor's success path — one task carrying a Planned commit
// subject, one without — and follows the result all the way to the range the
// Verifier would review (ADR-0207). It asserts the three things that make the
// planned subject usable: the subject reaches git byte-for-byte with the agent
// summary still as the body, a task with no plan commits in pop's default
// format, and both new keys survive the manifest rewrites the drain performs.
func TestPlannedSubjectCommittedVerbatimAndFallsBackToDefault(t *testing.T) {
	env := setupExecutorFixtureIsolated(t)
	writeTaskMD(t, env.demoDir(), "02-b.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, env.demoDir(), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", CommitSubject: plannedSubject},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	}, map[string]any{"commit_convention": "Conventional Commits: <type>(<scope>): <summary>"})

	preDrainHEAD, err := realGitInDir(env.root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	planned := drainTaskWithCommit(t, env, "01-a", "a.txt")
	fallback := drainTaskWithCommit(t, env, "02-b", "b.txt")

	for _, want := range []struct {
		sha, subject, summary string
	}{
		{planned.CommitSHA, plannedSubject, "summary for 01-a"},
		{fallback.CommitSHA, CommitSubject("demo", "02-b"), "summary for 02-b"},
	} {
		subject, err := realGitInDir(env.root, "log", "-1", "--format=%s", want.sha)
		if err != nil {
			t.Fatalf("log %s: %v", want.sha, err)
		}
		if subject != want.subject {
			t.Fatalf("commit subject = %q, want %q", subject, want.subject)
		}
		body, err := realGitInDir(env.root, "log", "-1", "--format=%b", want.sha)
		if err != nil {
			t.Fatalf("log body %s: %v", want.sha, err)
		}
		if strings.TrimSpace(body) != want.summary {
			t.Fatalf("commit body = %q, want the agent summary %q", body, want.summary)
		}
	}

	m := loadDemoManifest(t, env)
	if got := taskByID(t, m, "01-a"); got.CommitSubject != plannedSubject {
		t.Fatalf("planned subject after drain = %q, want %q", got.CommitSubject, plannedSubject)
	}
	if got := taskByID(t, m, "01-a"); got.Commit == nil || got.Commit.Subject != plannedSubject {
		t.Fatalf("recorded commit = %+v, want the planned subject recorded verbatim", got.Commit)
	}
	if m.CommitConvention == "" {
		t.Fatal("set-level commit_convention did not survive the drain's manifest rewrites")
	}

	var raw map[string]json.RawMessage
	data, err := os.ReadFile(env.demoManifest())
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if !strings.Contains(string(raw["commit_convention"]), "Conventional Commits") {
		t.Fatalf("commit_convention on disk = %s", raw["commit_convention"])
	}
	if !strings.Contains(string(raw["tasks"]), plannedSubject) {
		t.Fatalf("commit_subject not written back: %s", raw["tasks"])
	}

	// The end of the line: with no `tasks(...)` subject anywhere in the planned
	// commit, the range still resolves off the recorded base and SHAs.
	work := verifyWorkDiff(env.deps(), env.root, "demo", m)
	if work.Undetermined {
		t.Fatalf("verify range undetermined for a set committed under planned subjects: %+v", work)
	}
	if work.Range != preDrainHEAD+"..HEAD" {
		t.Fatalf("verify range = %q, want %q", work.Range, preDrainHEAD+"..HEAD")
	}
	for _, file := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(work.Stat, file) {
			t.Fatalf("verify stat omits %s:\n%s", file, work.Stat)
		}
	}
}

// TestImplementationSubjectFallback pins the one rule the commit path applies to
// a planned subject: take it as written, and take pop's format only when there is
// nothing to take.
func TestImplementationSubjectFallback(t *testing.T) {
	t.Parallel()
	m := &Manifest{Tasks: []Task{
		{ID: "01-a", CommitSubject: plannedSubject},
		{ID: "02-b"},
		{ID: "03-c", CommitSubject: "   "},
	}}
	for _, tc := range []struct{ taskID, want string }{
		{"01-a", plannedSubject},
		{"02-b", CommitSubject("2026-08-15-demo", "02-b")},
		{"03-c", CommitSubject("2026-08-15-demo", "03-c")},
	} {
		got := implementationSubject(&Selection{TaskSetID: "2026-08-15-demo", TaskID: tc.taskID, Manifest: m})
		if got != tc.want {
			t.Fatalf("subject for %s = %q, want %q", tc.taskID, got, tc.want)
		}
	}
}

// TestCommitConventionMalformedReadsAsAbsent: the convention is advisory prose,
// so a value pop cannot read leaves the set perfectly valid and rides through the
// next rewrite untouched.
func TestCommitConventionMalformedReadsAsAbsent(t *testing.T) {
	env := setupExecutorFixtureIsolated(t)
	writeManifestWithSetKeys(t, env.demoDir(), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"commit_convention": 42})

	m := loadDemoManifest(t, env)
	if m.CommitConvention != "" {
		t.Fatalf("malformed convention read as %q, want absent", m.CommitConvention)
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
	if string(raw["commit_convention"]) != "42" {
		t.Fatalf("malformed convention = %s, want it preserved verbatim", raw["commit_convention"])
	}
}
