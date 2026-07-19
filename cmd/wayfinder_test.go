package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
)

func TestWayfinderCommandTree(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"wayfinder", "status"}); err != nil {
		t.Fatalf("Find([wayfinder status]): %v", err)
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
