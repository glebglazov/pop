package store

import (
	"database/sql"
	"time"
)

// RecoveryWaiter is a registered quota-recovery wait: a drain that parked after
// exhausting the agent fallback chain and is waiting for the preset's cooldown
// to elapse before resuming the same open task (ADR-0100). It carries the
// registering process's owner identity (PID plus start token, the same pairing
// drains and gate holds use) so a waiter whose owner died can be swept by the
// opportunistic reconcile pass instead of deferring its set forever (ADR-0135).
type RecoveryWaiter struct {
	SetID        string
	Preset       string
	ResetAt      time.Time
	RuntimePath  string
	Priority     int
	PID          int
	ProcStart    string
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
		`INSERT INTO recovery_waiters (set_id, preset, reset_at, runtime_path, priority, pid, proc_start, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(set_id) DO UPDATE SET
		   preset = excluded.preset,
		   reset_at = excluded.reset_at,
		   runtime_path = excluded.runtime_path,
		   priority = excluded.priority,
		   pid = excluded.pid,
		   proc_start = excluded.proc_start,
		   registered_at = excluded.registered_at`,
		w.SetID,
		w.Preset,
		w.ResetAt.UTC().Format(timeLayout),
		w.RuntimePath,
		w.Priority,
		w.PID,
		nullString(w.ProcStart),
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
		`SELECT set_id, preset, reset_at, runtime_path, priority, pid, proc_start, registered_at
		 FROM recovery_waiters WHERE set_id = ?`, setID)
	var w RecoveryWaiter
	var resetAt, procStart, registeredAt sql.NullString
	if err := row.Scan(&w.SetID, &w.Preset, &resetAt, &w.RuntimePath, &w.Priority, &w.PID, &procStart, &registeredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	w.ProcStart = procStart.String
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
		`SELECT set_id, preset, reset_at, runtime_path, priority, pid, proc_start, registered_at
		 FROM recovery_waiters
		 ORDER BY priority DESC, registered_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RecoveryWaiter
	for rows.Next() {
		var w RecoveryWaiter
		var resetAt, procStart, registeredAt sql.NullString
		if err := rows.Scan(&w.SetID, &w.Preset, &resetAt, &w.RuntimePath, &w.Priority, &w.PID, &procStart, &registeredAt); err != nil {
			return nil, err
		}
		w.ProcStart = procStart.String
		w.ResetAt = parseTime(resetAt.String)
		w.RegisteredAt = parseTime(registeredAt.String)
		out = append(out, w)
	}
	return out, rows.Err()
}

// ReconcileRecoveryWaiters is the recovery-waiter arm of the opportunistic
// reconcile pass (ADR-0135): in one bounded transaction it deletes waiters whose
// registering process is no longer alive, using the same PID+start-token liveness
// the drains and gate holds sweep uses so a reused PID is not mistaken for the
// original owner. A dead owner's waiter (a kill -9 or terminal close) would
// otherwise linger forever, permanently deferring its set in the Queue and
// holding the checkout claim. It returns the number of waiters swept.
//
// Rows are fully read and the cursor closed before any DELETE is issued so the
// store's single connection is never asked to run a follow-up query with an open
// result set.
func (s *Store) ReconcileRecoveryWaiters() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT set_id, pid, proc_start FROM recovery_waiters`)
	if err != nil {
		return 0, err
	}
	type waiter struct {
		setID     string
		pid       int
		procStart string
	}
	var waiters []waiter
	for rows.Next() {
		var w waiter
		var procStart sql.NullString
		if err := rows.Scan(&w.setID, &w.pid, &procStart); err != nil {
			_ = rows.Close()
			return 0, err
		}
		w.procStart = procStart.String
		waiters = append(waiters, w)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	var swept int
	for _, w := range waiters {
		if s.alive(w.pid, w.procStart) {
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM recovery_waiters WHERE set_id = ?`, w.setID); err != nil {
			return 0, err
		}
		swept++
	}
	if swept == 0 {
		return 0, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return swept, nil
}
