package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
)

// PaneStatus represents the detected state of a monitored pane
type PaneStatus string

const (
	StatusWorking PaneStatus = "working"
	StatusUnread  PaneStatus = "unread"
	StatusIdle    PaneStatus = "idle"
	StatusUnknown PaneStatus = "unknown"

	// legacyStatusRead is the old name for StatusIdle, accepted as a CLI
	// alias and migrated transparently when loading state files written
	// by older versions of pop. Kept unexported because new code should
	// always use StatusIdle directly.
	legacyStatusRead PaneStatus = "read"

	// legacyStatusNeedsAttention is the old name for StatusUnread, accepted
	// as a CLI alias and migrated transparently when loading state files
	// written by older versions of pop. Kept unexported because new code
	// should always use StatusUnread directly.
	legacyStatusNeedsAttention PaneStatus = "needs_attention"
)

// PaneEntry represents a single monitored pane
type PaneEntry struct {
	PaneID      string     `json:"pane_id"`
	Session     string     `json:"session"`
	Status      PaneStatus `json:"status"`
	Following   bool       `json:"following,omitempty"`
	Note        string     `json:"note,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastVisited time.Time  `json:"last_visited,omitempty"`
}

// State holds the full monitor state
type State struct {
	Panes              map[string]*PaneEntry `json:"panes"`
	DashboardFollowing bool                  `json:"dashboard_following,omitempty"`
	path               string
}

// Deps holds external dependencies for the monitor package
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

// DefaultStatePath returns the default monitor state file path
func DefaultStatePath() string {
	return DefaultStatePathWith(defaultDeps)
}

// DefaultStatePathWith returns the default monitor state file path using provided dependencies
func DefaultStatePathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "monitor.json")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("DefaultStatePath: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop", "monitor.json")
}

// DefaultPIDPath returns the default daemon PID file path
func DefaultPIDPath() string {
	return DefaultPIDPathWith(defaultDeps)
}

// DefaultPIDPathWith returns the default daemon PID file path using provided dependencies
func DefaultPIDPathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "monitor.pid")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("DefaultPIDPath: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop", "monitor.pid")
}

// Load reads monitor state from disk
func Load(path string) (*State, error) {
	return LoadWith(defaultDeps, path)
}

// LoadWith reads monitor state using provided dependencies
func LoadWith(d *Deps, path string) (*State, error) {
	s := &State{
		Panes: make(map[string]*PaneEntry),
		path:  path,
	}

	data, err := d.FS.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, s); err != nil {
		debug.Error("monitor.Load %s: unmarshal: %v", path, err)
		return s, nil
	}
	if s.Panes == nil {
		s.Panes = make(map[string]*PaneEntry)
	}

	// Migrate legacy status names to their canonical forms:
	//   "read"            → "idle"   (merged statuses; "read" is only a CLI/state alias now)
	//   "needs_attention" → "unread" (rename for consistency with user mental model)
	// Rewriting in-memory means the next state.Save() will persist the
	// corrected value. The legacy names remain accepted via CLI alias so
	// installed agent plugins that emit the old strings keep working.
	for _, entry := range s.Panes {
		if entry.Status == legacyStatusRead {
			entry.Status = StatusIdle
		}
		if entry.Status == legacyStatusNeedsAttention {
			entry.Status = StatusUnread
		}
	}

	return s, nil
}

// Save writes monitor state to disk
func (s *State) Save() error {
	return s.SaveWith(defaultDeps)
}

// SaveWith writes monitor state using provided dependencies
func (s *State) SaveWith(d *Deps) error {
	dir := filepath.Dir(s.path)
	if err := d.FS.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return d.FS.WriteFile(s.path, data, 0644)
}

// SessionsWithUnread returns session names that have at least one pane
// in StatusUnread
func (s *State) SessionsWithUnread() map[string]bool {
	result := make(map[string]bool)
	for _, entry := range s.Panes {
		if entry.Status == StatusUnread {
			result[entry.Session] = true
		}
	}
	return result
}

// PanesUnread returns all pane entries with StatusUnread
func (s *State) PanesUnread() []*PaneEntry {
	var result []*PaneEntry
	for _, entry := range s.Panes {
		if entry.Status == StatusUnread {
			result = append(result, entry)
		}
	}
	return result
}

// PanesActive returns all pane entries with StatusUnread or StatusWorking
func (s *State) PanesActive() []*PaneEntry {
	var result []*PaneEntry
	for _, entry := range s.Panes {
		if entry.Status == StatusUnread || entry.Status == StatusWorking {
			result = append(result, entry)
		}
	}
	return result
}

// PanesAll returns all pane entries regardless of status
func (s *State) PanesAll() []*PaneEntry {
	result := make([]*PaneEntry, 0, len(s.Panes))
	for _, entry := range s.Panes {
		result = append(result, entry)
	}
	return result
}

// IsDaemonRunning checks if the daemon process is alive by reading the PID file
// and sending signal 0 to the process
func IsDaemonRunning(pidPath string) bool {
	return IsDaemonRunningWith(defaultDeps, pidPath)
}

// IsDaemonRunningWith checks daemon liveness using provided dependencies
func IsDaemonRunningWith(d *Deps, pidPath string) bool {
	data, err := d.FS.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
