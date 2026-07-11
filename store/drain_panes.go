package store

import (
	"database/sql"
	"time"
)

// DrainPane is the tmux pane the queue supervisor associates with a Task set
// drain, surfaced in the dashboard preview. It is transient preview data keyed
// by the caller-built scoped key (repository identity plus set id); the latest
// write for a set wins.
type DrainPane struct {
	ScopedKey   string
	Project     string
	RuntimePath string
	SetID       string
	PaneID      string
	RecordedAt  time.Time
	Source      string
}

// PutDrainPane upserts one drain-pane record under its scoped key.
func (s *Store) PutDrainPane(p DrainPane) error {
	_, err := s.db.Exec(
		`INSERT INTO drain_panes (scoped_key, project, runtime_path, set_id, pane_id, recorded_at, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(scoped_key) DO UPDATE SET
		   project=excluded.project, runtime_path=excluded.runtime_path,
		   set_id=excluded.set_id, pane_id=excluded.pane_id,
		   recorded_at=excluded.recorded_at, source=excluded.source`,
		p.ScopedKey, p.Project, p.RuntimePath, p.SetID, p.PaneID,
		p.RecordedAt.UTC().Format(timeLayout), p.Source)
	return err
}

// AllDrainPanes returns every recorded drain pane keyed by its scoped key.
func (s *Store) AllDrainPanes() (map[string]DrainPane, error) {
	rows, err := s.db.Query(
		`SELECT scoped_key, project, runtime_path, set_id, pane_id, recorded_at, source FROM drain_panes`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]DrainPane{}
	for rows.Next() {
		var p DrainPane
		var recorded sql.NullString
		if err := rows.Scan(&p.ScopedKey, &p.Project, &p.RuntimePath, &p.SetID, &p.PaneID, &recorded, &p.Source); err != nil {
			return nil, err
		}
		p.RecordedAt = parseTime(recorded.String)
		out[p.ScopedKey] = p
	}
	return out, rows.Err()
}
