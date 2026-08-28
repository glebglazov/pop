package dashboard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// The ladder's weakest pass, keyed on the repository rather than on a checkout
// (ADR-0241). Every test here drives the whole path — a fake tmux pane standing in
// a real worktree of a real repository, through a real launch into a model —
// because the pass's whole claim is that two identities agree: the git common
// directory the launch resolves from the pane's directory, and the one the
// repository group holding the sets is keyed under.

// repositoryFixture is the reported bug's shape: three registered sets in one
// repository, two real worktrees of it, and an untagged editor shell. The sets
// named by index are bound to the *trunk* worktree and nothing is ever bound to the
// sibling, which is exactly the case the checkout rung is right to refuse.
type repositoryFixture struct {
	d     *drain.Deps
	cfg   *config.Config
	rt    *queuetest.RecordingTmux
	stems []string
	// trunk is the checkout the bindings name; sibling is the second worktree no
	// binding mentions. Both share the repository's git common directory.
	trunk, sibling string
	// repo is the repository itself, which is what a Map has to be filed under to
	// answer the same pass the sets do.
	repo string
}

func newRepositoryFixture(t *testing.T, bind ...int) repositoryFixture {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "2026-01-01-done-1", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, _, rt := dashboardLaunchFixture(t, repo, setID)
	stems := registerDoneSets(t, repo, 3)
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	d.ViewPreset.Lift = true

	root := t.TempDir()
	trunk := filepath.Join(root, "trunk-wt")
	runGit(t, repo, "worktree", "add", "-b", "wt/trunk", trunk)
	sibling := filepath.Join(root, "sibling-wt")
	runGit(t, repo, "worktree", "add", "-b", "wt/sibling", sibling)

	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range bind {
		if err := binding.Put(d.Tasks, drain.SetScopedKey(repoKey, stems[i]), binding.Binding{
			RuntimePath: trunk, Branch: "wt/trunk",
		}); err != nil {
			t.Fatal(err)
		}
	}
	inPane(rt.Fake, "editor", "%7")
	if rt.Fake.PaneCwd == nil {
		rt.Fake.PaneCwd = map[string]string{}
	}
	return repositoryFixture{d: d, cfg: cfg, rt: rt, stems: stems, trunk: trunk, sibling: sibling, repo: repo}
}

// standIn puts the shell in a directory and returns the model a launch from it
// would take its first paint with.
func (f repositoryFixture) standIn(t *testing.T, dir string) QueueDashboard {
	t.Helper()
	f.rt.Fake.PaneCwd["%7"] = dir
	return openFromPane(t, f.d, f.cfg)
}

// repoOrder is the fixture's sets in the order the page already puts them in,
// which is the order the repository pass hands them back.
func (f repositoryFixture) repoOrder(t *testing.T) []string {
	t.Helper()
	return inBaselineOrder(unliftedOrder(t, f.d, f.cfg), f.stems)
}

// withMap files an active Map in the repository's own Task storage — where every
// Map of a repository lives, and the only locality a Trunk-rooted Map has.
func (f repositoryFixture) withMap(t *testing.T, mapID string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(f.d.Tasks, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(id.StorageDir, "maps", mapID)
	if err := os.MkdirAll(filepath.Join(dir, "issues"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("map.md", "Status: active\n\n## Destination\nChart it\n")
	write(filepath.Join("issues", "01-frontier.md"), "Type: research\nStatus: open\n\n# Q\n")
	return mapID
}

// Decision 2, which is the whole reason this pass merges: a repository holds a Task
// set and a Map, the shell is standing in neither's checkout, and both lift from
// the one pane — the sets ahead of the Maps, in kind precedence order. A first-hit
// pass would have answered with the sets and silently decided this repository has
// no Maps, which is nearly every repository pop knows.
func TestARepositoryLiftsItsTaskSetsAndItsMapsTogether(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	mapID := f.withMap(t, "2026-02-02-chart")
	want := append(f.repoOrder(t), mapID)

	m := f.standIn(t, f.sibling)

	if got := liftedBlock(t, m); !slices.Equal(got, want) {
		t.Fatalf("lifted %v, want the repository's sets ahead of its Map %v", got, want)
	}
	if !slices.Contains(rowIDs(m), mapID) {
		t.Fatalf("rows = %v, want the Map among them — this test needs a rendered Map row", rowIDs(m))
	}
}

// A Map alone in a repository lifts for a shell standing there: before this pass a
// Map answered only inside its own `pop-map-<id>` session, so an editor shell in the
// repository had the blind spot for a live Map that it had for an unbound set.
func TestShellInARepositoryLiftsItsMapWithNoSetInvolved(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	mapID := f.withMap(t, "2026-02-02-chart")
	f.d.ViewPreset = config.WorkViewPreset{
		Name:  "_maps-only",
		Label: "maps",
		Lift:  true,
		Hide:  &config.WorkViewPresetFilter{Status: []string{"done"}},
	}

	m := f.standIn(t, f.sibling)

	if got := liftedBlock(t, m); !slices.Equal(got, []string{mapID}) {
		t.Fatalf("lifted %v, want the repository's Map %v", got, []string{mapID})
	}
}

// The bug this pass exists for. A set is bound to one worktree of the repository
// and the shell is standing in a *sibling* worktree, which is inside no binding at
// all — so the checkout rung is silent and, before this pass, the dashboard opened
// with nothing lifted. The repository is the same repository either way, so the
// work lifts.
func TestShellInASiblingWorktreeLiftsTheRepositorysSets(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	want := f.repoOrder(t)

	m := f.standIn(t, f.sibling)

	if got := liftedBlock(t, m); !slices.Equal(got, want) {
		t.Fatalf("lifted %v from the sibling worktree, want the repository's sets %v", got, want)
	}
	if got := attributedSets(t, m); !slices.Contains(got, f.stems[1]) {
		t.Fatalf("attributed %v, want the set bound to the sibling's own trunk (%q) among them", got, f.stems[1])
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence: the lift says it", m.flash.Text())
	}
	if m.ListCursor() != 0 {
		t.Fatalf("cursor = %d, want the untouched first row: the pass moves rows, not the cursor", m.ListCursor())
	}
}

// Reached only when the passes above it are silent (decision 1). The shell is
// standing in the checkout the bindings name, so the checkout rung answers and the
// repository pass is never consulted: the answer is the bound sets alone, in that
// rung's own leading order, and the third set of the repository stays where the
// sort had it.
func TestPaneInABoundCheckoutIsAnsweredByTheCheckoutRungAlone(t *testing.T) {
	f := newRepositoryFixture(t, 1, 2)
	baseline := unliftedOrder(t, f.d, f.cfg)
	want := inBaselineOrder(baseline, []string{f.stems[1], f.stems[2]})

	m := f.standIn(t, f.trunk)

	if got := liftedBlock(t, m); !slices.Equal(got, want) {
		t.Fatalf("lifted %v, want the sets bound to this checkout alone %v — the repository pass must not widen a checkout answer", got, want)
	}
	if got := attributedSets(t, m); slices.Contains(got, f.stems[0]) {
		t.Fatalf("attributed %v, want the unbound set %q left out", got, f.stems[0])
	}
	if got, want := rowIDs(m), wantLiftedFirst(baseline, want...); !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// A pane pop opened for a set gets that set and nothing else, whatever the
// repository holds and wherever the pane stands: the tag means *this pane is that
// work*, which is one container, and the strongest rung is unchanged.
func TestTaggedPaneKeepsItsAnswerWhateverTheRepositoryHolds(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	tagged := f.stems[0]
	tagPane(f.rt.Fake, "%7", tmuxmod.TagSet, tagged)

	for _, dir := range []struct {
		name string
		path string
	}{
		{"a sibling worktree", f.sibling},
		{"the bound checkout", f.trunk},
	} {
		t.Run(dir.name, func(t *testing.T) {
			m := f.standIn(t, dir.path)

			if got := liftedBlock(t, m); !slices.Equal(got, []string{tagged}) {
				t.Fatalf("lifted %v, want only the tagged set %q", got, tagged)
			}
		})
	}
}

// Decision 3: the pass answers with every set in the repository's Task storage,
// bound or not. A set with no Worktree binding at all is unreachable by locality
// from anywhere — including its own trunk — and this is the pass that reaches it.
func TestARepositorysSetsLiftWhetherOrNotTheyAreBound(t *testing.T) {
	t.Run("no set in the repository is bound to anything", func(t *testing.T) {
		f := newRepositoryFixture(t)
		want := f.repoOrder(t)

		m := f.standIn(t, f.sibling)

		if got := liftedBlock(t, m); !slices.Equal(got, want) {
			t.Fatalf("lifted %v, want every set the repository holds %v", got, want)
		}
	})

	t.Run("one set is bound and the rest are not", func(t *testing.T) {
		f := newRepositoryFixture(t, 1)
		want := f.repoOrder(t)

		m := f.standIn(t, f.sibling)

		if got := liftedBlock(t, m); !slices.Equal(got, want) {
			t.Fatalf("lifted %v, want the bound set and the unbound ones alike %v", got, want)
		}
	})
}

// Nothing to answer for, so nothing is said. A shell outside any repository and a
// shell in a repository pop knows no work for are the same silence, and it is the
// silence the dashboard has always had for an unrelated shell.
func TestPaneWithNoRepositoryWorkLiftsNothingAndReportsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{"outside any repository", func(t *testing.T) string { return t.TempDir() }},
		{"a repository pop knows no work for", initGitRepoWithBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepositoryFixture(t, 1)
			baseline := unliftedOrder(t, f.d, f.cfg)

			m := f.standIn(t, tc.dir(t))

			if m.snap.Attribution != nil {
				t.Fatalf("attribution = %+v, want none", *m.snap.Attribution)
			}
			if got := liftedBlock(t, m); len(got) != 0 {
				t.Fatalf("lifted %v, want nothing", got)
			}
			if got := rowIDs(m); !slices.Equal(got, baseline) {
				t.Fatalf("rows = %v, want the untouched order %v", got, baseline)
			}
			if m.flash.Text() != "" {
				t.Fatalf("status = %q, want silence", m.flash.Text())
			}
			if m.ListCursor() != 0 {
				t.Fatalf("cursor = %d, want the untouched first row", m.ListCursor())
			}
		})
	}
}

// Decision 7's preset absolutism is untouched: a repository lift is still only an
// ordering of the rows the active preset produced. The preset hides every DONE row
// and this repository holds nothing else, so the pass answers and lifts nothing —
// and the human's own choice of preset is not widened to reveal what it named.
func TestARepositoryLiftDoesNotWidenThePreset(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	f.d.ViewPreset = config.WorkViewPreset{
		Name:  "_hide-done",
		Label: "in flight",
		Lift:  true,
		Hide:  &config.WorkViewPresetFilter{Status: []string{"done"}},
	}

	m := f.standIn(t, f.sibling)

	if len(m.snap.Containers) != 0 {
		t.Fatalf("rows = %v, want none — the pass must not widen the preset to reveal what it named", rowIDs(m))
	}
	if m.snap.Attribution == nil {
		t.Fatal("attribution = none: a hidden row is still attributed, it simply has no row to lift")
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence about rows nobody can see", m.flash.Text())
	}
	if f.d.ViewPreset.Name != "_hide-done" {
		t.Fatalf("preset = %q, want the human's own choice untouched", f.d.ViewPreset.Name)
	}
}

// commonDirForks is the git processes that asked a directory which repository it is
// in — the one fork the repository pass costs, and the one a snapshot build must
// never make (decision 5): a build's git memo lives for one load, the dashboard
// rebuilds every two seconds, and the pane's directory is read once and never
// re-read.
func commonDirForks(forks []string) []string {
	var asked []string
	for _, fork := range forks {
		if strings.Contains(fork, "--git-common-dir") {
			asked = append(asked, fork)
		}
	}
	return asked
}

// countGit puts a counting git in front of both seams a build reads through, and
// answers with the counter.
func countGit(d *drain.Deps) *countingGit {
	counter := &countingGit{inner: d.Tasks.Git}
	td := *d.Tasks
	td.Git = counter
	d.Tasks = &td
	pd := *d.Project
	pd.Git = counter
	d.Project = &pd
	return counter
}

// Decision 5: the pane's repository is one launch-time fact, not a question the
// ladder asks per build. A dashboard left open over many rebuilds must fork git for
// it exactly as often as it did at launch, which is once — the version that asked
// during the snapshot build would fork roughly eighteen hundred times an hour for
// an answer that cannot change.
func TestThePanesRepositoryIsResolvedOnceAtLaunch(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	f.rt.Fake.PaneCwd["%7"] = f.sibling
	want := f.repoOrder(t)
	counter := countGit(f.d)

	facts := LaunchPaneFacts(f.d.Tmux, f.d.Tasks)
	atLaunch := commonDirForks(counter.forks())
	if len(atLaunch) != 1 {
		t.Fatalf("launch asked which repository the pane is in %d times, want exactly once: %v", len(atLaunch), atLaunch)
	}
	if facts.RepoCommonDir == "" {
		t.Fatal("launch resolved no repository for a pane standing in a worktree")
	}

	snap, err := BuildPageSnapshot(f.d, f.cfg, PageWork, facts)
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	m := NewDashboardOn(f.d, f.cfg, snap, PageWork)
	for i := 0; i < 10; i++ {
		m = rebuild(t, m)
	}

	if after := commonDirForks(counter.forks()); len(after) != len(atLaunch) {
		t.Fatalf("after ten rebuilds the repository had been asked for %d times, want the launch's %d: %v", len(after), len(atLaunch), after)
	}
	// The ceiling means nothing unless the pass really ran on every one of those
	// builds: a fact nobody reads is cheap for the wrong reason.
	if got := liftedBlock(t, m); !slices.Equal(got, want) {
		t.Fatalf("lifted %v after ten rebuilds, want the repository's sets %v", got, want)
	}
}

// `pop work status` is a printed list, not a page anyone is standing in: it builds
// with empty pane facts, so it lifts nothing and never asks which repository
// anything is in. The same machine lifts on the dashboard, which is what makes the
// plain status output a fact about the surface rather than about a fixture that
// could not lift.
func TestWorkStatusLiftsNothingAndForksNoGitForAttribution(t *testing.T) {
	f := newRepositoryFixture(t, 1)
	f.rt.Fake.PaneCwd["%7"] = f.sibling
	counter := countGit(f.d)

	tables, err := BuildStatusTables(f.d, f.cfg)
	if err != nil {
		t.Fatalf("BuildStatusTables: %v", err)
	}

	if asked := commonDirForks(counter.forks()); len(asked) != 0 {
		t.Fatalf("status asked which repository something is in %v, want no attribution fork at all", asked)
	}
	for _, row := range tables.TaskSets.Rows {
		if row.Lifted {
			t.Fatalf("status marked %s: it has no pane to attribute", row.ID)
		}
	}
	if len(tables.TaskSets.Rows) != len(f.stems) {
		t.Fatalf("status listed %d rows, want the repository's %d — the fixture must carry the rows a lift would move", len(tables.TaskSets.Rows), len(f.stems))
	}

	m := f.standIn(t, f.sibling)
	if got := liftedBlock(t, m); len(got) == 0 {
		t.Fatal("the same machine lifted nothing on the dashboard: the status assertion above is about a fixture that could not lift")
	}
	if got := statusRowOrder(tables); !slices.Equal(got, unliftedOrder(t, f.d, f.cfg)) {
		t.Fatalf("status order = %v, want the page's own unlifted order", got)
	}
}

// statusRowOrder is the ids status printed, in the order it printed them.
func statusRowOrder(tables StatusTables) []string {
	var ids []string
	for _, row := range tables.TaskSets.Rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// A build handed no pane facts attributes nothing, whatever repository its rows
// live in: the surfaces that pass empty facts pay for no pass at all.
func TestEmptyPaneFactsReachNoRepositoryPass(t *testing.T) {
	f := newRepositoryFixture(t, 1)

	snap, err := BuildPageSnapshot(f.d, f.cfg, PageWork, work.PaneFacts{})
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}

	if snap.Attribution != nil {
		t.Fatalf("attribution = %+v, want none", *snap.Attribution)
	}
}
