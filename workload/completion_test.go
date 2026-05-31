package workload

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

func TestCompletePRDStemsFromDiscovery(t *testing.T) {
	root := t.TempDir()
	writeCompletionPRD(t, root, "alpha")
	writeCompletionPRD(t, root, "beta")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompletePRDStems(CompletionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 || stems[0] != "alpha" || stems[1] != "beta" {
		t.Fatalf("stems = %#v", stems)
	}
}

func TestCompleteIssueIDsRequiresPRD(t *testing.T) {
	root := t.TempDir()
	writeCompletionFixture(t, root, "feature", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	empty, err := CompleteIssueIDs(CompletionInput{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("without PRD: ids=%#v err=%v", empty, err)
	}

	ids, err := CompleteIssueIDs(CompletionInput{PRD: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "01-a" || ids[1] != "02-b" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestCompleteProjectNamesUsesPickerVisibleNames(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompletionPRD(t, projectDir, "svc")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err := CompleteProjectNamesWith(DefaultDeps(), project.DefaultDeps(), func(string) (*config.Config, error) {
		return config.Load(cfgPath)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "svc" {
		t.Fatalf("names = %#v", names)
	}
}

func TestCompletionDoesNotPersistWorkloadState(t *testing.T) {
	root := t.TempDir()
	writeCompletionPRD(t, root, "existing")
	writeCompletionPRD(t, root, "new-prd")

	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	seed := &GlobalState{
		Version: StateVersion,
		Workloads: map[string]*WorkloadEntry{
			canon: {IssueSets: []RegisteredIssueSet{{ID: "existing", Priority: 0}}},
		},
		path: statePath,
	}
	if err := seed.SaveWith(d); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Setenv("XDG_DATA_HOME", root)

	var notices bytes.Buffer
	d.NoticeOut = &notices

	stems, err := CompletePRDStemsWith(d, project.DefaultDeps(), config.Load, CompletionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 {
		t.Fatalf("stems = %#v", stems)
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("state mutated:\nbefore=%q\nafter=%q", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, "pop", "workloads-state.json")); !os.IsNotExist(err) {
		t.Fatal("expected no default state file write")
	}
	if notices.Len() != 0 {
		t.Fatalf("unexpected notices: %q", notices.String())
	}
}

func TestCompletionUnreadableDiscoveryReturnsEmptyWithoutError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	writeCompletionPRD(t, root, "a")
	issueDir := filepath.Join(root, "thoughts/issues")
	if err := os.Chmod(issueDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(issueDir, 0o755) })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompletePRDStems(CompletionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 0 {
		t.Fatalf("stems = %#v", stems)
	}
}

// writeCompletionPRD creates a minimal valid Issue set (no PRD pairing required).
func writeCompletionPRD(t *testing.T, dir, stem string) {
	t.Helper()
	writeCompletionFixture(t, dir, stem, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
}

func writeCompletionFixture(t *testing.T, root, stem string, issues []Issue) {
	t.Helper()
	issueDir := filepath.Join(root, "thoughts/issues", stem)
	for _, issue := range issues {
		path := filepath.Join(issueDir, issue.File)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(t, issueDir, issues)
}

func TestCompletePRDStemsUsesDefinitionOverride(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	writeCompletionPRD(t, defDir, "planned")

	stems, err := CompletePRDStems(CompletionInput{
		Path:               root,
		DefinitionOverride: defDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 1 || stems[0] != "planned" {
		t.Fatalf("stems = %#v", stems)
	}
}

func TestCompleteIssueIDsScopedToSelectedPRD(t *testing.T) {
	root := t.TempDir()
	writeCompletionFixture(t, root, "one", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeCompletionFixture(t, root, "two", []Issue{
		{ID: "99-z", File: "99-z.md", Title: "Z", Type: "AFK", Status: "open"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	ids, err := CompleteIssueIDs(CompletionInput{PRD: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "99-z" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestCompleteProjectNamesMissingConfigIsEmpty(t *testing.T) {
	names, err := CompleteProjectNamesWith(DefaultDeps(), project.DefaultDeps(), func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("names = %#v", names)
	}
}

func TestCompletionNeverWritesProgress(t *testing.T) {
	root := t.TempDir()
	writeCompletionFixture(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_, _ = CompletePRDStems(CompletionInput{})
	_, _ = CompleteIssueIDs(CompletionInput{PRD: "demo"})

	progressPath := filepath.Join(root, "thoughts/issues/demo/progress.txt")
	if _, err := os.Stat(progressPath); !os.IsNotExist(err) {
		t.Fatal("completion should not create progress.txt")
	}
}

func TestCompletePRDStemsDoesNotRegisterInStateFile(t *testing.T) {
	root := t.TempDir()
	writeCompletionPRD(t, root, "fresh")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))

	if _, err := CompletePRDStems(CompletionInput{}); err != nil {
		t.Fatal(err)
	}
	statePath := DefaultStatePath()
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file at %s", statePath)
	}
}

func TestCompletePRDStemsSorted(t *testing.T) {
	root := t.TempDir()
	for _, stem := range []string{"charlie", "alpha", "bravo"} {
		writeCompletionPRD(t, root, stem)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompletePRDStems(CompletionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stems, ",") != "alpha,bravo,charlie" {
		t.Fatalf("stems = %#v", stems)
	}
}
