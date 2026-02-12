package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// GlobCacheEntry stores cached results for a single glob pattern
type GlobCacheEntry struct {
	// BasePath is the resolved base directory (after EvalSymlinks)
	BasePath string `json:"base_path"`
	// Matches are the raw glob results (absolute paths, before isDirectory filtering)
	Matches []string `json:"matches"`
	// DirMtimes maps each tracked directory path to its mtime at cache time.
	// Includes the base directory and all intermediate directories whose
	// contents contribute to the glob result.
	DirMtimes map[string]time.Time `json:"dir_mtimes"`
}

// GlobCache holds cached glob expansion results
type GlobCache struct {
	// Version for future format changes
	Version int `json:"version"`
	// Entries maps the expanded glob pattern (after ~ expansion) to its cache entry
	Entries map[string]GlobCacheEntry `json:"entries"`
}

// DefaultCachePath returns the default cache file path
func DefaultCachePath() string {
	return DefaultCachePathWith(defaultDeps)
}

// DefaultCachePathWith returns the default cache file path using provided dependencies
func DefaultCachePathWith(d *Deps) string {
	if xdgCache := d.FS.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		return filepath.Join(xdgCache, "pop", "glob_cache.json")
	}
	home, _ := d.FS.UserHomeDir()
	return filepath.Join(home, ".cache", "pop", "glob_cache.json")
}

// loadGlobCache reads the cache file. Returns empty cache on any error.
func loadGlobCache(d *Deps, path string) *GlobCache {
	cache := &GlobCache{Version: 1, Entries: make(map[string]GlobCacheEntry)}

	data, err := d.FS.ReadFile(path)
	if err != nil {
		return cache
	}

	var loaded GlobCache
	if err := json.Unmarshal(data, &loaded); err != nil || loaded.Version != 1 {
		return cache
	}

	if loaded.Entries == nil {
		loaded.Entries = make(map[string]GlobCacheEntry)
	}

	return &loaded
}

// saveGlobCache writes the cache file. Errors are silently ignored (cache is best-effort).
func saveGlobCache(d *Deps, path string, cache *GlobCache) {
	dir := filepath.Dir(path)
	if err := d.FS.MkdirAll(dir, 0755); err != nil {
		return
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}

	d.FS.WriteFile(path, data, 0644)
}

// isCacheEntryValid checks if a cached glob entry is still valid by comparing
// stored directory mtimes against the current filesystem state.
func isCacheEntryValid(d *Deps, entry GlobCacheEntry) bool {
	for dirPath, cachedMtime := range entry.DirMtimes {
		info, err := d.FS.Stat(dirPath)
		if err != nil {
			return false
		}
		if !info.ModTime().Equal(cachedMtime) {
			return false
		}
	}
	return true
}

// expandGlobCached attempts to use cached glob results. Returns the matches,
// whether the cache was updated, and any error.
func expandGlobCached(d *Deps, pattern string, cache *GlobCache) ([]string, bool, error) {
	if entry, ok := cache.Entries[pattern]; ok {
		if isCacheEntryValid(d, entry) {
			return entry.Matches, false, nil
		}
	}

	// Cache miss — perform actual glob
	matches, resolvedBase, err := expandGlobWithBase(d, pattern)
	if err != nil {
		delete(cache.Entries, pattern)
		return nil, true, err
	}

	// Determine the pattern portion (after base split)
	_, pat := doublestar.SplitPattern(pattern)

	// Collect directory mtimes for cache validation
	dirMtimes := collectDirMtimes(d, resolvedBase, pat)

	cache.Entries[pattern] = GlobCacheEntry{
		BasePath:  resolvedBase,
		Matches:   matches,
		DirMtimes: dirMtimes,
	}

	return matches, true, nil
}

// collectDirMtimes gathers modification times for all directories whose contents
// contribute to a glob pattern's results. For a pattern like "*/*", this includes
// the base directory and all its immediate subdirectories.
func collectDirMtimes(d *Deps, resolvedBase string, pattern string) map[string]time.Time {
	mtimes := make(map[string]time.Time)

	if info, err := d.FS.Stat(resolvedBase); err == nil {
		mtimes[resolvedBase] = info.ModTime()
	}

	depth := countWildcardDepth(pattern)
	if depth > 1 {
		collectChildDirMtimes(d, resolvedBase, depth-1, mtimes)
	}

	return mtimes
}

// countWildcardDepth counts the number of path segments in a pattern that
// contain a wildcard character.
func countWildcardDepth(pattern string) int {
	segments := strings.Split(pattern, "/")
	count := 0
	for _, seg := range segments {
		if strings.Contains(seg, "*") {
			count++
		}
	}
	return count
}

// collectChildDirMtimes recursively collects mtimes of subdirectories
// up to the specified remaining depth.
func collectChildDirMtimes(d *Deps, dir string, remainingDepth int, mtimes map[string]time.Time) {
	if remainingDepth <= 0 {
		return
	}

	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		childPath := filepath.Join(dir, entry.Name())
		// Use Stat (not Lstat) to follow symlinks — we want the target dir's mtime
		info, err := d.FS.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}
		mtimes[childPath] = info.ModTime()

		if remainingDepth > 1 {
			collectChildDirMtimes(d, childPath, remainingDepth-1, mtimes)
		}
	}
}
