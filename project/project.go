package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/debug"
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

// SetDefaultDeps swaps the package-global dependencies used by the wrapper
// functions (SessionName, DetectRepoContext, etc.) and returns a function that
// restores the previous value. It exists so tests can observe or count the git
// and filesystem calls those wrappers make. Not for production use.
func SetDefaultDeps(d *Deps) (restore func()) {
	prev := defaultDeps
	defaultDeps = d
	return func() { defaultDeps = prev }
}

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
	GitRoot          string
	RepoName         string
	IsBare           bool
	IsLinkedWorktree bool   // checkout path is a linked worktree, not the main working tree
	MainWorktreePath string // non-bare main checkout path; parent of the common .git dir
}

// DetectRepoContext determines the git repo context from the current directory
// Uses default dependencies
func DetectRepoContext() (*RepoContext, error) {
	return DetectRepoContextWith(defaultDeps)
}

// DetectRepoContextWith determines the git repo context using provided dependencies
func DetectRepoContextWith(d *Deps) (*RepoContext, error) {
	cwd, err := d.FS.Getwd()
	if err != nil {
		return nil, err
	}
	return DetectRepoContextFromPathWith(d, cwd)
}

// DetectRepoContextFromPathWith determines git repo context for a checkout path
func DetectRepoContextFromPathWith(d *Deps, path string) (*RepoContext, error) {
	path = filepath.Clean(path)

	// Try to find bare repo root
	if bareRoot := findBareRootWith(d, path); bareRoot != "" {
		return &RepoContext{
			GitRoot:  bareRoot,
			RepoName: filepath.Base(bareRoot),
			IsBare:   true,
		}, nil
	}

	// Check git-common-dir for worktree of bare repo
	commonDir, err := d.Git.CommandInDir(path, "rev-parse", "--git-common-dir")
	if err == nil && commonDir != "" {
		isBare, err := d.Git.CommandInDir(commonDir, "config", "--get", "core.bare")
		if err != nil {
			debug.Error("DetectRepoContextFromPath: git config core.bare: %v", err)
		}
		if isBare == "true" {
			// For standard bare repos, commonDir IS the repo root.
			// For bare repos with a .git subdirectory, commonDir points to .git.
			gitRoot := commonDir
			if filepath.Base(commonDir) == ".git" {
				gitRoot = filepath.Dir(commonDir)
			}
			return &RepoContext{
				GitRoot:  gitRoot,
				RepoName: filepath.Base(gitRoot),
				IsBare:   true,
			}, nil
		}

		topLevel, err := d.Git.CommandInDir(path, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, err
		}
		topLevel = filepath.Clean(topLevel)

		canonCommonDir := commonDir
		if !filepath.IsAbs(canonCommonDir) {
			canonCommonDir = filepath.Join(path, canonCommonDir)
		}
		canonCommonDir = filepath.Clean(canonCommonDir)

		var mainWorktreePath string
		if filepath.Base(canonCommonDir) == ".git" {
			mainWorktreePath = filepath.Dir(canonCommonDir)
		}

		repoName := filepath.Base(topLevel)
		isLinked := mainWorktreePath != "" && filepath.Clean(path) != filepath.Clean(mainWorktreePath)
		if isLinked {
			repoName = repoBasenameFromCommonDir(canonCommonDir)
		}

		return &RepoContext{
			GitRoot:          topLevel,
			RepoName:         repoName,
			IsBare:           false,
			IsLinkedWorktree: isLinked,
			MainWorktreePath: mainWorktreePath,
		}, nil
	}

	// Regular repo (common dir unavailable — fall back to show-toplevel only)
	topLevel, err := d.Git.CommandInDir(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}

	return &RepoContext{
		GitRoot:  topLevel,
		RepoName: filepath.Base(topLevel),
		IsBare:   false,
	}, nil
}

// CurrentCheckoutPath returns the absolute path of the git worktree containing
// the current directory (git rev-parse --show-toplevel). This is the exact
// worktree path used to key the per-worktree Preferred workbench store
// (ADR-0078), so `pop workbench prefer` writes the entry for the checkout the
// operator is standing in — not merely their cwd. Uses default dependencies.
func CurrentCheckoutPath() (string, error) {
	return CurrentCheckoutPathWith(defaultDeps)
}

// CurrentCheckoutPathWith is the injectable variant.
func CurrentCheckoutPathWith(d *Deps) (string, error) {
	cwd, err := d.FS.Getwd()
	if err != nil {
		return "", err
	}
	topLevel, err := d.Git.CommandInDir(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not in a git worktree")
	}
	topLevel = strings.TrimSpace(topLevel)
	if topLevel == "" {
		return "", fmt.Errorf("not in a git worktree")
	}
	return filepath.Clean(topLevel), nil
}

// SessionName returns the sanitized tmux session name for a checkout path.
// Uses default dependencies.
func SessionName(path string) string {
	return SessionNameWith(defaultDeps, path)
}

// SessionNameWith returns the sanitized tmux session name using provided dependencies.
// Linked worktrees (bare or non-bare) use repoName/worktreeFolderName, with repoName
// from the git common dir; the main checkout uses the plain directory name; non-git
// paths fall back to the directory base name.
func SessionNameWith(d *Deps, path string) string {
	path = filepath.Clean(path)
	worktreeName := filepath.Base(path)
	ctx, err := DetectRepoContextFromPathWith(d, path)
	if err != nil {
		return sanitizeSessionName(worktreeName)
	}
	return TmuxSessionNameAt(ctx, path, worktreeName)
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
	isBare := false

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
		case line == "bare":
			isBare = true
		case line == "":
			if current.Path != "" && current.Name != ".bare" && !isBare {
				worktrees = append(worktrees, current)
			}
			current = Worktree{}
			isBare = false
		}
	}

	// Handle last entry if no trailing newline
	if current.Path != "" && current.Name != ".bare" && !isBare {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// TmuxSessionName generates a tmux-compatible session name for worktreeName when
// checkoutPath is unknown. Prefer TmuxSessionNameAt when the worktree path is known.
func TmuxSessionName(ctx *RepoContext, worktreeName string) string {
	return TmuxSessionNameAt(ctx, "", worktreeName)
}

// TmuxSessionNameAt generates a tmux-compatible session name for the worktree at
// checkoutPath. Linked worktrees (bare or non-bare) are prefixed with repoName.
func TmuxSessionNameAt(ctx *RepoContext, checkoutPath, worktreeName string) string {
	prefixed := ctx.IsBare
	if !prefixed && checkoutPath != "" && ctx.MainWorktreePath != "" {
		prefixed = filepath.Clean(checkoutPath) != filepath.Clean(ctx.MainWorktreePath)
	} else if !prefixed && ctx.IsLinkedWorktree {
		prefixed = true
	}
	var name string
	if prefixed {
		name = ctx.RepoName + "/" + worktreeName
	} else {
		name = worktreeName
	}
	return sanitizeSessionName(name)
}

// repoBasenameFromCommonDir derives a repository display name from a canonical git
// common directory (same rules as tasks.RepoBasename).
func repoBasenameFromCommonDir(commonDir string) string {
	base := filepath.Base(commonDir)
	switch base {
	case ".git", ".bare":
		return filepath.Base(filepath.Dir(commonDir))
	}
	return strings.TrimSuffix(base, ".git")
}

// FastSessionName returns a best-effort session name from a path without
// calling git. It uses the directory base name with tmux-safe sanitization.
//
// This matches SessionName for a repository's main checkout and for non-git paths.
// For every linked worktree (bare or non-bare) the exact name is
// repoName/worktreeFolderName; this returns only the worktree folder. Use it
// only for fuzzy/bulk matching (dashboard history sorting, test helpers) where
// speed matters more than exactness. See ADR-0005 and ADR-0157.
func FastSessionName(path string) string {
	return sanitizeSessionName(filepath.Base(path))
}

func sanitizeSessionName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

func findBareRootWith(d *Deps, startDir string) string {
	dir := startDir
	if dir == "" {
		var err error
		dir, err = d.FS.Getwd()
		if err != nil {
			debug.Error("findBareRoot: Getwd: %v", err)
			return ""
		}
	}
	for dir != "/" {
		gitDir := filepath.Join(dir, ".git")
		if info, err := d.FS.Stat(gitDir); err == nil && info.IsDir() {
			isBare, err := d.Git.CommandInDir(dir, "config", "--get", "core.bare")
			if err != nil {
				debug.Error("findBareRoot: git config core.bare in %s: %v", dir, err)
			}
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
		return hasNonEmptyWorktreesDir(d, gitDir)
	}

	// Check if path itself is a top-level bare repo (git clone --bare layout) with
	// worktrees/ subdirectory containing entries AND core.bare=true in config
	if isCoreBareWith(d, path) {
		return hasNonEmptyWorktreesDir(d, path)
	}

	return false
}

// hasNonEmptyWorktreesDir reports whether <basePath>/worktrees exists and contains entries.
func hasNonEmptyWorktreesDir(d *Deps, basePath string) bool {
	worktreesDir := filepath.Join(basePath, "worktrees")
	info, err := d.FS.Stat(worktreesDir)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := d.FS.ReadDir(worktreesDir)
	if err != nil {
		return false
	}
	return len(entries) > 0
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
	Name         string // Display name (e.g., "project/worktree" or just "project")
	ProjectLabel string // Repository display label — depth-aware Name without the trailing worktree segment (e.g. "project" for "project/worktree")
	Path         string // Full path to the project/worktree
	ProjectName  string // Base project name
	IsWorktree   bool   // Whether this is a worktree of a bare repo
	SessionName  string // Pre-computed tmux session name
}
