package wayfinder

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// mapKindFixture lays out one repository group holding five Maps — active,
// arrived, abandoned, archived (through the retired archive side-file) and one
// BROKEN by a retired Status: word — and returns the kind over it.
func mapKindFixture(t *testing.T) (*MapKind, repogroup.Group) {
	t.Helper()
	storageDir := "/data/repos/repo-aaaa"
	tasksDir := filepath.Join(storageDir, "tasks")
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	arrivedMap := filepath.Join(storageDir, "maps", "2026-07-02-arrived")
	abandonedMap := filepath.Join(storageDir, "maps", "2026-07-03-abandoned")
	archivedMap := filepath.Join(storageDir, "maps", "2026-07-04-archived")
	brokenMap := filepath.Join(storageDir, "maps", "2026-07-05-broken")
	files := map[string]string{
		filepath.Join(activeMap, "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues", "01-research.md"): "" +
			"Type: research\nStatus: open\n\n# Q\n",
		filepath.Join(activeMap, "issues", "02-blocked.md"): "" +
			"Type: research\nStatus: open\nBlocked by: 01\n\n# Q\n",
		filepath.Join(arrivedMap, "map.md"):                 "Status: arrived\n\n## Destination\nArrived\n",
		filepath.Join(abandonedMap, "map.md"):               "Status: abandoned\n\n## Destination\nNope\n",
		filepath.Join(archivedMap, "map.md"):                "Status: active\n\n## Destination\nHidden\n",
		filepath.Join(brokenMap, "map.md"):                  "Status: done\n\n## Destination\nRetired word\n",
		filepath.Join(storageDir, "wayfinder-archive.json"): `{"archived":["2026-07-04-archived"]}`,
	}
	wd := wayfinderTestDeps(t, t.TempDir(), "/repo/.git", files)
	group := repogroup.Group{
		DefPath:       tasksDir,
		StatePath:     tasks.StatePathFor(tasksDir),
		StorageDir:    storageDir,
		RepoKey:       "repo-key",
		RepoCommonDir: "/repo/.git",
		ProjectName:   "pop",
		Rep:           &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
	}
	k := NewMapKind(&MapKindDeps{
		Wayfinder: wd,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return []repogroup.Group{group}, nil },
	})
	return k, group
}

// TestMapKindLoadsVisibleMapsOnly pins what a Map contributes as a Work
// container: an arrived Map stays beside the active one — it is the lineage view
// for the sets it spawned, and Archive is what hides a Map (ADR-0172) — while
// abandoned, archived and BROKEN Maps are hidden, and the container's status cell
// is the ticket tally the dashboard has always shown (ADR-0130).
func TestMapKindLoadsVisibleMapsOnly(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	byID := map[string]work.Container{}
	for _, c := range containers {
		ids = append(ids, c.ID)
		byID[c.ID] = c
	}
	for _, shown := range []string{"2026-07-01-active", "2026-07-02-arrived"} {
		if !slices.Contains(ids, shown) {
			t.Fatalf("missing map container %q; got %v", shown, ids)
		}
	}
	for _, hidden := range []string{"2026-07-03-abandoned", "2026-07-04-archived", "2026-07-05-broken"} {
		if slices.Contains(ids, hidden) {
			t.Fatalf("hidden map %q still present: %v", hidden, ids)
		}
	}

	active := byID["2026-07-01-active"]
	if active.Kind != k.ID() {
		t.Fatalf("container kind = %q, want %q", active.Kind, k.ID())
	}
	if active.Project != "pop" || active.Checkout != "/repo/main" {
		t.Fatalf("container = %+v, want project pop in /repo/main", active)
	}
	if want := "WAYFINDING · 2 open / 1 frontier"; work.StatusCellText(k.StatusCell(active)) != want {
		t.Fatalf("StatusCell = %q, want %q", work.StatusCellText(k.StatusCell(active)), want)
	}
	if active.Worktree != "" {
		t.Fatalf("container = %+v, want a map row with a blank worktree", active)
	}
	if len(active.Items) != 2 {
		t.Fatalf("items = %+v, want the two tickets", active.Items)
	}
	if active.Items[0].Blocked {
		t.Fatalf("frontier ticket %+v reported blocked", active.Items[0])
	}
	if !active.Items[1].Blocked {
		t.Fatalf("ticket behind an open blocker %+v reported unblocked", active.Items[1])
	}
	if len(active.DetailSections) != 1 || active.DetailSections[0].Title != "Destination" {
		t.Fatalf("detail sections = %+v, want the Destination prose", active.DetailSections)
	}
}

// TestMapKindOrdersAndSummarises pins the Map kind's own comparator (project,
// then id descending) and its header phrase. Nothing here ranks a Map against a
// task set — the seam's kind precedence does that, and this comparator never sees
// another kind's container.
func TestMapKindOrdersAndSummarises(t *testing.T) {
	k, _ := mapKindFixture(t)
	snap, err := work.BuildSnapshot([]work.Kind{k})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range snap.Containers {
		ids = append(ids, c.ID)
	}
	want := []string{"2026-07-02-arrived", "2026-07-01-active"}
	if !slices.Equal(ids, want) {
		t.Fatalf("order = %v, want %v (newest Map of a project first)", ids, want)
	}
	if got := snap.SummaryLine(); got != "2 maps" {
		t.Fatalf("summary = %q, want %q", got, "2 maps")
	}
	if len(k.Summary(nil)) != 0 {
		t.Fatalf("Summary of no Maps = %v, want nothing said", k.Summary(nil))
	}
}

// TestMapKindVerbsFollowTheFrontier pins the verb surface: a Map with a frontier
// offers to work it, the two shared verbs are always there, and a ticket offers
// the work verb only while it is actually workable.
func TestMapKindVerbsFollowTheFrontier(t *testing.T) {
	k, _ := mapKindFixture(t)
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	verbs := func(actions []work.Action) []work.Verb {
		var out []work.Verb
		for _, a := range actions {
			out = append(out, a.Verb)
		}
		return out
	}
	if got, want := verbs(k.Actions(active)), []work.Verb{VerbWork, work.VerbShell, work.VerbCopyName}; !slices.Equal(got, want) {
		t.Fatalf("map actions = %v, want %v", got, want)
	}
	if got, want := verbs(k.ItemActions(active, active.Items[0])), []work.Verb{VerbWork, work.VerbCopyName}; !slices.Equal(got, want) {
		t.Fatalf("frontier ticket actions = %v, want %v", got, want)
	}
	if got, want := verbs(k.ItemActions(active, active.Items[1])), []work.Verb{work.VerbCopyName}; !slices.Equal(got, want) {
		t.Fatalf("blocked ticket actions = %v, want %v", got, want)
	}

	// copy-name copies the map id for a row and the bare ticket id for a ticket,
	// which is what each surface's paste target has always been.
	out, err := k.Perform(active, nil, work.VerbCopyName)
	if err != nil || out.Clipboard != "2026-07-01-active" {
		t.Fatalf("copy-name on the map = %+v, %v", out, err)
	}
	ticket := active.Items[0]
	out, err = k.Perform(active, &ticket, work.VerbCopyName)
	if err != nil || out.Clipboard != "01" {
		t.Fatalf("copy-name on a ticket = %+v, %v", out, err)
	}
	if _, err := k.Perform(active, nil, work.Verb("drain")); err == nil {
		t.Fatal("a Task-set verb on a Map should be refused")
	}
}

// TestMapKindWorkVerbOpensTheGrillingWindow pins the container-level work verb: it
// runs the same composite `pop map next` runs — one window per ticket inside the
// Map's own session — and hands the caller that session to switch to, rather than
// switching on its own (ADR-0158).
func TestMapKindWorkVerbOpensTheGrillingWindow(t *testing.T) {
	k, _ := mapKindFixture(t)
	fake := &tmuxtest.Fake{}
	k.d.Wayfinder.Tmux = fake
	k.d.Wayfinder.Trunk = func() (string, error) { return "/repo/trunk", nil }
	k.d.Wayfinder.Exe = func() (string, error) { return "/opt/pop/bin/pop", nil }

	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var active work.Container
	for _, c := range containers {
		if c.ID == "2026-07-01-active" {
			active = c
		}
	}
	out, err := k.Perform(active, nil, VerbWork)
	if err != nil {
		t.Fatalf("work verb: %v", err)
	}
	session := MapSessionName(active.ID)
	if out.Kind != work.OutcomeHandoff || out.Handoff.Kind != work.HandoffTmux || out.Handoff.Target != session {
		t.Fatalf("outcome = %+v, want a tmux handoff to %q", out, session)
	}
	if _, ok := fake.Windows[session]["01-research"]; !ok {
		t.Fatalf("windows = %v, want one named after the frontier ticket", fake.Windows[session])
	}
	if len(fake.Switched) != 0 {
		t.Fatalf("the verb moved the caller itself: %v", fake.Switched)
	}
	// The arrived Map has no frontier, so there is nothing to work and the verb
	// says so instead of opening an empty window.
	for _, c := range containers {
		if c.ID == "2026-07-02-arrived" {
			if _, err := k.Perform(c, nil, VerbWork); err == nil {
				t.Fatal("working a Map with an empty frontier should be refused")
			}
		}
	}
}
