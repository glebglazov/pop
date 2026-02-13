package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the config package
type Deps struct {
	FS deps.FileSystem
}

// DefaultDeps returns dependencies using real implementations
func DefaultDeps() *Deps {
	return &Deps{
		FS: deps.NewRealFileSystem(),
	}
}

var defaultDeps = DefaultDeps()

// UserDefinedCommand defines a custom keybinding for a picker
type UserDefinedCommand struct {
	Key     string `toml:"key"`     // Key binding (e.g., "ctrl-l")
	Label   string `toml:"label"`   // Display label for hints
	Command string `toml:"command"` // Shell command to execute
	Exit    bool   `toml:"exit"`    // Whether to exit picker after execution
}

// WorktreeConfig holds worktree-specific configuration
type WorktreeConfig struct {
	Commands []UserDefinedCommand `toml:"commands"`
}

// SelectConfig holds select-specific configuration
type SelectConfig struct {
	Commands []UserDefinedCommand `toml:"commands"`
}

// ProjectEntry represents a project configuration entry.
type ProjectEntry struct {
	Path         string `toml:"path"`
	DisplayDepth int    `toml:"display_depth"` // number of path segments to show in display name; 0 means use default (1)
}

// GetDisplayDepth returns the effective display depth.
// Returns 1 if not explicitly set (DisplayDepth == 0).
func (p ProjectEntry) GetDisplayDepth() int {
	if p.DisplayDepth <= 0 {
		return 1
	}
	return p.DisplayDepth
}

type Config struct {
	Includes               []string             `toml:"includes"`
	Projects               []ProjectEntry       `toml:"projects"`
	Commands               []UserDefinedCommand `toml:"commands"`
	ExcludeCurrentDir      bool                 `toml:"exclude_current_dir"`
	DisambiguationStrategy string               `toml:"disambiguation_strategy"`
	QuickAccessModifier    string               `toml:"quick_access_modifier"`
	Worktree               *WorktreeConfig      `toml:"worktree"`
	Select                 *SelectConfig        `toml:"select"`

	Warnings []string `toml:"-"` // non-serialized warnings from config loading
}

// ExpandedPath represents a resolved project path with display metadata
type ExpandedPath struct {
	Path         string
	DisplayDepth int // number of path segments to show in display name
}

// GetDisambiguationStrategy returns the configured disambiguation strategy.
// Defaults to "first_unique_segment" when not set or invalid.
func (c *Config) GetDisambiguationStrategy() string {
	if c.DisambiguationStrategy == "full_path" {
		return "full_path"
	}
	return "first_unique_segment"
}

// GetQuickAccessModifier returns the configured quick access modifier.
// Defaults to "alt" when not set or invalid.
func (c *Config) GetQuickAccessModifier() string {
	switch c.QuickAccessModifier {
	case "alt", "ctrl", "disabled":
		return c.QuickAccessModifier
	default:
		return "alt"
	}
}

// CommandsForMode returns the effective custom commands for the given mode
// ("select" or "worktree"). Section-specific commands override global ones
// matched by key.
func (c *Config) CommandsForMode(mode string) []UserDefinedCommand {
	byKey := make(map[string]UserDefinedCommand)
	for _, cmd := range c.Commands {
		byKey[cmd.Key] = cmd
	}

	var sectionCmds []UserDefinedCommand
	switch mode {
	case "select":
		if c.Select != nil {
			sectionCmds = c.Select.Commands
		}
	case "worktree":
		if c.Worktree != nil {
			sectionCmds = c.Worktree.Commands
		}
	}
	for _, cmd := range sectionCmds {
		byKey[cmd.Key] = cmd
	}

	// Collect in stable order: global order first, then section-only additions
	var result []UserDefinedCommand
	seen := make(map[string]bool)
	for _, cmd := range c.Commands {
		result = append(result, byKey[cmd.Key])
		seen[cmd.Key] = true
	}
	for _, cmd := range sectionCmds {
		if !seen[cmd.Key] {
			result = append(result, cmd)
			seen[cmd.Key] = true
		}
	}
	return result
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() string {
	return DefaultConfigPathWith(defaultDeps)
}

// DefaultConfigPathWith returns the default config file path using provided dependencies
func DefaultConfigPathWith(d *Deps) string {
	if xdgConfig := d.FS.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "pop", "config.toml")
	}
	home, _ := d.FS.UserHomeDir()
	return filepath.Join(home, ".config", "pop", "config.toml")
}

// Load reads the config file from the given path
func Load(path string) (*Config, error) {
	return LoadWith(defaultDeps, path)
}

// LoadWith reads the config file using provided dependencies for ~ expansion
func LoadWith(d *Deps, path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}

	configDir := filepath.Dir(path)
	for _, include := range cfg.Includes {
		expanded := expandHomeWith(d, include)
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(configDir, expanded)
		}

		var included Config
		if _, err := toml.DecodeFile(expanded, &included); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("include file %q not found, skipping", include))
				continue
			}
			return nil, fmt.Errorf("loading include %q: %w", include, err)
		}

		cfg.Projects = append(cfg.Projects, included.Projects...)
	}

	return &cfg, nil
}

// ExpandProjects resolves all project paths from the config
// Supports exact paths and glob patterns like ~/Dev/*/*
func (c *Config) ExpandProjects() ([]ExpandedPath, error) {
	return c.ExpandProjectsWith(defaultDeps)
}

// ExpandProjectsWith resolves all project paths using provided dependencies
func (c *Config) ExpandProjectsWith(d *Deps) ([]ExpandedPath, error) {
	cachePath := DefaultCachePathWith(d)
	cache := loadGlobCache(d, cachePath)
	cacheModified := false

	var projects []ExpandedPath
	seen := make(map[string]bool)

	addProject := func(path string, displayDepth int) {
		if !seen[path] && isDirectoryWith(d, path) {
			seen[path] = true
			projects = append(projects, ExpandedPath{Path: path, DisplayDepth: displayDepth})
		}
	}

	for _, entry := range c.Projects {
		expanded := expandHomeWith(d, entry.Path)
		displayDepth := entry.GetDisplayDepth()

		// Check if it's a glob pattern (only single * allowed, not **)
		if strings.Contains(expanded, "**") {
			continue // Skip recursive glob patterns
		}
		if strings.Contains(expanded, "*") {
			matches, updated, err := expandGlobCached(d, expanded, cache)
			if updated {
				cacheModified = true
			}
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				addProject(match, displayDepth)
			}
		} else {
			// Exact path - resolve symlinks
			resolved := expanded
			if r, err := d.FS.EvalSymlinks(expanded); err == nil {
				resolved = r
			}
			addProject(resolved, displayDepth)
		}
	}

	if cacheModified {
		saveGlobCache(d, cachePath, cache)
	}

	return removeSubsumedPaths(projects), nil
}

// removeSubsumedPaths filters out paths that are strict parents of other paths
// in the set. This implements "more specific wins" — if both /a/b and /a/b/c
// are in the list, /a/b is removed. Works transitively.
func removeSubsumedPaths(paths []ExpandedPath) []ExpandedPath {
	subsumed := make(map[string]bool)
	for _, p := range paths {
		for _, q := range paths {
			if p.Path != q.Path && strings.HasPrefix(q.Path, p.Path+"/") {
				subsumed[p.Path] = true
				break
			}
		}
	}

	var result []ExpandedPath
	for _, p := range paths {
		if !subsumed[p.Path] {
			result = append(result, p)
		}
	}
	return result
}

// expandHomeWith replaces ~ with the user's home directory
func expandHomeWith(d *Deps, path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := d.FS.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// expandGlobWithBase expands a glob pattern and returns both the matches
// and the resolved base path (after symlink resolution).
func expandGlobWithBase(d *Deps, pattern string) ([]string, string, error) {
	base, pat := doublestar.SplitPattern(pattern)

	// Resolve symlinks in the base path once (e.g., ~/Dev -> /private/Dev)
	resolvedBase := base
	if r, err := d.FS.EvalSymlinks(base); err == nil {
		resolvedBase = r
	}

	fsys := d.FS.DirFS(base)
	matches, err := doublestar.Glob(fsys, pat, doublestar.WithNoHidden())
	if err != nil {
		return nil, "", err
	}

	// Convert to absolute paths using the resolved base
	var results []string
	for _, match := range matches {
		results = append(results, filepath.Join(resolvedBase, match))
	}
	return results, resolvedBase, nil
}

func isDirectoryWith(d *Deps, path string) bool {
	info, err := d.FS.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
