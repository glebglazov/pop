package project

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// gitAnswers builds a MockGit answering rev-parse/config from a table keyed by
// the last argument, so each case reads as the git facts for one checkout shape.
// A missing key errors, which is how a broken checkout is expressed.
func gitAnswers(answers map[string]string) *deps.MockGit {
	return &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			key := args[len(args)-1]
			if v, ok := answers[key]; ok {
				return v, nil
			}
			return "", fmt.Errorf("fatal: not a git repository: %s", dir)
		},
	}
}

// noBareRootFS short-circuits findBareRootWith and reports no `.git` anywhere, so
// naming depends entirely on the git answers — the situation every checkout outside
// its repository's tree (managed worktrees included) is in.
func noBareRootFS() *deps.MockFileSystem {
	return &deps.MockFileSystem{
		StatFunc: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}
}

// gitPointerFS reports a `.git` pointer file at path holding the given contents.
// An empty contents means the pointer exists but cannot be read — a checkout that
// declares itself one and can say nothing more.
func gitPointerFS(path, contents string) *deps.MockFileSystem {
	gitPath := path + "/.git"
	return &deps.MockFileSystem{
		StatFunc: func(p string) (os.FileInfo, error) {
			if p == gitPath {
				return deps.MockFileInfo{NameVal: ".git", IsDirVal: false}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(p string) ([]byte, error) {
			if p == gitPath && contents != "" {
				return []byte(contents), nil
			}
			return nil, os.ErrPermission
		},
	}
}

func TestSessionNameForWith(t *testing.T) {
	t.Parallel()

	const managedRoot = "/data/pop/work/worktrees"

	tests := []struct {
		name      string
		deps      *Deps
		path      string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "trunk keeps its plain directory name",
			deps: &Deps{
				Git: gitAnswers(map[string]string{
					"--git-common-dir": "/projects/trunk/.git",
					"--git-dir":        "/projects/trunk/.git",
					"--show-toplevel":  "/projects/trunk",
					"core.bare":        "false",
				}),
				FS: noBareRootFS(),
			},
			path: "/projects/trunk",
			want: "trunk",
		},
		{
			name: "hand-made worktree of a trunk keeps the repo prefix",
			deps: &Deps{
				Git: gitAnswers(map[string]string{
					"--git-common-dir": "/projects/trunk/.git",
					"--git-dir":        "/projects/trunk/.git/worktrees/feature",
					"--show-toplevel":  "/elsewhere/feature",
					"core.bare":        "false",
				}),
				FS: noBareRootFS(),
			},
			path: "/elsewhere/feature",
			want: "trunk/feature",
		},
		{
			name: "managed worktree with healthy git keeps the repo prefix",
			deps: &Deps{
				Git: gitAnswers(map[string]string{
					"--git-common-dir": "/projects/pop/.git",
					"--git-dir":        "/projects/pop/.git/worktrees/2026-08-03-set",
					"--show-toplevel":  managedRoot + "/pop-b10dd6234b2d/2026-08-03-set",
					"core.bare":        "false",
				}),
				FS: noBareRootFS(),
			},
			path: managedRoot + "/pop-b10dd6234b2d/2026-08-03-set",
			want: "pop/2026-08-03-set",
		},
		{
			name: "managed worktree whose git admin dir is gone keeps the repo prefix",
			deps: &Deps{
				// Every rev-parse fails: this is a pruned or dangling worktree.
				Git: gitAnswers(nil),
				FS:  noBareRootFS(),
			},
			path:      managedRoot + "/pop-b10dd6234b2d/2026-08-03-set",
			want:      "pop/2026-08-03-set",
			wantErr:   true,
			errSubstr: "derived from the directory layout",
		},
		{
			name: "hand-made worktree with a dangling gitdir pointer keeps the repo prefix",
			deps: &Deps{
				Git: gitAnswers(nil),
				FS: gitPointerFS("/Users/dev/work/game-server-apple-arcade",
					"gitdir: /Users/dev/work/game_server/.git/worktrees/game-server-apple-arcade\n"),
			},
			path:      "/Users/dev/work/game-server-apple-arcade",
			want:      "game_server/game-server-apple-arcade",
			wantErr:   true,
			errSubstr: "derived from the directory layout",
		},
		{
			name: "bare repo worktree keeps the repo prefix",
			deps: &Deps{
				Git: gitAnswers(map[string]string{
					"--git-common-dir": "/repos/game_server.git",
					"core.bare":        "true",
				}),
				FS: noBareRootFS(),
			},
			path: "/elsewhere/feature",
			want: "game_server/feature",
		},
		{
			name: "<repo>/.bare layout is named after the repo, not .bare",
			deps: &Deps{
				Git: gitAnswers(map[string]string{
					"--git-common-dir": "/projects/annual_calendar/.bare",
					"core.bare":        "true",
				}),
				FS: noBareRootFS(),
			},
			path: "/projects/annual_calendar/alfa",
			want: "annual_calendar/alfa",
		},
		{
			name: "checkout whose layout says nothing degrades, and says so",
			deps: &Deps{
				Git: gitAnswers(nil),
				FS:  gitPointerFS("/Users/dev/work/broken", ""),
			},
			path:      "/Users/dev/work/broken",
			want:      "broken",
			wantErr:   true,
			errSubstr: "degrades to",
		},
		{
			name: "non-git directory is named after itself without complaint",
			deps: &Deps{
				Git: gitAnswers(nil),
				FS:  noBareRootFS(),
			},
			path: "/tmp/not-a-repo",
			want: "not-a-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SessionNameForWith(tt.deps, tt.path)
			if got != tt.want {
				t.Errorf("SessionNameForWith() = %q, want %q", got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("SessionNameForWith() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q does not name the mechanism (%q)", err, tt.errSubstr)
			}
			if err != nil && !strings.Contains(err.Error(), tt.path) {
				t.Errorf("error %q does not name the checkout %q", err, tt.path)
			}
		})
	}
}
