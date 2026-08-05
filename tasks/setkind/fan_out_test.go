package setkind

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
)

// fanOutFixture lays out groupCount repository groups, each with setsPerGroup
// registered task sets on disk, and returns a kind wired to read them for real —
// no injected Refresh, so the load runs the discovery and manifest pass the
// fan-out is about. The clock is fixed because a snapshot compared against
// another must not differ by the time between them.
func fanOutFixture(t *testing.T, groupCount, setsPerGroup int) *Deps {
	t.Helper()
	td := realDataDeps(t)
	groups := make([]repogroup.Group, 0, groupCount)
	for i := 0; i < groupCount; i++ {
		defPath := filepath.Join(t.TempDir(), "storage", "tasks")
		for j := 0; j < setsPerGroup; j++ {
			// Set ids are unique across groups because the registry keys a Task set by
			// its id alone: two repositories sharing a stem share one registration row.
			stem := fmt.Sprintf("2026-01-%02d-group-%d-set-%d", j+1, i, j+1)
			setDir := filepath.Join(defPath, stem)
			queuetest.WriteSpawnTaskMD(t, setDir, "01-a.md")
			queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{
				{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
			})
		}
		if _, err := tasks.RegisterWith(td, defPath, tasks.StatePathFor(defPath)); err != nil {
			t.Fatalf("RegisterWith(%s): %v", defPath, err)
		}
		groups = append(groups, staticForScan(scanFixture{
			Name:           fmt.Sprintf("proj-%d", i),
			ProjectPath:    filepath.Dir(filepath.Dir(defPath)),
			DefinitionPath: defPath,
			RepoKey:        fmt.Sprintf("repo-key-%d", i),
			RepoCommonDir:  filepath.Join(filepath.Dir(filepath.Dir(defPath)), ".git"),
		}, "main", false))
	}
	fixed := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	return &Deps{
		Tasks:   td,
		Project: &project.Deps{FS: td.FS, Git: td.Git},
		Config:  &config.Config{},
		Groups:  func() ([]repogroup.Group, error) { return groups, nil },
		Now:     func() time.Time { return fixed },
	}
}

// realDataDeps returns task deps over the real filesystem with the store
// isolated in a temp data dir, since the fixture's sets are real files a real
// refresh has to read.
func realDataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		GetwdFunc:        real.Getwd,
		UserHomeDirFunc:  func() (string, error) { return dir, nil },
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
	d.Git = &deps.MockGit{}
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}

// TestConcurrentLoadMatchesSerialLoad is the fan-out's whole contract (ADR-0189):
// running the groups at once may make a load faster and must make it no
// different. GOMAXPROCS pinned to one is the serial loop the fan-out replaced —
// same rows, same order, same cells.
func TestConcurrentLoadMatchesSerialLoad(t *testing.T) {
	kind := New(fanOutFixture(t, 4, 3))

	previous := runtime.GOMAXPROCS(1)
	serial, err := kind.Load()
	runtime.GOMAXPROCS(previous)
	if err != nil {
		t.Fatalf("serial Load: %v", err)
	}

	runtime.GOMAXPROCS(8)
	concurrent, err := kind.Load()
	runtime.GOMAXPROCS(previous)
	if err != nil {
		t.Fatalf("concurrent Load: %v", err)
	}

	if len(serial) != 12 {
		t.Fatalf("serial load returned %d containers, want the fixture's 12 — it is not exercising four groups", len(serial))
	}
	if !reflect.DeepEqual(serial, concurrent) {
		t.Fatalf("concurrent load differs from the serial one:\nserial     = %+v\nconcurrent = %+v", serial, concurrent)
	}
	projects := map[string]int{}
	for _, c := range concurrent {
		projects[c.Project]++
	}
	if len(projects) != 4 {
		t.Fatalf("containers span %d projects, want 4: %v", len(projects), projects)
	}
}

// TestGroupLoadsOverlap pins that the per-group read really does run beside its
// neighbours rather than merely producing the same answer: each group's refresh
// waits for a second one to join it, which a serial loop can never satisfy.
func TestGroupLoadsOverlap(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)

	var (
		mu       sync.Mutex
		live     int
		peak     int
		overlap  = make(chan struct{})
		joined   sync.Once
		timedOut bool
	)
	d := testDeps(t, []tasks.Row{{ID: "demo", Status: tasks.StatusReady}})
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		mu.Lock()
		live++
		if live > peak {
			peak = live
		}
		if live >= 2 {
			joined.Do(func() { close(overlap) })
		}
		mu.Unlock()
		select {
		case <-overlap:
		case <-time.After(10 * time.Second):
			mu.Lock()
			timedOut = true
			mu.Unlock()
		}
		mu.Lock()
		live--
		mu.Unlock()
		return &tasks.RefreshResult{Rows: []tasks.Row{{ID: "demo", Status: tasks.StatusReady}}}, nil
	}
	groups := []repogroup.Group{}
	for i := 0; i < 4; i++ {
		groups = append(groups, staticForScan(scanFixture{
			Name:           fmt.Sprintf("proj-%d", i),
			ProjectPath:    fmt.Sprintf("/repo-%d/main", i),
			DefinitionPath: fmt.Sprintf("/def-%d", i),
			RepoKey:        fmt.Sprintf("repo-key-%d", i),
			RepoCommonDir:  fmt.Sprintf("/repo-%d/.git", i),
		}, "main", false))
	}
	d.Groups = func() ([]repogroup.Group, error) { return groups, nil }

	if _, err := New(d).Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if timedOut {
		t.Fatal("no two group reads were ever in flight at once — the per-group load is running serially")
	}
	if peak > 4 {
		t.Fatalf("peak concurrent group reads = %d, want at most GOMAXPROCS (4)", peak)
	}
}
