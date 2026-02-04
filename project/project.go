package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Project represents a project directory
type Project struct {
	Name string
	Path string
}

// NewProject creates a Project from an absolute path
func NewProject(path string) Project {
	return Project{
		Name: filepath.Base(path),
		Path: path,
	}
}

// Worktree represents a git worktree
type Worktree struct {
	Name   string
	Branch string
	Path   string
}

// RepoContext holds information about the current git repository
type RepoContext struct {
	GitRoot  string
	RepoName string
	IsBare   bool
}

// DetectRepoContext determines the git repo context from the current directory
func DetectRepoContext() (*RepoContext, error) {
	// Try to find bare repo root
	if bareRoot := findBareRoot(); bareRoot != "" {
		return &RepoContext{
			GitRoot:  bareRoot,
			RepoName: filepath.Base(bareRoot),
			IsBare:   true,
		}, nil
	}

	// Check git-common-dir for worktree of bare repo
	commonDir, err := gitCommand("rev-parse", "--git-common-dir")
	if err == nil && commonDir != "" {
		isBare, _ := gitCommandInDir(commonDir, "config", "--get", "core.bare")
		if isBare == "true" {
			gitRoot := filepath.Dir(commonDir)
			return &RepoContext{
				GitRoot:  gitRoot,
				RepoName: filepath.Base(gitRoot),
				IsBare:   true,
			}, nil
		}
	}

	// Regular repo
	topLevel, err := gitCommand("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}

	return &RepoContext{
		GitRoot:  topLevel,
		RepoName: filepath.Base(topLevel),
		IsBare:   false,
	}, nil
}

// ListWorktrees returns all worktrees for the current repo context
func ListWorktrees(ctx *RepoContext) ([]Worktree, error) {
	output, err := gitCommandInDir(ctx.GitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktrees(output), nil
}

func parseWorktrees(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			current.Name = filepath.Base(current.Path)
		case strings.HasPrefix(line, "branch "):
			branch := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case line == "detached":
			current.Branch = "detached"
		case line == "":
			if current.Path != "" && current.Name != ".bare" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{}
		}
	}

	// Handle last entry if no trailing newline
	if current.Path != "" && current.Name != ".bare" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// TmuxSessionName generates a tmux-compatible session name
func TmuxSessionName(ctx *RepoContext, worktreeName string) string {
	var name string
	if ctx.IsBare {
		name = ctx.RepoName + "/" + worktreeName
	} else {
		name = worktreeName
	}
	// Replace dots and colons with underscores for tmux compatibility
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

func findBareRoot() string {
	dir, _ := os.Getwd()
	for dir != "/" {
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			isBare, _ := gitCommandInDir(dir, "config", "--get", "core.bare")
			if isBare == "true" {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func gitCommand(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCommandInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// HasWorktrees checks if a directory is a bare repo with worktrees (file-based, no git commands)
func HasWorktrees(path string) bool {
	// Check if .bare directory exists - this indicates a bare repo with worktrees
	bareDir := filepath.Join(path, ".bare")
	if info, err := os.Stat(bareDir); err == nil && info.IsDir() {
		return true
	}

	// Check if .git is a directory with worktrees/ subdirectory containing entries
	// This handles bare repos where .git dir itself is the bare repo (core.bare=true)
	gitWorktreesDir := filepath.Join(path, ".git", "worktrees")
	if info, err := os.Stat(gitWorktreesDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(gitWorktreesDir)
		if err == nil && len(entries) > 0 {
			return true
		}
	}

	return false
}

// ListWorktreesForPath returns worktrees for a given project path (file-based, no git commands)
func ListWorktreesForPath(path string) ([]Worktree, error) {
	var worktrees []Worktree

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".bare" || entry.Name() == ".git" {
			continue
		}

		wtPath := filepath.Join(path, entry.Name())
		gitFile := filepath.Join(wtPath, ".git")

		// Check if .git is a file (not directory) - indicates a worktree
		info, err := os.Stat(gitFile)
		if err != nil || info.IsDir() {
			continue
		}

		worktrees = append(worktrees, Worktree{
			Name: entry.Name(),
			Path: wtPath,
		})
	}

	return worktrees, nil
}

// ExpandedProject represents a project that may be a worktree
type ExpandedProject struct {
	Name        string // Display name (e.g., "project/worktree" or just "project")
	Path        string // Full path to the project/worktree
	ProjectName string // Base project name
	IsWorktree  bool   // Whether this is a worktree of a bare repo
}
