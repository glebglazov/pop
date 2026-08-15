package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// firstPaintForkCeiling is the number of git processes one page-A snapshot build
// is allowed to start on a machine whose registered sets are all terminal and all
// hidden by the row filter — the ordinary `pop work dashboard` open. It is a
// ceiling on the paint, not on the machine: it must not grow with the number of
// registered sets, which is what the fixture below varies.
//
// Two is the budget the read path is designed to fit in (ADR-0189): the verify
// overlay's repo-identity and work-SHA pair, both memoized per checkout, and
// nothing per row. A hidden terminal row is never asked for its verdict, so a
// dashboard of hidden rows should sit under even that.
const firstPaintForkCeiling = 2

// countingGit counts the git processes a load would start, delegating each to a
// real git so the load still reads a real repository. It is the guard ADR-0189
// asks for in place of a wall-clock assertion: the fork count is the fact that
// rotted, and it is the one a test can hold still.
type countingGit struct {
	inner deps.Git
	mu    sync.Mutex
	calls []string
}

func (g *countingGit) record(args []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, strings.Join(args, " "))
}

func (g *countingGit) Command(args ...string) (string, error) {
	g.record(args)
	return g.inner.Command(args...)
}

func (g *countingGit) CommandInDir(dir string, args ...string) (string, error) {
	g.record(args)
	return g.inner.CommandInDir(dir, args...)
}

func (g *countingGit) forks() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.calls...)
}

// TestFirstPaintForksUnderCeiling pins the first paint's git budget. The fixture
// is the authoring machine in miniature: six registered sets, all DONE, each
// bound to a checkout of its own — the shape that made the overlay's two forks
// per checkout into twelve. With a preset that hides every DONE row the paint
// renders no terminal row, so it must resolve no verdict and start no git
// process at all. (The shipped active preset would keep unfolded DONE visible;
// this test isolates the narrowing budget, not the default roster.)
func TestFirstPaintForksUnderCeiling(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "2026-01-01-done-1", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, _, _ := dashboardLaunchFixture(t, repo, setID)
	// Verification on is the expensive wiring: with it off the overlay is a no-op
	// and the ceiling would hold for the wrong reason.
	cfg.Work = &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}
	stems := registerDoneSets(t, repo, 6)
	bindSetsToOwnCheckouts(t, d, repo, stems)
	d.ViewPreset = config.WorkViewPreset{
		Name: "_hide-done",
		Hide: &config.WorkViewPresetFilter{Status: []string{"done"}},
	}

	counter := &countingGit{inner: d.Tasks.Git}
	td := *d.Tasks
	td.Git = counter
	d.Tasks = &td
	pd := *d.Project
	pd.Git = counter
	d.Project = &pd

	snap, err := BuildPageSnapshot(d, cfg, PageWork, work.PaneFacts{})
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	forks := counter.forks()
	if len(forks) > firstPaintForkCeiling {
		t.Fatalf("first paint started %d git processes, ceiling is %d: %v", len(forks), firstPaintForkCeiling, forks)
	}
	for _, row := range snap.Containers {
		if row.RawStatus == tasks.StatusDone {
			t.Fatalf("DONE row %s rendered with the filter hiding done sets: %+v", row.ID, row)
		}
	}

	// The ceiling means nothing unless the machine really carries the terminal rows
	// the paint declined to resolve, and unless resolving them really costs. With the
	// filter revealing them the same build lists all six — and pays per checkout for
	// it, which is exactly the bill the narrowed paint does not receive.
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	revealed, err := BuildPageSnapshot(d, cfg, PageWork, work.PaneFacts{})
	if err != nil {
		t.Fatalf("BuildPageSnapshot(include done): %v", err)
	}
	terminal := 0
	for _, row := range revealed.Containers {
		if tasks.TerminalStatus(row.RawStatus) || row.RawStatus == tasks.StatusNeedsVerify {
			terminal++
		}
	}
	if terminal != 6 {
		t.Fatalf("revealed terminal rows = %d, want 6: %+v", terminal, revealed.Containers)
	}
	if revealedForks := len(counter.forks()) - len(forks); revealedForks <= firstPaintForkCeiling {
		t.Fatalf("revealing six bound terminal rows forked %d gits, want more than the %d-fork ceiling — the fixture is not exercising the overlay",
			revealedForks, firstPaintForkCeiling)
	}
}

// registerDoneSets seeds n registered task sets in repo, each complete and so each
// DONE (the first is the fixture's own), and returns their stems.
func registerDoneSets(t *testing.T, repo string, n int) []string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	stems := []string{"2026-01-01-done-1"}
	for i := 2; i <= n; i++ {
		stem := fmt.Sprintf("2026-01-%02d-done-%d", i, i)
		setDir := filepath.Join(id.TasksDir, stem)
		queuetest.WriteSpawnTaskMD(t, setDir, "01-a.md")
		queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		})
		stems = append(stems, stem)
	}
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	return stems
}

// bindSetsToOwnCheckouts gives every set a Worktree binding to a real checkout of
// its own, so the verify overlay has as many distinct checkouts to resolve as
// there are sets and its per-checkout memo cannot hide the per-row cost.
func bindSetsToOwnCheckouts(t *testing.T, d *drain.Deps, repo string, stems []string) {
	t.Helper()
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, stem := range stems {
		wt := filepath.Join(root, stem)
		branch := "wt/" + stem
		runGit(t, repo, "worktree", "add", "-b", branch, wt)
		if err := binding.Put(d.Tasks, drain.SetScopedKey(repoKey, stem), binding.Binding{
			RuntimePath: wt, Branch: branch,
		}); err != nil {
			t.Fatal(err)
		}
	}
}
