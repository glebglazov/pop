package monitor

import (
	"time"
)

// Store provides a transactional interface for mutating monitor state.
// All persistence (load, find, mutate, save) is concentrated behind one
// seam, eliminating the duplicated load-find-mutate-save pattern that
// previously existed across cmd/session.go and cmd/pane.go.
type Store struct {
	path string
	deps *Deps
}

// NewStore creates a Store for the given state file path.
// If d is nil, DefaultDeps() is used.
func NewStore(path string, d *Deps) *Store {
	if d == nil {
		d = DefaultDeps()
	}
	return &Store{path: path, deps: d}
}

// UpdatePane loads state, applies the mutator to the named pane, and saves.
// If the pane is not found, it returns nil (no-op).
func (s *Store) UpdatePane(paneID string, mutator func(*PaneEntry)) error {
	state, err := LoadWith(s.deps, s.path)
	if err != nil {
		return err
	}
	entry, ok := state.Panes[paneID]
	if !ok {
		return nil
	}
	mutator(entry)
	return state.SaveWith(s.deps)
}

// MarkClear sets a pane's status to Clear. No-op if pane not found.
func (s *Store) MarkClear(paneID string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.Status = StatusClear
	})
}

// MarkUnread sets a pane's status to Unread. No-op if pane not found.
func (s *Store) MarkUnread(paneID string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.Status = StatusUnread
	})
}

// ToggleFollow flips a pane's Following flag. No-op if pane not found.
func (s *Store) ToggleFollow(paneID string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.Following = !entry.Following
	})
}

// SetNote sets a pane's note. No-op if pane not found.
func (s *Store) SetNote(paneID, note string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.Note = note
	})
}

// Remove deletes a pane from the monitor state.
func (s *Store) Remove(paneID string) error {
	state, err := LoadWith(s.deps, s.path)
	if err != nil {
		return err
	}
	delete(state.Panes, paneID)
	return state.SaveWith(s.deps)
}

// RecordVisit updates a pane's LastActiveAt to now. No-op if pane not found.
func (s *Store) RecordVisit(paneID string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.LastActiveAt = time.Now()
	})
}

// DismissUnread transitions a pane from Unread to Clear and records the
// visit time. Unlike MarkClear, the status flip is conditional: only
// panes currently in StatusUnread are changed. All tracked panes get
// their LastActiveAt updated. No-op if pane not found.
func (s *Store) DismissUnread(paneID string) error {
	return s.UpdatePane(paneID, func(entry *PaneEntry) {
		entry.LastActiveAt = time.Now()
		if entry.Status == StatusUnread {
			entry.Status = StatusClear
		}
	})
}

// DefaultStore returns a Store using the default state path and deps.
func DefaultStore() *Store {
	return NewStore(DefaultStatePath(), nil)
}
