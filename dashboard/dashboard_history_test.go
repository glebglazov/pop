package dashboard

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// Anything that puts the human into a pane records where they landed, so the
// project picker's recency ordering reflects where they have actually been
// (ADR-0188). Every dashboard handoff verb ends at one chokepoint, so these drive
// the verbs and read the rows back out of the store.

// recordedHistory reads the landing rows back through the same seam the pickers
// read them through.
func recordedHistory(t *testing.T, d *drain.Deps) []string {
	t.Helper()
	hist, err := history.LoadWith(&history.Deps{FS: d.Tasks.FS, Tmux: d.Tmux, Tasks: d.Tasks})
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	var paths []string
	for _, e := range hist.Entries {
		paths = append(paths, e.Path)
	}
	return paths
}

// assertRecorded asserts history holds exactly one landing, at want. Paths are
// compared symlink-resolved: a temp dir on macOS is reached through /var while the
// launcher's own resolution answers /private/var, and either spelling is the same
// checkout.
func assertRecorded(t *testing.T, got []string, want string) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("history = %v, want exactly the one landing %s", got, want)
	}
	if canonical(t, got[0]) != canonical(t, want) {
		t.Fatalf("history recorded %s, want %s", got[0], want)
	}
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := deps.NewRealFileSystem().EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// handoffVerb presses a row's action-menu key and follows the messages it
// produces to the handoff — the move itself, and the recording behind it. A verb
// the kind owns answers with its own outcome first and the dashboard turns that
// into the handoff, so the chain is walked rather than read one message deep.
func handoffVerb(t *testing.T, m QueueDashboard, key string) dashboardHandoffMsg {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	m = updated.(QueueDashboard)
	for cmd != nil {
		msg := cmd()
		if handoff, ok := msg.(dashboardHandoffMsg); ok {
			if handoff.err != nil || !handoff.quit {
				t.Fatalf("%s handoff = %+v, want quit after focus", key, handoff)
			}
			return handoff
		}
		if msg == nil {
			break
		}
		updated, cmd = m.Update(msg)
		m = updated.(QueueDashboard)
	}
	t.Fatalf("%s never handed off; status = %q, err = %v", key, m.statusMsg, m.actionErr)
	return dashboardHandoffMsg{}
}

// TestDashboardHandoffVerbsRecordTheSetsCheckout drives every handoff verb a task
// set offers and asserts each one recorded the set's bound checkout. A manually
// launched drain, verify and fold record like the rest: the line History draws is
// manual versus daemon, not human work versus machine work. The bound worktree is
// deliberately not the repository's trunk, so a recording that fell back to the
// repository would fail here.
func TestDashboardHandoffVerbsRecordTheSetsCheckout(t *testing.T) {
	openTask := []queuetest.SpawnTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}}
	doneTask := []queuetest.SpawnTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}}
	for _, tc := range []struct {
		name    string
		key     string
		rows    []queuetest.SpawnTask
		arrange func(row *DashboardRow)
	}{
		{name: "drain", key: "I", rows: openTask},
		{name: "verify", key: "V", rows: doneTask, arrange: func(row *DashboardRow) {
			row.RawStatus = tasks.StatusNeedsVerify
			row.VerifyMark = tasks.VerifyMarkUnverified
		}},
		{name: "fold", key: "F", rows: doneTask, arrange: func(row *DashboardRow) {
			row.RawStatus = tasks.StatusDone
			row.Provisioned = true
		}},
		{name: "assist", key: "S", rows: doneTask},
		{name: "shell", key: "O", rows: doneTask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, setID, _ := queuetest.SetupSpawnRepo(t, "history-"+tc.name, tc.rows)
			bound := filepath.Join(t.TempDir(), "history-"+tc.name+"-wt")
			runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
			d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
			repoKey, err := drain.ResolveRepoKey(d, repo)
			if err != nil {
				t.Fatal(err)
			}
			row.RepoKey = repoKey
			row.RuntimePath = bound
			row.ProjectPath = repo
			row.Checkout = bound
			row.Bound = true
			if tc.arrange != nil {
				tc.arrange(&row)
			}
			queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
				drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "history-" + tc.name, Project: "pop", Provisioned: row.Provisioned},
			})
			rt.Fake.Inside = true

			m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
			handoffVerb(t, m, tc.key)
			assertRecorded(t, recordedHistory(t, d), bound)
		})
	}
}

// TestDashboardMapHandoffRecordsTheMapsTrunk pins the Map answer: a Map has no
// checkout of its own, so what goes into History is the Trunk worktree its session
// is rooted at — for both the ticket-grilling verb and the Map's own assist.
func TestDashboardMapHandoffRecordsTheMapsTrunk(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{name: "work frontier ticket", key: "I"},
		{name: "assist", key: "S"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, cfg, row, f, _ := wayfinderSpawnFixture(t)
			f.Inside = true
			m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
			handoffVerb(t, m, tc.key)
			trunk := f.Live[wayfinderMapSession()]
			if trunk == "" {
				t.Fatal("the map session was rooted nowhere")
			}
			assertRecorded(t, recordedHistory(t, d), trunk)
		})
	}
}

// TestRoutineRefinementHandoffRecordsItsBoundDirectory drives the Routine
// refinement spawn — the one handoff whose launcher names a pane and no checkout —
// and asserts the landing recorded is the Routine's bound directory, read off the
// pane the operator was moved into.
func TestRoutineRefinementHandoffRecordsItsBoundDirectory(t *testing.T) {
	td := queuetest.TasksDeps(t, true)
	f := &tmuxtest.Fake{Inside: true}
	boundDir := t.TempDir()
	rd := &routine.Deps{
		FS:         deps.NewRealFileSystem(),
		Tasks:      td,
		Tmux:       f,
		InTmux:     func() bool { return true },
		Executable: func() (string, error) { return "/mock/bin/pop", nil },
	}
	if _, err := routine.AddWith(rd, "fresh", "every 6h", boundDir); err != nil {
		t.Fatalf("AddWith: %v", err)
	}
	kinds := func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{routine.NewKind(&routine.KindDeps{Routine: rd})}
	}
	d := &drain.Deps{Kinds: kinds, RoutineKinds: kinds, Tasks: td, Tmux: f, Project: project.DefaultDeps()}

	m := openPage(t, d, PageRoutines)
	handoffVerb(t, m, "R")
	assertRecorded(t, recordedHistory(t, d), boundDir)
}

// TestHandoffLandingReachesThePickerOrdering closes the loop the whole slice
// exists for: a checkout entered only through a dashboard handoff verb — never
// picked, never switched to — sorts as the most recent project in the picker's
// recency ordering.
func TestHandoffLandingReachesThePickerOrdering(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "history-picker", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "history-picker-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "history-picker", Project: "pop", Provisioned: false},
	})
	rt.Fake.Inside = true

	hd := &history.Deps{FS: d.Tasks.FS, Tmux: d.Tmux, Tasks: d.Tasks}
	before, err := history.LoadWith(hd)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "picked-by-hand")
	if err := before.Record(elsewhere); err != nil {
		t.Fatal(err)
	}

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	handoffVerb(t, m, "I")

	after, err := history.LoadWith(hd)
	if err != nil {
		t.Fatal(err)
	}
	sorted := after.SortByRecencyWith(hd, []project.Project{
		{Name: "handoff-only", Path: bound},
		{Name: "picked-by-hand", Path: elsewhere},
	})
	// Most recent last: the picker opens with its cursor at the bottom.
	if len(sorted) != 2 || sorted[1].Path != bound {
		t.Fatalf("picker order = %+v, want the handoff checkout %s last", sorted, bound)
	}
}
