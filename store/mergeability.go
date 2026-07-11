package store

import (
	"database/sql"
	"time"
)

// Mergeability is the stored merge verdict for a Done set's branch against its
// working checkout (ADR-0055). Verdict is "clean", "conflicts", or "unknown";
// BaseSHA and BranchSHA are the working and runtime HEADs the verdict was
// computed from, so a reader can gate recomputation on a SHA change instead of
// forking git on every poll.
type Mergeability struct {
	ScopedKey   string
	Project     string
	RuntimePath string
	WorkingPath string
	SetID       string
	Verdict     string
	BaseSHA     string
	BranchSHA   string
	ComputedAt  time.Time
}

// AllMergeability returns every mergeability row keyed by its scoped key.
func (s *Store) AllMergeability() (map[string]Mergeability, error) {
	rows, err := s.db.Query(
		`SELECT scoped_key, project, runtime_path, working_path, set_id,
		        verdict, base_sha, branch_sha, computed_at
		 FROM mergeability`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Mergeability{}
	for rows.Next() {
		var m Mergeability
		var computed sql.NullString
		if err := rows.Scan(&m.ScopedKey, &m.Project, &m.RuntimePath, &m.WorkingPath, &m.SetID,
			&m.Verdict, &m.BaseSHA, &m.BranchSHA, &computed); err != nil {
			return nil, err
		}
		m.ComputedAt = parseTime(computed.String)
		out[m.ScopedKey] = m
	}
	return out, rows.Err()
}

// PutMergeability upserts a single mergeability row.
func (s *Store) PutMergeability(m Mergeability) error {
	_, err := s.db.Exec(
		`INSERT INTO mergeability
		   (scoped_key, project, runtime_path, working_path, set_id, verdict, base_sha, branch_sha, computed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(scoped_key) DO UPDATE SET
		   project=excluded.project, runtime_path=excluded.runtime_path,
		   working_path=excluded.working_path, set_id=excluded.set_id,
		   verdict=excluded.verdict, base_sha=excluded.base_sha,
		   branch_sha=excluded.branch_sha, computed_at=excluded.computed_at`,
		m.ScopedKey, m.Project, m.RuntimePath, m.WorkingPath, m.SetID,
		m.Verdict, m.BaseSHA, m.BranchSHA, mergeTime(m.ComputedAt))
	return err
}

// DeleteMergeability forgets the row under scopedKey.
func (s *Store) DeleteMergeability(scopedKey string) error {
	_, err := s.db.Exec(`DELETE FROM mergeability WHERE scoped_key = ?`, scopedKey)
	return err
}

func mergeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}
