package store

import "time"

// HistoryEntry is one History row: a path and the last instant a human landed in
// it (ADR-0188). The path is opaque here — the recording side canonicalises real
// checkouts, and the monitor dashboard records a `tmux:<session>` pseudo-path for
// a standalone session that has no checkout of its own.
type HistoryEntry struct {
	Path       string
	LastAccess time.Time
}

// PutHistoryEntry upserts path's last-access instant. The write runs in its own
// transaction, which with _txlock=immediate takes the database's write lock up
// front: two recorders landing at the same moment serialise instead of one
// clobbering the other, the property the whole-file rewrite this replaces never
// had. An empty path or a zero instant is a no-op.
func (s *Store) PutHistoryEntry(path string, at time.Time) error {
	if path == "" || at.IsZero() {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO history_entries (path, last_access) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET last_access = excluded.last_access`,
		path, at.UTC().Format(timeLayout)); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteHistoryEntry drops path's row, so a reset checkout or a deleted worktree
// stops skewing recency ordering. Removing an absent path is a no-op.
func (s *Store) DeleteHistoryEntry(path string) error {
	_, err := s.db.Exec(`DELETE FROM history_entries WHERE path = ?`, path)
	return err
}

// AllHistoryEntries returns every recorded entry ordered by path. The order is
// for determinism only: every reader above this sorts by the timestamps.
func (s *Store) AllHistoryEntries() ([]HistoryEntry, error) {
	rows, err := s.db.Query(`SELECT path, last_access FROM history_entries ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var lastAccess string
		if err := rows.Scan(&e.Path, &lastAccess); err != nil {
			return nil, err
		}
		e.LastAccess = parseTime(lastAccess)
		out = append(out, e)
	}
	return out, rows.Err()
}

// FoldHistoryEntries folds a legacy history file's entries into the store, at
// most once per source. source is the file's path; the marker row it writes is
// what makes the fold once-only, because the caller deliberately leaves the file
// on disk as its own rollback (ADR-0188). It reports whether this call did the
// fold.
//
// Entries already present win: a row in the store was written after the store
// existed, so it is necessarily the newer landing.
func (s *Store) FoldHistoryEntries(source string, entries []HistoryEntry, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var folded int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM history_folds WHERE source = ?`, source).Scan(&folded); err != nil {
		return false, err
	}
	if folded > 0 {
		return false, nil
	}
	for _, e := range entries {
		if e.Path == "" || e.LastAccess.IsZero() {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO history_entries (path, last_access) VALUES (?, ?)
			 ON CONFLICT(path) DO NOTHING`,
			e.Path, e.LastAccess.UTC().Format(timeLayout)); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO history_folds (source, folded_at) VALUES (?, ?)`,
		source, at.UTC().Format(timeLayout)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
