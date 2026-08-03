package wayfinder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// spawnedFixture lays a whole repository's Task storage down on disk: a Map that
// spawned three sets — one part-done, one archived, one that no longer exists —
// and the two real sets behind them, registered the way `pop tasks register`
// would. Everything is real-FS because the point of the fixture is that the
// statuses come from the sets themselves.
func spawnedFixture(t *testing.T) (*Deps, string, Map) {
	t.Helper()
	storageDir := t.TempDir()
	fs := realFSWithDataHome(filepath.Join(storageDir, "xdg"))
	td := &tasks.Deps{FS: fs, Git: deps.NewRealGit()}
	t.Cleanup(func() { _ = td.CloseStore() })
	d := &Deps{FS: fs, Tasks: td}

	tasksDir := filepath.Join(storageDir, "tasks")
	writeTaskSet(t, tasksDir, "2026-07-10-landed", []tasks.Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: tasks.TaskDone},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: tasks.TaskOpen},
	})
	writeTaskSet(t, tasksDir, "2026-07-11-filed", []tasks.Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: tasks.TaskDone},
	})
	statePath := tasks.StatePathFor(tasksDir)
	if _, err := tasks.RegisterWith(td, tasksDir, statePath); err != nil {
		t.Fatalf("register sets: %v", err)
	}
	if _, err := tasks.ArchiveTaskSetWith(td, nil, nil,
		tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: tasksDir}, "2026-07-11-filed"); err != nil {
		t.Fatalf("archive set: %v", err)
	}

	mapDir := filepath.Join(storageDir, "maps", "2026-07-01-map")
	writeFixtureFile(t, filepath.Join(mapDir, "map.md"), "Status: active\n\n## Destination\nShip it\n")
	writeFixtureFile(t, filepath.Join(mapDir, "issues", "01-first.md"), "# 01 — First\n\n## Question\nA\n")
	writeFixtureFile(t, MapManifestPath(mapDir), `{
  "tickets": [
    {"id": "01", "file": "01-first.md", "title": "First", "type": "research", "status": "open", "blocked_by": []}
  ],
  "spawned_sets": ["2026-07-10-landed", "2026-07-11-filed", "2026-07-12-vanished"]
}
`)

	maps, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	if len(maps) != 1 || maps[0].Broken {
		t.Fatalf("maps = %+v, want one well-formed map", maps)
	}
	return d, storageDir, maps[0]
}

func writeTaskSet(t *testing.T, tasksDir, setID string, list []tasks.Task) {
	t.Helper()
	setDir := filepath.Join(tasksDir, setID)
	for _, task := range list {
		writeFixtureFile(t, filepath.Join(setDir, task.File), "## Acceptance criteria\n\n- [ ] ok\n")
	}
	data, err := json.MarshalIndent(map[string]any{"tasks": list}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(setDir, "index.json"), string(data))
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSpawnedSetsResolveLiveAndRenderOnBothSurfaces is the payoff of the manifest
// field: from a Map you can see whether the work it spawned has landed. It pins
// the three facts that make the block trustworthy — the status is the set's own
// and read at render time, an id that resolves to nothing still renders, and an
// archived set still renders with its status — and that `pop map show` prints the
// very same lines the dashboard's detail pane does.
func TestSpawnedSetsResolveLiveAndRenderOnBothSurfaces(t *testing.T) {
	d, storageDir, m := spawnedFixture(t)

	if want := []string{"2026-07-10-landed", "2026-07-11-filed", "2026-07-12-vanished"}; !slices.Equal(m.SpawnedSets, want) {
		t.Fatalf("map spawned sets = %v, want %v", m.SpawnedSets, want)
	}

	spawned := ResolveSpawnedSets(d, m)
	if len(spawned) != 3 {
		t.Fatalf("resolved = %+v, want one entry per recorded id", spawned)
	}
	landed, filed, vanished := spawned[0], spawned[1], spawned[2]
	if landed.Status != "IN PROGRESS" || !strings.HasPrefix(landed.Progress, "1/2 done") {
		t.Fatalf("landed set = %+v, want the set's own live status and tally", landed)
	}
	if !filed.Archived || filed.Status != string(tasks.StatusDone) {
		t.Fatalf("archived set = %+v, want it still resolved, with its status", filed)
	}
	if !vanished.Missing || vanished.Line() != "2026-07-12-vanished — (missing)" {
		t.Fatalf("unresolvable set = %+v, want a (missing) line", vanished)
	}

	var buf strings.Builder
	if err := RenderShow(&buf, m, ResolveSpawnedSets(d, m)); err != nil {
		t.Fatalf("RenderShow: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\nSpawned sets:\n") {
		t.Fatalf("show output has no spawned-sets block:\n%s", out)
	}

	// The dashboard's detail pane and the agent-session print are one read: the
	// section body is line-for-line what show wrote.
	section := mapDetailSection(t, d, storageDir, m.ID, "Spawned sets")
	for _, line := range strings.Split(section, "\n") {
		if !strings.Contains(out, "  "+line+"\n") {
			t.Fatalf("show output missing detail line %q:\n%s", line, out)
		}
	}
	if want := strings.Join([]string{landed.Line(), filed.Line(), vanished.Line()}, "\n"); section != want {
		t.Fatalf("detail section =\n%s\nwant\n%s", section, want)
	}
}

// TestSpawnedSetStatusIsNeverPersisted pins that reading lineage writes nothing:
// the ids are the record, and a status copied into the Map's files would be a
// stale second source the moment the set moved on.
func TestSpawnedSetStatusIsNeverPersisted(t *testing.T) {
	d, storageDir, m := spawnedFixture(t)
	before := map[string]string{}
	for _, rel := range []string{"map.md", MapManifestFileName} {
		before[rel] = readFixtureFile(t, filepath.Join(m.Dir, rel))
	}

	if _, err := ScanMapsInStorage(d, storageDir); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderShow(&buf, m, ResolveSpawnedSets(d, m)); err != nil {
		t.Fatal(err)
	}
	mapDetailSection(t, d, storageDir, m.ID, "Spawned sets")

	for rel, want := range before {
		if got := readFixtureFile(t, filepath.Join(m.Dir, rel)); got != want {
			t.Fatalf("%s changed by rendering lineage:\n%s", rel, got)
		}
	}
	// Nor is a status hiding in what the Map does store: the files name ids, and
	// nothing a spawned set could later contradict.
	for rel, content := range before {
		for _, word := range []string{"IN PROGRESS", string(tasks.StatusDone), "done,", "1/2", "archived"} {
			if strings.Contains(content, word) {
				t.Fatalf("%s carries the spawned set's live status %q:\n%s", rel, word, content)
			}
		}
	}
}

// TestSpawnedSetsUseTheInjectedStatusSeam covers the resolver over the seam a
// caller can supply, including the two shapes the sets themselves cannot easily
// produce on demand: a set whose storage cannot be read at all, and a Map that
// spawned nothing.
func TestSpawnedSetsUseTheInjectedStatusSeam(t *testing.T) {
	calls := 0
	d := &Deps{
		SetStatuses: func(defPath string) (map[string]SpawnedSet, error) {
			calls++
			return map[string]SpawnedSet{
				"set-a": {ID: "set-a", Status: "READY", Progress: "0/3 done, 3 open"},
			}, nil
		},
	}
	m := Map{ID: "m", Dir: filepath.Join("/store", "maps", "m"), SpawnedSets: []string{"set-a", "set-b"}}
	spawned := ResolveSpawnedSets(d, m)
	if len(spawned) != 2 || spawned[0].Line() != "set-a — READY · 0/3 done, 3 open" || !spawned[1].Missing {
		t.Fatalf("resolved = %+v", spawned)
	}
	if calls != 1 {
		t.Fatalf("set table read %d times, want one read for the whole Map", calls)
	}

	// A Map with no recorded ids never reaches for the sets at all, and renders no
	// section rather than an empty heading.
	if got := ResolveSpawnedSets(d, Map{ID: "m", Dir: m.Dir}); got != nil {
		t.Fatalf("resolved = %+v, want nothing for a Map that spawned nothing", got)
	}
	if sections := sectionsFor(Map{Destination: "Ship it"}, nil); len(sections) != 1 {
		t.Fatalf("sections = %+v, want no spawned-sets heading", sections)
	}
}

// mapDetailSection returns the named detail section the Map kind hands the Work
// dashboard for one Map, which is the only path the detail pane has to it.
func mapDetailSection(t *testing.T, d *Deps, storageDir, mapID, title string) string {
	t.Helper()
	tasksDir := filepath.Join(storageDir, "tasks")
	group := repogroup.Group{
		DefPath:       tasksDir,
		StatePath:     tasks.StatePathFor(tasksDir),
		StorageDir:    storageDir,
		RepoKey:       "repo-key",
		RepoCommonDir: "/repo/.git",
		ProjectName:   "pop",
		Rep:           &repogroup.Checkout{Name: "pop", ProjectPath: storageDir, RuntimePath: storageDir},
	}
	k := NewMapKind(&MapKindDeps{
		Wayfinder: d,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return []repogroup.Group{group}, nil },
	})
	containers, err := k.Load()
	if err != nil {
		t.Fatalf("map kind Load: %v", err)
	}
	var container work.Container
	for _, c := range containers {
		if c.ID == mapID {
			container = c
		}
	}
	for _, section := range container.DetailSections {
		if section.Title == title {
			return section.Body
		}
	}
	t.Fatalf("detail sections = %+v, want one titled %q", container.DetailSections, title)
	return ""
}

func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
