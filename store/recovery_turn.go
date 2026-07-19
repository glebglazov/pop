package store

import (
	"database/sql"
	"time"
)

// RecoveryBlockKind names why a post-cooldown recovery-turn acquisition was
// denied. The block itself is by design (ADR-0100); the kind explains it so the
// waiter's status line can name the actual blocker instead of claiming it is
// still waiting on quota.
type RecoveryBlockKind string

const (
	// RecoveryBlockGateHold: a checkout gate hold is parked on the path.
	RecoveryBlockGateHold RecoveryBlockKind = "gate_hold"
	// RecoveryBlockLiveDrain: a live drain is still running on the path.
	RecoveryBlockLiveDrain RecoveryBlockKind = "live_drain"
	// RecoveryBlockTurnHeld: another set already holds the recovery turn there.
	RecoveryBlockTurnHeld RecoveryBlockKind = "turn_held"
	// RecoveryBlockBehindWaiter: another waiter is first in the ordering.
	RecoveryBlockBehindWaiter RecoveryBlockKind = "behind_waiter"
)

// RecoveryBlock is the reason a recovery turn was denied after the waiter's
// cooldown elapsed: a kind plus the ID of the set that is blocking the path.
type RecoveryBlock struct {
	Kind  RecoveryBlockKind
	SetID string
}

// TryAcquireRecoveryTurn atomically grants a recovery turn to w when its preset
// cooldown has elapsed, no checkout gate hold or live drain blocks the path, no
// other turn is held there, and w is first among eligible waiters on that path
// (priority descending, then registration time ascending).
//
// When the turn is granted the returned *RecoveryBlock is nil. When it is denied
// after the cooldown has elapsed, the block names the actual blocker (kind + the
// blocking set's ID), computed inside the same acquisition transaction so there
// is no second query and no TOCTOU. Before the cooldown elapses the block is nil.
func (s *Store) TryAcquireRecoveryTurn(w RecoveryWaiter, now time.Time) (bool, *RecoveryBlock, error) {
	if w.SetID == "" || w.RuntimePath == "" || now.Before(w.ResetAt.UTC()) {
		return false, nil, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var holdSetID sql.NullString
	err = tx.QueryRow(
		`SELECT set_id FROM checkout_gate_holds WHERE runtime_path = ?`, w.RuntimePath).
		Scan(&holdSetID)
	if err != nil && err != sql.ErrNoRows {
		return false, nil, err
	}
	if holdSetID.Valid {
		return false, &RecoveryBlock{Kind: RecoveryBlockGateHold, SetID: holdSetID.String}, nil
	}

	rows, err := tx.Query(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND runtime_path = ?`,
		StateRunning, w.RuntimePath)
	if err != nil {
		return false, nil, err
	}
	for rows.Next() {
		var d Drain
		var started string
		var procStart sql.NullString
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started); err != nil {
			_ = rows.Close()
			return false, nil, err
		}
		d.ProcStart = procStart.String
		d.StartedAt = parseTime(started)
		d.State = StateRunning
		if s.drainAlive(d) {
			_ = rows.Close()
			return false, &RecoveryBlock{Kind: RecoveryBlockLiveDrain, SetID: d.SetID}, nil
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, nil, err
	}
	_ = rows.Close()

	var turnSetID sql.NullString
	err = tx.QueryRow(
		`SELECT set_id FROM recovery_turns WHERE runtime_path = ?`, w.RuntimePath).
		Scan(&turnSetID)
	if err != nil && err != sql.ErrNoRows {
		return false, nil, err
	}
	if turnSetID.Valid {
		return false, &RecoveryBlock{Kind: RecoveryBlockTurnHeld, SetID: turnSetID.String}, nil
	}

	nowStr := now.UTC().Format(timeLayout)
	var firstSetID string
	err = tx.QueryRow(
		`SELECT set_id FROM recovery_waiters
		 WHERE runtime_path = ? AND reset_at <= ?
		 ORDER BY priority DESC, registered_at ASC
		 LIMIT 1`,
		w.RuntimePath, nowStr).Scan(&firstSetID)
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if firstSetID != w.SetID {
		return false, &RecoveryBlock{Kind: RecoveryBlockBehindWaiter, SetID: firstSetID}, nil
	}

	_, err = tx.Exec(
		`INSERT INTO recovery_turns (runtime_path, set_id, acquired_at) VALUES (?, ?, ?)`,
		w.RuntimePath, w.SetID, now.UTC().Format(timeLayout))
	if err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}

// ReleaseRecoveryTurn drops the checkout-scoped recovery turn for one runtime
// path. A missing row is not an error.
func (s *Store) ReleaseRecoveryTurn(runtimePath string) error {
	if runtimePath == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM recovery_turns WHERE runtime_path = ?`, runtimePath)
	return err
}
