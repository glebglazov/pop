package project

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestParseBranches(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Branch
	}{
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name: "main and master ordered first",
			input: `refs/heads/feature-x
refs/heads/master
refs/heads/main
refs/heads/bugfix`,
			expected: []Branch{
				{Ref: "main"},
				{Ref: "master"},
				{Ref: "feature-x"},
				{Ref: "bugfix"},
			},
		},
		{
			name: "remote branches after locals, origin/HEAD excluded",
			input: `refs/heads/main
refs/remotes/origin/HEAD
refs/remotes/origin/main
refs/remotes/origin/feature`,
			expected: []Branch{
				{Ref: "main"},
				{Ref: "origin/main", IsRemote: true},
				{Ref: "origin/feature", IsRemote: true},
			},
		},
		{
			name: "only remotes",
			input: `refs/remotes/origin/HEAD
refs/remotes/origin/dev`,
			expected: []Branch{
				{Ref: "origin/dev", IsRemote: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBranches(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseBranches() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestListBranchesWith(t *testing.T) {
	var gotArgs []string
	d := &Deps{
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				gotArgs = args
				return "refs/heads/main\nrefs/remotes/origin/HEAD\nrefs/remotes/origin/main", nil
			},
		},
	}
	ctx := &RepoContext{GitRoot: "/repo"}

	branches, err := ListBranchesWith(d, ctx)
	if err != nil {
		t.Fatalf("ListBranchesWith() error: %v", err)
	}
	want := []Branch{{Ref: "main"}, {Ref: "origin/main", IsRemote: true}}
	if !reflect.DeepEqual(branches, want) {
		t.Errorf("ListBranchesWith() = %+v, want %+v", branches, want)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "for-each-ref" {
		t.Errorf("expected for-each-ref invocation, got %v", gotArgs)
	}
}

func TestDeriveWorktreeName(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		isRemote   bool
		wantBranch string
		wantDir    string
	}{
		{"local simple", "feature", false, "feature", "feature"},
		{"local with slash", "feature/login", false, "feature/login", "feature-login"},
		{"remote strips prefix", "origin/feature", true, "feature", "feature"},
		{"remote with slash", "origin/feature/login", true, "feature/login", "feature-login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch, dir := DeriveWorktreeName(tt.ref, tt.isRemote)
			if branch != tt.wantBranch || dir != tt.wantDir {
				t.Errorf("DeriveWorktreeName(%q, %v) = (%q, %q), want (%q, %q)",
					tt.ref, tt.isRemote, branch, dir, tt.wantBranch, tt.wantDir)
			}
		})
	}
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name string
		ctx  *RepoContext
		dir  string
		want string
	}{
		{
			name: "bare repo path is under git root",
			ctx:  &RepoContext{GitRoot: "/home/user/myrepo", RepoName: "myrepo", IsBare: true},
			dir:  "feature-x",
			want: "/home/user/myrepo/feature-x",
		},
		{
			name: "non-bare path is a sibling of the repo",
			ctx:  &RepoContext{GitRoot: "/home/user/myrepo", RepoName: "myrepo", IsBare: false},
			dir:  "feature-x",
			want: "/home/user/myrepo-feature-x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WorktreePath(tt.ctx, tt.dir); got != tt.want {
				t.Errorf("WorktreePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAddWorktreeNamedForksNewBranchOffBase locks the ADR-0076 semantic that the
// human-typed name is the NEW branch name and the picked ref is only the fork
// base. Regression guard: picking an already-checked-out branch (master) and
// typing a fresh name must `-b <name> ... master`, not re-check-out master.
func TestAddWorktreeNamedForksNewBranchOffBase(t *testing.T) {
	var addArgs []string
	d := &Deps{
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "show-ref" {
					return "", fmt.Errorf("not found") // no local branch named "feature-x"
				}
				if len(args) > 0 && args[0] == "worktree" {
					addArgs = args
				}
				return "", nil
			},
		},
	}
	ctx := &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true}
	path, err := AddWorktreeNamedWith(d, ctx, Branch{Ref: "master"}, "feature-x")
	if err != nil {
		t.Fatalf("AddWorktreeNamedWith() error: %v", err)
	}
	if path != "/repo/feature-x" {
		t.Errorf("path = %q, want %q", path, "/repo/feature-x")
	}
	want := []string{"worktree", "add", "-b", "feature-x", "/repo/feature-x", "master"}
	if !reflect.DeepEqual(addArgs, want) {
		t.Errorf("git args = %v, want %v", addArgs, want)
	}
}

func TestAddWorktreeWith(t *testing.T) {
	tests := []struct {
		name        string
		selection   Branch
		ctx         *RepoContext
		branchExist bool
		wantPath    string
		wantAddArgs []string
	}{
		{
			name:        "existing local branch is reused",
			selection:   Branch{Ref: "feature"},
			ctx:         &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true},
			branchExist: true,
			wantPath:    "/repo/feature",
			wantAddArgs: []string{"worktree", "add", "/repo/feature", "feature"},
		},
		{
			name:        "new branch created with -b when none exists",
			selection:   Branch{Ref: "feature"},
			ctx:         &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true},
			branchExist: false,
			wantPath:    "/repo/feature",
			wantAddArgs: []string{"worktree", "add", "-b", "feature", "/repo/feature", "feature"},
		},
		{
			name:        "remote selection creates local tracking branch",
			selection:   Branch{Ref: "origin/feature", IsRemote: true},
			ctx:         &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true},
			branchExist: false,
			wantPath:    "/repo/feature",
			wantAddArgs: []string{"worktree", "add", "-b", "feature", "/repo/feature", "origin/feature"},
		},
		{
			name:        "non-bare path is a sibling",
			selection:   Branch{Ref: "feature/login"},
			ctx:         &RepoContext{GitRoot: "/home/user/repo", RepoName: "repo", IsBare: false},
			branchExist: false,
			wantPath:    "/home/user/repo-feature-login",
			wantAddArgs: []string{"worktree", "add", "-b", "feature-login", "/home/user/repo-feature-login", "feature/login"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var addArgs []string
			d := &Deps{
				Git: &deps.MockGit{
					CommandInDirFunc: func(dir string, args ...string) (string, error) {
						if len(args) > 0 && args[0] == "show-ref" {
							if tt.branchExist {
								return "", nil
							}
							return "", fmt.Errorf("not found")
						}
						if len(args) > 0 && args[0] == "worktree" {
							addArgs = args
						}
						return "", nil
					},
				},
			}

			path, err := AddWorktreeWith(d, tt.ctx, tt.selection)
			if err != nil {
				t.Fatalf("AddWorktreeWith() error: %v", err)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
			if !reflect.DeepEqual(addArgs, tt.wantAddArgs) {
				t.Errorf("git args = %v, want %v", addArgs, tt.wantAddArgs)
			}
		})
	}
}

// TestAddWorktreeNamedRefusesDefaultNameWhenBaseBranchCheckedOutElsewhere pins
// the ADR-0152 create-flow regression: pressing Enter twice (accept the
// base-ref picker's default, then the name prompt's suggested default) forks
// off the base branch and reuses its own name for the new worktree. When that
// base is the branch already checked out in another worktree (the common
// case — the trunk's own branch), `git worktree add` refuses outright.
// Reproduced against real git: `fatal: 'master' is already used by worktree
// at '<path>'`. This test pins the friendly refusal AddWorktreeNamedWith now
// returns instead of letting that raw fatal reach the human.
func TestAddWorktreeNamedRefusesDefaultNameWhenBaseBranchCheckedOutElsewhere(t *testing.T) {
	selection := Branch{Ref: "master"}
	_, defaultDir := DeriveWorktreeName(selection.Ref, selection.IsRemote)
	if defaultDir != "master" {
		t.Fatalf("sanity: defaultDir = %q, want %q", defaultDir, "master")
	}

	ctx := &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true}
	d := &Deps{
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				switch {
				case len(args) > 0 && args[0] == "show-ref":
					return "", nil // "master" already exists locally — it's the base itself
				case len(args) > 1 && args[0] == "worktree" && args[1] == "list":
					return "worktree /repo\nbranch refs/heads/master\n\n", nil
				case len(args) > 1 && args[0] == "worktree" && args[1] == "add":
					t.Fatalf("git worktree add must not run once the branch is known checked out elsewhere")
				}
				return "", nil
			},
		},
	}

	_, err := AddWorktreeNamedWith(d, ctx, selection, defaultDir)
	if err == nil {
		t.Fatal("expected a refusal, got nil error")
	}
	if !errors.Is(err, ErrBranchCheckedOutElsewhere) {
		t.Fatalf("error = %v, want wrapping ErrBranchCheckedOutElsewhere", err)
	}
	if strings.Contains(err.Error(), "fatal:") {
		t.Fatalf("error leaked a raw git message: %v", err)
	}
	if !strings.Contains(err.Error(), "/repo") {
		t.Fatalf("error should name where the branch is already checked out: %v", err)
	}
}

// TestAddWorktreeNamedFreshNameUnaffectedByCheckedOutOtherBranch confirms the
// ordinary path — typing a fresh name unrelated to any checked-out branch —
// is untouched by the new check: no local branch matches, so
// LocalBranchExistsWith is false and the check never runs.
func TestAddWorktreeNamedFreshNameUnaffectedByCheckedOutOtherBranch(t *testing.T) {
	var addArgs []string
	d := &Deps{
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				switch {
				case len(args) > 0 && args[0] == "show-ref":
					return "", fmt.Errorf("not found") // no local branch named "feature-x"
				case len(args) > 1 && args[0] == "worktree" && args[1] == "list":
					t.Fatalf("checked-out lookup must not run for a fresh, non-colliding name")
				case len(args) > 0 && args[0] == "worktree":
					addArgs = args
				}
				return "", nil
			},
		},
	}
	ctx := &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true}
	path, err := AddWorktreeNamedWith(d, ctx, Branch{Ref: "master"}, "feature-x")
	if err != nil {
		t.Fatalf("AddWorktreeNamedWith() error: %v", err)
	}
	if path != "/repo/feature-x" {
		t.Errorf("path = %q, want %q", path, "/repo/feature-x")
	}
	want := []string{"worktree", "add", "-b", "feature-x", "/repo/feature-x", "master"}
	if !reflect.DeepEqual(addArgs, want) {
		t.Errorf("git args = %v, want %v", addArgs, want)
	}
}

func TestAddWorktreePropagatesError(t *testing.T) {
	d := &Deps{
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "worktree" {
					return "", fmt.Errorf("fatal: boom")
				}
				return "", fmt.Errorf("not found")
			},
		},
	}
	_, err := AddWorktreeWith(d, &RepoContext{GitRoot: "/repo", RepoName: "repo", IsBare: true}, Branch{Ref: "x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected worktree add error, got %v", err)
	}
}
