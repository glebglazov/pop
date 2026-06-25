package monitor

import (
	"fmt"
	"time"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
)

// ReportStatusInput carries a pane status report and resolved policy values.
// Policy is resolved in the command layer; monitor stays config-free.
type ReportStatusInput struct {
	PaneID                string
	Status                PaneStatus
	Label                 string
	NoRegister            bool
	DismissUnreadInActive bool
}

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

// Prune removes every tracked pane absent from live in a single load/save and
// returns the count removed. Unlike a loop over Remove (which saves per call),
// the per-tick deregistration stays one atomic write. Nothing to remove means
// no save.
func (s *Store) Prune(live map[string]bool) (int, error) {
	state, err := LoadWith(s.deps, s.path)
	if err != nil {
		return 0, err
	}
	removed := 0
	for paneID, entry := range state.Panes {
		if !live[paneID] {
			debug.Log("[monitor] %s (session=%s): deregistered (pane dead)", paneID, entry.Session)
			delete(state.Panes, paneID)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, state.SaveWith(s.deps)
}

// RecordVisit updates a tracked pane's LastActiveAt to now without changing
// status. Untracked panes are silently ignored (never auto-register).
func (s *Store) RecordVisit(paneID string) error {
	return s.upsert(paneID,
		func() (*PaneEntry, bool) {
			return nil, false
		},
		func(entry *PaneEntry) bool {
			entry.LastActiveAt = time.Now()
			return true
		},
	)
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

// upsert loads state, finds or registers the pane, applies mutations, and saves.
// register returns an entry and ok; ok=false means do not register (no-op).
// mutate returns whether the state file must be saved.
func (s *Store) upsert(
	paneID string,
	register func() (*PaneEntry, bool),
	mutate func(*PaneEntry) bool,
) error {
	state, err := LoadWith(s.deps, s.path)
	if err != nil {
		return err
	}

	entry, ok := state.Panes[paneID]
	if !ok {
		newEntry, shouldRegister := register()
		if !shouldRegister {
			return nil
		}
		state.Panes[paneID] = newEntry
		return state.SaveWith(s.deps)
	}

	if !mutate(entry) {
		return nil
	}
	return state.SaveWith(s.deps)
}

// ReportStatus applies the full status transition rule: auto-register an
// untracked pane unless registration is suppressed; apply label; record visit
// time when clearing; downgrade Unread to Clear when the pane is active and
// DismissUnreadInActive is true; no-op when the reported status already matches
// (but still save if visit time was updated).
func (s *Store) ReportStatus(tmux deps.Tmux, in ReportStatusInput) error {
	status := in.Status

	return s.upsert(in.PaneID,
		func() (*PaneEntry, bool) {
			if in.NoRegister {
				return nil, false
			}
			session, cmdName, err := TmuxPaneInfo(tmux, in.PaneID)
			if err != nil {
				debug.Error("[set-status] %s: failed to look up pane info, skipping: %v", in.PaneID, err)
				return nil, false
			}
			debug.Log("[set-status] %s: auto-registering in session=%s (cmd=%s) with status=%s", in.PaneID, session, cmdName, status)
			now := time.Now()
			entry := &PaneEntry{
				PaneID:       in.PaneID,
				Session:      session,
				Status:       status,
				UpdatedAt:    now,
				LastActiveAt: now,
			}
			applyLabel(entry, in.Label)
			return entry, true
		},
		func(entry *PaneEntry) bool {
			applyLabel(entry, in.Label)

			visitedNow := false
			if status == StatusClear {
				entry.LastActiveAt = time.Now()
				visitedNow = true
			}

			effectiveStatus := status
			if in.DismissUnreadInActive && status == StatusUnread && IsActiveTmuxPane(tmux, in.PaneID) {
				debug.Log("[set-status] %s: unread on active pane — downgrading to clear", in.PaneID)
				effectiveStatus = StatusClear
			}

			if entry.Status == effectiveStatus {
				return visitedNow
			}

			debug.Log("[set-status] %s (session=%s): %s → %s", in.PaneID, entry.Session, entry.Status, effectiveStatus)
			entry.Status = effectiveStatus
			entry.UpdatedAt = time.Now()
			return true
		},
	)
}

func applyLabel(entry *PaneEntry, label string) {
	if label != "" {
		entry.Label = label
	}
}

// SetFollowing applies the following transition rule: auto-register an
// untracked pane only when following (never when unfollowing); no-op when
// the following value already matches; on unfollow, clear any attached note;
// update timestamp on change.
func (s *Store) SetFollowing(tmux deps.Tmux, paneID string, follow bool) error {
	var registerErr error
	err := s.upsert(paneID,
		func() (*PaneEntry, bool) {
			if !follow {
				return nil, false
			}
			session, _, err := TmuxPaneInfo(tmux, paneID)
			if err != nil {
				registerErr = fmt.Errorf("look up pane: %w", err)
				return nil, false
			}
			debug.Log("[set-following] %s: auto-registering in session=%s with following=true", paneID, session)
			now := time.Now()
			return &PaneEntry{
				PaneID:       paneID,
				Session:      session,
				Status:       StatusClear,
				Following:    true,
				UpdatedAt:    now,
				LastActiveAt: now,
			}, true
		},
		func(entry *PaneEntry) bool {
			if entry.Following == follow {
				return false
			}
			debug.Log("[set-following] %s (session=%s): %v → %v", paneID, entry.Session, entry.Following, follow)
			entry.Following = follow
			entry.UpdatedAt = time.Now()
			if !follow {
				entry.Note = ""
			}
			return true
		},
	)
	if registerErr != nil {
		return registerErr
	}
	return err
}
