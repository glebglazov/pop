package store

import (
	"database/sql"
	"time"
)

// Drain states. A row is born running and reaches exactly one terminal — an
// exit reason describing how the process ended, never the set's work
// disposition (ADR-0056). Finished, quota_paused, and verify_failed are clean
// stops; interrupted and crashed are abnormal teardowns (see drainStateAbnormal),
// the axis that feeds backoff and parking.
const (
	StateRunning      = "running"
	StateFinished     = "finished"
	StateQuotaPaused  = "quota_paused"
	StateVerifyFailed = "verify_failed"
	StateInterrupted  = "interrupted"
	StateCrashed      = "crashed"
)

// Drain is one supervised execution of draining a Task set, keyed by
// (repository identity, set id) and carrying the owning process so liveness can
// be checked.
type Drain struct {
	ID          int64
	Repo        string
	SetID       string
	RuntimePath string
	PID         int
	// ProcStart is an opaque token identifying the owning process's start
	// instant. Paired with PID it survives PID reuse: a recorded PID that is
	// live but whose process now reports a different start token is a different
	// process, not this drain. Empty when start-time could not be captured.
	ProcStart        string
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
// the conflicting live drain. isAlive reports whether a recorded drain's process
// is still running (checked against its PID and ProcStart so a reused PID does
// not read as live); a nil isAlive treats every recorded drain as alive.
func (s *Store) StartDrain(d Drain, isAlive func(Drain) bool) (Drain, error) {
	if isAlive == nil {
		isAlive = func(Drain) bool { return true }
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Drain{}, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT id, set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND ((repo = ? AND set_id = ?) OR runtime_path = ?)`,
		StateRunning, d.Repo, d.SetID, d.RuntimePath)
	if err != nil {
		return Drain{}, err
	}
	var live []Drain
	for rows.Next() {
		var c Drain
		var started string
		var procStart sql.NullString
		if err := rows.Scan(&c.ID, &c.SetID, &c.RuntimePath, &c.PID, &procStart, &started); err != nil {
			_ = rows.Close()
			return Drain{}, err
		}
		c.ProcStart = procStart.String
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
		if isAlive(c) {
			return c, ErrDrainInProgress
		}
	}

	res, err := tx.Exec(
		`INSERT INTO drains (repo, set_id, runtime_path, pid, proc_start, started_at, state)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.Repo, d.SetID, d.RuntimePath, d.PID, nullString(d.ProcStart), d.StartedAt.UTC().Format(timeLayout), StateRunning)
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
// runtimePath, or nil when none is running there. A running row whose process is
// no longer alive (dead PID, or a reused PID with a different start token) is
// treated as not live.
func (s *Store) LiveDrainByRuntimePath(runtimePath string, isAlive func(Drain) bool) (*Drain, error) {
	if isAlive == nil {
		isAlive = func(Drain) bool { return true }
	}
	rows, err := s.db.Query(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND runtime_path = ? ORDER BY id DESC`,
		StateRunning, runtimePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d Drain
		var started string
		var procStart sql.NullString
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started); err != nil {
			return nil, err
		}
		d.ProcStart = procStart.String
		d.StartedAt = parseTime(started)
		d.State = StateRunning
		if isAlive(d) {
			return &d, nil
		}
	}
	return nil, rows.Err()
}

// ReconcileCrashed is the opportunistic reconcile pass every layer-2 reader runs
// before reading (ADR-0055): in one bounded transaction it finds running Drains
// whose owning process is no longer alive and transitions them to crashed,
// stamping finishedAt. isAlive is checked against each row's PID and ProcStart so
// a reused PID is not mistaken for a live drain; a still-live drain is left
// untouched. It forks nothing — it reads only the drains table — and returns the
// number of rows transitioned. A nil isAlive treats every row as alive (a no-op).
func (s *Store) ReconcileCrashed(isAlive func(Drain) bool, finishedAt time.Time) (int, error) {
	if isAlive == nil {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at FROM drains
		 WHERE state = ?`,
		StateRunning)
	if err != nil {
		return 0, err
	}
	var running []Drain
	for rows.Next() {
		var d Drain
		var started string
		var procStart sql.NullString
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started); err != nil {
			_ = rows.Close()
			return 0, err
		}
		d.ProcStart = procStart.String
		d.StartedAt = parseTime(started)
		d.State = StateRunning
		running = append(running, d)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	stamp := finishedAt.UTC().Format(timeLayout)
	var crashed int
	for _, d := range running {
		if isAlive(d) {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE drains SET state = ?, finished_at = ? WHERE id = ? AND state = ?`,
			StateCrashed, stamp, d.ID, StateRunning); err != nil {
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

// LatestTerminalByRuntimePath returns the most recently finished Drain that ran
// against runtimePath, or nil when none has reached a terminal there.
func (s *Store) LatestTerminalByRuntimePath(runtimePath string) (*Drain, error) {
	row := s.db.QueryRow(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at, state,
		        finished_at, exhausted_preset, exhausted_pinned, exhausted_reset_at
		 FROM drains
		 WHERE runtime_path = ? AND state <> ?
		 ORDER BY finished_at DESC, id DESC
		 LIMIT 1`,
		runtimePath, StateRunning)
	return scanDrain(row)
}

// AllDrains returns every Drain row (running and terminal) ordered oldest-first
// by id. It is the source for the Queue journal view (ADR-0055): each row
// contributes a "spawned" event at started_at and, once terminal, an outcome
// event at finished_at.
func (s *Store) AllDrains() ([]Drain, error) {
	return s.queryDrains(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at, state,
		        finished_at, exhausted_preset, exhausted_pinned, exhausted_reset_at
		 FROM drains ORDER BY id ASC`)
}

// RunningDrains returns every Drain still in the running state, ordered
// oldest-first by id. Liveness is the caller's concern (it pairs each row's PID
// and ProcStart against the OS); a reader that wants only live drains filters
// with its own isAlive predicate.
func (s *Store) RunningDrains() ([]Drain, error) {
	return s.queryDrains(
		`SELECT id, repo, set_id, runtime_path, pid, proc_start, started_at, state,
		        finished_at, exhausted_preset, exhausted_pinned, exhausted_reset_at
		 FROM drains WHERE state = ? ORDER BY id ASC`, StateRunning)
}

func (s *Store) queryDrains(query string, args ...any) ([]Drain, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Drain
	for rows.Next() {
		var d Drain
		var started, state string
		var procStart, finished, preset, resetAt sql.NullString
		var pinned int
		if err := rows.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started, &state,
			&finished, &preset, &pinned, &resetAt); err != nil {
			return nil, err
		}
		d.ProcStart = procStart.String
		d.StartedAt = parseTime(started)
		d.State = state
		d.FinishedAt = parseTime(finished.String)
		d.ExhaustedPreset = preset.String
		d.ExhaustedPinned = pinned != 0
		d.ExhaustedResetAt = parseTime(resetAt.String)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetBackoff summarises a (repo, set)'s recent Drain history for deriving Queue
// backoff and parking (ADR-0055): the count of consecutive abnormal terminals
// (crashed/interrupted) since the last clean one (finished/quota_paused), the
// instant of the most recent abnormal terminal, and the latest park-clear
// (unpark) event. Backoff delay and the parked decision are computed from these
// — no timer or flag is persisted.
type SetBackoff struct {
	ConsecutiveAbnormal int
	LastAbnormalAt      time.Time
	ParkClearedAt       time.Time
}

// ReadSetBackoff walks the (repo, set)'s terminal Drains newest-first, counting
// the unbroken run of abnormal terminals until the first clean one — a clean
// terminal resets the count for free, so no counter is stored — and reads the
// latest park-clear event.
func (s *Store) ReadSetBackoff(repo, setID string) (SetBackoff, error) {
	rows, err := s.db.Query(
		`SELECT state, finished_at FROM drains
		 WHERE repo = ? AND set_id = ? AND state <> ?
		 ORDER BY finished_at DESC, id DESC`,
		repo, setID, StateRunning)
	if err != nil {
		return SetBackoff{}, err
	}
	var info SetBackoff
	for rows.Next() {
		var state string
		var finished sql.NullString
		if err := rows.Scan(&state, &finished); err != nil {
			_ = rows.Close()
			return SetBackoff{}, err
		}
		if !drainStateAbnormal(state) {
			break
		}
		if info.ConsecutiveAbnormal == 0 {
			info.LastAbnormalAt = parseTime(finished.String)
		}
		info.ConsecutiveAbnormal++
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return SetBackoff{}, err
	}
	// Release the single connection before the next query: a loop that broke on a
	// clean terminal leaves rows open, and with SetMaxOpenConns(1) latestParkClear
	// would otherwise wait forever for a connection.
	if err := rows.Close(); err != nil {
		return SetBackoff{}, err
	}
	cleared, err := s.latestParkClear(repo, setID)
	if err != nil {
		return SetBackoff{}, err
	}
	info.ParkClearedAt = cleared
	return info, nil
}

func (s *Store) latestParkClear(repo, setID string) (time.Time, error) {
	row := s.db.QueryRow(
		`SELECT cleared_at FROM park_clears
		 WHERE repo = ? AND set_id = ?
		 ORDER BY cleared_at DESC, id DESC LIMIT 1`,
		repo, setID)
	var cleared sql.NullString
	switch err := row.Scan(&cleared); err {
	case nil:
		return parseTime(cleared.String), nil
	case sql.ErrNoRows:
		return time.Time{}, nil
	default:
		return time.Time{}, err
	}
}

// ParkClear is one durable park-clear (unpark) event: a human lifted the derived
// park on (repo, set) at ClearedAt.
type ParkClear struct {
	Repo      string
	SetID     string
	ClearedAt time.Time
}

// AllParkClears returns every park-clear event ordered oldest-first by id, for
// the Queue journal view (ADR-0055).
func (s *Store) AllParkClears() ([]ParkClear, error) {
	rows, err := s.db.Query(
		`SELECT repo, set_id, cleared_at FROM park_clears ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ParkClear
	for rows.Next() {
		var pc ParkClear
		var cleared string
		if err := rows.Scan(&pc.Repo, &pc.SetID, &cleared); err != nil {
			return nil, err
		}
		pc.ClearedAt = parseTime(cleared)
		out = append(out, pc)
	}
	return out, rows.Err()
}

// RecordParkClear appends a park-clear (unpark) event for (repo, set). It is the
// only durable addition to the otherwise-derived backoff/parking model: a clear
// newer than the set's latest abnormal Drain lifts the park (ADR-0055).
func (s *Store) RecordParkClear(repo, setID string, at time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO park_clears (repo, set_id, cleared_at) VALUES (?, ?, ?)`,
		repo, setID, at.UTC().Format(timeLayout))
	return err
}

// drainStateAbnormal reports whether a terminal state is an abnormal exit — a
// process torn down out from under the executor — versus a clean stop. This is
// the axis that feeds backoff and parking.
func drainStateAbnormal(state string) bool {
	return state == StateCrashed || state == StateInterrupted
}

func scanDrain(row *sql.Row) (*Drain, error) {
	var d Drain
	var started, state string
	var procStart, finished, preset, resetAt sql.NullString
	var pinned int
	err := row.Scan(&d.ID, &d.Repo, &d.SetID, &d.RuntimePath, &d.PID, &procStart, &started, &state,
		&finished, &preset, &pinned, &resetAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ProcStart = procStart.String
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
