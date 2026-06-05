package workload

import (
	"path/filepath"
	"strings"
	"testing"
)

func issueStatus(t *testing.T, env *execFixture, issueID string) string {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, issue := range m.Issues {
		if issue.ID == issueID {
			return issue.Status
		}
	}
	t.Fatalf("issue %s not found", issueID)
	return ""
}

func TestSkipIssueOpenToSkipped(t *testing.T) {
	env := setupExecutorFixture(t, false)

	result, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IssueSetID != "demo" || result.IssueID != "01-a" {
		t.Fatalf("skip target = %s/%s", result.IssueSetID, result.IssueID)
	}
	if s := issueStatus(t, env, "01-a"); s != "skipped" {
		t.Fatalf("issue 01-a status = %q, want skipped", s)
	}
	assertProgressContains(t, env, "SKIP", "skipped demo/01-a")
}

func TestSkipIssueHITLOpenToSkipped(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
	})

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := issueStatus(t, env, "01-a"); s != "skipped" {
		t.Fatalf("issue 01-a status = %q, want skipped", s)
	}
}

func TestSkipIssueDoneRejected(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is done") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipIssueFailedRejected(t *testing.T) {
	env := setupFailedIssueFixture(t)

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipIssueAlreadySkippedRejected(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipIssueUnblocksDependent(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	result, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    env.demoIssueRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}

	row := findRow(result.Refresh, "demo")
	if row == nil || row.Status != StatusReady {
		t.Fatalf("set status = %v, want READY", row)
	}

	sel, err := SelectIssueInSet(result.Refresh, "demo")
	if err != nil {
		t.Fatalf("select after skip: %v", err)
	}
	if sel.IssueID != "02-b" {
		t.Fatalf("selected %q, want 02-b", sel.IssueID)
	}
}

func TestSkippedIssueNotSelectedExplicit(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	refresh, err := RefreshWith(env.deps(), env.issuesDir, DefaultStatePathWith(env.deps()))
	if err != nil {
		t.Fatal(err)
	}

	_, err = SelectIssue(refresh, "demo", "01-a")
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkippedIssueNotSelectedAutomatic(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	refresh, err := RefreshWith(env.deps(), env.issuesDir, DefaultStatePathWith(env.deps()))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := firstEligibleIssue("demo", refresh.Manifests["demo"]); err == nil {
		t.Fatal("skipped issue was selected as eligible")
	}
}

func TestSkipIssueRejectsBareIdentifier(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "01-a",
	})
	assertExitCode(t, err, ExitSetup)
}

func TestSkipIssueRejectsAbsolutePath(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := SkipIssueWith(env.deps(), nil, nil, SkipIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    filepath.Join(env.demoDir(), "01-a.md"),
	})
	assertExitCode(t, err, ExitSetup)
}

func TestSkippedCountInProgressText(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Issues: []Issue{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "HITL", Status: "skipped"},
		},
	}
	got := BuildProgress(m, DeriveStatus(m))
	if !strings.Contains(got, "1 skipped") {
		t.Fatalf("progress = %q, want %q segment", got, "1 skipped")
	}
}
