package repogroup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// Repository-group resolution's own tests, moved here with the resolution when
// the Work kinds stopped owning it: what a group resolves to is the same question
// for every kind that scans per repository.

// trunkPtr states a repository's Trunk worktree as the path value it is.
func trunkPtr(path string) *config.TrunkPath {
	p := config.TrunkPath(path)
	return &p
}

// TestIntegrationTargetDerivedForkFree covers ADR-0060's integration target rules
// without forking git (a guard git fails the test on any static command): a
// non-bare repo's target is the main worktree (parent of the common dir) and
// needs no config; a bare repo's target is its config trunk; and a bare repo
// without a declared trunk surfaces a config-class error.
func TestIntegrationTargetDerivedForkFree(t *testing.T) {
	dataHome := t.TempDir()
	mkDeps := func() *Deps {
		fs := &deps.MockFileSystem{
			GetenvFunc: func(k string) string {
				if k == "XDG_DATA_HOME" {
					return dataHome
				}
				return ""
			},
			EvalSymlinksFunc: func(p string) (string, error) { return p, nil },
			StatFunc:         func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			ReadFileFunc:     func(string) ([]byte, error) { return nil, os.ErrNotExist },
			UserHomeDirFunc:  func() (string, error) { return dataHome, nil },
		}
		guard := &deps.MockGit{
			CommandInDirFunc: func(_ string, args ...string) (string, error) {
				return "", errors.New("unexpected git: " + strings.Join(args, " "))
			},
		}
		return &Deps{Tasks: &tasks.Deps{FS: fs, Git: guard}, Project: &project.Deps{FS: fs, Git: guard}}
	}

	t.Run("non-bare resolves target with no config", func(t *testing.T) {
		d := mkDeps()
		scans := []Checkout{{Name: "repo", ProjectPath: "/repo", RuntimePath: "/repo"}}
		st, err := FromMarker(d, &config.Config{}, "/repo/.git", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.Bare || st.ConfigError != "" || st.Rep == nil || st.Rep.ProjectPath != "/repo" {
			t.Fatalf("non-bare static = %+v rep = %+v", st, st.Rep)
		}
	})

	t.Run("bare uses config trunk", func(t *testing.T) {
		d := mkDeps()
		cfg := &config.Config{Repo: map[string]config.RepoOverrideConfig{
			"/repo/main": {Trunk: trunkPtr("/repo/main")},
		}}
		scans := []Checkout{
			{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"},
			{Name: "repo/main", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		}
		st, err := FromMarker(d, cfg, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.ConfigError != "" || st.Rep == nil || st.Rep.ProjectPath != "/repo/main" {
			t.Fatalf("bare+trunk static = %+v rep = %+v", st, st.Rep)
		}
	})

	t.Run("bare without trunk surfaces config error", func(t *testing.T) {
		d := mkDeps()
		scans := []Checkout{{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"}}
		st, err := FromMarker(d, &config.Config{}, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.Rep != nil || !st.Bare || st.ConfigError == "" {
			t.Fatalf("bare-no-trunk static = %+v rep = %+v", st, st.Rep)
		}
	})
}
// TestHeadBranchFromCheckout covers ADR-0060's fork-free branch read: a main
// worktree's branch is parsed from <checkout>/.git/HEAD, a linked worktree's via
// its `.git` gitdir pointer, a detached HEAD yields "", and the common-dir
// fallback applies when there is no `.git` entry.
func TestHeadBranchFromCheckout(t *testing.T) {
	files := map[string]string{
		"/main/.git/HEAD":              "ref: refs/heads/trunk\n",
		"/wt/.git":                     "gitdir: /repo/.git/worktrees/wt\n",
		"/repo/.git/worktrees/wt/HEAD": "ref: refs/heads/feature\n",
		"/detached/.git/HEAD":          "a1b2c3d4e5f6\n",
		"/common-only/.git/HEAD":       "ref: refs/heads/from-common\n",
	}
	dirs := map[string]bool{"/main/.git": true, "/detached/.git": true}
	fs := &deps.MockFileSystem{
		StatFunc: func(p string) (os.FileInfo, error) {
			if dirs[p] {
				return deps.MockFileInfo{NameVal: filepath.Base(p), IsDirVal: true}, nil
			}
			if _, ok := files[p]; ok {
				return deps.MockFileInfo{NameVal: filepath.Base(p)}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(p string) ([]byte, error) {
			if data, ok := files[p]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		},
	}
	td := &tasks.Deps{FS: fs}

	cases := []struct {
		name      string
		checkout  string
		commonDir string
		want      string
	}{
		{"main worktree", "/main", "/main/.git", "trunk"},
		{"linked worktree", "/wt", "/repo/.git", "feature"},
		{"detached", "/detached", "/detached/.git", ""},
		{"common-dir fallback", "/common-only", "/common-only/.git", "from-common"},
		{"missing", "/nope", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HeadBranch(td, tc.checkout, tc.commonDir); got != tc.want {
				t.Fatalf("HeadBranch(%q, %q) = %q, want %q", tc.checkout, tc.commonDir, got, tc.want)
			}
		})
	}
}
