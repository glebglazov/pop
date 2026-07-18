package store

import (
	"database/sql"
	"time"
)

// SpawnIntent is a durable pending-spawn marker: the supervisor recorded that it
// dispatched `pop tasks implement <set>` into a pane but that drain has not yet
// reached BeginDrain (no running Drain row exists yet). While a fresh intent is
// present the dispatcher treats the set as busy, so a fast re-poll cannot send a
// second implement into the same pane. It carries the recording process's owner
// identity (PID plus start token, the same pairing drains use) so an intent
// whose owner died can be swept, and created_at so it expires if the spawned
// process never reaches BeginDrain.
type SpawnIntent struct {
	Repo        string
	SetID       string
	RuntimePath string
	PID         int
	ProcStart   string
	CreatedAt   time.Time
}

// PutSpawnIntent records (or refreshes) the pending-spawn marker for one
// (repo, set). Re-dispatching the same set overwrites the prior intent.
func (s *Store) PutSpawnIntent(si SpawnIntent) error {
	if si.Repo == "" || si.SetID == "" {
		return nil
	}
	createdAt := si.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO spawn_intents (repo, set_id, runtime_path, pid, proc_start, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(repo, set_id) DO UPDATE SET
		   runtime_path = excluded.runtime_path,
		   pid = excluded.pid,
		   proc_start = excluded.proc_start,
		   created_at = excluded.created_at`,
		si.Repo,
		si.SetID,
		si.RuntimePath,
		si.PID,
		nullString(si.ProcStart),
		createdAt.UTC().Format(timeLayout))
	return err
}

// DeleteSpawnIntent removes the pending-spawn marker for one (repo, set). A
// missing row is not an error. BeginDrain calls this once the running Drain row
// exists, so the intent stops shadowing the now-visible drain.
func (s *Store) DeleteSpawnIntent(repo, setID string) error {
	if repo == "" || setID == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM spawn_intents WHERE repo = ? AND set_id = ?`, repo, setID)
	return err
}

// SpawnIntentsForRepo returns the intents for one repository whose created_at is
// strictly after freshAfter (i.e. still within their TTL). Expired intents are
// omitted so a spawn that never reached BeginDrain stops blocking re-selection.
// Owner liveness is left to the caller (it needs process access outside the
// store), mirroring how running Drains are read then liveness-filtered.
func (s *Store) SpawnIntentsForRepo(repo string, freshAfter time.Time) ([]SpawnIntent, error) {
	if repo == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT repo, set_id, runtime_path, pid, proc_start, created_at
		 FROM spawn_intents WHERE repo = ? AND created_at > ?`,
		repo, freshAfter.UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SpawnIntent
	for rows.Next() {
		var si SpawnIntent
		var procStart, createdAt sql.NullString
		if err := rows.Scan(&si.Repo, &si.SetID, &si.RuntimePath, &si.PID, &procStart, &createdAt); err != nil {
			return nil, err
		}
		si.ProcStart = procStart.String
		si.CreatedAt = parseTime(createdAt.String)
		out = append(out, si)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReconcileSpawnIntents is the spawn-intent arm of the opportunistic reconcile
// pass: in one bounded transaction it deletes intents that have expired (created
// at or before cutoff) or whose recording process is no longer alive, using the
// same PID+start-token liveness the drains and gate holds use. It returns the
// number of intents swept.
//
// Rows are fully read and the cursor closed before any DELETE is issued so the
// store's single connection is never asked to run a follow-up statement with an
// open result set.
func (s *Store) ReconcileSpawnIntents(cutoff time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT repo, set_id, pid, proc_start, created_at FROM spawn_intents`)
	if err != nil {
		return 0, err
	}
	type intent struct {
		repo      string
		setID     string
		pid       int
		procStart string
		createdAt time.Time
	}
	var intents []intent
	for rows.Next() {
		var in intent
		var procStart, createdAt sql.NullString
		if err := rows.Scan(&in.repo, &in.setID, &in.pid, &procStart, &createdAt); err != nil {
			_ = rows.Close()
			return 0, err
		}
		in.procStart = procStart.String
		in.createdAt = parseTime(createdAt.String)
		intents = append(intents, in)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	cutoffUTC := cutoff.UTC()
	var swept int
	for _, in := range intents {
		expired := !in.createdAt.After(cutoffUTC)
		dead := !s.alive(in.pid, in.procStart)
		if !expired && !dead {
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM spawn_intents WHERE repo = ? AND set_id = ?`, in.repo, in.setID); err != nil {
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
