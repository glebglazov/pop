package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
)

// Deps holds external dependencies for the history package
type Deps struct {
	FS   deps.FileSystem
	Tmux deps.Tmux
}

// DefaultDeps returns dependencies using real implementations
func DefaultDeps() *Deps {
	return &Deps{
		FS:   deps.NewRealFileSystem(),
		Tmux: deps.NewRealTmux(),
	}
}

var defaultDeps = DefaultDeps()

// Entry represents a history entry for a project
type Entry struct {
	Path       string    `json:"path"`
	LastAccess time.Time `json:"last_access"`
}

// History manages project access history
type History struct {
	Entries []Entry `json:"entries"`
	path    string
}

// DefaultHistoryPath returns the default history file path
func DefaultHistoryPath() string {
	return DefaultHistoryPathWith(defaultDeps)
}

// DefaultHistoryPathWith returns the default history file path using provided dependencies
func DefaultHistoryPathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "history.json")
	}
	home, _ := d.FS.UserHomeDir()
	return filepath.Join(home, ".local", "share", "pop", "history.json")
}

// Load reads history from the given path
func Load(path string) (*History, error) {
	return LoadWith(defaultDeps, path)
}

// LoadWith reads history using provided dependencies
func LoadWith(d *Deps, path string) (*History, error) {
	h := &History{path: path}

	data, err := d.FS.ReadFile(path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, h); err != nil {
		return h, nil // Return empty history on parse error
	}

	// Dedupe entries by resolved path, keeping most recent timestamp
	h.dedupeEntriesBy(d.FS.EvalSymlinks)

	return h, nil
}

// dedupeEntriesBy merges entries that resolve to the same canonical path,
// keeping the most recent timestamp for each
func (h *History) dedupeEntriesBy(evalSymlinks func(string) (string, error)) {
	type canonicalEntry struct {
		resolvedPath string
		lastAccess   time.Time
	}

	seen := make(map[string]*canonicalEntry)

	for _, e := range h.Entries {
		resolved := e.Path
		if r, err := evalSymlinks(e.Path); err == nil {
			resolved = r
		}

		if existing, ok := seen[resolved]; ok {
			// Keep the more recent timestamp
			if e.LastAccess.After(existing.lastAccess) {
				existing.lastAccess = e.LastAccess
			}
		} else {
			seen[resolved] = &canonicalEntry{
				resolvedPath: resolved,
				lastAccess:   e.LastAccess,
			}
		}
	}

	// Rebuild entries with canonical paths
	h.Entries = make([]Entry, 0, len(seen))
	for _, ce := range seen {
		h.Entries = append(h.Entries, Entry{
			Path:       ce.resolvedPath,
			LastAccess: ce.lastAccess,
		})
	}
}

// Save writes history to disk
func (h *History) Save() error {
	return h.SaveWith(defaultDeps)
}

// SaveWith writes history using provided dependencies
func (h *History) SaveWith(d *Deps) error {
	dir := filepath.Dir(h.path)
	if err := d.FS.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}

	return d.FS.WriteFile(h.path, data, 0644)
}

// Record marks a project as accessed
func (h *History) Record(path string) {
	now := time.Now()

	// Update existing or add new
	found := false
	for i := range h.Entries {
		if h.Entries[i].Path == path {
			h.Entries[i].LastAccess = now
			found = true
			break
		}
	}

	if !found {
		h.Entries = append(h.Entries, Entry{
			Path:       path,
			LastAccess: now,
		})
	}
}

// Remove deletes a project from history
func (h *History) Remove(path string) {
	h.RemoveWith(defaultDeps, path)
}

// RemoveWith deletes a project from history using provided dependencies
func (h *History) RemoveWith(d *Deps, path string) {
	for i := range h.Entries {
		if h.Entries[i].Path == path {
			h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
			return
		}
	}
}

// SortByRecency sorts projects by recency (oldest first, most recent last)
// Projects not in history are placed at the beginning, sorted alphabetically
func (h *History) SortByRecency(projects []project.Project) []project.Project {
	return h.SortByRecencyWith(defaultDeps, projects)
}

// SortByRecencyWith sorts projects by recency using provided dependencies
func (h *History) SortByRecencyWith(d *Deps, projects []project.Project) []project.Project {
	// Build lookup map
	accessTimes := make(map[string]time.Time)
	for _, e := range h.Entries {
		accessTimes[e.Path] = e.LastAccess
	}

	// Helper to look up access time
	getAccessTime := func(path string) (time.Time, bool) {
		if t, ok := accessTimes[path]; ok {
			return t, true
		}
		return time.Time{}, false
	}

	sorted := make([]project.Project, len(projects))
	copy(sorted, projects)

	sort.SliceStable(sorted, func(i, j int) bool {
		ti, oki := getAccessTime(sorted[i].Path)
		tj, okj := getAccessTime(sorted[j].Path)

		if oki && okj {
			// Both have history: older first (ascending order)
			return ti.Before(tj)
		}
		if oki {
			// i has history, j doesn't: j comes first (no history at top)
			return false
		}
		if okj {
			// j has history, i doesn't: i comes first (no history at top)
			return true
		}
		// Neither has history: alphabetical
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

// TmuxSessionActivity returns a map of session name to activity timestamp
func TmuxSessionActivity() map[string]int64 {
	return TmuxSessionActivityWith(defaultDeps)
}

// TmuxSessionActivityWith returns session activity using provided dependencies
func TmuxSessionActivityWith(d *Deps) map[string]int64 {
	activity := make(map[string]int64)

	out, err := d.Tmux.ListSessions()
	if err != nil {
		return activity
	}

	for _, line := range strings.Split(out, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[0]
			ts, _ := strconv.ParseInt(parts[1], 10, 64)
			activity[name] = ts
		}
	}

	return activity
}

// SortWorktreesByActivity sorts worktrees by tmux session activity
func SortWorktreesByActivity(worktrees []project.Worktree, ctx *project.RepoContext) []project.Worktree {
	activity := TmuxSessionActivity()

	sorted := make([]project.Worktree, len(worktrees))
	copy(sorted, worktrees)

	sort.SliceStable(sorted, func(i, j int) bool {
		si := project.TmuxSessionName(ctx, sorted[i].Name)
		sj := project.TmuxSessionName(ctx, sorted[j].Name)

		ai, oki := activity[si]
		aj, okj := activity[sj]

		if oki && okj {
			return ai > aj
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}
