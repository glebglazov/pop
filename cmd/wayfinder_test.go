package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
)

func TestWayfinderCommandTree(t *testing.T) {
	for _, path := range [][]string{
		{"wayfinder", "status"},
		{"wayfinder", "show"},
		{"wayfinder", "archive"},
		{"wayfinder", "unarchive"},
	} {
		if _, _, err := rootCmd.Find(path); err != nil {
			t.Fatalf("Find(%v): %v", path, err)
		}
	}
}

func TestWayfinderShowRendersMap(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "demo")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"): "Status: active\n\n## Destination\nShip it\n\n## Decisions so far\n- one decision",
		filepath.Join(mapDir, "issues", "01-first.md"):  "Type: research\nStatus: resolved\n",
		filepath.Join(mapDir, "issues", "02-second.md"): "Type: task\nBlocked by: 01\n",
	}
	d := wayfinderTestDepsForCmd(t, dataHome, commonDir, files)

	var buf bytes.Buffer
	if err := runWayfinderShowWith(d, &buf, "demo"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Destination: Ship it", "Frontier:", "02-second", "Resolved:", "01-first"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWayfinderArchiveRoundTrip(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "demo")
	mapPath := filepath.Join(mapDir, "map.md")
	original := "## Destination\nShip it"
	files := map[string]string{mapPath: original}
	d := wayfinderTestDepsForCmd(t, dataHome, commonDir, files)
	d.FS.(*deps.MockFileSystem).WriteFileFunc = func(path string, data []byte, perm os.FileMode) error {
		files[path] = string(data)
		return nil
	}

	var archiveBuf bytes.Buffer
	if err := runWayfinderArchiveWith(d, &archiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archiveBuf.String(), "Archived wayfinder map demo") {
		t.Fatalf("archive output = %q", archiveBuf.String())
	}
	if files[mapPath] != original {
		t.Fatal("archive mutated map.md")
	}

	var statusBuf bytes.Buffer
	if err := runWayfinderStatusWith(d, &statusBuf, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statusBuf.String(), "demo") {
		t.Fatalf("archived map visible in default status:\n%s", statusBuf.String())
	}

	var unarchiveBuf bytes.Buffer
	if err := runWayfinderUnarchiveWith(d, &unarchiveBuf, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unarchiveBuf.String(), "Unarchived wayfinder map demo") {
		t.Fatalf("unarchive output = %q", unarchiveBuf.String())
	}
}

func TestWayfinderShowUnknownMap(t *testing.T) {
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	err := runWayfinderShowWith(d, &bytes.Buffer{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
}

func TestWayfinderStatusOutsideGitRepo(t *testing.T) {
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
	err := runWayfinderStatusWith(d, &bytes.Buffer{}, false)
	if err == nil {
		t.Fatal("expected error outside git repository")
	}
}

var errNotGit = errString("fatal: not a git repository")

type errString string

func (e errString) Error() string { return string(e) }

func TestWayfinderStatusEmpty(t *testing.T) {
	d := wayfinderTestDepsForCmd(t, "/data", "/repo/.git", nil)
	var buf bytes.Buffer
	if err := runWayfinderStatusWith(d, &buf, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No wayfinder maps.") {
		t.Fatalf("output = %q", buf.String())
	}
}

func wayfinderTestDepsForCmd(t *testing.T, dataHome, commonDir string, files map[string]string) *wayfinder.Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
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
