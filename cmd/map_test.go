package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

func TestMapCommandTree(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"map", "status"},
		{"map", "show"},
		{"map", "register"},
		{"map", "archive"},
		{"map", "unarchive"},
	} {
		if _, _, err := rootCmd.Find(path); err != nil {
			t.Fatalf("Find(%v): %v", path, err)
		}
	}
	// The rename is a hard cut: cobra resolves an unknown first argument to the
	// root command, so the old family is gone exactly when nothing named
	// wayfinder answers.
	if cmd, _, _ := rootCmd.Find([]string{"wayfinder", "status"}); cmd.CommandPath() != "pop" {
		t.Fatalf("pop wayfinder should not exist; Find resolved %q", cmd.CommandPath())
	}
	for _, cmd := range []*cobra.Command{mapCmd, mapStatusCmd, mapShowCmd, mapRegisterCmd, mapArchiveCmd, mapUnarchiveCmd} {
		if strings.Contains(cmd.CommandPath(), "wayfinder") {
			t.Fatalf("command path still says wayfinder: %q", cmd.CommandPath())
		}
	}
}

func TestMapShowRendersMap(t *testing.T) {
	t.Parallel()
	dataHome := "/data"
	commonDir := "/repo/.git"
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", dataHome, ""))
	fs := cmdTestFS(dataHome, "")
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: fs}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "maps", "demo")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                 "Status: active\n\n## Destination\nShip it\n\n## Decisions so far\n- one decision",
		filepath.Join(mapDir, "issues", "01-first.md"):  "Type: research\nStatus: resolved\n",
		filepath.Join(mapDir, "issues", "02-second.md"): "Type: task\nBlocked by: 01\n",
	}
	d := wayfinderTestDepsForCmd(t, dataHome, commonDir, files)

	var buf bytes.Buffer
	if err := runMapShowWith(d, &buf, "demo"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Destination: Ship it", "Frontier:", "02-second", "Resolved:", "01-first"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestMapRegisterValidatesThenRegisters walks the charting-to-registered path the
// way a session does: a malformed manifest comes back as a fix list and no row,
// the fix registers, and a re-run is a no-op.
func TestMapRegisterValidatesThenRegisters(t *testing.T) {
	t.Parallel()
	d, storageDir, dataHome := mapRegistryTestDeps(t, map[string]string{
		"maps/2026-08-03-demo/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/2026-08-03-demo/issues/01-first.md": "## Question\nWhy?\n",
		"maps/2026-08-03-demo/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"parked","blocked_by":["09"]}` +
			`],"spawned_sets":[]}`,
	})

	err := runMapRegisterWith(d, &bytes.Buffer{}, "2026-08-03-demo")
	if err == nil {
		t.Fatal("expected a malformed manifest to refuse registration")
	}
	for _, want := range []string{"MALFORMED", `unknown status "parked"`, `unresolved blocker "09"`, "pop map register 2026-08-03-demo"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, err.Error())
		}
	}

	manifest := filepath.Join(storageDir, "maps", "2026-08-03-demo", "index.json")
	fixed := `{"tickets":[{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]}],"spawned_sets":[]}`
	if err := os.WriteFile(manifest, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runMapRegisterWith(d, &buf, "2026-08-03-demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Registered map 2026-08-03-demo") {
		t.Fatalf("register output = %q", buf.String())
	}
	if !registeredMap(t, d, "2026-08-03-demo") {
		t.Fatal("register wrote no work_containers row")
	}

	var again bytes.Buffer
	if err := runMapRegisterWith(d, &again, "2026-08-03-demo"); err != nil {
		t.Fatalf("register must be re-runnable: %v", err)
	}
	if !strings.Contains(again.String(), "already registered") {
		t.Fatalf("re-register output = %q", again.String())
	}

	// Plain, never managed: wayfinding writes nothing into the repository, so
	// registration provisions no checkout and the verb has no flag to ask for one.
	if mapRegisterCmd.Flags().Lookup("managed") != nil || mapRegisterCmd.HasAvailableFlags() {
		t.Fatalf("pop map register grew flags: %v", mapRegisterCmd.Flags().FlagUsages())
	}
	assertNoWorktreesProvisioned(t, dataHome)
}

func TestMapArchiveRoundTrip(t *testing.T) {
	t.Parallel()
	d, storageDir, _ := mapRegistryTestDeps(t, oneTicketMapFiles("demo"))
	mapPath := filepath.Join(storageDir, "maps", "demo", "map.md")
	original, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := runMapArchiveWith(d, &bytes.Buffer{}, "demo"); err == nil {
		t.Fatal("expected archive to refuse an unregistered map")
	} else if !strings.Contains(err.Error(), "pop map register demo") {
		t.Fatalf("error = %v", err)
	}
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "demo"); err != nil {
		t.Fatal(err)
	}

	var archiveBuf bytes.Buffer
	if err := runMapArchiveWith(d, &archiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archiveBuf.String(), "Archived map demo") {
		t.Fatalf("archive output = %q", archiveBuf.String())
	}
	if after, err := os.ReadFile(mapPath); err != nil || string(after) != string(original) {
		t.Fatalf("archive mutated map.md (%v)", err)
	}

	var statusBuf bytes.Buffer
	if err := runMapStatusWith(d, &statusBuf, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statusBuf.String(), "demo") {
		t.Fatalf("archived map visible in default status:\n%s", statusBuf.String())
	}

	var unarchiveBuf bytes.Buffer
	if err := runMapUnarchiveWith(d, &unarchiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unarchiveBuf.String(), "Unarchived map demo") {
		t.Fatalf("unarchive output = %q", unarchiveBuf.String())
	}
}

// TestMapShellCompletionOffersMapIDs pins the completion split: every verb
// offers the visible Maps, unarchive offers only the filed-away one.
func TestMapShellCompletionOffersMapIDs(t *testing.T) {
	t.Parallel()
	files := oneTicketMapFiles("visible")
	for rel, content := range oneTicketMapFiles("filed-away") {
		files[rel] = content
	}
	d, _, _ := mapRegistryTestDeps(t, files)
	if err := runMapRegisterWith(d, &bytes.Buffer{}, "filed-away"); err != nil {
		t.Fatal(err)
	}
	if err := runMapArchiveWith(d, &bytes.Buffer{}, "filed-away"); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []*cobra.Command{mapShowCmd, mapRegisterCmd, mapArchiveCmd} {
		got, directive := cmd.ValidArgsFunction(cmd, nil, "")
		if !slices.Equal(got, []string{"visible"}) {
			t.Fatalf("%s completion = %v, want [visible]", cmd.Name(), got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("%s completion directive = %v", cmd.Name(), directive)
		}
	}
	got, _ := mapUnarchiveCmd.ValidArgsFunction(mapUnarchiveCmd, nil, "")
	if !slices.Equal(got, []string{"filed-away"}) {
		t.Fatalf("unarchive completion = %v, want [filed-away]", got)
	}
	if second, _ := mapShowCmd.ValidArgsFunction(mapShowCmd, []string{"visible"}, ""); second != nil {
		t.Fatalf("completion offered a second positional: %v", second)
	}
}

func TestMapShowUnknownMap(t *testing.T) {
	t.Parallel()
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", "/data", ""))
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	err := runMapShowWith(d, &bytes.Buffer{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
}

func TestMapStatusOutsideGitRepo(t *testing.T) {
	t.Parallel()
	d := &wayfinder.Deps{
		FS: deps.NewRealFileSystem(),
		Tasks: &tasks.Deps{
			FS: deps.NewRealFileSystem(),
			Git: &deps.MockGit{
				CommandInDirFunc: func(dir string, args ...string) (string, error) {
					return "", errNotGit
				},
			},
		},
	}
	err := runMapStatusWith(d, &bytes.Buffer{}, false)
	if err == nil {
		t.Fatal("expected error outside git repository")
	}
}

var errNotGit = errString("fatal: not a git repository")

type errString string

func (e errString) Error() string { return string(e) }

func TestMapStatusEmpty(t *testing.T) {
	t.Parallel()
	setCmdLayerDeps(t, newTestCmdDeps(t, "/mock/cwd", "/data", ""))
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	var buf bytes.Buffer
	if err := runMapStatusWith(d, &buf, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No wayfinder maps.") {
		t.Fatalf("output = %q", buf.String())
	}
}

// mapRegistryTestDeps wires cmd-layer deps at a real temp data dir. Registration
// and archival are rows in pop.db, which cannot ride the filesystem seam, so the
// verbs that write them need a real store. Keys in files are relative to the
// repository's Task-storage root; it returns that root and the data dir.
func mapRegistryTestDeps(t *testing.T, files map[string]string) (*wayfinder.Deps, string, string) {
	t.Helper()
	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg")
	commonDir := filepath.Join(root, "repo", ".git")
	fs := cmdTestFS(dataHome, "")
	td := &tasks.Deps{
		FS: fs,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) { return commonDir, nil },
		},
	}
	t.Cleanup(func() { _ = td.CloseStore() })
	wd := &wayfinder.Deps{FS: fs, Tasks: td}
	setCmdLayerDeps(t, &Deps{Dir: filepath.Join(root, "repo"), FS: fs, Tasks: td, Wayfinder: wd})

	id, err := tasks.IdentityFromCommonDir(td, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(id.StorageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wd, id.StorageDir, dataHome
}

// oneTicketMapFiles is the smallest Map that registers cleanly.
func oneTicketMapFiles(id string) map[string]string {
	return map[string]string{
		"maps/" + id + "/map.md":             "Status: active\n\n## Destination\nShip it\n",
		"maps/" + id + "/issues/01-first.md": "## Question\nWhy?\n",
		"maps/" + id + "/index.json": `{"tickets":[` +
			`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]}` +
			`],"spawned_sets":[]}`,
	}
}

func registeredMap(t *testing.T, d *wayfinder.Deps, mapID string) bool {
	t.Helper()
	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := s.FindWorkContainer(wayfinder.MapRef(mapID))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func assertNoWorktreesProvisioned(t *testing.T, dataHome string) {
	t.Helper()
	err := filepath.WalkDir(dataHome, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "worktrees" {
			t.Fatalf("registering a map provisioned a worktree root at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func wayfinderTestDepsForCmd(t *testing.T, dataHome, commonDir string, files map[string]string) *wayfinder.Deps {
	t.Helper()
	fs := &deps.MockFileSystem{
		GetwdFunc: func() (string, error) { return "/mock/cwd", nil },
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return "/mock/home", nil },
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			entries := dirEntriesForCmd(path, files)
			if entries == nil {
				return nil, os.ErrNotExist
			}
			return entries, nil
		},
		ReadFileFunc: func(path string) ([]byte, error) {
			if content, ok := files[path]; ok {
				return []byte(content), nil
			}
			return nil, os.ErrNotExist
		},
	}
	return &wayfinder.Deps{
		FS: fs,
		Tasks: &tasks.Deps{
			FS: fs,
			Git: &deps.MockGit{
				CommandInDirFunc: func(dir string, args ...string) (string, error) {
					return commonDir, nil
				},
			},
		},
	}
}

func dirEntriesForCmd(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	dirs := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) && filePath != path {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		if rel == "" {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		name := parts[0]
		if len(parts) == 1 {
			children[name] = false
			continue
		}
		children[name] = true
		dirs[name] = true
	}
	if len(children) == 0 {
		return nil
	}
	var out []os.DirEntry
	for name, isDir := range children {
		out = append(out, deps.MockDirEntry{NameVal: name, IsDirVal: isDir || dirs[name]})
	}
	return out
}
