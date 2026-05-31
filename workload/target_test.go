package workload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIssueSetTargetBareAndPath(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	refresh := refreshFixture(t, root)

	id, err := ResolveIssueSetTarget(DefaultDeps(), refresh, root, "demo")
	if err != nil || id != "demo" {
		t.Fatalf("bare id = %q err=%v", id, err)
	}

	id, err = ResolveIssueSetTarget(DefaultDeps(), refresh, root, "thoughts/issues/demo")
	if err != nil || id != "demo" {
		t.Fatalf("tree path = %q err=%v", id, err)
	}

	issueDir := filepath.Join(root, "thoughts/issues/demo")
	if err := os.Chdir(issueDir); err != nil {
		t.Fatal(err)
	}
	id, err = ResolveIssueSetTarget(DefaultDeps(), refresh, issueDir, ".")
	if err != nil || id != "demo" {
		t.Fatalf("dot path = %q err=%v", id, err)
	}
}

func TestResolveIssueSetTargetRejectsUnknownPath(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", nil)
	refresh := refreshFixture(t, root)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_, err := ResolveIssueSetTarget(DefaultDeps(), refresh, root, "thoughts/issues/missing")
	if err == nil {
		t.Fatal("expected unknown Issue set error")
	}
}

func TestResolveWorkloadTargetsSpanningIssuePath(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	setID, issueID, err := ResolveWorkloadTargets(DefaultDeps(), refresh, root, "", "thoughts/issues/demo/01-a.md")
	if err != nil {
		t.Fatal(err)
	}
	if setID != "demo" || issueID != "01-a" {
		t.Fatalf("got %s/%s", setID, issueID)
	}
}

func TestResolveWorkloadTargetsIssueSetRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveWorkloadTargets(DefaultDeps(), refresh, root, "demo", "01-add-auth.md")
	if err != nil {
		t.Fatal(err)
	}
	if setID != "demo" || issueID != "add-auth" {
		t.Fatalf("got %s/%s", setID, issueID)
	}
}

func TestResolveWorkloadTargetsBareFilenameRequiresIssueSet(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	_, _, err := ResolveWorkloadTargets(DefaultDeps(), refresh, root, "", "01-a.md")
	if err == nil {
		t.Fatal("expected error for bare filename without issue set")
	}
}

func TestResolveWorkloadTargetsMismatchRejected(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "alpha", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "beta", []Issue{
		{ID: "01-b", File: "01-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_, _, err := ResolveWorkloadTargets(DefaultDeps(), refresh, root, "alpha", "thoughts/issues/beta/01-b.md")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestResolveWorkloadTargetsStrictIDVsFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveWorkloadTargets(DefaultDeps(), refresh, root, "demo", "add-auth")
	if err != nil {
		t.Fatal(err)
	}
	if setID != "demo" || issueID != "add-auth" {
		t.Fatalf("got %s/%s", setID, issueID)
	}

	_, _, err = ResolveWorkloadTargets(DefaultDeps(), refresh, root, "demo", "01-add-auth")
	if err == nil {
		t.Fatal("expected unknown issue for extensionless file-like value")
	}
}

func TestIssueSetPathCompletions(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "alpha", nil)
	setupManifest(t, root, "beta", nil)
	refresh := refreshFixture(t, root)

	out := issueSetPathCompletions(refresh, "thoughts/issues/a")
	if len(out) != 1 || out[0] != "thoughts/issues/alpha" {
		t.Fatalf("completions = %#v", out)
	}
}

func refreshFixture(t *testing.T, root string) *RefreshResult {
	t.Helper()
	result, err := Refresh(root)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
