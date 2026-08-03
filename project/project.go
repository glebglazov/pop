package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/repokey"
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
		canonCommonDir := canonicalGitPath(path, commonDir)

		isBare, err := d.Git.CommandInDir(commonDir, "config", "--get", "core.bare")
		if err != nil {
			debug.Error("DetectRepoContextFromPath: git config core.bare: %v", err)
		}
		if isBare == "true" {
			// For standard bare repos, commonDir IS the repo root.
			// For bare repos with a .git subdirectory, commonDir points to .git.
			gitRoot := canonCommonDir
			if filepath.Base(canonCommonDir) == ".git" {
				gitRoot = filepath.Dir(canonCommonDir)
			}
			return &RepoContext{
				GitRoot: gitRoot,
				// The <repo>/.bare layout names the repo <repo>, not ".bare":
				// the same helper the linked-worktree branch uses, so one
				// repository is named identically whichever branch resolves it.
				RepoName: repoBasenameFromCommonDir(canonCommonDir),
				IsBare:   true,
			}, nil
		}

		topLevel, err := d.Git.CommandInDir(path, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, err
		}
		topLevel = filepath.Clean(topLevel)

		var mainWorktreePath string
		if filepath.Base(canonCommonDir) == ".git" {
			mainWorktreePath = filepath.Dir(canonCommonDir)
		}

		isLinked, known := linkedWorktreeWith(d, path, canonCommonDir)
		if !known {
			isLinked = mainWorktreePath != "" && filepath.Clean(path) != mainWorktreePath
		}
		if !isLinked && mainWorktreePath == "" {
			// The checkout is the main working tree, so it is its own
			// MainWorktreePath whatever the common dir is called — separate-git-dir
			// and submodule trunks land here, and without this their linked
			// worktrees would have nothing to be compared against and would lose
			// the prefix.
			mainWorktreePath = topLevel
		}

		repoName := filepath.Base(topLevel)
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

// canonicalGitPath absolutises a path git reported relative to the directory it
// was asked about (`rev-parse` answers ".git" for a main checkout).
func canonicalGitPath(gitCwd, reported string) string {
	reported = strings.TrimSpace(reported)
	if reported == "" {
		return ""
	}
	if !filepath.IsAbs(reported) {
		reported = filepath.Join(gitCwd, reported)
	}
	return filepath.Clean(reported)
}

// linkedWorktreeWith asks git whether path is a linked worktree: a linked
// worktree's own git directory is <commonDir>/worktrees/<name>, while a main
// checkout's is the common dir itself. This is the only test that holds for a
// trunk whose git directory is *not* named ".git" — separate-git-dir trunks and
// submodules — where comparing the checkout against a ".git"-derived main
// worktree path answers nothing. known is false when git declined to say, so the
// caller can fall back to the layout comparison.
func linkedWorktreeWith(d *Deps, path, canonCommonDir string) (linked, known bool) {
	gitDir, err := d.Git.CommandInDir(path, "rev-parse", "--git-dir")
	if err != nil {
		return false, false
	}
	canonGitDir := canonicalGitPath(path, gitDir)
	if canonGitDir == "" || canonCommonDir == "" {
		return false, false
	}
	return canonGitDir != canonCommonDir, true
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

// SessionNameWith returns the sanitized tmux session name using provided dependencies,
// logging rather than returning the diagnosis when git cannot answer for the checkout.
// It is the best-effort form for bulk and unattended callers (dashboard rows, drain
// scans); a surface facing a human should call SessionNameForWith and show the
// diagnosis, because the name it gets back may be missing its project prefix.
func SessionNameWith(d *Deps, path string) string {
	name, err := SessionNameForWith(d, path)
	if err != nil {
		debug.Error("SessionName: %v", err)
	}
	return name
}

// SessionNameFor returns the session name for a checkout path together with the
// diagnosis when git could not answer for it. Uses default dependencies.
func SessionNameFor(path string) (string, error) {
	return SessionNameForWith(defaultDeps, path)
}

// SessionNameForWith returns the sanitized tmux session name and, when git could not
// answer for the checkout, an error naming why.
//
// Linked worktrees (bare or non-bare) use repoName/worktreeFolderName, with repoName
// from the git common dir; the main checkout uses the plain directory name. When git
// fails — a pruned worktree administrative directory, a trunk that moved or is
// unmounted, a stray GIT_DIR — the prefix is recovered from the checkout's directory
// layout instead (see SessionNameFromLayoutWith), and only a checkout whose layout
// says nothing degrades to the bare directory name. Either way the failure is
// returned, because a checkout that git cannot answer for is broken whether or not
// its name survived: it is the silence, not the rename alone, that let one checkout
// end up reachable under two session names.
//
// A path that is not a checkout at all — no `.git` entry, not in the managed-worktree
// layout — is simply a directory: its bare name is the right answer and no error is
// returned.
func SessionNameForWith(d *Deps, path string) (string, error) {
	path = filepath.Clean(path)
	worktreeName := filepath.Base(path)
	ctx, err := DetectRepoContextFromPathWith(d, path)
	if err == nil {
		return TmuxSessionNameAt(ctx, path, worktreeName), nil
	}

	if name, ok := SessionNameFromLayoutWith(d, path); ok {
		return name, fmt.Errorf("git cannot answer for checkout %s (its git administrative directory is missing or unreadable): %w; session name %q derived from the directory layout instead", path, err, name)
	}
	degraded := sanitizeSessionName(worktreeName)
	if !hasGitEntry(d, path) {
		return degraded, nil
	}
	return degraded, fmt.Errorf("git cannot answer for checkout %s (its git administrative directory is missing or unreadable): %w; session name degrades to %q with no project prefix", path, err, degraded)
}

// SessionNameFromLayoutWith returns the session name a checkout's directory layout
// implies, reading only the filesystem — no git fork. It knows the two layouts that
// carry a repository name in the path itself:
//
//   - pop's managed-worktree root, <root>/<basename>-<shortHash>/<worktree>, whose
//     parent directory is a repo key;
//   - any linked worktree, whose `.git` is a pointer file naming
//     <commonDir>/worktrees/<name>.
//
// ok is false for a main checkout or a plain directory, where the bare directory name
// is already the right answer, and for a layout it cannot read.
//
// Two callers need this. A checkout git cannot answer for would otherwise lose its
// prefix and become a second session for the same directory. And the project picker
// must name every configured path without forking git per path (ADR-0005, ADR-0110),
// which is what made its worktree paths bare in the first place.
func SessionNameFromLayoutWith(d *Deps, path string) (string, bool) {
	path = filepath.Clean(path)
	if key := filepath.Base(filepath.Dir(path)); repokey.HasKeyShape(key) {
		return sanitizeSessionName(repokey.Basename(key) + "/" + filepath.Base(path)), true
	}
	if commonDir, ok := commonDirFromGitPointer(d, path); ok {
		return sanitizeSessionName(repoBasenameFromCommonDir(commonDir) + "/" + filepath.Base(path)), true
	}
	return "", false
}

// commonDirFromGitPointer reads a linked worktree's `.git` pointer file and returns
// the repository's common directory: the pointer names <commonDir>/worktrees/<name>,
// so stripping the last two components recovers it. ok is false when `.git` is a
// directory (a main checkout or a bare repo), is unreadable, or does not point into a
// worktrees/ administrative directory — the pointer is read, never followed, so a
// trunk that has moved or is unmounted still answers.
func commonDirFromGitPointer(d *Deps, path string) (string, bool) {
	gitPath := filepath.Join(path, ".git")
	info, err := d.FS.Stat(gitPath)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := d.FS.ReadFile(gitPath)
	if err != nil {
		return "", false
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if pointer == "" {
		return "", false
	}
	adminDir := canonicalGitPath(path, pointer)
	if filepath.Base(filepath.Dir(adminDir)) != "worktrees" {
		return "", false
	}
	return filepath.Dir(filepath.Dir(adminDir)), true
}

// hasGitEntry reports whether path carries a `.git` entry at all, which is what
// separates a broken checkout from a directory that was never a checkout. Only the
// former has a name to lose.
func hasGitEntry(d *Deps, path string) bool {
	_, err := d.FS.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// FastSessionNameWith is FastSessionName plus every prefix the filesystem can prove:
// the fork-free naming for bulk surfaces that must not pay a git call per item (the
// project picker's expansion, ADR-0005/ADR-0110). It agrees with SessionName for
// managed worktrees, linked worktrees, main checkouts and plain directories, and
// differs only for a worktree of a bare repository, whose repository name lives
// nowhere in its path.
func FastSessionNameWith(d *Deps, path string) string {
	if name, ok := SessionNameFromLayoutWith(d, path); ok {
		return name
	}
	return FastSessionName(path)
}

// CheckoutSession returns the tmux session that belongs to a checkout: the one
// answer to "where does work on this directory happen" (ADR-0180). Uses default
// dependencies.
func CheckoutSession(path string) (string, error) {
	return CheckoutSessionWith(defaultDeps, path)
}

// CheckoutSessionWith is the injectable variant, and the single owner of the rule.
//
// Everything that opens a pane for a checkout derives from it — a Task set's drain,
// verify, assist, fold and runtime shell, the Work daemon's unattended auto-drain,
// a Routine's fire pane, and the `ctrl+g` worktree open — so all of them land in
// the same session and a checkout is never reachable under two names.
//
// The name is SessionNameForWith's, with its diagnosis: a checkout git cannot answer
// for still gets a name, and the caller is told why it may be the wrong one. An empty
// path is an error with no name, because no session belongs to nothing — a caller with
// no checkout in hand must refuse rather than name a session after its cwd.
func CheckoutSessionWith(d *Deps, path string) (string, error) {
	if d == nil {
		d = defaultDeps
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("no checkout to derive a tmux session from")
	}
	return SessionNameForWith(d, path)
}

// CheckoutSessionNameWith is CheckoutSessionWith for a caller with nowhere to show a
// diagnosis — an unattended spawn, a bulk row — logging the reason instead.
func CheckoutSessionNameWith(d *Deps, path string) string {
	name, err := CheckoutSessionWith(d, path)
	if err != nil {
		debug.Error("CheckoutSession: %v", err)
	}
	return name
}

// CheckoutSessionOrWith is CheckoutSessionNameWith for a directory that may not be a
// checkout at all, answering fallback for those. A Routine's bound directory is any
// directory the operator named, so routines land their panes in one shared session
// rather than in a session named after some directory git has never heard of.
func CheckoutSessionOrWith(d *Deps, path, fallback string) string {
	if d == nil {
		d = defaultDeps
	}
	if _, err := DetectRepoContextFromPathWith(d, path); err != nil {
		return fallback
	}
	return CheckoutSessionNameWith(d, path)
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
