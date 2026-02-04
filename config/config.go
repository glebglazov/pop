package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bmatcuk/doublestar/v4"
)

type Config struct {
	Projects []string `toml:"projects"`
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "pop", "config.toml")
	}
	home, _ := os.UserHomeDir()
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
	var projects []string
	seen := make(map[string]bool)

	for _, pattern := range c.Projects {
		expanded := expandHome(pattern)

		// Check if it's a glob pattern
		if strings.Contains(expanded, "*") {
			matches, err := expandGlob(expanded)
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				if !seen[match] && isDirectory(match) {
					seen[match] = true
					projects = append(projects, match)
				}
			}
		} else {
			// Exact path
			if !seen[expanded] && isDirectory(expanded) {
				seen[expanded] = true
				projects = append(projects, expanded)
			}
		}
	}

	return projects, nil
}

// expandHome replaces ~ with the user's home directory
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// expandGlob expands a glob pattern to matching paths
func expandGlob(pattern string) ([]string, error) {
	// Use doublestar for ** support
	base, pattern := doublestar.SplitPattern(pattern)
	fsys := os.DirFS(base)
	matches, err := doublestar.Glob(fsys, pattern)
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

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
