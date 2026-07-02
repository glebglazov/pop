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
	if imported.TaskSetID != setID {
		t.Fatalf("imported id = %q, want %q", imported.TaskSetID, setID)
	}
	if canonical(t, imported.Path) != canonical(t, filepath.Join(dst.tasksDir, setID)) {
		t.Fatalf("imported path = %q, want %q", imported.Path, filepath.Join(dst.tasksDir, setID))
	}
	if data, err := os.ReadFile(filepath.Join(imported.Path, "progress.txt")); err != nil || string(data) != "history\n" {
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

func TestImportRejectsCollision(t *testing.T) {
	env := newTransferEnv(t)
	const setID = "2026-06-01-user-auth"
	env.writeSet(t, setID, nil)

	archive := filepath.Join(env.root, setID+".tar.gz")
	if err := writeTaskSetArchive(filepath.Join(env.tasksDir, setID), setID, archive); err != nil {
		t.Fatal(err)
	}

	_, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
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
	if imported.TaskSetID != wantID {
		t.Fatalf("imported id = %q, want %q", imported.TaskSetID, wantID)
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
	if imported.TaskSetID != wantID {
		t.Fatalf("imported id = %q, want %q", imported.TaskSetID, wantID)
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

func TestImportRejectsMultipleTopLevelDirs(t *testing.T) {
	env := newTransferEnv(t)
	archive := filepath.Join(env.root, "multi.tar.gz")
	if err := writeMultiRootArchive(t, archive); err != nil {
		t.Fatal(err)
	}

	_, err := ImportWith(env.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: env.resolveInput(),
		ArchivePath:  archive,
	})
	assertTransferExitCode(t, err, ExitSetup)
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

func writeMultiRootArchive(t *testing.T, path string) error {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"one/index.json", "two/index.json"} {
		payload := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
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
	top, err := extractTaskSetArchive(exported.Path, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(tempDir, top, "streams", "01-a", "attempt-001.jsonl.gz")
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
