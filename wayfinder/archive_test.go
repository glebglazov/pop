package wayfinder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
)

// realFSWithDataHome delegates every file operation to the real filesystem while
// routing the machine-global data dir at a temp location, so a test that opens
// pop.db never reaches the developer's store.
func realFSWithDataHome(dataHome string) deps.FileSystem {
	real := deps.NewRealFileSystem()
	return &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		GetwdFunc:        real.Getwd,
		UserHomeDirFunc:  real.UserHomeDir,
		StatFunc:         real.Stat,
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		RemoveAllFunc:    real.RemoveAll,
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}
}

// registryFixture lays Maps down on a real temp filesystem with the store
// redirected beside them. Registration and archival are registry rows in pop.db,
// which cannot ride the filesystem seam, so anything touching them is real-disk.
// Paths in files are relative to the repository's Task storage root.
func registryFixture(t *testing.T, files map[string]string) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	commonDir := filepath.Join(root, "repo", ".git")
	fs := realFSWithDataHome(filepath.Join(root, "xdg"))
	td := &tasks.Deps{
		FS: fs,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) { return commonDir, nil },
		},
	}
	t.Cleanup(func() { _ = td.CloseStore() })

	id, err := tasks.IdentityFromCommonDir(td, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(id.StorageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	trunk := filepath.Join(root, "repo")
	return &Deps{
		FS:    fs,
		Tasks: td,
		Trunk: func() (string, error) { return trunk, nil },
	}, id.StorageDir
}

// oneTicketMap is the smallest Map that registers: a destination plus one ticket
// its manifest names.
func oneTicketMap(id string) map[string]string {
	return map[string]string{
		"maps/" + id + "/map.md":             "Status: active\n\n## Destination\nShip it\n\n## Decisions so far\n- one decision",
		"maps/" + id + "/issues/01-first.md": "## Question\nWhy?\n",
		"maps/" + id + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","title":"First","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
}

func mustRegister(t *testing.T, d *Deps, mapID string) {
	t.Helper()
	if _, err := RegisterMap(d, "", mapID); err != nil {
		t.Fatalf("RegisterMap(%s): %v", mapID, err)
	}
}

func archivedInRegistry(t *testing.T, d *Deps, mapID string) bool {
	t.Helper()
	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	row, found, err := s.FindWorkContainer(MapRef(mapID))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("map %s has no registry row", mapID)
	}
	return row.Archived
}

func TestArchiveMapWritesRegistryBitAndRoundTrips(t *testing.T) {
	d, storageDir := registryFixture(t, oneTicketMap("2026-08-03-demo-map"))
	mapPath := filepath.Join(storageDir, "maps", "2026-08-03-demo-map", "map.md")
	original, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	mustRegister(t, d, "2026-08-03-demo-map")

	if _, err := ArchiveMap(d, "", "2026-08-03-demo-map"); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveMap(d, "", "2026-08-03-demo-map"); err != nil {
		t.Fatalf("second archive should be idempotent: %v", err)
	}
	if !archivedInRegistry(t, d, "2026-08-03-demo-map") {
		t.Fatal("archive did not set the registry bit")
	}
	after, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("archive must not mutate map.md")
	}

	snap, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 0 {
		t.Fatalf("default status should hide archived map: %+v", snap.Rows)
	}
	all, err := BuildStatus(d, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 1 {
		t.Fatalf("all status rows = %+v", all.Rows)
	}
	if !all.Rows[0].Archived || !strings.Contains(FormatStatusCell(all.Rows[0]), "[archived]") {
		t.Fatalf("archived label missing: %+v", all.Rows[0])
	}

	if _, err := UnarchiveMap(d, "", "2026-08-03-demo-map"); err != nil {
		t.Fatal(err)
	}
	if archivedInRegistry(t, d, "2026-08-03-demo-map") {
		t.Fatal("unarchive did not clear the registry bit")
	}
	restored, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Rows) != 1 || restored.Rows[0].ID != "2026-08-03-demo-map" {
		t.Fatalf("restored map missing from default status: %+v", restored.Rows)
	}
}

func TestArchiveRefusals(t *testing.T) {
	d, _ := registryFixture(t, oneTicketMap("2026-08-03-demo-map"))

	if _, err := ArchiveMap(d, "", "2026-08-03-demo-map"); err == nil {
		t.Fatal("expected error archiving an unregistered map")
	} else if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("error = %v", err)
	}
	mustRegister(t, d, "2026-08-03-demo-map")
	if _, err := UnarchiveMap(d, "", "2026-08-03-demo-map"); err == nil {
		t.Fatal("expected error unarchiving a non-archived map")
	} else if !strings.Contains(err.Error(), "not archived") {
		t.Fatalf("error = %v", err)
	}
	if _, err := ArchiveMap(d, "", "missing-map"); err == nil {
		t.Fatal("expected error archiving unknown map")
	} else if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
}

// TestLegacyArchiveStateFoldsIntoRegistry pins the read-path fold: the retired
// side-file's ids become registry rows carrying the archived bit, the file goes,
// and the Map stays hidden on every scan that follows.
func TestLegacyArchiveStateFoldsIntoRegistry(t *testing.T) {
	files := oneTicketMap("2026-07-04-archived")
	files["maps/2026-07-05-open/map.md"] = "Status: active\n\n## Destination\nStay visible\n"
	files[legacyArchiveStateFile] = `{"archived":["2026-07-04-archived"]}`
	d, storageDir := registryFixture(t, files)

	maps, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatalf("ScanMapsInStorage: %v", err)
	}
	byID := map[string]Map{}
	for _, m := range maps {
		byID[m.ID] = m
	}
	if !byID["2026-07-04-archived"].Archived {
		t.Fatalf("legacy archived map did not fold into the archived bit: %+v", maps)
	}
	if byID["2026-07-05-open"].Archived {
		t.Fatal("a map the side-file never named came back archived")
	}
	if !archivedInRegistry(t, d, "2026-07-04-archived") {
		t.Fatal("fold left no archived registry row")
	}
	if _, err := os.Stat(filepath.Join(storageDir, legacyArchiveStateFile)); !os.IsNotExist(err) {
		t.Fatalf("legacy archive file survived the fold: %v", err)
	}

	// The bit now lives where every other kind's does, so it survives a scan with
	// no file left to read it from.
	again, err := ScanMapsInStorage(d, storageDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range again {
		if m.ID == "2026-07-04-archived" && !m.Archived {
			t.Fatal("archived bit lost on the scan after the fold")
		}
	}

	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("fold registered %d containers, want only the archived one: %+v", len(rows), rows)
	}
}
