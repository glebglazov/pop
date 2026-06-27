package tasks

import (
	"strings"
	"testing"
)

func TestResolveTaskSetTargetAcceptsIdentifier(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", nil)
	refresh := refreshFixture(t, root)

	id, err := ResolveTaskSetTarget(refresh, "demo")
	if err != nil || id != "demo" {
		t.Fatalf("identifier = %q err=%v", id, err)
	}

	id, err = ResolveTaskSetTarget(refresh, "")
	if err != nil || id != "" {
		t.Fatalf("empty = %q err=%v", id, err)
	}
}

func TestResolveTaskSetTargetRejectsNonIdentifierForms(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
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
			_, err := ResolveTaskSetTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
			if !strings.Contains(err.Error(), "demo") {
				t.Fatalf("error should list valid identifiers: %v", err)
			}
		})
	}
}

func TestResolveTaskTargetAcceptsIdentifierAndRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, taskID, err := ResolveTaskTarget(refresh, "demo")
	if err != nil || setID != "demo" || taskID != "" {
		t.Fatalf("identifier = %s/%s err=%v", setID, taskID, err)
	}

	setID, taskID, err = ResolveTaskTarget(refresh, "demo/01-add-auth.md")
	if err != nil || setID != "demo" || taskID != "add-auth" {
		t.Fatalf("relative file = %s/%s err=%v", setID, taskID, err)
	}

	setID, taskID, err = ResolveTaskTarget(refresh, "")
	if err != nil || setID != "" || taskID != "" {
		t.Fatalf("empty = %s/%s err=%v", setID, taskID, err)
	}
}

func TestResolveTaskSetTargetAcceptsTrailingSlash(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", nil)
	refresh := refreshFixture(t, root)

	id, err := ResolveTaskSetTarget(refresh, "demo/")
	if err != nil || id != "demo" {
		t.Fatalf("trailing slash = %q err=%v", id, err)
	}

	// A remainder with an inner slash after trimming stays rejected.
	if _, err := ResolveTaskSetTarget(refresh, "a/b/"); err == nil {
		t.Fatalf("expected rejection for inner-slash form")
	}
}

func TestResolveTaskTargetAcceptsTrailingSlashAsWholeSet(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, taskID, err := ResolveTaskTarget(refresh, "demo/")
	if err != nil || setID != "demo" || taskID != "" {
		t.Fatalf("trailing slash = %s/%s err=%v", setID, taskID, err)
	}

	// The trailing-slash form resolves identically to the bare set.
	bareID, bareTask, bareErr := ResolveTaskTarget(refresh, "demo")
	if bareID != setID || bareTask != taskID || (bareErr == nil) != (err == nil) {
		t.Fatalf("trailing slash diverged from bare: %s/%s vs %s/%s", setID, taskID, bareID, bareTask)
	}

	// An inner-slash form after trimming stays rejected.
	if _, _, err := ResolveTaskTarget(refresh, "a/b/"); err == nil {
		t.Fatalf("expected rejection for inner-slash form")
	}
}

func TestResolveTaskTargetRejectsInvalidForms(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	cases := map[string]string{
		"bare task identifier": "add-auth",
		"bare filename":        "01-add-auth.md",
		"relative path":        "./demo/01-add-auth.md",
		"absolute path":        "/tmp/demo/01-add-auth.md",
		"home path":            "~/demo/01-add-auth.md",
		"unknown set file":     "missing/01-add-auth.md",
		"extra path segments":  "thoughts/issues/demo/01-add-auth.md",
		"non-md tail":          "demo/01-add-auth",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := ResolveTaskTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
		})
	}
}

func TestResolveTaskTargetRejectionListsIdentifiers(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	_, _, err := ResolveTaskTarget(refresh, "01-a")
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("bare task ID error = %v", err)
	}
}

func TestResolveTaskFileTargetRequiresSetRelativeFile(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "add-auth", File: "01-add-auth.md", Title: "Auth", Type: "AFK", Status: "open"},
	})
	refresh := refreshFixture(t, root)

	setID, taskID, err := ResolveTaskFileTarget(refresh, "demo/01-add-auth.md")
	if err != nil || setID != "demo" || taskID != "add-auth" {
		t.Fatalf("relative file = %s/%s err=%v", setID, taskID, err)
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
			_, _, err := ResolveTaskFileTarget(refresh, raw)
			if err == nil {
				t.Fatalf("expected rejection for %q", raw)
			}
		})
	}
}

func TestTaskTargetIdentifierCompletions(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "alpha", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "beta", nil)
	refresh := refreshFixture(t, root)

	ids := taskTargetIdentifierCompletions(refresh, "")
	if len(ids) != 2 || ids[0] != "alpha/" || ids[1] != "beta/" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files := taskTargetIdentifierCompletions(refresh, "alpha/")
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

func TestActionableTaskTargetCompletionsNeverOfferDoneThings(t *testing.T) {
	refresh := &RefreshResult{Manifests: map[string]*Manifest{
		"finished": {Valid: true, Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Status: "done"},
		}},
		"deferred": {Valid: true, Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Status: "done"},
			{ID: "02-b", File: "02-b.md", Status: "skipped"},
		}},
		"mixed": {Valid: true, Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Status: "done"},
			{ID: "02-b", File: "02-b.md", Status: "open"},
		}},
		"broken": {Valid: false},
	}}

	ids := actionableTaskTargetCompletions(refresh, "")
	if strings.Join(ids, ",") != "broken/,deferred/,mixed/" {
		t.Fatalf("actionable identifiers = %#v", ids)
	}

	files := actionableTaskTargetCompletions(refresh, "mixed/")
	if len(files) != 1 || files[0] != "mixed/02-b.md" {
		t.Fatalf("actionable files = %#v", files)
	}

	// The unfiltered variant still offers Done sets and done tasks (timings).
	all := taskTargetIdentifierCompletions(refresh, "")
	if strings.Join(all, ",") != "broken/,deferred/,finished/,mixed/" {
		t.Fatalf("unfiltered identifiers = %#v", all)
	}
	allFiles := taskTargetIdentifierCompletions(refresh, "mixed/")
	if len(allFiles) != 2 {
		t.Fatalf("unfiltered files = %#v", allFiles)
	}
}

func refreshFixture(t *testing.T, root string) *RefreshResult {
	t.Helper()
	result, err := RegisterWith(DefaultDeps(), root, StatePathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
