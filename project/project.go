package project

import (
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the project package
type Deps struct {
	Git deps.Git
	FS  deps.FileSystem
}

// DefaultDeps returns dependencies using real implementations
func DefaultDeps() *Deps {
	return &Deps{
		Git: deps.NewRealGit(),
		FS:  deps.NewRealFileSystem(),
	}
}

var defaultDeps = DefaultDeps()

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
// Uses default dependencies
func DetectRepoContext() (*RepoContext, error) {
	return DetectRepoContextWith(defaultDeps)
}

// DetectRepoContextWith determines the git repo context using provided dependencies
func DetectRepoContextWith(d *Deps) (*RepoContext, error) {
	// Try to find bare repo root
	if bareRoot := findBareRootWith(d); bareRoot != "" {
		return &RepoContext{
			GitRoot:  bareRoot,
			RepoName: filepath.Base(bareRoot),
			IsBare:   true,
		}, nil
	}

	// Check git-common-dir for worktree of bare repo
	commonDir, err := d.Git.Command("rev-parse", "--git-common-dir")
	if err == nil && commonDir != "" {
		isBare, _ := d.Git.CommandInDir(commonDir, "config", "--get", "core.bare")
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
	topLevel, err := d.Git.Command("rev-parse", "--show-toplevel")
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
// Uses default dependencies
func ListWorktrees(ctx *RepoContext) ([]Worktree, error) {
	return ListWorktreesWith(defaultDeps, ctx)
}

// ListWorktreesWith returns all worktrees using provided dependencies
func ListWorktreesWith(d *Deps, ctx *RepoContext) ([]Worktree, error) {
	output, err := d.Git.CommandInDir(ctx.GitRoot, "worktree", "list", "--porcelain")
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

func findBareRootWith(d *Deps) string {
	dir, _ := d.FS.Getwd()
	for dir != "/" {
		gitDir := filepath.Join(dir, ".git")
		if info, err := d.FS.Stat(gitDir); err == nil && info.IsDir() {
			isBare, _ := d.Git.CommandInDir(dir, "config", "--get", "core.bare")
			if isBare == "true" {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// HasWorktrees checks if a directory is a bare repo with worktrees (file-based, no git commands)
// Uses default dependencies
func HasWorktrees(path string) bool {
	return HasWorktreesWith(defaultDeps, path)
}

// HasWorktreesWith checks if a directory is a bare repo with worktrees using provided dependencies
func HasWorktreesWith(d *Deps, path string) bool {
	// Check if .bare directory exists - this indicates a bare repo with worktrees
	bareDir := filepath.Join(path, ".bare")
	if info, err := d.FS.Stat(bareDir); err == nil && info.IsDir() {
		return true
	}

	// Check if .git is a directory with worktrees/ subdirectory containing entries
	// AND core.bare=true in config (to avoid false positives from stale worktree metadata)
	gitDir := filepath.Join(path, ".git")
	if info, err := d.FS.Stat(gitDir); err == nil && info.IsDir() {
		if !isCoreBareWith(d, gitDir) {
			return false
		}
		gitWorktreesDir := filepath.Join(gitDir, "worktrees")
		if info, err := d.FS.Stat(gitWorktreesDir); err == nil && info.IsDir() {
			entries, err := d.FS.ReadDir(gitWorktreesDir)
			if err == nil && len(entries) > 0 {
				return true
			}
		}
	}

	return false
}

// isCoreBareWith checks if core.bare=true in the git config file (without running git)
func isCoreBareWith(d *Deps, gitDir string) bool {
	configPath := filepath.Join(gitDir, "config")
	data, err := d.FS.ReadFile(configPath)
	if err != nil {
		return false
	}

	// Simple parsing: look for "bare = true" in [core] section
	lines := strings.Split(string(data), "\n")
	inCoreSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inCoreSection = strings.HasPrefix(strings.ToLower(line), "[core]")
			continue
		}
		if inCoreSection {
			// Normalize and check for bare = true
			normalized := strings.ReplaceAll(strings.ToLower(line), " ", "")
			if normalized == "bare=true" {
				return true
			}
		}
	}
	return false
}

// ListWorktreesForPath returns worktrees for a given project path (file-based, no git commands)
// Uses default dependencies
func ListWorktreesForPath(path string) ([]Worktree, error) {
	return ListWorktreesForPathWith(defaultDeps, path)
}

// ListWorktreesForPathWith returns worktrees using provided dependencies
func ListWorktreesForPathWith(d *Deps, path string) ([]Worktree, error) {
	var worktrees []Worktree

	entries, err := d.FS.ReadDir(path)
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
		info, err := d.FS.Stat(gitFile)
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
