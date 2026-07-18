package store

import (
	"database/sql"
	"time"
)

// TryAcquireRecoveryTurn atomically grants a recovery turn to w when its preset
// cooldown has elapsed, no checkout gate hold or live drain blocks the path, no
// other turn is held there, and w is first among eligible waiters on that path
// (priority descending, then registration time ascending).
func (s *Store) TryAcquireRecoveryTurn(w RecoveryWaiter, now time.Time) (bool, error) {
	if w.SetID == "" || w.RuntimePath == "" || now.Before(w.ResetAt.UTC()) {
		return false, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var holdSetID sql.NullString
	err = tx.QueryRow(
		`SELECT set_id FROM checkout_gate_holds WHERE runtime_path = ?`, w.RuntimePath).
		Scan(&holdSetID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if holdSetID.Valid {
		return false, nil
	}

	rows, err := tx.Query(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND runtime_path = ?`,
		StateRunning, w.RuntimePath)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var d Drain
		var started string
		var procStart sql.NullString
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started); err != nil {
			_ = rows.Close()
			return false, err
		}
		d.ProcStart = procStart.String
		d.StartedAt = parseTime(started)
		d.State = StateRunning
		if s.drainAlive(d) {
			_ = rows.Close()
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	_ = rows.Close()

	var turnSetID sql.NullString
	err = tx.QueryRow(
		`SELECT set_id FROM recovery_turns WHERE runtime_path = ?`, w.RuntimePath).
		Scan(&turnSetID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if turnSetID.Valid {
		return false, nil
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
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if firstSetID != w.SetID {
		return false, nil
	}

	_, err = tx.Exec(
		`INSERT INTO recovery_turns (runtime_path, set_id, acquired_at) VALUES (?, ?, ?)`,
		w.RuntimePath, w.SetID, now.UTC().Format(timeLayout))
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
