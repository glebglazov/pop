package drain

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// countingGit is a real git seam that records how many subprocesses each
// (directory, question) pair costs.
type countingGit struct {
	inner deps.Git
	mu    sync.Mutex
	calls map[string]int
}

func newCountingGit() *countingGit {
	return &countingGit{inner: deps.NewRealGit(), calls: map[string]int{}}
}

func (g *countingGit) Command(args ...string) (string, error) {
	g.record("", args)
	return g.inner.Command(args...)
}

func (g *countingGit) CommandInDir(dir string, args ...string) (string, error) {
	g.record(dir, args)
	return g.inner.CommandInDir(dir, args...)
}

func (g *countingGit) record(dir string, args []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls[filepath.Clean(dir)+" :: git "+strings.Join(args, " ")]++
}

// repeats returns the pairs asked more than once, with their counts.
func (g *countingGit) repeats() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]int{}
	for k, n := range g.calls {
		if n > 1 {
			out[k] = n
		}
	}
	return out
}

func (g *countingGit) total() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		n += c
	}
	return n
}

func (g *countingGit) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = map[string]int{}
}

func (g *countingGit) sortedPairs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	keys := make([]string, 0, len(g.calls))
	for k := range g.calls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// gitMemoFixture wires a real Work load — the Task-set and Map kinds over a real
// repository with a registered set — through one counting git seam, shared by the
// task and project dependencies as production shares its real git.
func gitMemoFixture(t *testing.T) (*Deps, *config.Config, *countingGit, string) {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "memo-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	git := newCountingGit()
	td := queuetest.TasksDeps(t, true)
	td.Git = git
	d := &Deps{
		Tasks:   td,
		Project: &project.Deps{Git: git, FS: deps.NewRealFileSystem()},
		Tmux:    queuetest.NewRecordingTmux(false, "0"),
	}
	bindSetInPlace(t, d, repo, setID)
	git.reset()
	return d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, git, repo
}

// repeatingKind is a Work kind that asks git the same two questions its Load
// needs several times over, as the real adapters do — each derivation resolving
// the checkout's common dir for itself.
type repeatingKind struct {
	work.Kind
	git  deps.Git
	dir  string
	asks int
}

func (k *repeatingKind) ID() work.KindID { return ref.KindTaskSet }

func (k *repeatingKind) Load() ([]work.Container, error) {
	for i := 0; i < k.asks; i++ {
		if _, err := k.git.CommandInDir(k.dir, "rev-parse", "--git-common-dir"); err != nil {
			return nil, err
		}
		if _, err := k.git.CommandInDir(k.dir, "rev-parse", "HEAD"); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (k *repeatingKind) Less(work.Container, work.Container) bool { return false }
func (k *repeatingKind) Summary([]work.Container) []string        { return nil }

// TestWorkLoadForksGitOncePerQuestion pins the memo's contract at the wiring
// seam: whatever kinds a load lists read through a git seam scoped to that load,
// so the common dir a dozen derivations each need costs one fork. The wiring
// list is handed the load's deps rather than capturing its own — the CLI's list
// (cmd/deps.go) is installed exactly this way, which is why it benefits too.
func TestWorkLoadForksGitOncePerQuestion(t *testing.T) {
	d, cfg, git, repo := gitMemoFixture(t)
	const asks = 12
	var wired *repeatingKind
	d.Kinds = func(load *Deps, _ *config.Config) []work.Kind {
		wired = &repeatingKind{git: load.Tasks.Git, dir: repo, asks: asks}
		return []work.Kind{wired}
	}

	if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if wired == nil {
		t.Fatal("wiring list was never asked for its kinds")
	}
	if repeats := git.repeats(); len(repeats) > 0 {
		t.Fatalf("load repeated git questions: %v", repeats)
	}
	if got := git.total(); got != 2 {
		t.Fatalf("load forked git %d times for %d asks of 2 questions, want 2", got, asks)
	}
}

// TestRoutinePageLoadForksGitOncePerQuestion pins the same contract on the
// Routine page's wiring list: the memo sits at the seam every read surface wires
// through, not in one page.
func TestRoutinePageLoadForksGitOncePerQuestion(t *testing.T) {
	d, cfg, git, repo := gitMemoFixture(t)
	d.RoutineKinds = func(load *Deps, _ *config.Config) []work.Kind {
		return []work.Kind{&repeatingKind{git: load.Tasks.Git, dir: repo, asks: 5}}
	}

	if _, err := work.BuildSnapshot(d.RoutinePageKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if got := git.total(); got != 2 {
		t.Fatalf("routine page load forked git %d times, want 2", got)
	}
}

// TestWorkLoadMemoLastsOneLoad pins the memo's lifetime: the second load re-asks
// every question the first one cached, so a dashboard poll two seconds after a
// commit reads the moved HEAD rather than replaying the old one.
func TestWorkLoadMemoLastsOneLoad(t *testing.T) {
	d, cfg, git, repo := gitMemoFixture(t)
	d.Kinds = func(load *Deps, _ *config.Config) []work.Kind {
		return []work.Kind{&repeatingKind{git: load.Tasks.Git, dir: repo, asks: 3}}
	}

	head := func() string {
		t.Helper()
		if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
			t.Fatalf("BuildSnapshot: %v", err)
		}
		out, err := deps.NewRealGit().CommandInDir(repo, "rev-parse", "HEAD")
		if err != nil {
			t.Fatalf("rev-parse HEAD: %v", err)
		}
		return out
	}

	before := head()
	firstLoad := git.total()

	writeFile(t, filepath.Join(repo, "second.txt"), "more\n")
	runGit(t, repo, "add", "second.txt")
	runGit(t, repo, "commit", "-m", "second")

	git.reset()
	after := head()
	if after == before {
		t.Fatal("fixture did not move HEAD")
	}
	if got := git.total(); got != firstLoad {
		t.Fatalf("second load forked %d, first forked %d: the memo outlived its load", got, firstLoad)
	}
	// The second load asked its own questions, so what it reports is the new HEAD.
	if out, err := d.WithGitMemo().Tasks.Git.CommandInDir(repo, "rev-parse", "HEAD"); err != nil || out != after {
		t.Fatalf("fresh memo read HEAD %q (err %v), want %q", out, err, after)
	}
}

// TestStatusBuildSharesOneMemoWithItsRunView pins the other half of the load:
// `pop work status` derives its run view off the snapshot the status build
// returns, so the deps the snapshot carries must be the ones that already paid
// for the load's forks.
func TestStatusBuildSharesOneMemoWithItsRunView(t *testing.T) {
	d, cfg, git, repo := gitMemoFixture(t)

	snap, err := BuildStatus(d, cfg)
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if len(git.repeats()) > 0 {
		t.Fatalf("status build repeated git questions: %v", git.repeats())
	}
	forks := git.total()
	if forks == 0 {
		t.Fatal("status build forked no git: the fixture no longer reads through the seam")
	}

	// What the run view derivation asks of the snapshot's deps, the scan already
	// resolved. It asks about the canonical checkout, as every derivation below
	// the scan does.
	canon, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if _, err := snap.Tasks.Git.CommandInDir(canon, "rev-parse", "--git-common-dir"); err != nil {
		t.Fatalf("rev-parse through the snapshot deps: %v", err)
	}
	if got := git.total(); got != forks {
		t.Fatalf("run-view read cost %d extra forks, want 0", got-forks)
	}
}

// TestSupervisorWiringIsNotMemoized pins the deliberate exception: the
// supervisor creates worktrees and moves branches between asking a kind for
// candidates and dispatching it, so its wiring list reads through the caller's
// own git seam and sees each write.
func TestSupervisorWiringIsNotMemoized(t *testing.T) {
	d, cfg, git, repo := gitMemoFixture(t)
	d.Kinds = func(load *Deps, _ *config.Config) []work.Kind {
		return []work.Kind{&repeatingKind{git: load.Tasks.Git, dir: repo, asks: 4}}
	}

	if _, err := work.BuildSnapshot(d.AdvanceKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if got := git.total(); got != 8 {
		t.Fatalf("supervisor wiring forked %d times, want 8 (no memo across a tick's writes)", got)
	}
}
