package drain

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// countingDefinitionFS counts the reads a run-view build makes under one
// definition path. Only that subtree is counted, because it is the whole subject:
// the Active-worktrees section is the only part of the view that touches a set's
// manifest or task markdown, so a read under here is a read the section caused.
type countingDefinitionFS struct {
	deps.FileSystem
	defRoot string
	total   int
	reads   map[string]int
}

func (fs *countingDefinitionFS) ReadFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, fs.defRoot) {
		fs.total++
		fs.reads[path]++
	}
	return fs.FileSystem.ReadFile(path)
}

// countingDefinitionDeps points td's filesystem at a counter over the checkout's
// canonical definition path — canonical because that is the path the refresh
// reads through, symlinks already resolved, and a prefix that did not match it
// would count nothing and prove nothing.
func countingDefinitionDeps(t *testing.T, td *tasks.Deps, tasksDir string) (*countingDefinitionFS, string) {
	t.Helper()
	defRoot, err := tasks.CanonicalDefinitionPathWith(td, tasksDir)
	if err != nil {
		t.Fatalf("CanonicalDefinitionPathWith: %v", err)
	}
	counting := &countingDefinitionFS{FileSystem: td.FS, defRoot: defRoot, reads: map[string]int{}}
	td.FS = counting
	return counting, defRoot
}

// bindSetToCheckout provisions one Worktree binding for setID in the checkout at
// repo, which is what puts the set in the Active-worktrees section.
func bindSetToCheckout(t *testing.T, td *tasks.Deps, id *tasks.RepositoryIdentity, repo, setID string) {
	t.Helper()
	err := binding.Put(td, SetScopedKey(repoIdentityKey(id), setID), WorktreeBinding{
		RuntimePath: repo, Project: "pop", Branch: setID + "-branch", Provisioned: true,
	})
	if err != nil {
		t.Fatalf("binding.Put(%s): %v", setID, err)
	}
}

// TestRunViewDefersWorktreeBindingsUntilRead pins the ADR-0189 laziness with
// counted file reads: a caller that builds the run view and renders only the
// Summary headline — which is `pop work status` and every tick of the daemon's
// view diff — reads not one file of any bound checkout's definition path. The
// second half proves the fixture can tell laziness apart from an empty section:
// asking for the section really does read those files.
func TestRunViewDefersWorktreeBindingsUntilRead(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "done-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	bindSetToCheckout(t, td, id, repo, setID)
	counting, _ := countingDefinitionDeps(t, td, id.TasksDir)

	view := BuildRunView(StatusSnapshot{Tasks: td}, time.Now().UTC())
	RenderRunSummary(io.Discard, view)
	if counting.total != 0 {
		t.Fatalf("build + summary read %d definition files, want 0 (the section is not computed unless read): %v", counting.total, counting.reads)
	}

	if got := view.WorktreeBindings(); len(got) != 0 {
		t.Fatalf("Active worktrees = %+v, want empty (the DONE binding stays hidden)", got)
	}
	if counting.total == 0 {
		t.Fatal("reading the section read no definition file, so this fixture cannot tell deferral from a section with nothing to do")
	}
}

// TestWorktreeBindingsRefreshOncePerCheckout pins the second half of the ADR-0189
// fix: the refresh behind the DONE filter is per checkout, so three sets bound to
// one checkout scan its definition path once between them rather than once each.
// The section they render is the same either way.
func TestWorktreeBindingsRefreshOncePerCheckout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo, firstSet, _ := queuetest.SetupSpawnRepo(t, "set-a", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	td := tasks.DefaultDeps()
	t.Cleanup(func() { _ = td.CloseStore() })

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	sets := []string{firstSet, "set-b", "set-c"}
	for _, stem := range sets[1:] {
		setDir := filepath.Join(id.TasksDir, stem)
		queuetest.WriteSpawnTaskMD(t, setDir, "01-a.md")
		queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
	}
	if _, err := tasks.RegisterWith(td, id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatalf("RegisterWith: %v", err)
	}
	for _, stem := range sets {
		bindSetToCheckout(t, td, id, repo, stem)
	}
	counting, defRoot := countingDefinitionDeps(t, td, id.TasksDir)

	view := BuildRunView(StatusSnapshot{Tasks: td}, time.Now().UTC())
	items := view.WorktreeBindings()
	if len(items) != len(sets) {
		t.Fatalf("Active worktrees = %+v, want all %d bindings of the checkout", items, len(sets))
	}
	for _, stem := range sets {
		manifest := filepath.Join(defRoot, stem, "index.json")
		if got := counting.reads[manifest]; got != 1 {
			t.Fatalf("manifest of %s read %d times, want 1 (one refresh per checkout, not one per binding): %v", stem, got, counting.reads)
		}
	}

	// The rendered section is unchanged by the memo, and the baseline pays nothing
	// twice: the section resolved above is the one it prints.
	readsBeforeRender := counting.total
	var out strings.Builder
	RenderRunBaseline(&out, view)
	if counting.total != readsBeforeRender {
		t.Fatalf("baseline render made %d further definition reads, want 0 (the section memoizes)", counting.total-readsBeforeRender)
	}
	checkout := filepath.Base(repo)
	for _, stem := range sets {
		want := checkout + " (in " + checkout + "): " + stem + " branch=" + stem + "-branch at " + repo + " — bound"
		if !strings.Contains(out.String(), want) {
			t.Fatalf("baseline missing Active-worktrees line %q:\n%s", want, out.String())
		}
	}
}
