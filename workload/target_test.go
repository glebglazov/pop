package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIssueSetTargetPathAndDot(t *testing.T) {
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

	id, err := ResolveIssueSetTarget(DefaultDeps(), refresh, root, "thoughts/issues/demo")
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

func TestResolveIssueSetTargetAcceptsIdentifier(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", nil)
	refresh := refreshFixture(t, root)

	id, err := ResolveIssueSetTarget(DefaultDeps(), refresh, root, "demo")
	if err != nil || id != "demo" {
		t.Fatalf("identifier = %q err=%v", id, err)
	}

	_, err = ResolveIssueSetTarget(DefaultDeps(), refresh, root, filepath.Join(root, "thoughts/issues/demo"))
	if err == nil || !strings.Contains(err.Error(), "CWD-relative path") {
		t.Fatalf("absolute path error = %v", err)
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
	if !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("error = %v", err)
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

func TestResolveIssueTargetPathAndBareFilename(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveIssueTarget(DefaultDeps(), refresh, root, "thoughts/issues/demo/01-a.md")
	if err != nil || setID != "demo" || issueID != "01-a" {
		t.Fatalf("tree path = %s/%s err=%v", setID, issueID, err)
	}

	issueDir := filepath.Join(root, "thoughts/issues/demo")
	setID, issueID, err = ResolveIssueTarget(DefaultDeps(), refresh, issueDir, "01-a.md")
	if err != nil || setID != "demo" || issueID != "01-a" {
		t.Fatalf("bare filename = %s/%s err=%v", setID, issueID, err)
	}
}

func TestResolveIssueTargetAcceptsIssueSetIdentifierAndRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveIssueTarget(DefaultDeps(), refresh, root, "demo")
	if err != nil || setID != "demo" || issueID != "" {
		t.Fatalf("identifier = %s/%s err=%v", setID, issueID, err)
	}

	setID, issueID, err = ResolveIssueTarget(DefaultDeps(), refresh, root, "demo/01-add-auth.md")
	if err != nil || setID != "demo" || issueID != "add-auth" {
		t.Fatalf("relative file = %s/%s err=%v", setID, issueID, err)
	}

	_, _, err = ResolveIssueTarget(DefaultDeps(), refresh, root, "add-auth")
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("issue ID error = %v", err)
	}

	_, _, err = ResolveIssueTarget(DefaultDeps(), refresh, root, filepath.Join(root, "thoughts/issues/demo/01-add-auth.md"))
	if err == nil || !strings.Contains(err.Error(), "CWD-relative path") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestResolveIssueTargetRejectsUnknownRelativePathWithWorkloadIdentifiers(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	_, _, err := ResolveIssueTarget(DefaultDeps(), refresh, root, "thoughts/issues/missing/01-a.md")
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("error = %v", err)
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
