package wayfinder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func wayfinderWritableTestDeps(t *testing.T, dataHome, commonDir string, files map[string]string) *Deps {
	t.Helper()
	d := wayfinderTestDeps(t, dataHome, commonDir, files)
	d.FS.(*deps.MockFileSystem).WriteFileFunc = func(path string, data []byte, perm os.FileMode) error {
		files[path] = string(data)
		return nil
	}
	return d
}

func TestArchiveMapPersistsAndIsIdempotent(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "demo-map")
	mapPath := filepath.Join(mapDir, "map.md")
	original := "Status: active\n\n## Destination\nShip it\n\n## Decisions so far\n- one decision"
	files := map[string]string{
		mapPath: original,
	}
	d := wayfinderWritableTestDeps(t, dataHome, commonDir, files)

	if _, err := ArchiveMap(d, "", "demo-map"); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveMap(d, "", "demo-map"); err != nil {
		t.Fatalf("second archive should be idempotent: %v", err)
	}

	statePath := filepath.Join(id.StorageDir, archiveStateFile)
	raw, ok := files[statePath]
	if !ok {
		t.Fatalf("archive state not written at %s", statePath)
	}
	var state archiveState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Archived) != 1 || state.Archived[0] != "demo-map" {
		t.Fatalf("archived = %+v", state.Archived)
	}
	if files[mapPath] != original {
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

	if _, err := UnarchiveMap(d, "", "demo-map"); err != nil {
		t.Fatal(err)
	}
	after, err := BuildStatus(d, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Rows) != 1 || after.Rows[0].ID != "demo-map" {
		t.Fatalf("restored map missing from default status: %+v", after.Rows)
	}
}

func TestUnarchiveUnknownAndNotArchivedErrors(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "demo-map")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"): "## Destination\nShip it",
	}
	d := wayfinderWritableTestDeps(t, dataHome, commonDir, files)

	if _, err := UnarchiveMap(d, "", "demo-map"); err == nil {
		t.Fatal("expected error unarchiving non-archived map")
	} else if !strings.Contains(err.Error(), "not archived") {
		t.Fatalf("error = %v", err)
	}
	if _, err := ArchiveMap(d, "", "missing-map"); err == nil {
		t.Fatal("expected error archiving unknown map")
	} else if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
}
