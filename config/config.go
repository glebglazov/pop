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

type Config struct {
	Projects          []string `toml:"projects"`
	ExcludeCurrentDir bool     `toml:"exclude_current_dir"`
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

	for _, pattern := range c.Projects {
		expanded := expandHomeWith(d, pattern)

		// Check if it's a glob pattern
		if strings.Contains(expanded, "*") {
			matches, err := expandGlobWith(d, expanded)
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				if !seen[match] && isDirectoryWith(d, match) {
					seen[match] = true
					projects = append(projects, match)
				}
			}
		} else {
			// Exact path
			if !seen[expanded] && isDirectoryWith(d, expanded) {
				seen[expanded] = true
				projects = append(projects, expanded)
			}
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

// expandGlobWith expands a glob pattern to matching paths
func expandGlobWith(d *Deps, pattern string) ([]string, error) {
	// Use doublestar for ** support
	base, pat := doublestar.SplitPattern(pattern)
	fsys := d.FS.DirFS(base)
	matches, err := doublestar.Glob(fsys, pat)
	if err != nil {
		return nil, err
	}

	// Convert to absolute paths
	var results []string
	for _, match := range matches {
		results = append(results, filepath.Join(base, match))
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
