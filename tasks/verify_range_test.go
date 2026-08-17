package tasks

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// errNotAncestor stands in for the failing exit status git returns from
// `merge-base --is-ancestor` when the commit is not reachable from HEAD — the
// same answer a missing object gives.
var errNotAncestor = errors.New("exit status 1")

// rangeRepo is a real git repository plus the calls the range resolver made
// against it, so a test can assert both the range and how it was reached.
type rangeRepo struct {
	dir   string
	calls *[][]string
	deps  *Deps
}

// newRangeRepo initializes a repository with an identity to commit with and
// wires a Deps whose git records every command before running it for real.
func newRangeRepo(t *testing.T) *rangeRepo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := realGitInDir(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	calls := &[][]string{}
	real := deps.NewRealGit()
	git := &deps.MockGit{CommandInDirFunc: func(d string, args ...string) (string, error) {
		*calls = append(*calls, args)
		return real.CommandInDir(d, args...)
	}}
	return &rangeRepo{dir: dir, calls: calls, deps: &Deps{FS: deps.NewRealFileSystem(), Git: git}}
}

// commit writes one file and commits it under subject, returning the new SHA.
// Each extra paragraph becomes its own `-m`, exactly as the commit path builds a
// message body and its trailer.
func (r *rangeRepo) commit(t *testing.T, file, subject string, paragraphs ...string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, file), []byte(subject+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	if _, err := realGitInDir(r.dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	args := []string{"commit", "-m", subject}
	for _, p := range paragraphs {
		args = append(args, "-m", p)
	}
	if _, err := realGitInDir(r.dir, args...); err != nil {
		t.Fatalf("git commit %q: %v", subject, err)
	}
	sha, err := realGitInDir(r.dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return sha
}

// commitTask commits the way the executor does: the task's subject, the agent's
// summary, and the set's Pop-Task trailer as the message's last paragraph.
func (r *rangeRepo) commitTask(t *testing.T, file, subject, taskID string) string {
	t.Helper()
	return r.commit(t, file, subject, "summary for "+taskID, TaskTrailer("demo", taskID))
}

func (r *rangeRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := realGitInDir(r.dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// grepped reports whether the resolver searched commit messages.
func (r *rangeRepo) grepped() bool {
	for _, call := range *r.calls {
		for _, arg := range call {
			if arg == "--grep" || strings.HasPrefix(arg, "--grep=") {
				return true
			}
		}
	}
	return false
}

// subjectsIn lists the subjects of the commits a resolved range covers.
func (r *rangeRepo) subjectsIn(t *testing.T, rng string) []string {
	t.Helper()
	out := r.git(t, "log", "--format=%s", rng)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

// recordedSet builds the manifest an executor would have left behind: a set base
// plus one recorded commit per task.
func recordedSet(base string, commits ...TaskCommit) *Manifest {
	m := &Manifest{Stem: "demo", BaseCommit: base, BaseCommitRecorded: true}
	for i, c := range commits {
		commit := c
		m.Tasks = append(m.Tasks, Task{
			ID:     []string{"01-a", "02-b", "03-c"}[i],
			Type:   "AFK",
			Status: TaskDone,
			Commit: &commit,
		})
	}
	return m
}

// TestVerifyRangeFromRecordedBase: with the base and every recorded task commit
// still reachable, the range is exactly `base..HEAD` and no commit-message
// search happens — the subjects here are a house convention with no `tasks(...)`
// prefix to grep for, which is the whole point of recording the base (ADR-0207).
func TestVerifyRangeFromRecordedBase(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	base := repo.commit(t, "trunk.txt", "chore: seed the trunk")
	first := repo.commit(t, "a.txt", "feat(store): add the a path")
	second := repo.commit(t, "b.txt", "feat(store): add the b path")

	m := recordedSet(base,
		TaskCommit{SHA: first, Subject: "feat(store): add the a path"},
		TaskCommit{SHA: second, Subject: "feat(store): add the b path"},
	)
	work := verifyWorkDiff(repo.deps, repo.dir, "demo", m)

	if work.Range != base+"..HEAD" {
		t.Fatalf("range = %q, want %q", work.Range, base+"..HEAD")
	}
	if repo.grepped() {
		t.Fatalf("resolver searched commit messages with the base intact: %v", *repo.calls)
	}
	for _, file := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(work.Stat, file) {
			t.Fatalf("stat omits %s:\n%s", file, work.Stat)
		}
	}
	if strings.Contains(work.Stat, "trunk.txt") {
		t.Fatalf("stat includes the pre-set commit:\n%s", work.Stat)
	}
}

// TestVerifyRangeAfterRebaseUsesTheTaskTrailer: a rebase onto a newer trunk
// rewrites every task SHA while leaving the old base an ancestor, so `base..HEAD`
// would swallow the commits others landed meanwhile. The rewritten SHAs are what
// catch it: the range restarts at the earliest commit carrying the set's Pop-Task
// trailer, and the foreign commit below it stays out (ADR-0216).
func TestVerifyRangeAfterRebaseUsesTheTaskTrailer(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	base := repo.commit(t, "trunk.txt", "chore: seed the trunk")
	branch := repo.git(t, "rev-parse", "--abbrev-ref", "HEAD")
	first := repo.commitTask(t, "a.txt", "feat(store): add the a path", "01-a")
	second := repo.commitTask(t, "b.txt", "feat(store): add the b path", "02-b")

	repo.git(t, "checkout", "-b", "trunk", base)
	foreign := repo.commit(t, "other.txt", "fix(api): someone else's commit")
	repo.git(t, "checkout", branch)
	repo.git(t, "rebase", "--onto", "trunk", base)

	m := recordedSet(base,
		TaskCommit{SHA: first, Subject: "feat(store): add the a path"},
		TaskCommit{SHA: second, Subject: "feat(store): add the b path"},
	)
	work := verifyWorkDiff(repo.deps, repo.dir, "demo", m)

	if work.Undetermined {
		t.Fatal("range read as undetermined although the trailer survived the rebase")
	}
	if work.Range != foreign+"..HEAD" {
		t.Fatalf("range = %q, want the rebased set's own commits %q", work.Range, foreign+"..HEAD")
	}
	got := repo.subjectsIn(t, work.Range)
	want := []string{"feat(store): add the b path", "feat(store): add the a path"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("range covers %v, want exactly the set's own commits %v", got, want)
	}
	if strings.Contains(work.Stat, "other.txt") {
		t.Fatalf("stat includes another author's commit:\n%s", work.Stat)
	}
}

// TestVerifyRangeRecoveryFromTheRootCommit: the set's first commit is the
// repository's own first commit, so the recovered anchor has no parent and the
// empty tree stands in for it — the range still covers everything the set landed.
func TestVerifyRangeRecoveryFromTheRootCommit(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	first := repo.commitTask(t, "a.txt", "feat(store): add the a path", "01-a")
	repo.commitTask(t, "b.txt", "feat(store): add the b path", "02-b")

	// A rebase from the root rewrites every SHA, the root commit's included, while
	// keeping every message — so layer one cannot answer and layer two must.
	repo.git(t, "rebase", "--root", "--force-rebase")

	m := recordedSet("", TaskCommit{SHA: first, Subject: "feat(store): add the a path"})
	work := verifyWorkDiff(repo.deps, repo.dir, "demo", m)

	if work.Undetermined {
		t.Fatalf("work = %+v, want a range recovered from the root commit", work)
	}
	if work.Range != emptyTreeSHA+"..HEAD" {
		t.Fatalf("range = %q, want the empty tree standing in for the missing parent %q", work.Range, emptyTreeSHA+"..HEAD")
	}
	for _, file := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(work.Stat, file) {
			t.Fatalf("stat omits %s:\n%s", file, work.Stat)
		}
	}
}

// TestVerifyRangeIgnoresACommitQuotingAPlannedSubject: a revert, a fixup, or a
// merge summary repeats a commit's subject in its own message. Such a commit is
// older than the set's first commit, so anchoring on it would silently widen the
// changeset — only the trailer pop wrote anchors the range (ADR-0216).
func TestVerifyRangeIgnoresACommitQuotingAPlannedSubject(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	base := repo.commit(t, "trunk.txt", "chore: seed the trunk")
	branch := repo.git(t, "rev-parse", "--abbrev-ref", "HEAD")
	first := repo.commitTask(t, "a.txt", "feat(store): add the a path", "01-a")

	repo.git(t, "checkout", "-b", "trunk", base)
	// The quoting commit, and a foreign one under it, both belong to trunk.
	repo.commit(t, "other.txt", "fix(api): someone else's commit")
	quoting := repo.commit(t, "revert.txt", `Revert "feat(store): add the a path"`,
		`This reverts an earlier attempt at "feat(store): add the a path".`)
	repo.git(t, "checkout", branch)
	repo.git(t, "rebase", "--onto", "trunk", base)

	m := recordedSet(base, TaskCommit{SHA: first, Subject: "feat(store): add the a path"})
	work := verifyWorkDiff(repo.deps, repo.dir, "demo", m)

	if work.Range != quoting+"..HEAD" {
		t.Fatalf("range = %q, want it to start after the quoting commit %q", work.Range, quoting+"..HEAD")
	}
	if got := repo.subjectsIn(t, work.Range); strings.Join(got, "|") != "feat(store): add the a path" {
		t.Fatalf("range covers %v, want only the set's own commit", got)
	}
	for _, file := range []string{"revert.txt", "other.txt"} {
		if strings.Contains(work.Stat, file) {
			t.Fatalf("stat includes %s, a commit the set did not make:\n%s", file, work.Stat)
		}
	}
}

// TestVerifyRangeUndeterminedWithoutATrailer: a set that drained before the
// trailer existed has nothing left to resolve against once its recorded SHAs go
// unreachable — the recorded subjects are deliberately not searched, because a
// commit merely quoting one is a wrong answer where this is a loud failure.
func TestVerifyRangeUndeterminedWithoutATrailer(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	base := repo.commit(t, "trunk.txt", "chore: seed the trunk")
	first := repo.commit(t, "a.txt", "feat(store): add the a path")
	second := repo.commit(t, "b.txt", "feat(store): add the b path")

	// The rebase kept the subjects, dropped the SHAs, and there is no trailer.
	repo.git(t, "reset", "--hard", base)
	repo.commit(t, "a.txt", "feat(store): add the a path")

	m := recordedSet(base,
		TaskCommit{SHA: first, Subject: "feat(store): add the a path"},
		TaskCommit{SHA: second, Subject: "feat(store): add the b path"},
	)
	work := verifyWorkDiff(repo.deps, repo.dir, "demo", m)

	if !work.Undetermined {
		t.Fatalf("work = %+v, want an undetermined range", work)
	}
	if work.Range != "" {
		t.Fatalf("undetermined view still named a range %q", work.Range)
	}
}

// TestVerifyRangeLegacySetGrepsSubjectPrefix: a set drained before the base was
// recorded still resolves the old way — the earliest `tasks(<slug>):` commit
// starts the range.
func TestVerifyRangeLegacySetGrepsSubjectPrefix(t *testing.T) {
	t.Parallel()
	repo := newRangeRepo(t)
	repo.commit(t, "trunk.txt", "chore: seed the trunk")
	first := repo.commit(t, "a.txt", CommitSubject("demo", "01-a"))
	repo.commit(t, "b.txt", CommitSubject("demo", "02-b"))

	work := verifyWorkDiff(repo.deps, repo.dir, "demo", &Manifest{Stem: "demo"})

	if work.Range != first+"^..HEAD" {
		t.Fatalf("range = %q, want %q", work.Range, first+"^..HEAD")
	}
	if !repo.grepped() {
		t.Fatalf("a set with no recorded base must still find its commits by subject: %v", *repo.calls)
	}
	if work.Undetermined {
		t.Fatal("a legacy set with findable commits must not read as undetermined")
	}
}

// TestVerifyPhaseParksWhenRangeUndetermined: the verify phase turns an
// unresolvable range into a NEEDS-HUMAN park that says so, without invoking the
// Verifier — a review of a guessed range is worse than no review.
func TestVerifyPhaseParksWhenRangeUndetermined(t *testing.T) {
	t.Parallel()
	// A checkout whose history holds neither the recorded SHAs (merge-base fails)
	// nor the recorded subjects (the log search comes back empty).
	git := &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return "sha1\n", nil
		case len(args) >= 1 && args[0] == "merge-base":
			return "", errNotAncestor
		}
		return "", nil
	}}
	tasks := []Task{{
		ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done",
		Commit: &TaskCommit{SHA: "gone", Subject: "feat(store): add the a path"},
	}}
	d, m := setupDrainVerifyFixture(t, git, tasks, map[string]any{"base_commit": "alsogone"})
	if !m.BaseCommitRecorded {
		t.Fatal("fixture set recorded no base commit")
	}

	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier was invoked although the set's commit range is unknown")
			return "", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusVerifyFailed {
		t.Fatalf("status = %q, want VERIFY-FAILED", status)
	}
	if verdict == nil || Verdict(verdict.Verdict) != VerdictNeedsHuman {
		t.Fatalf("verdict = %+v, want NEEDS-HUMAN", verdict)
	}
	if !strings.Contains(verdict.Findings, "commit range could not be determined") {
		t.Fatalf("findings = %q, want the range-undetermined message", verdict.Findings)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "sha1"); stored == nil || Verdict(stored.Verdict) != VerdictNeedsHuman {
		t.Fatalf("stored verdict = %+v, want NEEDS-HUMAN at sha1", stored)
	}
}
