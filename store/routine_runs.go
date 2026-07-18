package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrRoutineRunInProgress reports that a live running Routine run already holds
// the routine a StartRoutineRun tried to claim.
var ErrRoutineRunInProgress = errors.New("routine run already in progress")

// Routine run outcomes.
const (
	RoutineRunRunning   = "running"
	RoutineRunSucceeded = "succeeded"
	RoutineRunFailed    = "failed"
	RoutineRunSkipped   = "skipped"
)

// RoutineRun is one lifecycle row for a Routine run in the execution-state store.
type RoutineRun struct {
	ID          int64
	RoutineID   string
	FiredAt     time.Time
	Outcome     string
	SkipReason  string
	FailReason  string
	ReportPath  string
	PID         int
	ProcStart   string
	FinishedAt  time.Time
}

// StartRoutineRun inserts a running row and enforces per-routine exclusivity in
// one transaction. It refuses when a live running row already exists for the
// same routine_id. isAlive reports whether a recorded run's process is still
// running; a nil isAlive treats every recorded run as alive.
func (s *Store) StartRoutineRun(run RoutineRun, isAlive func(RoutineRun) bool) (RoutineRun, error) {
	if isAlive == nil {
		isAlive = func(RoutineRun) bool { return true }
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RoutineRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT id, routine_id, fired_at, pid, proc_start FROM routine_runs
		 WHERE routine_id = ? AND outcome = ?`,
		run.RoutineID, RoutineRunRunning)
	if err != nil {
		return RoutineRun{}, err
	}
	var live *RoutineRun
	for rows.Next() {
		var c RoutineRun
		var fired string
		var procStart sql.NullString
		if err := rows.Scan(&c.ID, &c.RoutineID, &fired, &c.PID, &procStart); err != nil {
			_ = rows.Close()
			return RoutineRun{}, err
		}
		c.ProcStart = procStart.String
		c.FiredAt, _ = time.Parse(timeLayout, fired)
		c.Outcome = RoutineRunRunning
		if isAlive(c) {
			live = &c
			break
		}
	}
	if err := rows.Close(); err != nil {
		return RoutineRun{}, err
	}
	if live != nil {
		return *live, ErrRoutineRunInProgress
	}

	res, err := tx.Exec(
		`INSERT INTO routine_runs (routine_id, fired_at, outcome, pid, proc_start)
		 VALUES (?, ?, ?, ?, ?)`,
		run.RoutineID,
		run.FiredAt.UTC().Format(timeLayout),
		RoutineRunRunning,
		run.PID,
		nullString(run.ProcStart),
	)
	if err != nil {
		return RoutineRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RoutineRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoutineRun{}, err
	}
	run.ID = id
	run.Outcome = RoutineRunRunning
	return run, nil
}

// FinishRoutineRun transitions a running row to a terminal outcome.
func (s *Store) FinishRoutineRun(id int64, outcome, reportPath, failReason string, finishedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE routine_runs
		 SET outcome = ?, report_path = ?, fail_reason = ?, finished_at = ?
		 WHERE id = ? AND outcome = ?`,
		outcome,
		reportPath,
		failReason,
		finishedAt.UTC().Format(timeLayout),
		id,
		RoutineRunRunning,
	)
	return err
}

// LastRoutineRun returns the most recent run row for a routine, if any.
func (s *Store) LastRoutineRun(routineID string) (*RoutineRun, error) {
	row := s.db.QueryRow(
		`SELECT id, routine_id, fired_at, outcome, skip_reason, fail_reason, report_path,
		        pid, proc_start, finished_at
		 FROM routine_runs
		 WHERE routine_id = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		routineID)
	return scanRoutineRun(row)
}

// LastRoutineFireTime returns the fired_at instant of the routine's most recent
// non-skipped run, or zero when the routine has never fired.
func (s *Store) LastRoutineFireTime(routineID string) (time.Time, error) {
	row := s.db.QueryRow(
		`SELECT fired_at FROM routine_runs
		 WHERE routine_id = ? AND outcome <> ?
		 ORDER BY fired_at DESC
		 LIMIT 1`,
		routineID, RoutineRunSkipped)
	var fired string
	if err := row.Scan(&fired); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	t, _ := time.Parse(timeLayout, fired)
	return t, nil
}

// LiveRoutineRun returns a live running row for routineID when isAlive reports
// the owning process is still running. Nil when none is live.
func (s *Store) LiveRoutineRun(routineID string, isAlive func(RoutineRun) bool) (*RoutineRun, error) {
	if isAlive == nil {
		isAlive = func(RoutineRun) bool { return true }
	}
	rows, err := s.db.Query(
		`SELECT id, routine_id, fired_at, outcome, skip_reason, fail_reason, report_path,
		        pid, proc_start, finished_at
		 FROM routine_runs
		 WHERE routine_id = ? AND outcome = ?`,
		routineID, RoutineRunRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		if isAlive(run) {
			return &run, nil
		}
	}
	return nil, rows.Err()
}

// InsertSkippedRoutineRun records a skipped fire with its reason.
func (s *Store) InsertSkippedRoutineRun(run RoutineRun) (RoutineRun, error) {
	stamp := run.FiredAt.UTC().Format(timeLayout)
	res, err := s.db.Exec(
		`INSERT INTO routine_runs (routine_id, fired_at, outcome, skip_reason, finished_at)
		 VALUES (?, ?, ?, ?, ?)`,
		run.RoutineID,
		stamp,
		RoutineRunSkipped,
		run.SkipReason,
		stamp,
	)
	if err != nil {
		return RoutineRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RoutineRun{}, err
	}
	run.ID = id
	run.Outcome = RoutineRunSkipped
	run.FinishedAt = run.FiredAt
	return run, nil
}

// ReconcileCrashedRoutineRuns transitions running rows whose owning process is
// no longer alive to failed, stamping finishedAt. It returns the number of rows
// transitioned. A nil isAlive treats every row as alive (a no-op).
func (s *Store) ReconcileCrashedRoutineRuns(isAlive func(RoutineRun) bool, finishedAt time.Time) (int, error) {
	if isAlive == nil {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT id, routine_id, fired_at, outcome, skip_reason, fail_reason, report_path,
		        pid, proc_start, finished_at
		 FROM routine_runs
		 WHERE outcome = ?`,
		RoutineRunRunning)
	if err != nil {
		return 0, err
	}
	var running []RoutineRun
	for rows.Next() {
		run, err := scanRoutineRunRow(rows)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		running = append(running, run)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	stamp := finishedAt.UTC().Format(timeLayout)
	reason := "process no longer alive"
	var crashed int
	for _, run := range running {
		if isAlive(run) {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE routine_runs
			 SET outcome = ?, fail_reason = ?, finished_at = ?
			 WHERE id = ? AND outcome = ?`,
			RoutineRunFailed, reason, stamp, run.ID, RoutineRunRunning); err != nil {
			return 0, err
		}
		crashed++
	}
	if crashed == 0 {
		return 0, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return crashed, nil
}

// ListAllRoutineRuns returns every routine run row, oldest first.
func (s *Store) ListAllRoutineRuns() ([]RoutineRun, error) {
	rows, err := s.db.Query(
		`SELECT id, routine_id, fired_at, outcome, skip_reason, fail_reason, report_path,
		        pid, proc_start, finished_at
		 FROM routine_runs
		 ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var runs []RoutineRun
	for rows.Next() {
		run, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// ListRoutineRuns returns all run rows for a routine, newest first.
func (s *Store) ListRoutineRuns(routineID string) ([]RoutineRun, error) {
	rows, err := s.db.Query(
		`SELECT id, routine_id, fired_at, outcome, skip_reason, fail_reason, report_path,
		        pid, proc_start, finished_at
		 FROM routine_runs
		 WHERE routine_id = ?
		 ORDER BY id DESC`,
		routineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []RoutineRun
	for rows.Next() {
		run, err := scanRoutineRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanRoutineRunRow(rows *sql.Rows) (RoutineRun, error) {
	var run RoutineRun
	var fired string
	var finished sql.NullString
	var procStart sql.NullString
	if err := rows.Scan(
		&run.ID,
		&run.RoutineID,
		&fired,
		&run.Outcome,
		&run.SkipReason,
		&run.FailReason,
		&run.ReportPath,
		&run.PID,
		&procStart,
		&finished,
	); err != nil {
		return RoutineRun{}, err
	}
	run.ProcStart = procStart.String
	run.FiredAt, _ = time.Parse(timeLayout, fired)
	if finished.Valid {
		run.FinishedAt, _ = time.Parse(timeLayout, finished.String)
	}
	return run, nil
}

func scanRoutineRun(row *sql.Row) (*RoutineRun, error) {
	var run RoutineRun
	var fired string
	var finished sql.NullString
	var procStart sql.NullString
	if err := row.Scan(
		&run.ID,
		&run.RoutineID,
		&fired,
		&run.Outcome,
		&run.SkipReason,
		&run.FailReason,
		&run.ReportPath,
		&run.PID,
		&procStart,
		&finished,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	run.ProcStart = procStart.String
	run.FiredAt, _ = time.Parse(timeLayout, fired)
	if finished.Valid {
		run.FinishedAt, _ = time.Parse(timeLayout, finished.String)
	}
	return &run, nil
}
