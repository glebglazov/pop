package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestStateLockPathUsesXDGData(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return "/xdg/data"
			}
			return ""
		},
	}}
	got := StateLockPathWith(d)
	want := "/xdg/data/pop/tasks-state.lock"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestAcquireReleaseStateLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	lock, err := acquireStateLock(d, &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := StateLockPathWith(d)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta StateLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.PID != os.Getpid() || meta.StartedAt.IsZero() {
		t.Fatalf("metadata = %#v", meta)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present: %v", err)
	}
}

func TestStateLockRefusesLiveConcurrentUpdate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{
		FS:           deps.NewRealFileSystem(),
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
	}

	first, err := acquireStateLock(d, &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, err = acquireStateLock(d, &bytes.Buffer{}, false)
	if !errors.Is(err, ErrStateLockBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestStateLockRecoversStaleLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{
		FS:           deps.NewRealFileSystem(),
		ProcessAlive: func(int) bool { return false },
	}

	lockPath := StateLockPathWith(d)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := StateLockMetadata{
		PID:       999999,
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	payload, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(lockPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	lock, err := acquireStateLock(d, &notice, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if !strings.Contains(notice.String(), "stale task state lock") {
		t.Fatalf("notice = %q", notice.String())
	}
}

func TestStateLockRecoversMalformedLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	lockPath := StateLockPathWith(d)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	lock, err := acquireStateLock(d, &notice, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if !strings.Contains(notice.String(), "malformed task state lock") {
		t.Fatalf("notice = %q", notice.String())
	}
}

func TestUpdateGlobalStateRemovesLockAfterWrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	statePath := filepath.Join(root, "pop", "workloads-state.json")
	d := DefaultDeps()

	err := UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		state.Entry("/project/a").TaskSets = append(state.Entry("/project/a").TaskSets, RegisteredTaskSet{ID: "a", Priority: 0})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	lockPath := StateLockPathWith(d)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present: %v", err)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks["/project/a"].TaskSets) != 1 {
		t.Fatalf("state = %#v", state.Tasks["/project/a"])
	}
}

func TestUpdateGlobalStateMergePreservesOtherProjects(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	statePath := filepath.Join(root, "state.json")
	d := DefaultDeps()

	initial := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{
			"/project/a": {TaskSets: []RegisteredTaskSet{{ID: "keep", Priority: 5}}},
		},
		path: statePath,
	}
	if err := initial.SaveWith(d); err != nil {
		t.Fatal(err)
	}

	err := UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		state.Entry("/project/b").TaskSets = append(state.Entry("/project/b").TaskSets, RegisteredTaskSet{ID: "new", Priority: 3})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	a := state.Tasks["/project/a"]
	if a == nil || len(a.TaskSets) != 1 || a.TaskSets[0].ID != "keep" || a.TaskSets[0].Priority != 5 {
		t.Fatalf("project a = %#v", a)
	}
	b := state.Tasks["/project/b"]
	if b == nil || len(b.TaskSets) != 1 || b.TaskSets[0].ID != "new" {
		t.Fatalf("project b = %#v", b)
	}
}

func TestMigrateLegacyStateRefusesCorruptState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	// A surviving per-repo state.json with an unsupported version is surfaced by
	// the fold run on first load, not silently dropped.
	legacyPath := StatePathFor(filepath.Join(t.TempDir(), "tasks"))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"version":99}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadGlobalStateWith(d, legacyPath)
	if err == nil {
		t.Fatal("expected corrupt state error")
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("err = %v", err)
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"version":99}` {
		t.Fatalf("legacy state overwritten or removed: %q", data)
	}
}

func TestMigrateLegacyStateRefusesMalformedState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	legacyPath := StatePathFor(filepath.Join(t.TempDir(), "tasks"))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadGlobalStateWith(d, legacyPath)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentDistinctProjectUpdates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	statePath := filepath.Join(root, "pop", "workloads-state.json")
	d := DefaultDeps()

	defA := filepath.Join(root, "project-a")
	defB := filepath.Join(root, "project-b")
	setupManifest(t, defA, "a-feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, defB, "b-feature", []Task{
		{ID: "01-b", File: "01-b.md", Title: "B", Type: "AFK", Status: "open"},
	})

	canonA, err := CanonicalDefinitionPath(defA)
	if err != nil {
		t.Fatal(err)
	}
	canonB, err := CanonicalDefinitionPath(defB)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	run := func(defPath string) {
		defer wg.Done()
		disc, err := DiscoverWith(d, defPath)
		if err != nil {
			errs <- err
			return
		}
		err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
			mergeNewRegistrations(d, defPath, disc, state, nil)
			return nil
		})
		errs <- err
	}

	wg.Add(2)
	go run(canonA)
	go run(canonB)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks[canonA].TaskSets) != 1 || state.Tasks[canonA].TaskSets[0].ID != "a-feature" {
		t.Fatalf("project a = %#v", state.Tasks[canonA])
	}
	if len(state.Tasks[canonB].TaskSets) != 1 || state.Tasks[canonB].TaskSets[0].ID != "b-feature" {
		t.Fatalf("project b = %#v", state.Tasks[canonB])
	}
}

func TestRefreshConcurrentRegistrationAndPriority(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	defA := filepath.Join(root, "project-a")
	defB := filepath.Join(root, "project-b")
	setupManifest(t, defA, "a-feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, defB, "b-feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	d := DefaultDeps()
	canonA, err := CanonicalDefinitionPath(defA)
	if err != nil {
		t.Fatal(err)
	}
	canonB, err := CanonicalDefinitionPath(defB)
	if err != nil {
		t.Fatal(err)
	}
	// defA and defB share the same parent, so both resolve to one per-repository
	// state.json — the file the priority update and the registration both target.
	statePath := StatePathFor(canonA)

	seed := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{
			canonB: {TaskSets: []RegisteredTaskSet{{ID: "b-feature", Priority: 0}}},
		},
		path: statePath,
	}
	if err := seed.SaveWith(d); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := RegisterWith(d, defA, statePath)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := SetPriorityWith(d, nil, nil, ResolveInput{DefinitionOverride: defB}, "b-feature", 42)
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks[canonA].TaskSets) != 1 || state.Tasks[canonA].TaskSets[0].ID != "a-feature" {
		t.Fatalf("project a = %#v", state.Tasks[canonA])
	}
	if len(state.Tasks[canonB].TaskSets) != 1 || state.Tasks[canonB].TaskSets[0].Priority != 42 {
		t.Fatalf("project b = %#v", state.Tasks[canonB])
	}
}

func TestRefreshEmptyInspectionDoesNotCreateStateOrLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	statePath := DefaultStatePath()
	d := DefaultDeps()

	result, err := RefreshWith(d, root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("rows = %d", len(result.Rows))
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("expected no state file")
	}
	if _, err := os.Stat(StateLockPathWith(d)); !os.IsNotExist(err) {
		t.Fatal("expected no state lock file")
	}
}

func TestRefreshReadOnlyDoesNotRewriteExistingState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	setupManifest(t, root, "existing", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	seed := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{
			canon: {TaskSets: []RegisteredTaskSet{{ID: "existing", Priority: 0}}},
		},
		path: statePath,
	}
	if err := seed.SaveWith(d); err != nil {
		t.Fatal(err)
	}
	before := registeredTaskSetsFor(t, d, canon)

	result, err := RefreshWith(d, root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 || result.NeedsSave {
		t.Fatalf("unexpected mutation: %#v", result)
	}

	after := registeredTaskSetsFor(t, d, canon)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state rewritten:\nbefore=%#v\nafter=%#v", before, after)
	}
}
