package tasks

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// laterProcess is what a second `pop work dashboard` sees: the same machine —
// the same files, the same cache database — with none of the process state the
// first one accumulated. The in-process tier is swapped cold for the caller too,
// since a memo that survived would answer before the persisted one is asked.
func laterProcess(t *testing.T, d *Deps) *Deps {
	t.Helper()
	if err := d.CloseCacheDB(); err != nil {
		t.Fatalf("close the first process's cache handle: %v", err)
	}
	next := *d
	next.cacheDB = nil
	t.Cleanup(func() { _ = next.CloseCacheDB() })
	withManifestMemo(t, manifestMemoCapacity)
	return &next
}

// withoutPersistedTier blocks d's cache database with a regular file where the
// cache directory has to go, so the test sees the in-process tier alone. It is
// the degradation a human gets from an unwritable cache directory, not a test-only
// switch (ADR-0243 decision 4).
func withoutPersistedTier(t *testing.T, d *Deps) {
	t.Helper()
	cacheDir := popCacheDirWith(d)
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d.CacheDB() != nil {
		t.Fatal("the cache database opened where its directory cannot exist")
	}
}

// persistedRows reports how many rows the manifest table holds in total, and the
// content key stored for dir.
func persistedRows(t *testing.T, d *Deps, dir string) (int, string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+CacheDBPathWith(d))
	if err != nil {
		t.Fatalf("open the cache database: %v", err)
	}
	defer func() { _ = db.Close() }()
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM manifest_entries`).Scan(&rows); err != nil {
		t.Fatalf("count manifest rows: %v", err)
	}
	var key string
	err = db.QueryRow(`SELECT content_key FROM manifest_entries WHERE dir = ?`, dir).Scan(&key)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("read the content key for %s: %v", dir, err)
	}
	return rows, key
}

// The persisted tier's whole point: the process that pays for validation is not
// the process that benefits. A later run is served the same manifest, down to
// its validation errors, without opening a task markdown.
func TestPersistedManifestTierServesALaterProcess(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setDir := filepath.Join(root, "demo")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "done", BlockedBy: []string{"01-a"}},
	})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(setDir, ManifestFileName)

	fresh := LoadManifest(d, "demo", manifestPath)
	if !fresh.Valid {
		t.Fatalf("first load invalid: %v", fresh.Errors)
	}
	coldReads := counting.mdReads
	if coldReads != 2 {
		t.Fatalf("markdown reads on the cold load = %d, want one per task (2)", coldReads)
	}

	later := laterProcess(t, d)
	served := LoadManifest(later, "demo", manifestPath)
	if counting.mdReads != coldReads {
		t.Fatalf("markdown reads in the later process = %d, want the persisted tier to serve it (%d)", counting.mdReads, coldReads)
	}
	if !reflect.DeepEqual(fresh, served) {
		t.Fatalf("served manifest differs from the loaded one:\n served %#v\n fresh  %#v", served, fresh)
	}

	// Mutating what the persisted tier hands out must not reach the next reader,
	// exactly as it must not for a process-tier hit.
	served.Tasks[0].Status = TaskDone
	served.HumanCompleted = true
	served.Errors = append(served.Errors, "invented")
	third := laterProcess(t, later)
	again := LoadManifest(third, "demo", manifestPath)
	if !reflect.DeepEqual(fresh, again) {
		t.Fatalf("a caller's mutation leaked into the persisted entry: %#v", again)
	}
	if counting.mdReads != coldReads {
		t.Fatalf("markdown reads after a third process = %d, want %d", counting.mdReads, coldReads)
	}
}

// A malformed set is an answer like any other, and the tier owes it the same
// fidelity: the errors and the invalidity are what the surface renders.
func TestPersistedManifestTierServesAMalformedSetFaithfully(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setDir := filepath.Join(root, "demo")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeTaskMD(t, setDir, "01-a.md", "# A\n\nnothing a validator would accept\n")
	writeManifestWithSetKeys(t, setDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"source_map": "2026-08-01-a-map", "worktree": "old-name", "invented_key": 7})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(setDir, ManifestFileName)

	fresh := LoadManifest(d, "demo", manifestPath)
	if fresh.Valid || len(fresh.Errors) == 0 {
		t.Fatalf("fixture is not malformed: valid=%v errors=%v", fresh.Valid, fresh.Errors)
	}
	if fresh.SourceMap == "" || len(fresh.Unknown) == 0 || len(fresh.DeprecatedKeys) == 0 {
		t.Fatalf("fixture exercises too little of the manifest: %#v", fresh)
	}
	reads := counting.mdReads

	served := LoadManifest(laterProcess(t, d), "demo", manifestPath)
	if counting.mdReads != reads {
		t.Fatalf("markdown reads in the later process = %d, want %d", counting.mdReads, reads)
	}
	if !reflect.DeepEqual(fresh, served) {
		t.Fatalf("served malformed manifest differs:\n served %#v\n fresh  %#v", served, fresh)
	}
}

// The tier holds one row per set directory and re-validates the moment the
// directory moves under it — an edited markdown, a markdown nobody listed. Both
// are inputs the manifest's own bytes say nothing about, and both are what the
// content key is for.
func TestPersistedManifestTierInvalidatesOnEveryDirectoryChange(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setDir := filepath.Join(root, "demo")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(setDir, ManifestFileName)

	if m := LoadManifest(d, "demo", manifestPath); !m.Valid {
		t.Fatalf("first load invalid: %v", m.Errors)
	}
	reads := counting.mdReads
	rows, firstKey := persistedRows(t, d, setDir)
	if rows != 1 || firstKey == "" {
		t.Fatalf("after one set: %d rows, key %q — want one row carrying its key", rows, firstKey)
	}

	// Edited in place: same file, different content.
	writeTaskMD(t, setDir, "01-a.md", "# A\n\nno acceptance criteria anywhere in this file\n")
	edited := LoadManifest(laterProcess(t, d), "demo", manifestPath)
	if edited.Valid {
		t.Fatalf("the persisted tier served a stale valid manifest after an edit")
	}
	if len(edited.Errors) != 1 || !strings.Contains(edited.Errors[0], "missing acceptance criteria section") {
		t.Fatalf("errors after the edit = %v", edited.Errors)
	}
	if counting.mdReads <= reads {
		t.Fatalf("markdown reads after the edit = %d, want the entry re-validated (>%d)", counting.mdReads, reads)
	}
	reads = counting.mdReads
	rows, editedKey := persistedRows(t, d, setDir)
	if rows != 1 {
		t.Fatalf("rows after re-validating the same directory = %d, want the row replaced (1)", rows)
	}
	if editedKey == firstKey {
		t.Fatalf("the stored content key did not move with the directory: %q", editedKey)
	}

	// A markdown the manifest never names is the other way a set turns MALFORMED
	// without a byte of index.json changing.
	writeTaskMD(t, setDir, "02-unlisted.md", "## Acceptance criteria\n\n- [ ] ok\n")
	orphaned := LoadManifest(laterProcess(t, d), "demo", manifestPath)
	if orphaned.Valid || !strings.Contains(strings.Join(orphaned.Errors, "\n"), "02-unlisted.md: no manifest entry") {
		t.Fatalf("adding an unlisted markdown was served stale: valid=%v errors=%v", orphaned.Valid, orphaned.Errors)
	}
	if counting.mdReads <= reads {
		t.Fatalf("markdown reads after the orphan appeared = %d, want a re-validation (>%d)", counting.mdReads, reads)
	}

	// And removing it again: the name set invalidates in both directions.
	if err := os.Remove(filepath.Join(setDir, "02-unlisted.md")); err != nil {
		t.Fatal(err)
	}
	restored := LoadManifest(laterProcess(t, d), "demo", manifestPath)
	if len(restored.Errors) != 1 || !strings.Contains(restored.Errors[0], "missing acceptance criteria section") {
		t.Fatalf("errors after removing the orphan = %v, want only the edited markdown's", restored.Errors)
	}
	if rows, _ := persistedRows(t, d, setDir); rows != 1 {
		t.Fatalf("rows after four validations of one directory = %d, want 1", rows)
	}
}

// A cache that cannot be opened is a cache that misses, at every point where a
// tier would have been consulted: the answers stay right and every load pays the
// full validation, which is precisely the behaviour of no persisted tier at all.
func TestManifestLoadDegradesWhenTheCacheIsUnusable(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, counting := countingDeps(t, root)
	withoutPersistedTier(t, d)
	manifestPath := filepath.Join(root, "demo", ManifestFileName)

	fresh := LoadManifest(d, "demo", manifestPath)
	if !fresh.Valid {
		t.Fatalf("load without a cache invalid: %v", fresh.Errors)
	}
	if counting.mdReads != 1 {
		t.Fatalf("markdown reads = %d, want 1", counting.mdReads)
	}

	served := LoadManifest(laterProcess(t, d), "demo", manifestPath)
	if !reflect.DeepEqual(fresh, served) {
		t.Fatalf("load without a cache differs across processes:\n %#v\n %#v", served, fresh)
	}
	if counting.mdReads != 2 {
		t.Fatalf("markdown reads across two cacheless processes = %d, want the validation re-run (2)", counting.mdReads)
	}
}

// The stored shape is a hand-written mirror of the loaded one, so a field added
// to Manifest or Task is a field this tier silently drops until someone adds it
// here too. This is that someone.
func TestPersistedManifestMirrorsEveryLoadedField(t *testing.T) {
	t.Parallel()
	sameFields := func(loaded, stored any) []string {
		var missing []string
		storedType := reflect.TypeOf(stored)
		fields := map[string]bool{}
		for i := 0; i < storedType.NumField(); i++ {
			fields[storedType.Field(i).Name] = true
		}
		loadedType := reflect.TypeOf(loaded)
		for i := 0; i < loadedType.NumField(); i++ {
			if name := loadedType.Field(i).Name; !fields[name] {
				missing = append(missing, name)
			}
		}
		return missing
	}
	if missing := sameFields(Manifest{}, persistedManifest{}); missing != nil {
		t.Fatalf("persistedManifest does not carry %v", missing)
	}
	if missing := sameFields(Task{}, persistedTask{}); missing != nil {
		t.Fatalf("persistedTask does not carry %v", missing)
	}
}

// A round trip through the stored shape has to return the manifest that went in,
// including the distinctions Task's authored-manifest JSON deliberately drops —
// an effort that was written down against one that defaulted — and the nil-ness
// of every slice and map, which is what makes a served manifest compare equal to
// a freshly loaded one.
func TestPersistedManifestRoundTripsEveryDistinction(t *testing.T) {
	t.Parallel()
	failedAfter := 3
	full := &Manifest{
		Stem: "demo", Dir: "/sets/demo", Path: "/sets/demo/index.json",
		Tasks: []Task{
			{
				ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: TaskOpen,
				BlockedBy: []string{"00-x"}, FailedAfter: &failedAfter,
				Effort: DefaultTaskEffort, EffortExplicit: true,
				Origin: "human", Commit: &TaskCommit{SHA: "abc", Subject: "feat: a"},
				CommitSubject: "feat: a",
			},
			{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: TaskDone, Effort: DefaultTaskEffort},
		},
		Raw:     json.RawMessage(`{"tasks":[]}`),
		Errors:  []string{"one"},
		Valid:   false,
		Unknown: map[string]json.RawMessage{"invented": json.RawMessage(`7`)},
		SourceMap: "2026-08-01-a-map", BaseCommit: "", BaseCommitRecorded: true,
		CommitConvention: "match the log", HumanCompleted: true,
		DeprecatedKeys: []string{"worktree"},
	}

	payload, err := json.Marshal(newPersistedManifest(full))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var record persistedManifest
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := record.manifest(); !reflect.DeepEqual(got, full) {
		t.Fatalf("round trip lost something:\n got  %#v\n want %#v", got, full)
	}

	// The empty manifest is the nil-ness case: every slice and map absent, and an
	// absent one must not come back as an empty one.
	empty := &Manifest{}
	payload, err = json.Marshal(newPersistedManifest(empty))
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	record = persistedManifest{}
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if got := record.manifest(); !reflect.DeepEqual(got, empty) {
		t.Fatalf("an empty manifest did not round trip: %#v", got)
	}
}
