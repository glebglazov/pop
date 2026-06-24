package store

import (
	"database/sql"
	"time"
)

// Drain states. A row is born running and reaches exactly one terminal — an
// exit reason describing how the process ended, never the set's work
// disposition (ADR-0056). Crashed is reserved for the reconciliation slice.
const (
	StateRunning     = "running"
	StateFinished    = "finished"
	StateQuotaPaused = "quota_paused"
	StateInterrupted = "interrupted"
	StateCrashed     = "crashed"
)

// Drain is one supervised execution of draining a Task set, keyed by
// (repository identity, set id) and carrying the owning process so liveness can
// be checked.
type Drain struct {
	ID               int64
	Repo             string
	SetID            string
	RuntimePath      string
	PID              int
	StartedAt        time.Time
	State            string
	FinishedAt       time.Time
	ExhaustedPreset  string
	ExhaustedPinned  bool
	ExhaustedResetAt time.Time
}

const timeLayout = time.RFC3339Nano

// StartDrain inserts a running Drain and enforces mutual exclusion in one
// transaction: it refuses (ErrDrainInProgress) when a live running Drain
// already exists for the same (repo, set) or the same runtime checkout. A
// running row whose PID is no longer alive is stale (a crash the reconciliation
// slice will heal) and does not block. On refusal the returned Drain describes
// the conflicting live drain. isAlive reports whether a recorded PID is still
// running; a nil isAlive treats every recorded PID as alive.
func (s *Store) StartDrain(d Drain, isAlive func(pid int) bool) (Drain, error) {
	if isAlive == nil {
		isAlive = func(int) bool { return true }
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Drain{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT id, set_id, runtime_path, pid, started_at FROM drains
		 WHERE state = ? AND ((repo = ? AND set_id = ?) OR runtime_path = ?)`,
		StateRunning, d.Repo, d.SetID, d.RuntimePath)
	if err != nil {
		return Drain{}, err
	}
	var live []Drain
	for rows.Next() {
		var c Drain
		var started string
		if err := rows.Scan(&c.ID, &c.SetID, &c.RuntimePath, &c.PID, &started); err != nil {
			_ = rows.Close()
			return Drain{}, err
		}
		c.StartedAt = parseTime(started)
		c.State = StateRunning
		live = append(live, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Drain{}, err
	}
	_ = rows.Close()

	for _, c := range live {
		if isAlive(c.PID) {
			return c, ErrDrainInProgress
		}
	}

	res, err := tx.Exec(
		`INSERT INTO drains (repo, set_id, runtime_path, pid, started_at, state)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.Repo, d.SetID, d.RuntimePath, d.PID, d.StartedAt.UTC().Format(timeLayout), StateRunning)
	if err != nil {
		return Drain{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Drain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Drain{}, err
	}
	d.ID = id
	d.State = StateRunning
	return d, nil
}

// FinishDrain transitions a running Drain to a terminal exit reason. The
// exhausted-preset fields are meaningful only for StateQuotaPaused; pass zero
// values otherwise.
func (s *Store) FinishDrain(id int64, state string, exhaustedPreset string, exhaustedPinned bool, exhaustedResetAt, finishedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE drains
		   SET state = ?, finished_at = ?, exhausted_preset = ?, exhausted_pinned = ?, exhausted_reset_at = ?
		 WHERE id = ?`,
		state,
		finishedAt.UTC().Format(timeLayout),
		nullString(exhaustedPreset),
		boolToInt(exhaustedPinned),
		nullTime(exhaustedResetAt),
		id)
	return err
}

// CancelDrain removes a Drain row outright. It is used when a drain never
// executed (declined at the confirmation gate) so no terminal applies and the
// running row must not linger and block the next start.
func (s *Store) CancelDrain(id int64) error {
	_, err := s.db.Exec(`DELETE FROM drains WHERE id = ?`, id)
	return err
}

// LiveDrainByRuntimePath returns the live running Drain executing against
// runtimePath, or nil when none is running there. A running row whose PID is no
// longer alive is treated as not live.
func (s *Store) LiveDrainByRuntimePath(runtimePath string, isAlive func(pid int) bool) (*Drain, error) {
	if isAlive == nil {
		isAlive = func(int) bool { return true }
	}
	rows, err := s.db.Query(
		`SELECT id, repo, set_id, runtime_path, pid, started_at FROM drains
		 WHERE state = ? AND runtime_path = ? ORDER BY id DESC`,
		StateRunning, runtimePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d Drain
		var started string
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &started); err != nil {
			return nil, err
		}
		d.StartedAt = parseTime(started)
		d.State = StateRunning
		if isAlive(d.PID) {
			return &d, nil
		}
	}
	return nil, rows.Err()
}

// LatestTerminalByRuntimePath returns the most recently finished Drain that ran
// against runtimePath, or nil when none has reached a terminal there.
func (s *Store) LatestTerminalByRuntimePath(runtimePath string) (*Drain, error) {
	row := s.db.QueryRow(
		`SELECT id, repo, set_id, runtime_path, pid, started_at, state,
		        finished_at, exhausted_preset, exhausted_pinned, exhausted_reset_at
		 FROM drains
		 WHERE runtime_path = ? AND state <> ?
		 ORDER BY finished_at DESC, id DESC
		 LIMIT 1`,
		runtimePath, StateRunning)
	return scanDrain(row)
}

func scanDrain(row *sql.Row) (*Drain, error) {
	var d Drain
	var started, state string
	var finished, preset, resetAt sql.NullString
	var pinned int
	err := row.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &started, &state,
		&finished, &preset, &pinned, &resetAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.StartedAt = parseTime(started)
	d.State = state
	d.FinishedAt = parseTime(finished.String)
	d.ExhaustedPreset = preset.String
	d.ExhaustedPinned = pinned != 0
	d.ExhaustedResetAt = parseTime(resetAt.String)
	return &d, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(timeLayout)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
