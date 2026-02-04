package config

import (
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

// WorktreeCommand defines a custom command for the worktree picker
type WorktreeCommand struct {
	Key     string `toml:"key"`     // Key binding (e.g., "ctrl-l")
	Label   string `toml:"label"`   // Display label for hints
	Command string `toml:"command"` // Shell command to execute
	Exit    bool   `toml:"exit"`    // Whether to exit picker after execution
}

// WorktreeConfig holds worktree-specific configuration
type WorktreeConfig struct {
	Commands []WorktreeCommand `toml:"commands"`
}

type Config struct {
	Projects          []string        `toml:"projects"`
	ExcludeCurrentDir bool            `toml:"exclude_current_dir"`
	Worktree          *WorktreeConfig `toml:"worktree"`
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
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ExpandProjects resolves all project paths from the config
// Supports exact paths and glob patterns like ~/Dev/*/*
func (c *Config) ExpandProjects() ([]string, error) {
	return c.ExpandProjectsWith(defaultDeps)
}

// ExpandProjectsWith resolves all project paths using provided dependencies
func (c *Config) ExpandProjectsWith(d *Deps) ([]string, error) {
	var projects []string
	seen := make(map[string]bool)

	addProject := func(path string) {
		if !seen[path] && isDirectoryWith(d, path) {
			seen[path] = true
			projects = append(projects, path)
		}
	}

	for _, pattern := range c.Projects {
		expanded := expandHomeWith(d, pattern)

		// Check if it's a glob pattern
		if strings.Contains(expanded, "*") {
			// Resolve symlinks on the base path once, then use it for all matches
			matches, err := expandGlobWithResolvedBase(d, expanded)
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				addProject(match)
			}
		} else {
			// Exact path - resolve symlinks
			resolved := expanded
			if r, err := d.FS.EvalSymlinks(expanded); err == nil {
				resolved = r
			}
			addProject(resolved)
		}
	}

	return projects, nil
}

// expandHomeWith replaces ~ with the user's home directory
func expandHomeWith(d *Deps, path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := d.FS.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// expandGlobWithResolvedBase expands a glob pattern, resolving symlinks in the base path once
func expandGlobWithResolvedBase(d *Deps, pattern string) ([]string, error) {
	// Use doublestar for ** support
	base, pat := doublestar.SplitPattern(pattern)

	// Resolve symlinks in the base path once (e.g., ~/Dev -> /private/Dev)
	resolvedBase := base
	if r, err := d.FS.EvalSymlinks(base); err == nil {
		resolvedBase = r
	}

	fsys := d.FS.DirFS(base)
	matches, err := doublestar.Glob(fsys, pat)
	if err != nil {
		return nil, err
	}

	// Convert to absolute paths using the resolved base
	var results []string
	for _, match := range matches {
		results = append(results, filepath.Join(resolvedBase, match))
	}
	return results, nil
}

func isDirectoryWith(d *Deps, path string) bool {
	info, err := d.FS.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
