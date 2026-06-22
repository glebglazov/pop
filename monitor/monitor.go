package monitor

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
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
	StatusClear   PaneStatus = "clear"
	StatusUnknown PaneStatus = "unknown"

	// legacyStatusIdle and legacyStatusRead are deprecated aliases for
	// StatusClear, accepted at the CLI boundary and migrated when loading
	// state files written by older versions of pop.
	legacyStatusIdle PaneStatus = "idle"
	legacyStatusRead PaneStatus = "read"

	// legacyStatusNeedsAttention is the old name for StatusUnread, accepted
	// as a CLI alias and migrated transparently when loading state files
	// written by older versions of pop. Kept unexported because new code
	// should always use StatusUnread directly.
	legacyStatusNeedsAttention PaneStatus = "needs_attention"
)

// PaneEntry represents a single monitored pane
type PaneEntry struct {
	PaneID    string     `json:"pane_id"`
	Session   string     `json:"session"`
	Status    PaneStatus `json:"status"`
	Label     string     `json:"label,omitempty"`
	Following bool       `json:"following,omitempty"`
	Note      string     `json:"note,omitempty"`
	// Topic is a short, machine-set phrase describing what the pane's
	// conversation is about. Unlike a user-authored Note it carries no
	// staleness/timestamp; it lives for the pane's whole monitored lifetime
	// and is cleared only when the pane is retired (the whole entry is
	// removed). unfollow does not touch it (it clears only Note).
	Topic        string    `json:"topic,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `json:"last_active_at,omitempty"`
}

// UnmarshalJSON implements backward-compatible deserialization: older state
// files written with the "last_visited" key are transparently migrated to
// "last_active_at". When both keys are present, "last_active_at" wins.
func (p *PaneEntry) UnmarshalJSON(data []byte) error {
	type Alias PaneEntry
	aux := &struct {
		LastVisited time.Time `json:"last_visited"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if !aux.LastVisited.IsZero() && p.LastActiveAt.IsZero() {
		p.LastActiveAt = aux.LastVisited
	}
	return nil
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

// dataDirWith returns the XDG-resolved pop data directory (the scope of the
// monitor state). The daemon address is derived from this path so that
// distinct data dirs never collide on a port (ADR 0021).
func dataDirWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("dataDir: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop")
}

// DefaultStatePath returns the default monitor state file path
func DefaultStatePath() string {
	return DefaultStatePathWith(defaultDeps)
}

// DefaultStatePathWith returns the default monitor state file path using provided dependencies
func DefaultStatePathWith(d *Deps) string {
	return filepath.Join(dataDirWith(d), "monitor.json")
}

// DefaultPIDPath returns the default daemon PID file path
func DefaultPIDPath() string {
	return DefaultPIDPathWith(defaultDeps)
}

// DefaultPIDPathWith returns the default daemon PID file path using provided dependencies
func DefaultPIDPathWith(d *Deps) string {
	return filepath.Join(dataDirWith(d), "monitor.pid")
}

// derivedPortRange spans the IANA dynamic/private range (49152–65535).
const (
	derivedPortBase = 49152
	derivedPortSpan = 65536 - derivedPortBase
)

// DerivedAddr returns the loopback address whose port is derived from the data
// dir. Pure function of the data dir — no env, no config (ADR 0021). Distinct
// data dirs map to distinct ports, so a daemon for one data dir never collides
// with a daemon for another (e.g. a test instance vs. the real one).
func DerivedAddr() string {
	return DerivedAddrWith(defaultDeps)
}

// DerivedAddrWith returns the derived address using provided dependencies.
func DerivedAddrWith(d *Deps) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(dataDirWith(d)))
	port := derivedPortBase + int(h.Sum32()%derivedPortSpan)
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// DefaultAddr returns the TCP address the monitor daemon listens on: the
// POP_MONITOR_ADDR env override if set, else the data-dir-derived address.
// Config-file overrides sit between these two and are applied in the command
// layer (the monitor package must not import config — ADR 0001).
func DefaultAddr() string {
	return DefaultAddrWith(defaultDeps)
}

// DefaultAddrWith returns the env-or-derived address using provided dependencies.
func DefaultAddrWith(d *Deps) string {
	if v := d.FS.Getenv("POP_MONITOR_ADDR"); v != "" {
		return v
	}
	return DerivedAddrWith(d)
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
		// Do NOT fall back to empty state here. A caller that loads, mutates,
		// and saves would persist that empty state over the real file, wiping
		// the whole pane registry. Atomic writes (SaveWith) mean a torn read
		// can no longer produce this; a genuine parse error now means real
		// corruption, which must fail loudly rather than silently reset.
		debug.Error("monitor.Load %s: unmarshal: %v", path, err)
		return nil, fmt.Errorf("monitor.Load %s: %w", path, err)
	}
	if s.Panes == nil {
		s.Panes = make(map[string]*PaneEntry)
	}

	// Migrate legacy status names to their canonical forms:
	//   "idle", "read"    → "clear"
	//   "needs_attention" → "unread"
	// Rewriting in-memory means the next state.Save() will persist the
	// corrected value. The legacy names remain accepted via CLI alias so
	// installed agent plugins that emit the old strings keep working.
	for _, entry := range s.Panes {
		entry.Status = normalizeStatus(entry.Status)
	}

	return s, nil
}

// Save writes monitor state to disk
func (s *State) Save() error {
	return s.SaveWith(defaultDeps)
}

// SaveWith writes monitor state using provided dependencies.
//
// The write is atomic: data is written to a temp file in the same directory
// and then renamed over the target. rename(2) is atomic on a single
// filesystem, so a concurrent reader always sees either the complete old file
// or the complete new one — never a truncated or partially-written file.
//
// This matters because the state file is a full-rewrite store mutated by many
// unsynchronized processes (every `pop pane set-status` hook plus the daemon
// poll). A plain truncate-in-place write left a window where a reader could
// observe an empty/partial file; combined with LoadWith treating an unparseable
// file as empty state, that wiped the whole pane registry from time to time.
func (s *State) SaveWith(d *Deps) error {
	dir := filepath.Dir(s.path)
	if err := d.FS.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// PID keeps temp names distinct across the concurrent writer processes
	// that share this file. Within a single process writes are serialized by
	// the daemon mutex, so a constant PID suffix is enough.
	tmp := fmt.Sprintf("%s.tmp-%d", s.path, os.Getpid())
	if err := d.FS.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := d.FS.Rename(tmp, s.path); err != nil {
		d.FS.RemoveAll(tmp)
		return err
	}
	return nil
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
