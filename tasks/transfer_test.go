package tasks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

type transferEnv struct {
	root     string
	worktree string
	dataHome string
	tasksDir string
	deps     *Deps
}

func newTransferEnv(t *testing.T) *transferEnv {
	t.Helper()
	root := t.TempDir()
	worktree := filepath.Join(root, "repo")
	commonDir := filepath.Join(worktree, ".git")
	dataHome := filepath.Join(root, "data")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)
	id, err := ResolveRepositoryIdentity(d, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStorage(d, id); err != nil {
		t.Fatal(err)
	}
	return &transferEnv{
		root:     root,
		worktree: worktree,
		dataHome: dataHome,
		tasksDir: id.TasksDir,
		deps:     d,
	}
}

func (e *transferEnv) writeSet(t *testing.T, id string, mutate func(dir string)) {
	t.Helper()
	dir := filepath.Join(e.tasksDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(dir)
	}
}

func (e *transferEnv) resolveInput() ResolveInput {
	return ResolveInput{CWD: e.worktree}
}

func TestExportImportRoundtrip(t *testing.T) {
	src := newTransferEnv(t)
	const setID = "2026-06-01-user-auth"
	src.writeSet(t, setID, func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "progress.txt"), []byte("history\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	exported, err := ExportWith(src.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: src.resolveInput(),
		TaskSetIDs:   []string{setID},
		OutputPath:   filepath.Join(src.root, setID+".tar.gz"),
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(exported.Path); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	dst := newTransferEnv(t)
	imported, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  exported.Path,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Sets) != 1 {
		t.Fatalf("imported %d sets, want 1: %#v", len(imported.Sets), imported.Sets)
	}
	if imported.Sets[0].TaskSetID != setID {
		t.Fatalf("imported id = %q, want %q", imported.Sets[0].TaskSetID, setID)
	}
	if canonical(t, imported.Sets[0].Path) != canonical(t, filepath.Join(dst.tasksDir, setID)) {
		t.Fatalf("imported path = %q, want %q", imported.Sets[0].Path, filepath.Join(dst.tasksDir, setID))
	}
	if data, err := os.ReadFile(filepath.Join(imported.Sets[0].Path, "progress.txt")); err != nil || string(data) != "history\n" {
		t.Fatalf("progress.txt not preserved: err=%v data=%q", err, data)
	}

	statePath := StatePathFor(dst.tasksDir)
	canonDef, err := CanonicalDefinitionPathWith(dst.deps, dst.tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalStateWith(dst.deps, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canonDef]
	if entry == nil || len(entry.TaskSets) != 1 || entry.TaskSets[0].ID != setID || entry.TaskSets[0].Priority != 0 {
		t.Fatalf("registration = %#v", entry)
	}
}

func TestExportRejectsFileReference(t *testing.T) {
	env := newTransferEnv(t)
	env.writeSet(t, "demo", nil)

	_, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{"demo/01-a.md"},
	})
	assertTransferExitCode(t, err, ExitSetup)
}

func TestExportDefaultOutputName(t *testing.T) {
	env := newTransferEnv(t)
	const setID = "2026-06-01-demo"
	env.writeSet(t, setID, nil)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	exported, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{setID},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := canonical(t, filepath.Join(env.root, setID+".tar.gz"))
	if canonical(t, exported.Path) != want {
		t.Fatalf("output path = %q, want %q", exported.Path, want)
	}
}

// archiveTopLevelDirs returns the sorted set of top-level directory names
// contained in a tar.gz archive.
func archiveTopLevelDirs(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		root := strings.SplitN(filepath.ToSlash(header.Name), "/", 2)[0]
		if root != "" {
			seen[root] = true
		}
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// archiveHasEntry reports whether the archive contains an entry with the given
// slash-separated name.
func archiveHasEntry(t *testing.T, path, name string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		if filepath.ToSlash(header.Name) == name {
			return true
		}
	}
	return false
}

func TestExportMultipleSets(t *testing.T) {
	env := newTransferEnv(t)
	env.writeSet(t, "2026-06-01-alpha", nil)
	env.writeSet(t, "2026-06-02-beta", nil)

	archive := filepath.Join(env.root, "multi.tar.gz")
	exported, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{"2026-06-01-alpha", "2026-06-02-beta"},
		OutputPath:   archive,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	got := archiveTopLevelDirs(t, exported.Path)
	want := []string{"2026-06-01-alpha", "2026-06-02-beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level dirs = %v, want %v", got, want)
	}
	for _, id := range want {
		if !archiveHasEntry(t, exported.Path, id+"/index.json") {
			t.Fatalf("index.json for %q missing from archive", id)
		}
	}
}

func TestExportDedupesRepeatedIDs(t *testing.T) {
	env := newTransferEnv(t)
	env.writeSet(t, "2026-06-01-alpha", nil)
	env.writeSet(t, "2026-06-02-beta", nil)

	archive := filepath.Join(env.root, "dedupe.tar.gz")
	exported, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{"2026-06-01-alpha", "2026-06-01-alpha", "2026-06-02-beta"},
		OutputPath:   archive,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(exported.TaskSetIDs) != 2 {
		t.Fatalf("deduped ids = %v, want 2 entries", exported.TaskSetIDs)
	}
	got := archiveTopLevelDirs(t, exported.Path)
	want := []string{"2026-06-01-alpha", "2026-06-02-beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level dirs = %v, want %v", got, want)
	}
}

func TestExportMissingIDIsAtomic(t *testing.T) {
	env := newTransferEnv(t)
	env.writeSet(t, "2026-06-01-alpha", nil)

	archive := filepath.Join(env.root, "atomic.tar.gz")
	_, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{"2026-06-01-alpha", "2026-06-09-nope"},
		OutputPath:   archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), "2026-06-09-nope") {
		t.Fatalf("error does not name the missing id: %v", err)
	}
	if _, statErr := os.Stat(archive); !os.IsNotExist(statErr) {
		t.Fatalf("archive was written despite missing id: %v", statErr)
	}
}

func TestExportMultiSetDefaultName(t *testing.T) {
	env := newTransferEnv(t)
	env.writeSet(t, "2026-06-01-alpha", nil)
	env.writeSet(t, "2026-06-02-beta", nil)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(env.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	fixed := time.Date(2026, 6, 13, 14, 30, 0, 0, time.Local)
	exported, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{"2026-06-01-alpha", "2026-06-02-beta"},
		Now:          fixed,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	want := canonical(t, filepath.Join(env.root, "pop-tasks-2026-06-13-1430.tar.gz"))
	if canonical(t, exported.Path) != want {
		t.Fatalf("output path = %q, want %q", exported.Path, want)
	}
}

func TestImportDisambiguatesCollidingSet(t *testing.T) {
	const existing = "2026-06-01-user-auth"

	// Build the archive from src first; newTransferEnv resets the global
	// XDG_DATA_HOME, so the import target (dst) must be created last.
	src := newTransferEnv(t)
	src.writeSet(t, existing, nil)
	archive := filepath.Join(src.root, existing+".tar.gz")
	if err := writeTaskSetArchive(filepath.Join(src.tasksDir, existing), existing, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	dst.writeSet(t, existing, func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("original\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	fixed := time.Date(2026, 6, 13, 14, 30, 0, 0, time.Local)
	imported, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
		Now:          fixed,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	wantID := "2026-06-13-user-auth"
	if len(imported.Sets) != 1 || imported.Sets[0].TaskSetID != wantID {
		t.Fatalf("imported = %#v, want single set %q", imported.Sets, wantID)
	}
	// The existing set is left untouched.
	if data, err := os.ReadFile(filepath.Join(dst.tasksDir, existing, "marker.txt")); err != nil || string(data) != "original\n" {
		t.Fatalf("existing set was touched: err=%v data=%q", err, data)
	}
	// The imported set lands under the dated identifier.
	if _, err := os.Stat(filepath.Join(dst.tasksDir, wantID, "index.json")); err != nil {
		t.Fatalf("disambiguated set missing: %v", err)
	}
}

func TestImportUnresolvableCollisionIsAtomic(t *testing.T) {
	// Build the archive from src first; the import target (dst) is created last
	// so its XDG_DATA_HOME wins at import time.
	src := newTransferEnv(t)
	src.writeSet(t, "2026-06-01-user-auth", nil)
	src.writeSet(t, "2026-06-02-fresh", nil)
	archive := filepath.Join(src.root, "multi.tar.gz")
	sets := []exportSet{
		{id: "2026-06-01-user-auth", dir: filepath.Join(src.tasksDir, "2026-06-01-user-auth")},
		{id: "2026-06-02-fresh", dir: filepath.Join(src.tasksDir, "2026-06-02-fresh")},
	}
	if err := writeTaskSetsArchive(sets, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	// Occupy every rung of the disambiguation ladder for the colliding set.
	dst.writeSet(t, "2026-06-01-user-auth", nil)
	dst.writeSet(t, "2026-06-13-user-auth", nil)
	dst.writeSet(t, "2026-06-13-1430-user-auth", nil)

	fixed := time.Date(2026, 6, 13, 14, 30, 0, 0, time.Local)
	_, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
		Now:          fixed,
	})
	assertTransferExitCode(t, err, ExitSetup)
	// The well-formed sibling must not have been installed.
	if _, statErr := os.Stat(filepath.Join(dst.tasksDir, "2026-06-02-fresh")); !os.IsNotExist(statErr) {
		t.Fatalf("fresh set installed despite unresolvable collision on sibling: %v", statErr)
	}
}

func TestImportAsAddsDatePrefix(t *testing.T) {
	env := newTransferEnv(t)
	const setID = "2026-06-01-user-auth"
	env.writeSet(t, setID, nil)

	archive := filepath.Join(env.root, setID+".tar.gz")
	if err := writeTaskSetArchive(filepath.Join(env.tasksDir, setID), setID, archive); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 6, 13, 14, 30, 0, 0, time.Local)
	imported, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
		AsID:         "user-auth",
		Now:          fixed,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantID := "2026-06-13-user-auth"
	if imported.Sets[0].TaskSetID != wantID {
		t.Fatalf("imported id = %q, want %q", imported.Sets[0].TaskSetID, wantID)
	}
}

func TestImportAsDisambiguatesWithHHMM(t *testing.T) {
	env := newTransferEnv(t)
	const setID = "2026-06-01-user-auth"
	env.writeSet(t, setID, nil)
	env.writeSet(t, "2026-06-13-user-auth", nil)

	archive := filepath.Join(env.root, setID+".tar.gz")
	if err := writeTaskSetArchive(filepath.Join(env.tasksDir, setID), setID, archive); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 6, 13, 14, 30, 0, 0, time.Local)
	imported, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
		AsID:         "user-auth",
		Now:          fixed,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantID := "2026-06-13-1430-user-auth"
	if imported.Sets[0].TaskSetID != wantID {
		t.Fatalf("imported id = %q, want %q", imported.Sets[0].TaskSetID, wantID)
	}
}

func TestImportRejectsMalformedArchive(t *testing.T) {
	env := newTransferEnv(t)
	archive := filepath.Join(env.root, "bad.tar.gz")
	if err := writeBrokenArchive(t, archive, "broken-set", `not json`); err != nil {
		t.Fatal(err)
	}

	_, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
}

func TestImportRejectsPathTraversal(t *testing.T) {
	env := newTransferEnv(t)
	archive := filepath.Join(env.root, "evil.tar.gz")
	if err := writeRawTarEntry(t, archive, "../escape.txt", []byte("nope")); err != nil {
		t.Fatal(err)
	}

	_, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
}

func TestImportMultipleSets(t *testing.T) {
	src := newTransferEnv(t)
	src.writeSet(t, "2026-06-01-alpha", nil)
	src.writeSet(t, "2026-06-02-beta", nil)
	archive := filepath.Join(src.root, "multi.tar.gz")
	// Archive top-level order is reversed to prove registration re-orders by
	// identifier.
	sets := []exportSet{
		{id: "2026-06-02-beta", dir: filepath.Join(src.tasksDir, "2026-06-02-beta")},
		{id: "2026-06-01-alpha", dir: filepath.Join(src.tasksDir, "2026-06-01-alpha")},
	}
	if err := writeTaskSetsArchive(sets, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	imported, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Sets) != 2 {
		t.Fatalf("imported %d sets, want 2: %#v", len(imported.Sets), imported.Sets)
	}
	for i, want := range []string{"2026-06-01-alpha", "2026-06-02-beta"} {
		if imported.Sets[i].TaskSetID != want {
			t.Fatalf("set[%d] id = %q, want %q", i, imported.Sets[i].TaskSetID, want)
		}
		if !filepath.IsAbs(imported.Sets[i].Path) {
			t.Fatalf("installed path %q is not absolute", imported.Sets[i].Path)
		}
		if _, err := os.Stat(filepath.Join(imported.Sets[i].Path, "index.json")); err != nil {
			t.Fatalf("installed set %q missing manifest: %v", want, err)
		}
	}

	statePath := StatePathFor(dst.tasksDir)
	canonDef, err := CanonicalDefinitionPathWith(dst.deps, dst.tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalStateWith(dst.deps, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canonDef]
	if entry == nil || len(entry.TaskSets) != 2 {
		t.Fatalf("registration = %#v", entry)
	}
	if entry.TaskSets[0].ID != "2026-06-01-alpha" || entry.TaskSets[1].ID != "2026-06-02-beta" {
		t.Fatalf("registration order = %q, %q", entry.TaskSets[0].ID, entry.TaskSets[1].ID)
	}
	for _, set := range entry.TaskSets {
		if set.Priority != 0 {
			t.Fatalf("set %q priority = %d, want 0", set.ID, set.Priority)
		}
	}
}

func TestImportRegistersAfterExistingRegistrations(t *testing.T) {
	// Build the archive from src first; the import target (dst) is created last
	// so its XDG_DATA_HOME wins at import time.
	src := newTransferEnv(t)
	src.writeSet(t, "2026-06-01-alpha", nil)
	src.writeSet(t, "2026-06-02-beta", nil)
	archive := filepath.Join(src.root, "multi.tar.gz")
	sets := []exportSet{
		{id: "2026-06-02-beta", dir: filepath.Join(src.tasksDir, "2026-06-02-beta")},
		{id: "2026-06-01-alpha", dir: filepath.Join(src.tasksDir, "2026-06-01-alpha")},
	}
	if err := writeTaskSetsArchive(sets, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	// A pre-registered set with a high identifier must stay first — imported
	// sets are appended after it, not merged into a global re-sort.
	dst.writeSet(t, "2026-09-09-existing", nil)
	statePath := StatePathFor(dst.tasksDir)
	if _, err := RegisterWith(dst.deps, dst.tasksDir, statePath); err != nil {
		t.Fatal(err)
	}

	if _, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	canonDef, err := CanonicalDefinitionPathWith(dst.deps, dst.tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalStateWith(dst.deps, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canonDef]
	var got []string
	for _, s := range entry.TaskSets {
		got = append(got, s.ID)
	}
	want := []string{"2026-09-09-existing", "2026-06-01-alpha", "2026-06-02-beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registration order = %v, want %v", got, want)
	}
}

func TestImportMalformedSetIsAtomic(t *testing.T) {
	src := newTransferEnv(t)
	src.writeSet(t, "2026-06-01-good", nil)
	src.writeSet(t, "2026-06-02-bad", func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("not json"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	archive := filepath.Join(src.root, "multi.tar.gz")
	sets := []exportSet{
		{id: "2026-06-01-good", dir: filepath.Join(src.tasksDir, "2026-06-01-good")},
		{id: "2026-06-02-bad", dir: filepath.Join(src.tasksDir, "2026-06-02-bad")},
	}
	if err := writeTaskSetsArchive(sets, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	_, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), "2026-06-02-bad") {
		t.Fatalf("error does not name the malformed set: %v", err)
	}
	for _, id := range []string{"2026-06-01-good", "2026-06-02-bad"} {
		if _, statErr := os.Stat(filepath.Join(dst.tasksDir, id)); !os.IsNotExist(statErr) {
			t.Fatalf("set %q installed despite malformed sibling: %v", id, statErr)
		}
	}
}

func TestImportAsRejectedForMultiSet(t *testing.T) {
	src := newTransferEnv(t)
	src.writeSet(t, "2026-06-01-alpha", nil)
	src.writeSet(t, "2026-06-02-beta", nil)
	archive := filepath.Join(src.root, "multi.tar.gz")
	sets := []exportSet{
		{id: "2026-06-01-alpha", dir: filepath.Join(src.tasksDir, "2026-06-01-alpha")},
		{id: "2026-06-02-beta", dir: filepath.Join(src.tasksDir, "2026-06-02-beta")},
	}
	if err := writeTaskSetsArchive(sets, archive); err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	_, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  archive,
		AsID:         "renamed",
	})
	assertTransferExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), "--as") {
		t.Fatalf("error should explain the --as multi-set guard: %v", err)
	}
	for _, id := range []string{"2026-06-01-alpha", "2026-06-02-beta"} {
		if _, statErr := os.Stat(filepath.Join(dst.tasksDir, id)); !os.IsNotExist(statErr) {
			t.Fatalf("set %q installed despite --as guard rejection: %v", id, statErr)
		}
	}
}

func TestImportRejectsTraversalInAnyTopLevelDir(t *testing.T) {
	env := newTransferEnv(t)
	archive := filepath.Join(env.root, "evil-multi.tar.gz")
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := writeOrderedArchive(t, archive, [][2]string{
		{"2026-06-01-good/index.json", manifest},
		{"2026-06-02-bad/../../escape.txt", "nope"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
	if _, statErr := os.Stat(filepath.Join(env.tasksDir, "2026-06-01-good")); !os.IsNotExist(statErr) {
		t.Fatalf("good set installed despite traversal entry in a sibling dir: %v", statErr)
	}
}

func assertTransferExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected exit code %d, got nil", want)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != want {
		t.Fatalf("expected exit code %d, got %v", want, err)
	}
}

func writeBrokenArchive(t *testing.T, path, setID, manifest string) error {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     setID + "/index.json",
		Mode:     0o644,
		Size:     int64(len(manifest)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(manifest)); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeRawTarEntry(t *testing.T, path, name string, data []byte) error {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeOrderedArchive writes a tar.gz with the given (name, content) entries in
// order, letting tests craft archives with arbitrary — including malicious —
// entry names.
func writeOrderedArchive(t *testing.T, path string, entries [][2]string) error {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		name, payload := entry[0], entry[1]
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(payload)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(payload)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func TestExportIncludesStreams(t *testing.T) {
	env := newTransferEnv(t)
	const setID = "2026-06-01-demo"
	env.writeSet(t, setID, func(dir string) {
		streamDir := filepath.Join(dir, "streams", "01-a")
		if err := os.MkdirAll(streamDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(streamDir, "attempt-001.jsonl.gz"), []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	archive := filepath.Join(env.root, setID+".tar.gz")
	exported, err := ExportWith(env.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: env.resolveInput(),
		TaskSetIDs:   []string{setID},
		OutputPath:   archive,
	})
	if err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	tops, err := extractTaskSetsArchive(exported.Path, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tops) != 1 {
		t.Fatalf("extracted %d top-level dirs, want 1: %v", len(tops), tops)
	}
	got := filepath.Join(tempDir, tops[0], "streams", "01-a", "attempt-001.jsonl.gz")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("stream not in archive: %v", err)
	}
}

func TestValidateArchiveEntryName(t *testing.T) {
	if _, err := validateArchiveEntryName("../etc/passwd"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape error, got %v", err)
	}
	if _, err := validateArchiveEntryName("/abs"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute error, got %v", err)
	}
}
