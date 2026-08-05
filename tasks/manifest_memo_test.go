package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// countingSetFS counts the reads and stats a manifest load makes, so a test can
// tell "served from the memo" apart from "validated again": the validation's cost
// is a full read of every task markdown, so a markdown read is the observable
// fact, not a timing.
type countingSetFS struct {
	deps.FileSystem
	root      string
	mdReads   int
	fileReads map[string]int
	stats     map[string]int
}

func newCountingSetFS(inner deps.FileSystem, root string) *countingSetFS {
	return &countingSetFS{
		FileSystem: inner,
		root:       root,
		fileReads:  map[string]int{},
		stats:      map[string]int{},
	}
}

func (fs *countingSetFS) ReadFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, fs.root) {
		fs.fileReads[path]++
		if strings.HasSuffix(path, ".md") {
			fs.mdReads++
		}
	}
	return fs.FileSystem.ReadFile(path)
}

func (fs *countingSetFS) Stat(path string) (os.FileInfo, error) {
	fs.stats[path]++
	return fs.FileSystem.Stat(path)
}

// withManifestMemo swaps the process memo for a fresh one of the given capacity,
// so a test starts cold and cannot be served an entry another test's fixture put
// there. Tests using it must not run in parallel: the memo is process-wide by
// design.
func withManifestMemo(t *testing.T, capacity int) *deps.ContentMemo[*Manifest] {
	t.Helper()
	previous := manifestMemo
	memo := deps.NewContentMemo[*Manifest](capacity)
	manifestMemo = memo
	t.Cleanup(func() { manifestMemo = previous })
	return memo
}

func countingDeps(t *testing.T, root string) (*Deps, *countingSetFS) {
	t.Helper()
	d := newTestDeps(t)
	counting := newCountingSetFS(d.FS, root)
	d.FS = counting
	return d, counting
}

// TestManifestMemoServesUntouchedSetAndInvalidatesOnEdit is the memo's whole
// contract in one path: an untouched set is validated once however many times it
// is walked, and an edited task markdown — the input the manifest's own bytes say
// nothing about — is not served from the entry it invalidates.
func TestManifestMemoServesUntouchedSetAndInvalidatesOnEdit(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(root, "demo", ManifestFileName)

	first := LoadManifest(d, "demo", manifestPath)
	if !first.Valid {
		t.Fatalf("first load invalid: %v", first.Errors)
	}
	if counting.mdReads != 1 {
		t.Fatalf("markdown reads on first load = %d, want 1", counting.mdReads)
	}

	second := LoadManifest(d, "demo", manifestPath)
	if !second.Valid {
		t.Fatalf("second load invalid: %v", second.Errors)
	}
	if counting.mdReads != 1 {
		t.Fatalf("markdown reads after repeat load = %d, want the memo to serve it (1)", counting.mdReads)
	}
	if second == first {
		t.Fatalf("memo served the same manifest pointer; a caller's mutation would leak into the entry")
	}
	if len(second.Tasks) != 1 || second.Tasks[0].ID != "01-a" || second.Tasks[0].Effort != DefaultTaskEffort {
		t.Fatalf("served manifest differs from the loaded one: %#v", second.Tasks)
	}

	// Mutating what a hit returns must not reach the entry: the next reader asks
	// about the file, not about the last caller's transition.
	second.Tasks[0].Status = TaskDone
	second.HumanCompleted = true
	third := LoadManifest(d, "demo", manifestPath)
	if third.Tasks[0].Status != TaskOpen || third.HumanCompleted {
		t.Fatalf("mutation leaked into the memo: status %q human_completed %v", third.Tasks[0].Status, third.HumanCompleted)
	}

	// The markdown loses its acceptance-criteria section. Nothing in the manifest
	// changed, so only the per-file stamp can catch this.
	writeTaskMD(t, filepath.Join(root, "demo"), "01-a.md", "# A\n\nno criteria here at all\n")
	edited := LoadManifest(d, "demo", manifestPath)
	if edited.Valid {
		t.Fatalf("edited markdown served a stale valid manifest")
	}
	if len(edited.Errors) != 1 || !strings.Contains(edited.Errors[0], "missing acceptance criteria section") {
		t.Fatalf("errors after edit = %v", edited.Errors)
	}
	if counting.mdReads != 2 {
		t.Fatalf("markdown reads after edit = %d, want the entry invalidated (2)", counting.mdReads)
	}
}

// TestManifestMemoOrphanMarkdownFlipsSetMalformed pins the reason the key covers
// the directory's names and not just the manifest: writing a slice nobody listed
// is the one way a set turns MALFORMED without a byte of the manifest changing.
func TestManifestMemoOrphanMarkdownFlipsSetMalformed(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	setDir := filepath.Join(root, "demo")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, _ := countingDeps(t, root)
	manifestPath := filepath.Join(setDir, ManifestFileName)

	if m := LoadManifest(d, "demo", manifestPath); !m.Valid {
		t.Fatalf("first load invalid: %v", m.Errors)
	}

	writeTaskMD(t, setDir, "02-unlisted.md", "## Acceptance criteria\n\n- [ ] ok\n")

	after := LoadManifest(d, "demo", manifestPath)
	if after.Valid {
		t.Fatalf("orphan markdown served a stale valid manifest")
	}
	if len(after.Errors) != 1 || !strings.Contains(after.Errors[0], "02-unlisted.md: no manifest entry") {
		t.Fatalf("errors = %v", after.Errors)
	}

	// And removing it again returns the set to READY rather than pinning the
	// MALFORMED answer: the name set is part of the key in both directions.
	if err := os.Remove(filepath.Join(setDir, "02-unlisted.md")); err != nil {
		t.Fatal(err)
	}
	if m := LoadManifest(d, "demo", manifestPath); !m.Valid {
		t.Fatalf("load after removing the orphan invalid: %v", m.Errors)
	}
}

// TestManifestMemoBoundEvictsLeastRecentlyWalkedSet pins the bound the daemon
// depends on. The observable is the same as everywhere else: an evicted set
// re-reads its markdown, a resident one does not.
func TestManifestMemoBoundEvictsLeastRecentlyWalkedSet(t *testing.T) {
	if manifestMemo.Capacity() != manifestMemoCapacity || manifestMemoCapacity <= 0 {
		t.Fatalf("production memo capacity = %d, want the bounded %d", manifestMemo.Capacity(), manifestMemoCapacity)
	}

	memo := withManifestMemo(t, 2)
	root := t.TempDir()
	for _, stem := range []string{"a", "b", "c"} {
		setupManifest(t, root, stem, []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
	}
	d, counting := countingDeps(t, root)
	load := func(stem string) {
		t.Helper()
		if m := LoadManifest(d, stem, filepath.Join(root, stem, ManifestFileName)); !m.Valid {
			t.Fatalf("load %s invalid: %v", stem, m.Errors)
		}
	}

	load("a")
	load("b")
	load("c")
	if memo.Len() != 2 {
		t.Fatalf("memo holds %d entries at capacity 2", memo.Len())
	}
	if counting.mdReads != 3 {
		t.Fatalf("markdown reads for three cold sets = %d, want 3", counting.mdReads)
	}

	// "a" aged out, so it validates again; "c" is still resident.
	load("a")
	if counting.mdReads != 4 {
		t.Fatalf("markdown reads after re-walking the evicted set = %d, want 4", counting.mdReads)
	}
	load("c")
	if counting.mdReads != 4 {
		t.Fatalf("resident set re-validated: markdown reads = %d, want 4", counting.mdReads)
	}
}

// TestRepeatedDefinitionWalkValidatesOnce is the poll, in miniature: the second
// walk of an unchanged definition path reads no task markdown at all. It also
// pins where the memo sits — at the manifest load, below the refresh — by showing
// the refresh's own impure prelude, the storage-layout migration probe, still runs
// on every walk.
func TestRepeatedDefinitionWalkValidatesOnce(t *testing.T) {
	withManifestMemo(t, 8)
	root := t.TempDir()
	for _, stem := range []string{"one", "two"} {
		setupManifest(t, root, stem, []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
			{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
		})
	}
	d := newTestDeps(t)
	canon, err := CanonicalDefinitionPathWith(d, root)
	if err != nil {
		t.Fatalf("CanonicalDefinitionPathWith: %v", err)
	}
	counting := newCountingSetFS(d.FS, canon)
	d.FS = counting
	statePath := filepath.Join(root, "state.json")

	if _, err := RefreshWith(d, root, statePath); err != nil {
		t.Fatal(err)
	}
	if counting.mdReads != 4 {
		t.Fatalf("markdown reads on the first walk = %d, want one per task (4)", counting.mdReads)
	}
	// The probe MigrateStorageLayout makes on every refresh: the storage directory
	// the tasks directory lives in.
	storageDir := filepath.Dir(canon)
	firstWalkProbes := counting.stats[storageDir]
	if firstWalkProbes == 0 {
		t.Fatalf("migration never probed %s; the fixture cannot see the refresh's own work", storageDir)
	}

	result, err := RefreshWith(d, root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if counting.mdReads != 4 {
		t.Fatalf("markdown reads after re-walking an unchanged path = %d, want the memo to serve both sets (4)", counting.mdReads)
	}
	if counting.stats[storageDir] <= firstWalkProbes {
		t.Fatalf("migration probes = %d after two walks, want the refresh above the memo to run again", counting.stats[storageDir])
	}
	if len(result.Manifests) != 2 {
		t.Fatalf("second walk manifests = %d, want 2", len(result.Manifests))
	}
	for stem, m := range result.Manifests {
		if !m.Valid || m.Stem != stem || len(m.Tasks) != 2 {
			t.Fatalf("served manifest %s: valid=%v stem=%q tasks=%d", stem, m.Valid, m.Stem, len(m.Tasks))
		}
	}
}
