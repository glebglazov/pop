package workload

import (
	"strings"
	"testing"
)

func TestResolveIssueSetTargetAcceptsIdentifier(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", nil)
	refresh := refreshFixture(t, root)

	id, err := ResolveIssueSetTarget(refresh, "demo")
	if err != nil || id != "demo" {
		t.Fatalf("identifier = %q err=%v", id, err)
	}

	id, err = ResolveIssueSetTarget(refresh, "")
	if err != nil || id != "" {
		t.Fatalf("empty = %q err=%v", id, err)
	}
}

func TestResolveIssueSetTargetRejectsNonIdentifierForms(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	cases := map[string]string{
		"relative path":      "./demo",
		"dot":                ".",
		"absolute path":      "/tmp/demo",
		"home path":          "~/demo",
		"set-relative file":  "demo/01-a.md",
		"bare filename":      "01-a.md",
		"unknown identifier": "missing",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveIssueSetTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
			if !strings.Contains(err.Error(), "demo") {
				t.Fatalf("error should list valid identifiers: %v", err)
			}
		})
	}
}

func TestResolveIssueTargetAcceptsIdentifierAndRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveIssueTarget(refresh, "demo")
	if err != nil || setID != "demo" || issueID != "" {
		t.Fatalf("identifier = %s/%s err=%v", setID, issueID, err)
	}

	setID, issueID, err = ResolveIssueTarget(refresh, "demo/01-add-auth.md")
	if err != nil || setID != "demo" || issueID != "add-auth" {
		t.Fatalf("relative file = %s/%s err=%v", setID, issueID, err)
	}

	setID, issueID, err = ResolveIssueTarget(refresh, "")
	if err != nil || setID != "" || issueID != "" {
		t.Fatalf("empty = %s/%s err=%v", setID, issueID, err)
	}
}

func TestResolveIssueTargetRejectsInvalidForms(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	cases := map[string]string{
		"bare issue identifier": "add-auth",
		"bare filename":         "01-add-auth.md",
		"relative path":         "./demo/01-add-auth.md",
		"absolute path":         "/tmp/demo/01-add-auth.md",
		"home path":             "~/demo/01-add-auth.md",
		"unknown set file":      "missing/01-add-auth.md",
		"extra path segments":   "thoughts/issues/demo/01-add-auth.md",
		"non-md tail":           "demo/01-add-auth",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveIssueTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
		})
	}
}

func TestResolveIssueTargetRejectionListsIdentifiers(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	_, _, err := ResolveIssueTarget(refresh, "01-a")
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("bare issue ID error = %v", err)
	}
}

func TestResolveIssueFileTargetRequiresSetRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Issue{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, issueID, err := ResolveIssueFileTarget(refresh, "demo/01-add-auth.md")
	if err != nil || setID != "demo" || issueID != "add-auth" {
		t.Fatalf("relative file = %s/%s err=%v", setID, issueID, err)
	}

	rejected := map[string]string{
		"empty":           "",
		"bare identifier": "demo",
		"bare filename":   "01-add-auth.md",
		"relative path":   "./demo/01-add-auth.md",
		"absolute path":   "/tmp/demo/01-add-auth.md",
		"unknown set":     "missing/01-add-auth.md",
		"non-md tail":     "demo/01-add-auth",
	}
	for name, raw := range rejected {
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveIssueFileTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
		})
	}
}

func TestIssueTargetIdentifierCompletions(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "alpha", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "beta", nil)
	refresh := refreshFixture(t, root)

	ids := issueTargetIdentifierCompletions(refresh, "")
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "beta" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files := issueTargetIdentifierCompletions(refresh, "alpha/")
	if len(files) != 2 || files[0] != "alpha/01-a.md" || files[1] != "alpha/02-b.md" {
		t.Fatalf("set-relative files = %#v", files)
	}

	for _, out := range [][]string{ids, files} {
		for _, candidate := range out {
			if strings.Contains(candidate, "thoughts/") || strings.HasPrefix(candidate, "./") || strings.HasPrefix(candidate, "/") {
				t.Fatalf("completion offered a filesystem path segment: %q", candidate)
			}
		}
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
