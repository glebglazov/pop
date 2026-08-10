package store

import (
	"database/sql"
	"sort"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// taskSetKind is the Work-kind value every Task-set registration keys under in
// the cross-kind registry. Registration is two rows: the registry row that says
// pop looks after this container, and the task-set-side row holding what only a
// Task set has.
const taskSetKind = string(ref.KindTaskSet)

// SetReg is one Task set's registration metadata at the store layer: the
// machine-local priority, archived, and auto-drain bits ADR-0055 moves off the
// per-repository state.json into the global store. It is keyed by
// (DefPath, SetID), where DefPath identifies the repository's Task storage.
// Registration order — which the status table renders by — is carried by the
// registry row's autoincrement seq, not this struct.
//
// The fields span the two tables a registration lives in: SetID and Archived
// belong to the cross-kind registry (Archived means the same thing for every Work
// kind, so a Map files away through the same bit), and the rest are
// task-set-local. Callers see one struct because a registration is one fact to
// them.
type SetReg struct {
	DefPath   string
	SetID     string
	Priority  int
	Archived  bool
	AutoDrain bool
	// WorktreeManaged and WorktreeName carry the seeded worktree directive
	// (ADR-0059), read once at first registration. WorktreeManaged requests a
	// pop-provisioned managed worktree; else a non-empty WorktreeName adopts the
	// existing worktree of that name; else there is no directive. Provisioning is
	// lazy — these record intent only.
	WorktreeManaged bool
	WorktreeName    string
	// MutedUntil and MuteSecret are the container's Mute, read from the registry
	// row beside Archived (ADR-0200 decision 1). They are read-only here: a mute
	// is written by MuteWorkContainer alone, never as a side effect of restating a
	// registration, so a registration round-trip through PutSet leaves an existing
	// mute exactly where it was.
	MutedUntil time.Time
	MuteSecret bool
}

// AllSets returns every registration grouped by def_path, each slice ordered by
// registration order (the registry's seq autoincrement).
func (s *Store) AllSets() (map[string][]SetReg, error) {
	rows, err := s.db.Query(
		`SELECT t.def_path, c.id, t.priority, c.archived, c.muted_until, c.mute_secret,
		        t.auto_drain, t.worktree_managed, t.worktree_name
		   FROM work_containers c
		   JOIN task_set_registrations t ON t.container_seq = c.seq
		  WHERE c.kind = ?
		  ORDER BY c.seq`, taskSetKind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]SetReg{}
	for rows.Next() {
		var r SetReg
		var archived, muteSecret, autoDrain, worktreeManaged int
		var mutedUntil sql.NullString
		if err := rows.Scan(&r.DefPath, &r.SetID, &r.Priority, &archived, &mutedUntil, &muteSecret,
			&autoDrain, &worktreeManaged, &r.WorktreeName); err != nil {
			return nil, err
		}
		r.Archived = archived != 0
		r.MutedUntil = parseTime(mutedUntil.String)
		r.MuteSecret = muteSecret != 0
		r.AutoDrain = autoDrain != 0
		r.WorktreeManaged = worktreeManaged != 0
		out[r.DefPath] = append(out[r.DefPath], r)
	}
	return out, rows.Err()
}

// PutSet upserts one registration. On update the existing registry row is kept,
// so a metadata change (priority, archived, auto-drain toggle) never reorders the
// set in the status table.
func (s *Store) PutSet(r SetReg) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := putSetTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceAllSets makes the store's registrations exactly those in all, in one
// transaction: a registration the view no longer holds is unregistered, and the
// rest are upserted with def_paths in sorted order and each def_path's
// registrations in slice order, so a newly-registered set lands after the ones
// already there. It mirrors the whole-store rewrite the file-backed state did,
// kept atomic by the single writer — but reconciles rather than truncating,
// because the registry row it would delete and re-insert carries a seq and a
// registered_at that outlive any one view of the registrations.
func (s *Store) ReplaceAllSets(all map[string][]SetReg) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	defs := make([]string, 0, len(all))
	for def := range all {
		defs = append(defs, def)
	}
	sort.Strings(defs)
	keep := make(map[string]bool)
	for _, regs := range all {
		for _, r := range regs {
			keep[r.SetID] = true
		}
	}
	gone, err := unkeptTaskSetIDs(tx, keep)
	if err != nil {
		return err
	}
	for _, id := range gone {
		if err := deleteTaskSetTx(tx, id); err != nil {
			return err
		}
	}
	for _, def := range defs {
		for _, r := range all[def] {
			r.DefPath = def
			if err := putSetTx(tx, r); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// putSetTx writes one registration's two rows. The registry row is upserted on
// (kind, id) so its seq and registered_at survive an update — a set's place in
// the status table, and the day this machine took it on, are not the caller's to
// restate — while archived, the one cross-kind bit, is written there and
// everything kind-local goes to the task-set-side row. The mute columns are
// absent from the DO UPDATE list for the same reason the seq is: a mute outlives
// any one view of a registration, and MuteWorkContainer is its only writer.
func putSetTx(tx *sql.Tx, r SetReg) error {
	if _, err := tx.Exec(
		`INSERT INTO work_containers (kind, id, archived, registered_at)
		 VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		 ON CONFLICT(kind, id) DO UPDATE SET archived = excluded.archived`,
		taskSetKind, r.SetID, boolToInt(r.Archived)); err != nil {
		return err
	}
	var seq int64
	if err := tx.QueryRow(
		`SELECT seq FROM work_containers WHERE kind = ? AND id = ?`,
		taskSetKind, r.SetID).Scan(&seq); err != nil {
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO task_set_registrations
		   (container_seq, def_path, priority, auto_drain, worktree_managed, worktree_name)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(container_seq) DO UPDATE SET
		   def_path=excluded.def_path, priority=excluded.priority,
		   auto_drain=excluded.auto_drain,
		   worktree_managed=excluded.worktree_managed,
		   worktree_name=excluded.worktree_name`,
		seq, r.DefPath, r.Priority, boolToInt(r.AutoDrain),
		boolToInt(r.WorktreeManaged), r.WorktreeName)
	return err
}

// unkeptTaskSetIDs returns the registered Task-set ids absent from keep, read to
// completion before any delete runs: the store holds one connection, so a live
// cursor and a write on the same transaction cannot interleave.
func unkeptTaskSetIDs(tx *sql.Tx, keep map[string]bool) ([]string, error) {
	rows, err := tx.Query(`SELECT id FROM work_containers WHERE kind = ? ORDER BY seq`, taskSetKind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	return gone, rows.Err()
}

// deleteTaskSetTx unregisters one Task set, task-set-side row first so the delete
// stands on its own rather than on the foreign key's cascade being enabled.
func deleteTaskSetTx(tx *sql.Tx, setID string) error {
	if _, err := tx.Exec(
		`DELETE FROM task_set_registrations WHERE container_seq IN
		   (SELECT seq FROM work_containers WHERE kind = ? AND id = ?)`,
		taskSetKind, setID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM work_containers WHERE kind = ? AND id = ?`, taskSetKind, setID)
	return err
}
