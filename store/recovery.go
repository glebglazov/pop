package store

import (
	"database/sql"
	"time"
)

// RecoveryWaiter is a registered quota-recovery wait: a drain that parked after
// exhausting the agent fallback chain and is waiting for the preset's cooldown
// to elapse before resuming the same open task (ADR-0100).
type RecoveryWaiter struct {
	SetID        string
	Preset       string
	ResetAt      time.Time
	RuntimePath  string
	Priority     int
	RegisteredAt time.Time
}

// PutRecoveryWaiter registers (or refreshes) a recovery waiter for one task set.
// The set_id is UNIQUE: a second registration for the same set replaces the
// first, so a crash-restart of the same drain does not duplicate the row. The
// latest write wins; nothing is ever read stale.
func (s *Store) PutRecoveryWaiter(w RecoveryWaiter) error {
	if w.SetID == "" || w.Preset == "" || w.ResetAt.IsZero() || w.RuntimePath == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO recovery_waiters (set_id, preset, reset_at, runtime_path, priority, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(set_id) DO UPDATE SET
		   preset = excluded.preset,
		   reset_at = excluded.reset_at,
		   runtime_path = excluded.runtime_path,
		   priority = excluded.priority,
		   registered_at = excluded.registered_at`,
		w.SetID,
		w.Preset,
		w.ResetAt.UTC().Format(timeLayout),
		w.RuntimePath,
		w.Priority,
		w.RegisteredAt.UTC().Format(timeLayout))
	return err
}

// DeleteRecoveryWaiter removes the recovery waiter for one task set. A missing
// row is not an error (the waiter may already have been consumed or never
// registered).
func (s *Store) DeleteRecoveryWaiter(setID string) error {
	if setID == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM recovery_waiters WHERE set_id = ?`, setID)
	return err
}

// GetRecoveryWaiter returns the recovery waiter for one task set, or nil when
// no waiter is registered.
func (s *Store) GetRecoveryWaiter(setID string) (*RecoveryWaiter, error) {
	if setID == "" {
		return nil, nil
	}
	row := s.db.QueryRow(
		`SELECT set_id, preset, reset_at, runtime_path, priority, registered_at
		 FROM recovery_waiters WHERE set_id = ?`, setID)
	var w RecoveryWaiter
	var resetAt, registeredAt sql.NullString
	if err := row.Scan(&w.SetID, &w.Preset, &resetAt, &w.RuntimePath, &w.Priority, &registeredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	w.ResetAt = parseTime(resetAt.String)
	w.RegisteredAt = parseTime(registeredAt.String)
	return &w, nil
}

// AllRecoveryWaiters returns every registered recovery waiter, ordered by
// priority descending (highest first) then registration time ascending
// (earliest first). This is the coordinator's view: when multiple waiters
// contend for a recovery turn, the highest-priority earliest-registered set
// goes first.
func (s *Store) AllRecoveryWaiters() ([]RecoveryWaiter, error) {
	rows, err := s.db.Query(
		`SELECT set_id, preset, reset_at, runtime_path, priority, registered_at
		 FROM recovery_waiters
		 ORDER BY priority DESC, registered_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RecoveryWaiter
	for rows.Next() {
		var w RecoveryWaiter
		var resetAt, registeredAt sql.NullString
		if err := rows.Scan(&w.SetID, &w.Preset, &resetAt, &w.RuntimePath, &w.Priority, &registeredAt); err != nil {
			return nil, err
		}
		w.ResetAt = parseTime(resetAt.String)
		w.RegisteredAt = parseTime(registeredAt.String)
		out = append(out, w)
	}
	return out, rows.Err()
}
