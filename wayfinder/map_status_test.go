package wayfinder

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// statusKindFixture is the Map kind over a real registry fixture, so a status
// verb performed through the kind writes the same files and rows the command line
// writes. It returns the kind, the deps behind it and the group it scans.
func statusKindFixture(t *testing.T, mapID string) (*MapKind, *Deps, repogroup.Group) {
	t.Helper()
	d, storageDir := registryFixture(t, oneTicketMap(mapID))
	d.Tmux = &tmuxtest.Fake{Live: map[string]string{MapSessionName(mapID): "/repo"}}
	tasksDir := filepath.Join(storageDir, "tasks")
	trunk, err := d.Trunk()
	if err != nil {
		t.Fatal(err)
	}
	group := repogroup.Group{
		DefPath:     tasksDir,
		StatePath:   tasks.StatePathFor(tasksDir),
		StorageDir:  storageDir,
		RepoKey:     "repo-key",
		ProjectName: "pop",
		Rep:         &repogroup.Checkout{Name: "pop", ProjectPath: trunk, RuntimePath: trunk},
	}
	k := NewMapKind(&MapKindDeps{
		Wayfinder: d,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return []repogroup.Group{group}, nil },
	})
	return k, d, group
}

func loadedMapIDs(t *testing.T, k *MapKind) []string {
	t.Helper()
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	return ids
}

func mapContainer(t *testing.T, k *MapKind, id string) work.Container {
	t.Helper()
	containers, err := k.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range containers {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("map %q is not loaded; got %v", id, loadedMapIDs(t, k))
	return work.Container{}
}

// Abandonment is a real write with a real reversal: the word lands on disk, the
// row leaves the dashboard, and open brings it back — from abandoned exactly as
// from arrived, so neither terminal word is a dead end.
func TestAbandonAndReopenRoundTrip(t *testing.T) {
	k, d, _ := statusKindFixture(t, "demo-map")
	mustRegister(t, d, "demo-map")

	result, err := AbandonMap(d, "", "demo-map")
	if err != nil {
		t.Fatalf("AbandonMap: %v", err)
	}
	if result.Status != MapAbandoned || result.Previous != MapActive || result.Unchanged {
		t.Fatalf("abandon result = %+v, want active -> abandoned", result)
	}
	if got := mapStatusOnDisk(t, d, "", "demo-map"); got != MapAbandoned {
		t.Fatalf("on-disk status = %q, want abandoned", got)
	}
	// Abandonment leaves the session standing: it is often typed from one of the
	// Map's own panes, unlike arrival, which exists to end them.
	if !d.Tmux.HasSession(MapSessionName("demo-map")) {
		t.Fatal("abandon tore down the map's session")
	}
	if ids := loadedMapIDs(t, k); slices.Contains(ids, "demo-map") {
		t.Fatalf("abandoned map still on the dashboard: %v", ids)
	}

	// Re-declaring is a no-op, not an error.
	again, err := AbandonMap(d, "", "demo-map")
	if err != nil || !again.Unchanged {
		t.Fatalf("second abandon = %+v, %v", again, err)
	}

	back, err := ReopenMap(d, "", "demo-map")
	if err != nil {
		t.Fatalf("ReopenMap: %v", err)
	}
	if back.Status != MapActive || back.Previous != MapAbandoned {
		t.Fatalf("reopen result = %+v, want abandoned -> active", back)
	}
	if ids := loadedMapIDs(t, k); !slices.Contains(ids, "demo-map") {
		t.Fatalf("reopened map missing from the dashboard: %v", ids)
	}

	// The same reopen reverses arrival, and unlike `pop map open` it touches no
	// session at all.
	if _, err := ArriveMap(d, "", "demo-map"); err != nil {
		t.Fatalf("ArriveMap: %v", err)
	}
	if _, err := ReopenMap(d, "", "demo-map"); err != nil {
		t.Fatalf("ReopenMap after arrive: %v", err)
	}
	if got := mapStatusOnDisk(t, d, "", "demo-map"); got != MapActive {
		t.Fatalf("status after reopening an arrived map = %q, want active", got)
	}
	if d.Tmux.HasSession(MapSessionName("demo-map")) {
		t.Fatal("reopen recreated the session arrival killed; that is `pop map open`'s job")
	}
}

// Every one of the Map's four status verbs performed through the kind, as the
// dashboard performs them: each writes in place and the next Load reflects it.
func TestMapKindStatusVerbsWriteAndTakeEffectOnTheNextLoad(t *testing.T) {
	k, d, _ := statusKindFixture(t, "demo-map")
	mustRegister(t, d, "demo-map")
	row := mapContainer(t, k, "demo-map")

	out, err := k.Perform(row, nil, VerbArchive)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if out.Kind != work.OutcomeRefresh {
		t.Fatalf("archive outcome = %+v, want a refresh", out)
	}
	if !archivedInRegistry(t, d, "demo-map") {
		t.Fatal("archive verb wrote no registry bit")
	}
	if ids := loadedMapIDs(t, k); slices.Contains(ids, "demo-map") {
		t.Fatalf("archived map still listed by default: %v", ids)
	}

	// Unarchiving is reachable only because the show-archived view lists the row at
	// all, so that is the state the verb is performed from.
	k.d.IncludeArchived = true
	archivedRow := mapContainer(t, k, "demo-map")
	if !archivedRow.Archived {
		t.Fatal("archived row does not say it is archived")
	}
	if cell := work.StatusCellText(k.StatusCell(archivedRow)); !strings.Contains(cell, "archived") {
		t.Fatalf("status cell = %q, want it to name the archived state", cell)
	}
	if _, err := k.Perform(archivedRow, nil, VerbUnarchive); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if archivedInRegistry(t, d, "demo-map") {
		t.Fatal("unarchive verb left the registry bit set")
	}
	k.d.IncludeArchived = false
	if ids := loadedMapIDs(t, k); !slices.Contains(ids, "demo-map") {
		t.Fatalf("unarchived map missing from the default view: %v", ids)
	}

	if _, err := k.Perform(row, nil, VerbAbandon); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if got := mapStatusOnDisk(t, d, "", "demo-map"); got != MapAbandoned {
		t.Fatalf("status after the abandon verb = %q", got)
	}
	if ids := loadedMapIDs(t, k); slices.Contains(ids, "demo-map") {
		t.Fatalf("abandoned map still listed: %v", ids)
	}

	if _, err := k.Perform(row, nil, VerbReopen); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := mapStatusOnDisk(t, d, "", "demo-map"); got != MapActive {
		t.Fatalf("status after the reopen verb = %q", got)
	}
	if ids := loadedMapIDs(t, k); !slices.Contains(ids, "demo-map") {
		t.Fatalf("reopened map missing: %v", ids)
	}

	// Arrive is not among them: it is a command-line ceremony, not a status write
	// (ADR-0186).
	if _, err := k.Perform(row, nil, work.Verb("arrive")); err == nil {
		t.Fatal("the kind performed an arrive verb; arrival is offered nowhere in the dashboard")
	}
}

// Archiving an unregistered Map fails with the corrective the command line gives,
// rather than silently doing nothing.
func TestMapKindArchiveOfUnregisteredMapReportsTheCorrective(t *testing.T) {
	k, _, _ := statusKindFixture(t, "demo-map")
	row := mapContainer(t, k, "demo-map")

	_, err := k.Perform(row, nil, VerbArchive)
	if err == nil {
		t.Fatal("archiving an unregistered map succeeded")
	}
	if !strings.Contains(err.Error(), "pop map register demo-map") {
		t.Fatalf("error = %q, want the register corrective", err)
	}
}

// The show-archived flag is about filing, not about status: it reveals an archived
// Map and nothing else the default view hides.
func TestShowArchivedRevealsOnlyArchivedMaps(t *testing.T) {
	k, _ := mapKindFixture(t)
	k.d.IncludeArchived = true
	ids := loadedMapIDs(t, k)
	if !slices.Contains(ids, "2026-07-04-archived") {
		t.Fatalf("archived map missing with show-archived on: %v", ids)
	}
	for _, stillHidden := range []string{"2026-07-03-abandoned", "2026-07-05-broken"} {
		if slices.Contains(ids, stillHidden) {
			t.Fatalf("show-archived revealed %q: %v", stillHidden, ids)
		}
	}
}
