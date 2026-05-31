package workload

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
)

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func pathEqual(t *testing.T, want, got string) {
	t.Helper()
	wantCanon, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantCanon = filepath.Clean(want)
	}
	gotCanon, err := filepath.EvalSymlinks(got)
	if err != nil {
		gotCanon = filepath.Clean(got)
	}
	if wantCanon != gotCanon {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveByExactProjectName(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "my-app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Projects: []config.ProjectEntry{{Path: projectDir}},
	}
	d := DefaultDeps()
	pd := project.DefaultDeps()

	resolved, err := ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return cfg, nil
	}, ResolveInput{ProjectName: "my-app"})
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, projectDir, resolved.ProjectPath)
	pathEqual(t, projectDir, resolved.DefinitionPath)
}

func TestResolveRejectsAmbiguousProjectName(t *testing.T) {
	projects := []project.ExpandedProject{
		{Name: "app", Path: "/a/group/app"},
		{Name: "app", Path: "/a/other/app"},
	}
	_, err := MatchPickerProject("app", projects)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous project") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "/a/group/app") || !strings.Contains(err.Error(), "/a/other/app") {
		t.Fatalf("error missing candidates: %v", err)
	}
}

func TestResolveConcreteWorktreeNotBareContainer(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	mainWT := filepath.Join(repoRoot, "main")
	featureWT := filepath.Join(repoRoot, "feature")

	if err := os.MkdirAll(filepath.Join(repoRoot, ".bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, wt := range []string{mainWT, featureWT} {
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../.bare/worktrees/"+filepath.Base(wt)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{
		Projects: []config.ProjectEntry{{Path: repoRoot}},
	}
	d := DefaultDeps()
	pd := project.DefaultDeps()

	projects, err := ListPickerProjectsWith(pd, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %#v", projects)
	}

	resolved, err := ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return cfg, nil
	}, ResolveInput{ProjectName: "repo/main"})
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, mainWT, resolved.ProjectPath)

	_, err = ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return cfg, nil
	}, ResolveInput{ProjectName: "repo"})
	if err == nil {
		t.Fatal("expected unknown project for bare container name")
	}
}

func TestResolveCWDFallbackOutsideGit(t *testing.T) {
	root := t.TempDir()
	d := DefaultDeps()
	pd := project.DefaultDeps()

	resolved, err := ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, ResolveInput{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, root, resolved.ProjectPath)
	pathEqual(t, root, resolved.DefinitionPath)
}

func TestNormalizeProjectPathToGitRoot(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	subdir := filepath.Join(root, "pkg", "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	normalized, err := NormalizeProjectPath(subdir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		want = root
	}
	if normalized != want {
		t.Fatalf("normalized = %q, want %q", normalized, want)
	}
}

func TestDefinitionOverridePreservesExactDirectory(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	defDir := filepath.Join(root, "thoughts-worktree")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	pd := project.DefaultDeps()
	resolved, err := ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, ResolveInput{
		Path:               root,
		DefinitionOverride: defDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, defDir, resolved.DefinitionPath)
	if resolved.DefinitionPath == resolved.ProjectPath {
		t.Fatal("definition override should not collapse to git root")
	}
}

func TestSetPrioritySignedAndStableTies(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	writeFile(t, filepath.Join(root, "thoughts/prds/a.md"), "# A\n")
	writeFile(t, filepath.Join(root, "thoughts/prds/b.md"), "# B\n")
	setupManifest(t, root, "a", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "b", []Issue{
		{ID: "01-b", File: "01-b.md", Title: "B", Type: "AFK", Status: "open"},
	})

	statePath := DefaultStatePath()
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Workloads: map[string]*WorkloadEntry{canon: {PRDs: []RegisteredPRD{
			{ID: "a", Priority: 0},
			{ID: "b", Priority: 0},
		}}},
		path: statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	input := ResolveInput{CWD: root}

	result, err := SetPriorityWith(d, project.DefaultDeps(), func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, input, "a", -5)
	if err != nil {
		t.Fatal(err)
	}
	if result.OldPriority != 0 || result.NewPriority != -5 {
		t.Fatalf("priority change = %d -> %d", result.OldPriority, result.NewPriority)
	}

	var activeIDs []string
	for _, row := range result.Refresh.Rows {
		if row.Status == StatusReady || row.Status == StatusBlocked || row.Status == StatusFailed || row.Status == StatusUnplanned || row.Status == StatusMalformed {
			activeIDs = append(activeIDs, row.ID)
		}
	}
	if len(activeIDs) != 2 || activeIDs[0] != "b" || activeIDs[1] != "a" {
		t.Fatalf("active order = %v, want [b a]", activeIDs)
	}
}

func TestSetPriorityRejectsInvalidPRDIdentifier(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	writeFile(t, filepath.Join(root, "thoughts/prds/feature.md"), "# Feature\n")

	if _, err := RefreshWith(DefaultDeps(), root, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	input := ResolveInput{CWD: root}

	for _, id := range []string{"feature.md", "Feature", "feat", "feature-framework"} {
		_, err := SetPriorityWith(d, project.DefaultDeps(), func(string) (*config.Config, error) {
			return nil, os.ErrNotExist
		}, input, id, 1)
		if err == nil {
			t.Fatalf("expected error for %q", id)
		}
		if !strings.Contains(err.Error(), "valid:") {
			t.Fatalf("error for %q = %v", id, err)
		}
	}
}

func TestMarkAutoPickSkipsNonRunnableHigherPriority(t *testing.T) {
	rows := []Row{
		{ID: "blocked-high", Status: StatusBlocked, Priority: 10, PriorityShow: "10"},
		{ID: "ready-low", Status: StatusReady, Priority: 0, PriorityShow: "0"},
	}
	MarkAutoPick(rows)
	if rows[0].AutoPick {
		t.Fatal("blocked row should not be AUTO")
	}
	if !rows[1].AutoPick || rows[1].PriorityShow != "0 AUTO" {
		t.Fatalf("ready row = %#v", rows[1])
	}
}

func TestRefreshMarksAutoPickInRender(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "thoughts/prds/blocked.md"), "# Blocked\n")
	writeFile(t, filepath.Join(root, "thoughts/prds/ready.md"), "# Ready\n")
	setupManifest(t, root, "blocked", []Issue{
		{ID: "01-hitl", File: "01-hitl.md", Title: "H", Type: "HITL", Status: "open"},
	})
	setupManifest(t, root, "ready", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Workloads: map[string]*WorkloadEntry{canon: {PRDs: []RegisteredPRD{
			{ID: "blocked", Priority: 10},
			{ID: "ready", Priority: 0},
		}}},
		path: statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "0 AUTO") {
		t.Fatalf("missing AUTO marker:\n%s", out)
	}
}

func TestDefinitionOverrideUsesCanonicalPathAsStateKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	defRoot := filepath.Join(root, "planning")
	writeFile(t, filepath.Join(defRoot, "thoughts/prds/x.md"), "# X\n")

	d := DefaultDeps()
	result, err := RefreshWith(d, defRoot, DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, defRoot, result.DefinitionPath)

	state, err := LoadGlobalState(DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(defRoot)
	if state.Workloads[canon] == nil {
		t.Fatalf("state keys = %#v", state.Workloads)
	}
}

func TestResolveByPathFlag(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	subdir := filepath.Join(root, "src")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	pd := project.DefaultDeps()
	resolved, err := ResolvePathsWith(d, pd, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, ResolveInput{Path: subdir})
	if err != nil {
		t.Fatal(err)
	}
	pathEqual(t, root, resolved.ProjectPath)
}

func TestResolveWithMockGitOutsideRepo(t *testing.T) {
	d := &Deps{
		FS: &deps.MockFileSystem{
			GetwdFunc: func() (string, error) { return "/tmp/planning", nil },
			EvalSymlinksFunc: func(path string) (string, error) {
				return path, nil
			},
		},
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				return "", os.ErrNotExist
			},
		},
	}

	normalized, err := NormalizeProjectPathWith(d, "/tmp/planning/sub")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "/tmp/planning/sub" {
		t.Fatalf("normalized = %q", normalized)
	}
}
