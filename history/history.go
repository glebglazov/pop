package history

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/project"
)

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
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "history.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "pop", "history.json")
}

// Load reads history from the given path
func Load(path string) (*History, error) {
	h := &History{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, h); err != nil {
		return h, nil // Return empty history on parse error
	}

	return h, nil
}

// Save writes history to disk
func (h *History) Save() error {
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(h.path, data, 0644)
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

// SortByRecency sorts projects by recency (oldest first, most recent last)
// Projects not in history are placed at the beginning, sorted alphabetically
func (h *History) SortByRecency(projects []project.Project) []project.Project {
	// Build lookup map
	accessTimes := make(map[string]time.Time)
	for _, e := range h.Entries {
		accessTimes[e.Path] = e.LastAccess
	}

	sorted := make([]project.Project, len(projects))
	copy(sorted, projects)

	sort.SliceStable(sorted, func(i, j int) bool {
		ti, oki := accessTimes[sorted[i].Path]
		tj, okj := accessTimes[sorted[j].Path]

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
	activity := make(map[string]int64)

	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{session_activity}")
	out, err := cmd.Output()
	if err != nil {
		return activity
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
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
