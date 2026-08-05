package wayfinder

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
)

// mapFanOutFixture lays out groupCount repository groups, each holding two active
// Maps, and returns the kind over them. It is the multi-group shape the per-group
// scan fans out over (ADR-0189); the single-group fixtures beside it cannot tell a
// fan-out from a loop.
func mapFanOutFixture(t *testing.T, groupCount int) *MapKind {
	t.Helper()
	files := map[string]string{}
	groups := make([]repogroup.Group, 0, groupCount)
	for i := 0; i < groupCount; i++ {
		storageDir := fmt.Sprintf("/data/repos/repo-%d", i)
		tasksDir := filepath.Join(storageDir, "tasks")
		for j := 0; j < 2; j++ {
			mapDir := filepath.Join(storageDir, "maps", fmt.Sprintf("2026-07-%02d-map-%d", j+1, i))
			files[filepath.Join(mapDir, "map.md")] = "Status: active\n\n## Destination\nShip it\n"
			files[filepath.Join(mapDir, "issues", "01-research.md")] = "Type: research\nStatus: open\n\n# Q\n"
		}
		groups = append(groups, repogroup.Group{
			DefPath:       tasksDir,
			StatePath:     tasks.StatePathFor(tasksDir),
			StorageDir:    storageDir,
			RepoKey:       fmt.Sprintf("repo-key-%d", i),
			RepoCommonDir: fmt.Sprintf("/repo-%d/.git", i),
			ProjectName:   fmt.Sprintf("proj-%d", i),
			Rep: &repogroup.Checkout{
				Name:        fmt.Sprintf("proj-%d", i),
				ProjectPath: fmt.Sprintf("/repo-%d/main", i),
				RuntimePath: fmt.Sprintf("/repo-%d/main", i),
			},
		})
	}
	wd := wayfinderTestDeps(t, t.TempDir(), "/repo/.git", files)
	return NewMapKind(&MapKindDeps{
		Wayfinder: wd,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return groups, nil },
	})
}

// TestMapKindConcurrentLoadMatchesSerialLoad pins the fan-out's contract on the
// Map side: scanning every group's storage at once must give the same rows, in
// the same order, as scanning them one after another.
func TestMapKindConcurrentLoadMatchesSerialLoad(t *testing.T) {
	k := mapFanOutFixture(t, 3)

	previous := runtime.GOMAXPROCS(1)
	serial, err := k.Load()
	runtime.GOMAXPROCS(previous)
	if err != nil {
		t.Fatalf("serial Load: %v", err)
	}

	runtime.GOMAXPROCS(8)
	concurrent, err := k.Load()
	runtime.GOMAXPROCS(previous)
	if err != nil {
		t.Fatalf("concurrent Load: %v", err)
	}

	if len(serial) != 6 {
		t.Fatalf("serial load returned %d containers, want the fixture's 6 — it is not exercising three groups", len(serial))
	}
	if !reflect.DeepEqual(serial, concurrent) {
		t.Fatalf("concurrent load differs from the serial one:\nserial     = %+v\nconcurrent = %+v", serial, concurrent)
	}
}
