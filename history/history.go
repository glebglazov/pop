// Package history is pop's record of where its human has been: one row per path
// with the instant they last landed there, which is what the project picker, the
// worktree picker and the monitor dashboard order by. The rows live in the
// execution-state store, borrowed through tasks.Deps like every other layer-2
// fact (ADR-0140/0188); the standalone history.json this replaces is folded in
// once on first read and then ignored.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// Deps holds external dependencies for the history package
type Deps struct {
	FS   deps.FileSystem
	Tmux tmux.Tmux

	// Tasks carries the process-cached execution-state store handle the rows live
	// in (ADR-0140). A nil holder means no store is wired: History then loads to
	// whatever the legacy file holds and nothing persists, which is what a Deps
	// built for the filesystem or tmux seam alone wants.
	Tasks *tasks.Deps
}

// DefaultDeps returns dependencies using real implementations
func DefaultDeps() *Deps {
	return &Deps{
		FS:    deps.NewRealFileSystem(),
		Tmux:  tmux.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude()),
		Tasks: tasks.DefaultDeps(),
	}
}

var defaultDeps = DefaultDeps()

// legacyHistoryFile is the standalone recency file History lived in before its
// rows moved into the execution-state store. It is read once per load and never
// written; the fold that consumes it leaves it on disk (ADR-0188).
const legacyHistoryFile = "history.json"

// Entry represents a history entry for a project
type Entry struct {
	Path       string    `json:"path"`
	LastAccess time.Time `json:"last_access"`
}

// History is a loaded snapshot of the recency rows plus the seam they came from.
// Entries is what every reader sorts by; Record and Remove write through to the
// store and update the snapshot in step, so a picker that records mid-loop
// re-sorts against what it just wrote. The store handle is resolved per write
// rather than held, because it is process-cached anyway and a load that found no
// database yet must still be able to record the landing that creates one.
//
// A History built as a bare literal — a test, or the empty fallback a caller
// substitutes when the load failed — carries no seam, and its Record and Remove
// mutate the snapshot only.
type History struct {
	Entries []Entry `json:"entries"`
	tasks   *tasks.Deps
}

// LegacyHistoryPath returns the path of the pre-store history.json.
func LegacyHistoryPath() string {
	return LegacyHistoryPathWith(defaultDeps)
}

// LegacyHistoryPathWith returns the path of the pre-store history.json using
// provided dependencies.
func LegacyHistoryPathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", legacyHistoryFile)
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("LegacyHistoryPath: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop", legacyHistoryFile)
}

// Load reads history using the package default dependencies
func Load() (*History, error) {
	return LoadWith(defaultDeps)
}

// LoadWith reads history from the execution-state store, folding a surviving
// history.json in first. The fold is once-only against a marker row rather than
// against the file's absence, because the file is deliberately left on disk as
// the only rollback for a store with no other copy of these rows (ADR-0188): a
// path the human has since reset must not be resurrected from it.
func LoadWith(d *Deps) (*History, error) {
	legacy, err := legacyEntries(d)
	if err != nil {
		return nil, err
	}
	if d.Tasks == nil {
		return &History{Entries: legacy}, nil
	}
	// The fold is a write, so it needs the database created; a load with nothing
	// to fold stays a pure reader and never materialises an empty one.
	s, ok, err := d.Tasks.Store(len(legacy) > 0)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &History{tasks: d.Tasks}, nil
	}
	if len(legacy) > 0 {
		rows := make([]store.HistoryEntry, 0, len(legacy))
		for _, e := range legacy {
			rows = append(rows, store.HistoryEntry{Path: e.Path, LastAccess: e.LastAccess})
		}
		if _, err := s.FoldHistoryEntries(LegacyHistoryPathWith(d), rows, time.Now()); err != nil {
			return nil, err
		}
	}
	rows, err := s.AllHistoryEntries()
	if err != nil {
		return nil, err
	}
	h := &History{tasks: d.Tasks, Entries: make([]Entry, 0, len(rows))}
	for _, r := range rows {
		h.Entries = append(h.Entries, Entry{Path: r.Path, LastAccess: r.LastAccess})
	}
	return h, nil
}

// legacyEntries reads and canonicalises the pre-store history.json. A missing
// file is the steady state once a machine has folded; an unparseable one is
// logged and treated as empty, since the store is the source of truth and a
// corrupt rollback file must not stop a picker from rendering.
func legacyEntries(d *Deps) ([]Entry, error) {
	path := LegacyHistoryPathWith(d)
	data, err := d.FS.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file History
	if err := json.Unmarshal(data, &file); err != nil {
		debug.Error("history: parse legacy file %s: %v", path, err)
		return nil, nil
	}
	// The file accumulated symlink-aliased spellings of the same checkout over
	// years of writes; the store is keyed by path, so the aliases have to collapse
	// before they become rows.
	file.dedupeEntriesBy(d.FS.EvalSymlinks)
	return file.Entries, nil
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
	// Sort for deterministic order — map iteration above is randomized
	sort.Slice(h.Entries, func(i, j int) bool {
		return h.Entries[i].Path < h.Entries[j].Path
	})
}

// Record marks a project as accessed: a single-row upsert in the store, plus the
// same update on the loaded snapshot so a caller that sorts after recording sees
// its own write. Callers treat the error as best-effort — recency bookkeeping
// never blocks the landing it describes.
func (h *History) Record(path string) error {
	now := time.Now().UTC()

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

	if h.tasks == nil {
		return nil
	}
	// A landing is worth creating the database for: this is the write that makes
	// the picker's first ordering possible on a fresh machine.
	s, _, err := h.tasks.Store(true)
	if err != nil {
		return err
	}
	return s.PutHistoryEntry(path, now)
}

// RecordWith records a landing at path for a caller that only ever writes: a
// dashboard handoff verb, a Map session verb. They hold no snapshot to sort, so
// the load exists only to reach the store seam, and the whole call is best-effort
// like Record itself — recency bookkeeping never blocks the landing it describes.
func RecordWith(d *Deps, path string) error {
	h, err := LoadWith(d)
	if err != nil {
		return err
	}
	return h.Record(path)
}

// Remove deletes a project from history, dropping its row and its place in the
// loaded snapshot.
func (h *History) Remove(path string) error {
	for i := range h.Entries {
		if h.Entries[i].Path == path {
			h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
			break
		}
	}
	if h.tasks == nil {
		return nil
	}
	// A removal never needs to create anything: with no database there is no row.
	s, ok, err := h.tasks.Store(false)
	if err != nil || !ok {
		return err
	}
	return s.DeleteHistoryEntry(path)
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

	sessions, err := d.Tmux.Sessions()
	if err != nil {
		return activity
	}

	for _, s := range sessions {
		activity[s.Name] = s.Activity
	}

	return activity
}
