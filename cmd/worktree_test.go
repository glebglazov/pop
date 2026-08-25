package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
)

func runWorktreeFoldGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func addWorktreeFoldCommit(t *testing.T, repo, name string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(repo), name)
	runWorktreeFoldGit(t, repo, "worktree", "add", "-b", name, path)
	if err := os.WriteFile(filepath.Join(path, "work.txt"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runWorktreeFoldGit(t, path, "add", "work.txt")
	runWorktreeFoldGit(t, path, "commit", "-m", "worktree work")
	return path
}

func TestWorktreeFoldDefaultsToCurrentCheckoutAndKeepsIt(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	wt := addWorktreeFoldCommit(t, repo, "human-work")
	d := newTestCmdDeps(t, wt, filepath.Join(parent, "xdg"), filepath.Join(parent, "config"))
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	err := runWorktreeFoldWith(d, cfg, wt, "", binding.FoldOptions{
		Yes: true, In: tasks.NonInteractiveReader{}, ConfirmCheckoutFold: true,
	}, io.Discard)
	if err != nil {
		t.Fatalf("fold current worktree: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("fold deleted checkout: %v", err)
	}
	if got := runWorktreeFoldGit(t, wt, "branch", "--show-current"); got != "human-work" {
		t.Fatalf("checkout branch = %q, want human-work", got)
	}
	branchTip := runWorktreeFoldGit(t, repo, "rev-parse", "human-work")
	if trunkTip := runWorktreeFoldGit(t, repo, "rev-parse", "HEAD"); trunkTip != branchTip {
		t.Fatalf("trunk tip = %s, branch tip = %s", trunkTip, branchTip)
	}
}

func TestWorktreeFoldNamedUnboundManagedWorktreeSurvives(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	d := newTestCmdDeps(t, repo, filepath.Join(parent, "xdg"), filepath.Join(parent, "config"))
	b, err := binding.ProvisionScratchWorktree(d.tasksDeps(), repo, "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision managed worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(b.RuntimePath, "managed.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runWorktreeFoldGit(t, b.RuntimePath, "add", "managed.txt")
	runWorktreeFoldGit(t, b.RuntimePath, "commit", "-m", "managed work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	err = runWorktreeFoldWith(d, cfg, repo, filepath.Base(b.RuntimePath), binding.FoldOptions{
		Yes: true, In: tasks.NonInteractiveReader{}, ConfirmCheckoutFold: true,
	}, io.Discard)
	if err != nil {
		t.Fatalf("fold named managed worktree: %v", err)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("fold deleted managed checkout: %v", err)
	}
	if got := runWorktreeFoldGit(t, b.RuntimePath, "branch", "--show-current"); got != b.Branch {
		t.Fatalf("checkout branch = %q, want %q", got, b.Branch)
	}
}

// A live binding is pop's own bookkeeping, so the verb asks about it and folds on
// the answer instead of sending the human to `pop tasks fold` (ADR-0233). The
// picker's Fold action reaches this same verb through a tagged pane, which is where
// the question is answered.
func TestWorktreeFoldBoundCheckoutAsksThenFoldsAndKeepsBoth(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	wt := addWorktreeFoldCommit(t, repo, "bound-work")
	d := newTestCmdDeps(t, wt, filepath.Join(parent, "xdg"), filepath.Join(parent, "config"))
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Put(d.tasksDeps(), binding.Key(id, "set-bound"), binding.Adopt(d.tasksDeps(), wt, "bound-work", repo)); err != nil {
		t.Fatal(err)
	}
	answers, answered, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer answers.Close()
	if _, err := answered.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	answered.Close()

	var out bytes.Buffer
	err = runWorktreeFoldWith(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, wt, "", binding.FoldOptions{
		In: answers, ConfirmCheckoutFold: true,
	}, &out)
	if err != nil {
		t.Fatalf("fold of a bound checkout: %v\n%s", err, out.String())
	}
	for _, want := range []string{"is bound to", "set-bound"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("confirmation missing %q:\n%s", want, out.String())
		}
	}
	branchTip := runWorktreeFoldGit(t, repo, "rev-parse", "bound-work")
	if trunkTip := runWorktreeFoldGit(t, repo, "rev-parse", "HEAD"); trunkTip != branchTip {
		t.Fatalf("trunk tip = %s, branch tip = %s", trunkTip, branchTip)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("fold deleted the bound checkout: %v", err)
	}
	// The set is not finished — pop cannot even read a status for it — so the fold
	// leaves it where it lives.
	if _, _, ok, err := binding.FindBySetID(d.tasksDeps(), "set-bound"); err != nil || !ok {
		t.Fatalf("unfinished set lost its binding: ok=%v err=%v", ok, err)
	}
}

func TestWorktreeFoldNonInteractiveRequiresYes(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	wt := addWorktreeFoldCommit(t, repo, "needs-yes")
	d := newTestCmdDeps(t, wt, filepath.Join(parent, "xdg"), filepath.Join(parent, "config"))
	trunkBefore := runWorktreeFoldGit(t, repo, "rev-parse", "HEAD")

	err := runWorktreeFoldWith(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, wt, "", binding.FoldOptions{
		In: tasks.NonInteractiveReader{}, ConfirmCheckoutFold: true,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("err = %v, want --yes refusal", err)
	}
	if got := runWorktreeFoldGit(t, repo, "rev-parse", "HEAD"); got != trunkBefore {
		t.Fatalf("trunk moved without --yes: %s -> %s", trunkBefore, got)
	}
}

func TestWorktreeFoldCommandAcceptsOptionalNameAndYes(t *testing.T) {
	if worktreeFoldCmd.Use != "fold [<name>]" {
		t.Fatalf("Use = %q, want optional picker-visible name", worktreeFoldCmd.Use)
	}
	if flag := worktreeFoldCmd.Flags().Lookup("yes"); flag == nil || flag.Shorthand != "y" {
		t.Fatalf("--yes flag = %#v, want -y/--yes", flag)
	}
	if err := worktreeFoldCmd.Args(worktreeFoldCmd, []string{"one", "two"}); err == nil {
		t.Fatal("fold accepted more than one worktree name")
	}
}

func TestLaunchWorktreeFoldSpawnsTaggedPaneWithoutPickerOutput(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	wt := addWorktreeFoldCommit(t, repo, "picker-fold")
	fake := &tmuxtest.Fake{Inside: true}
	item := &ui.Item{Name: "picker-fold", Path: wt}

	if err := launchWorktreeFold(fake, item); err != nil {
		t.Fatalf("launchWorktreeFold: %v", err)
	}
	session := checkoutSessionName(wt)
	panes := fake.Windows[session][tmuxmod.DrainWindow]
	if len(panes) != 1 {
		t.Fatalf("fold panes = %v, want one", panes)
	}
	pane := panes[0]
	if got := fake.PaneTagValues[pane][tmuxmod.TagFold]; got != wt {
		t.Fatalf("@pop_fold = %q, want selected path %q", got, wt)
	}
	if got := fake.SentCommands[pane]; len(got) != 1 || got[0] != "pop worktree fold Enter" {
		t.Fatalf("sent commands = %v, want pop worktree fold", got)
	}
	if got := fake.PaneCwd[pane]; got != wt {
		t.Fatalf("pane cwd = %q, want %q", got, wt)
	}
	if got := fake.PaneTitles[pane]; got != "fold · picker-fold" {
		t.Fatalf("pane title = %q", got)
	}
}

func TestLaunchWorktreeFoldOutsideTmuxNamesHeadlessVerb(t *testing.T) {
	fake := &tmuxtest.Fake{}
	err := launchWorktreeFold(fake, &ui.Item{Name: "wt", Path: "/repo/wt"})
	if err == nil || !strings.Contains(err.Error(), "pop worktree fold") {
		t.Fatalf("error = %v, want refusal naming pop worktree fold", err)
	}
	if len(fake.Live) != 0 || len(fake.Windows) != 0 {
		t.Fatalf("outside-tmux refusal spawned state: live=%v windows=%v", fake.Live, fake.Windows)
	}
}

// countingGitDeps swaps project's package-global dependencies for ones whose
// git calls are counted, and returns the live counter plus a restore func.
// findBareRoot is short-circuited (Stat always misses) so the only way to ring
// the counter is an actual git subprocess — exactly the "heavy call" we guard.
func countingGitDeps(t *testing.T) (gitCalls *int, restore func()) {
	t.Helper()
	n := 0
	count := func(...string) (string, error) { n++; return "", nil }
	d := &project.Deps{
		Git: &deps.MockGit{
			CommandFunc:      func(args ...string) (string, error) { return count(args...) },
			CommandInDirFunc: func(dir string, args ...string) (string, error) { return count(args...) },
		},
		FS: &deps.MockFileSystem{
			StatFunc:  func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			GetwdFunc: func() (string, error) { return "/tmp", nil },
		},
	}
	return &n, project.SetDefaultDeps(d)
}

// isolatedWorktreeTestTasksDeps builds a *tasks.Deps rooted at a temp XDG data
// dir, so buildWorktreeItems' binding-store classification never touches the
// real machine's pop data (ADR-0145).
func isolatedWorktreeTestTasksDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	return newTestCmdDeps(t, "", "", "").tasksDeps()
}

func TestBuildWorktreeItems(t *testing.T) {
	t.Parallel()
	t.Run("worktree with active session gets icon", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/feature"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity, isolatedWorktreeTestTasksDeps(t))

		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		if items[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
		if items[0].Context != "feature-branch" {
			t.Errorf("Context = %q, want %q", items[0].Context, "feature-branch")
		}
	})

	t.Run("worktree without session has no icon", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity, isolatedWorktreeTestTasksDeps(t))

		if items[0].Icon != "" {
			t.Errorf("Icon = %q, want empty", items[0].Icon)
		}
	})

	t.Run("mixed session and no-session worktrees", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "active", Path: "/repo/active", Branch: "main"},
			{Name: "idle", Path: "/repo/idle", Branch: "dev"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/active"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity, isolatedWorktreeTestTasksDeps(t))

		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
		if items[0].Icon != iconDirSession {
			t.Errorf("active worktree: Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
		if items[1].Icon != "" {
			t.Errorf("idle worktree: Icon = %q, want empty", items[1].Icon)
		}
	})

	t.Run("session icon matches SessionName for path", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/feature"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity, isolatedWorktreeTestTasksDeps(t))

		if items[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
	})

	t.Run("ordinary worktree gets no marker", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, map[string]int64{}, isolatedWorktreeTestTasksDeps(t))

		if items[0].Marker != "" {
			t.Errorf("Marker = %q, want empty", items[0].Marker)
		}
	})
}

// TestBuildWorktreeItemsMarksUnboundManagedWorktree provisions a real scratch
// worktree via git, so it deliberately does not run in the package's t.Parallel()
// pool (ADR-0152): overlapping it with TestBuildWorktreeItemsTasksNoGitCalls's
// brief global project-deps swap (countingGitDeps) would race on that shared
// package-level variable.
func TestBuildWorktreeItemsMarksUnboundManagedWorktree(t *testing.T) {
	td := isolatedWorktreeTestTasksDeps(t)
	repo := t.TempDir()
	initGitRepoWithCommitCmd(t, repo)
	b, err := binding.ProvisionScratchWorktree(td, repo, "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision scratch worktree: %v", err)
	}
	worktrees := []project.Worktree{
		{Name: filepath.Base(b.RuntimePath), Path: b.RuntimePath, Branch: b.Branch},
	}

	items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, map[string]int64{}, td)

	if items[0].Marker != iconUnboundManaged {
		t.Errorf("Marker = %q, want %q", items[0].Marker, iconUnboundManaged)
	}
}

// TestBuildWorktreeItemsDistinguishesBoundManagedWorktree pins the third marker
// state: a managed worktree that still has a live Task set bound must not read
// on screen as either an unbound managed worktree or a human one. Like the
// unbound test it provisions a real worktree, so it stays out of the package's
// t.Parallel() pool.
func TestBuildWorktreeItemsDistinguishesBoundManagedWorktree(t *testing.T) {
	td := isolatedWorktreeTestTasksDeps(t)
	repo := t.TempDir()
	initGitRepoWithCommitCmd(t, repo)
	b, err := binding.ProvisionScratchWorktree(td, repo, "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision scratch worktree: %v", err)
	}
	id, err := tasks.ResolveRepositoryIdentity(td, b.RuntimePath)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	if err := binding.Put(td, binding.Key(id, "set-bound"), binding.Adopt(td, b.RuntimePath, b.Branch, repo)); err != nil {
		t.Fatalf("bind set to managed worktree: %v", err)
	}

	worktrees := []project.Worktree{
		{Name: filepath.Base(b.RuntimePath), Path: b.RuntimePath, Branch: b.Branch},
		{Name: filepath.Base(repo), Path: repo, Branch: "master"},
	}

	items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, map[string]int64{}, td)

	if items[0].Marker != iconBoundManaged {
		t.Errorf("bound managed worktree: Marker = %q, want %q", items[0].Marker, iconBoundManaged)
	}
	if items[0].Marker == iconUnboundManaged {
		t.Errorf("bound managed worktree shares the unbound glyph %q", iconUnboundManaged)
	}
	if items[1].Marker != "" {
		t.Errorf("ordinary worktree: Marker = %q, want empty", items[1].Marker)
	}
}

// historyTestDeps builds a cmd history seam whose store lives under an isolated
// data dir, seeded with the two entries the removal cases work against. It seeds
// them through the legacy-file fold, which is also the shape a real machine's
// first read after this migration has.
func historyTestDeps(t *testing.T) *history.Deps {
	t.Helper()
	dataHome := t.TempDir()
	legacy := filepath.Join(dataHome, "pop", "history.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"entries":[
		{"path":"/repo/feature","last_access":"2026-06-01T10:00:00Z"},
		{"path":"/repo/main","last_access":"2026-06-02T10:00:00Z"}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cd := newTestCmdDeps(t, "", dataHome, filepath.Join(dataHome, "config"))
	t.Cleanup(func() { _ = cd.Tasks.CloseStore() })
	return cd.historyDeps()
}

func historyEntryPaths(t *testing.T, d *history.Deps) []string {
	t.Helper()
	hist, err := history.LoadWith(d)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	out := make([]string, 0, len(hist.Entries))
	for _, e := range hist.Entries {
		out = append(out, e.Path)
	}
	return out
}

func TestRemoveFromHistoryWith(t *testing.T) {
	t.Parallel()

	t.Run("removes deleted worktree entry", func(t *testing.T) {
		d := historyTestDeps(t)

		removeFromHistoryWith(d, "/repo/feature")

		if got := historyEntryPaths(t, d); len(got) != 1 || got[0] != "/repo/main" {
			t.Errorf("entries = %v, want only /repo/main", got)
		}
	})

	t.Run("load failure leaves the entry alone", func(t *testing.T) {
		d := historyTestDeps(t)
		// Fold first, so the rows exist independently of the file.
		if got := historyEntryPaths(t, d); len(got) != 2 {
			t.Fatalf("entries before failure = %v, want both", got)
		}
		d.FS.(*deps.MockFileSystem).ReadFileFunc = func(string) ([]byte, error) { return nil, os.ErrPermission }

		removeFromHistoryWith(d, "/repo/feature")

		d.FS.(*deps.MockFileSystem).ReadFileFunc = deps.NewRealFileSystem().ReadFile
		if got := historyEntryPaths(t, d); len(got) != 2 {
			t.Errorf("entries = %v, want both untouched despite the load failure", got)
		}
	})

	t.Run("missing entry is a no-op", func(t *testing.T) {
		d := historyTestDeps(t)

		removeFromHistoryWith(d, "/repo/unknown")

		if got := historyEntryPaths(t, d); len(got) != 2 {
			t.Errorf("entries = %v, want both untouched", got)
		}
	})
}

// --- Worktree create-path Workbench shaping (ADR-0075/0076) ---

// shapeSpy records which branch of shapeWorktreeSession ran. Each closure sets
// its flag, so a test can assert exactly one path fired.
type shapeSpy struct {
	resolveCalled  bool
	promptCalled   bool
	createdTmpl    string
	createdSession string
	createdPath    string
	historyPath    string
	attached       string
	flatCalled     bool
}

// newShapeDeps builds worktreeShapeDeps whose behavior is driven by the given
// pick_on_create toggle, resolved set, and prompt result. All side effects are
// captured in the returned spy.
func newShapeDeps(pickOn bool, workbenches []config.Workbench, promptName string, promptConfirmed bool) (*worktreeShapeDeps, *shapeSpy) {
	spy := &shapeSpy{}
	d := &worktreeShapeDeps{
		LoadConfig:   func() (*config.Config, error) { return &config.Config{}, nil },
		PickOnCreate: func(cfg *config.Config) bool { return pickOn },
		ResolveWorkbenches: func(cfg *config.Config, path string) []config.Workbench {
			spy.resolveCalled = true
			return workbenches
		},
		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) {
			return "", nil
		},
		PromptWorkbench: func(order []string, wbs []config.Workbench) (string, bool, error) {
			spy.promptCalled = true
			return promptName, promptConfirmed, nil
		},
		FindWorkbench: findWorkbench,
		CreateSession: func(tmpl config.Workbench, sessionName, path string) error {
			spy.createdTmpl = tmpl.Name
			spy.createdSession = sessionName
			spy.createdPath = path
			return nil
		},
		SessionName:   func(path string) string { return "sess-" + path },
		SessionExists: func(sessionName string) bool { return false },
		RecordHistory: func(path string) { spy.historyPath = path },
		Attach:        func(sessionName string) error { spy.attached = sessionName; return nil },
		Flat: func(ctx *project.RepoContext, item *ui.Item) error {
			spy.flatCalled = true
			return nil
		},
	}
	return d, spy
}

func TestShapeWorktreeSession_PickAWorkbench(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d, spy := newShapeDeps(true, wbs, "gs-dev", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.promptCalled {
		t.Error("expected the Workbench prompt to be shown")
	}
	if spy.createdTmpl != "gs-dev" {
		t.Errorf("CreateSession tmpl = %q, want gs-dev", spy.createdTmpl)
	}
	if spy.createdSession != "sess-/repo/feature" || spy.createdPath != "/repo/feature" {
		t.Errorf("CreateSession(session=%q, path=%q), want sess-/repo/feature and /repo/feature", spy.createdSession, spy.createdPath)
	}
	if spy.historyPath != "/repo/feature" {
		t.Errorf("RecordHistory path = %q, want /repo/feature", spy.historyPath)
	}
	if spy.attached != "sess-/repo/feature" {
		t.Errorf("Attach target = %q, want sess-/repo/feature", spy.attached)
	}
	if spy.flatCalled {
		t.Error("flat session must not run when a Workbench is chosen")
	}
}

// TestShapeWorktreeSession_PreferredAutoApplies asserts a resolved preferred
// workbench (ADR-0078) auto-applies silently, building the session and attaching
// without a prompt, whether or not pick_on_create is on.
func TestShapeWorktreeSession_PreferredAutoApplies(t *testing.T) {
	t.Parallel()
	for _, pickOn := range []bool{false, true} {
		name := "pick_on_create_off"
		if pickOn {
			name = "pick_on_create_on"
		}
		t.Run(name, func(t *testing.T) {
			wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
			d, spy := newShapeDeps(pickOn, wbs, "gs-dev", true)
			d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
				return "gs-dev", nil
			}

			if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
				t.Fatalf("shapeWorktreeSession: %v", err)
			}

			if spy.promptCalled {
				t.Error("prompt must be suppressed when a preferred workbench resolves")
			}
			if spy.createdTmpl != "gs-dev" {
				t.Errorf("CreateSession tmpl = %q, want gs-dev", spy.createdTmpl)
			}
			if spy.historyPath != "/repo/feature" {
				t.Errorf("RecordHistory path = %q, want /repo/feature", spy.historyPath)
			}
			if spy.attached != "sess-/repo/feature" {
				t.Errorf("Attach target = %q, want sess-/repo/feature", spy.attached)
			}
			if spy.flatCalled {
				t.Error("flat session must not run when a preferred workbench resolves")
			}
		})
	}
}

// TestShapeWorktreeSession_StalePreferredFallsThrough asserts a stale preferred
// workbench (empty name + warning) never blocks: with pick_on_create off it
// falls through to today's flat session.
func TestShapeWorktreeSession_StalePreferredFallsThrough(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d, spy := newShapeDeps(false, wbs, "gs-dev", true)
	d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
		return "", []string{"preferred workbench \"ghost\" does not resolve; ignoring"}
	}

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.createdTmpl != "" {
		t.Errorf("CreateSession must not run for a stale preferred workbench, got %q", spy.createdTmpl)
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through for a stale preferred workbench")
	}
}

func TestShapeWorktreeSession_NoWorkbenchFallsThrough(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}}
	// The "no workbench" sentinel: confirmed choice, empty name.
	d, spy := newShapeDeps(true, wbs, "", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.promptCalled {
		t.Error("expected the Workbench prompt to be shown")
	}
	if spy.createdTmpl != "" {
		t.Errorf("CreateSession must not run for the no-workbench sentinel, got %q", spy.createdTmpl)
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through for the no-workbench choice")
	}
}

func TestShapeWorktreeSession_EscFallsThrough(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}}
	// Esc: not confirmed. The worktree already exists, so fall through to flat.
	d, spy := newShapeDeps(true, wbs, "", false)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.createdTmpl != "" {
		t.Error("CreateSession must not run when the prompt is cancelled")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through when the prompt is cancelled")
	}
}

func TestShapeWorktreeSession_ToggleOffSkipsPrompt(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d, spy := newShapeDeps(false, wbs, "gs-dev", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.resolveCalled {
		t.Error("ResolveWorkbenches must not be consulted when pick_on_create is off")
	}
	if spy.promptCalled {
		t.Error("Workbench prompt must not be shown when pick_on_create is off")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session when pick_on_create is off")
	}
}

func TestShapeWorktreeSession_EmptySetSkipsPrompt(t *testing.T) {
	t.Parallel()
	d, spy := newShapeDeps(true, nil, "", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.resolveCalled {
		t.Error("expected ResolveWorkbenches to be consulted when pick_on_create is on")
	}
	if spy.promptCalled {
		t.Error("prompt must be skipped when no Workbenches resolve")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session when the resolved Workbench set is empty")
	}
}

// TestOpenWorktreeWithShaping_SessionAbsentShapes asserts the select-path gate:
// when the target session does not exist, openWorktreeWithShaping runs the same
// birth-time shaping the create flow uses (prompt + build + attach), not a flat
// attach.
func TestOpenWorktreeWithShaping_SessionAbsentShapes(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d, spy := newShapeDeps(true, wbs, "gs-dev", true)
	d.SessionExists = func(sessionName string) bool { return false }

	if err := openWorktreeWithShaping(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("openWorktreeWithShaping: %v", err)
	}

	if !spy.promptCalled {
		t.Error("expected the Workbench prompt when the session is absent")
	}
	if spy.createdTmpl != "gs-dev" {
		t.Errorf("CreateSession tmpl = %q, want gs-dev", spy.createdTmpl)
	}
	if spy.attached != "sess-/repo/feature" {
		t.Errorf("Attach target = %q, want sess-/repo/feature", spy.attached)
	}
	if spy.flatCalled {
		t.Error("flat attach must not run when the session is absent and a Workbench is chosen")
	}
}

// TestOpenWorktreeWithShaping_SessionPresentAttachesFlat asserts ADR-0075: a
// worktree whose session already exists attaches flat with no reshaping — the
// config/prompt/build seams are never touched.
func TestOpenWorktreeWithShaping_SessionPresentAttachesFlat(t *testing.T) {
	t.Parallel()
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d, spy := newShapeDeps(true, wbs, "gs-dev", true)
	d.SessionExists = func(sessionName string) bool { return true }

	if err := openWorktreeWithShaping(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("openWorktreeWithShaping: %v", err)
	}

	if !spy.flatCalled {
		t.Error("expected the flat attach when the session already exists")
	}
	if spy.resolveCalled || spy.promptCalled {
		t.Error("an existing session must not be reshaped (no resolve/prompt)")
	}
	if spy.createdTmpl != "" {
		t.Errorf("CreateSession must not run for an existing session, got %q", spy.createdTmpl)
	}
	if spy.attached != "" {
		t.Errorf("shaping Attach must not run for an existing session, got %q", spy.attached)
	}
}

// TestTrunkBranchCursorIndexPreselectsTrunkBranch pins the managed-create
// base-ref picker's preselection (ADR-0152): the cursor index lands on the
// Trunk worktree's own branch — even when that branch is not the main/master
// default — so Enter accepts it with no further input, while the main/master
// default stays at the bottom for the cursor-at-end fallback.
func TestTrunkBranchCursorIndexPreselectsTrunkBranch(t *testing.T) {
	t.Parallel()
	root, _, td := setupCmdRepoTest(t)
	cd := cmdLayerDeps().configDeps()

	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	defaultBranch := git("branch", "--show-current")
	git("checkout", "-b", "feature-work")

	ctx := &project.RepoContext{GitRoot: root, RepoName: filepath.Base(root)}
	branches, err := project.ListBranches(ctx)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	items, _ := baseRefPickerItems(branches)

	want := -1
	for i, it := range items {
		if it.Name == "feature-work" {
			want = i
		}
	}
	if want < 0 {
		t.Fatalf("feature-work not among base-ref items: %+v", items)
	}
	if got := trunkBranchCursorIndex(td, cd, nil, root, items); got != want {
		t.Errorf("trunkBranchCursorIndex = %d, want %d (the trunk's own branch, feature-work)", got, want)
	}
	if got := items[len(items)-1].Name; got != defaultBranch {
		t.Errorf("bottom item = %q, want the %q default under the cursor-at-end fallback", got, defaultBranch)
	}
}

// TestBaseRefPickerItemsPutsMainFirstBranchesAtBottom pins the reversal both
// create flows share: ListBranches orders main/master first, and the picker's
// bottom-anchored cursor must find them on the bottom row.
func TestBaseRefPickerItemsPutsMainFirstBranchesAtBottom(t *testing.T) {
	t.Parallel()
	branches := []project.Branch{{Ref: "main"}, {Ref: "zebra"}, {Ref: "origin/ahead", IsRemote: true}}
	items, byRef := baseRefPickerItems(branches)
	wantOrder := []string{"origin/ahead", "zebra", "main"}
	if len(items) != len(wantOrder) {
		t.Fatalf("items = %d, want %d", len(items), len(wantOrder))
	}
	for i, ref := range wantOrder {
		if items[i].Name != ref || items[i].Path != ref {
			t.Errorf("items[%d] = %+v, want ref %q", i, items[i], ref)
		}
	}
	if b, ok := byRef["zebra"]; !ok || b.Ref != "zebra" {
		t.Errorf("byRef lookup lost the zebra branch: %+v", byRef)
	}
}

func TestWorktreeHelpHasNoPhantomCreateBinding(t *testing.T) {
	t.Parallel()
	// ctrl-n is cursor-down in the picker; a create binding never shipped.
	// Guard against a help line assigning create to ctrl-n while allowing its
	// real navigation binding.
	for _, line := range strings.Split(worktreeDashboardCmd.Long, "\n") {
		if strings.Contains(line, "ctrl-n") && strings.Contains(strings.ToLower(line), "create") {
			t.Errorf("worktree dashboard help advertises a false ctrl-n create binding: %q", line)
		}
	}
}

func TestWorktreeHelpAndReadmeListCurrentBindings(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []string{
		"ctrl-p", "ctrl-n", "ctrl-b", "ctrl-f", "ctrl-u", "alt-backspace",
		"ctrl-h", "enter", "ctrl-a", "ctrl-t", "ctrl-l", "ctrl-k", "ctrl-r",
		"ctrl-y", "ctrl-d", "ctrl-x", "alt-c", "alt-1..9", "esc", "ctrl-c",
	} {
		if !strings.Contains(worktreeDashboardCmd.Long, binding) {
			t.Errorf("worktree dashboard long help omits %s", binding)
		}
		if !strings.Contains(string(readme), "`"+binding+"`") && binding != "alt-1..9" {
			t.Errorf("README worktree keybindings omit %s", binding)
		}
	}
	if !strings.Contains(string(readme), "`alt-1..9`") {
		t.Error("README worktree keybindings omit alt-1..9")
	}
}

// TestBuildWorktreeItemsTasksNoGitCalls guards against reintroducing the
// per-worktree git-call storm (commit 59d4af8, fixed in 417eaeb). Session
// names must be derived from the already-known RepoContext, not by calling
// project.SessionName(path) — which spawns 2-3 git subprocesses per worktree —
// inside the build loop. Building items for many worktrees must cost zero git
// calls regardless of count.
//
// It cannot run in the package's t.Parallel() pool: countingGitDeps swaps the
// process-global project deps, so any other parallel test running git during
// the swap window is counted here and fails this one spuriously.
func TestBuildWorktreeItemsTasksNoGitCalls(t *testing.T) {
	for _, ctx := range []*project.RepoContext{
		{IsBare: true, RepoName: "myrepo"},
		{IsBare: false},
	} {
		worktrees := make([]project.Worktree, 20)
		for i := range worktrees {
			name := fmt.Sprintf("wt-%d", i)
			worktrees[i] = project.Worktree{Name: name, Path: "/repo/" + name, Branch: name}
		}

		gitCalls, restore := countingGitDeps(t)
		buildWorktreeItems(ctx, worktrees, map[string]int64{}, isolatedWorktreeTestTasksDeps(t))
		restore()

		if *gitCalls != 0 {
			t.Errorf("IsBare=%v: buildWorktreeItems taskd %d git calls for %d worktrees, want 0 (per-item git derivation regressed)", ctx.IsBare, *gitCalls, len(worktrees))
		}
	}
}
