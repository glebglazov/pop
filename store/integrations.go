package store

import "time"

// Integration is one durable integration event: the record that a set's branch
// was merged into its base. ADR-0055 makes integration an explicit appended
// event rather than something inferred from a vanished binding. BaseRef is the
// base the branch merged into and BranchSHA the integrated branch's HEAD, so the
// event carries enough provenance to answer "was this set integrated, and from
// what?" without reading binding absence.
type Integration struct {
	ScopedKey    string
	SetID        string
	Project      string
	IntegratedAt time.Time
	BaseRef      string
	BranchSHA    string
}

// RecordIntegration appends one integration event. The table is append-only;
// the latest row for a set is its integration of record.
func (s *Store) RecordIntegration(ev Integration) error {
	_, err := s.db.Exec(
		`INSERT INTO integrations (scoped_key, set_id, project, integrated_at, base_ref, branch_sha)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ScopedKey, ev.SetID, ev.Project, mergeTime(ev.IntegratedAt), ev.BaseRef, ev.BranchSHA)
	return err
}

// IntegrationsForSet returns every integration event for setID, newest first.
func (s *Store) IntegrationsForSet(setID string) ([]Integration, error) {
	return s.scanIntegrations(
		`SELECT scoped_key, set_id, project, integrated_at, base_ref, branch_sha
		 FROM integrations WHERE set_id = ? ORDER BY id DESC`, setID)
}

// AllIntegrations returns every integration event, newest first.
func (s *Store) AllIntegrations() ([]Integration, error) {
	return s.scanIntegrations(
		`SELECT scoped_key, set_id, project, integrated_at, base_ref, branch_sha
		 FROM integrations ORDER BY id DESC`)
}

func (s *Store) scanIntegrations(query string, args ...any) ([]Integration, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Integration
	for rows.Next() {
		var ev Integration
		var at string
		if err := rows.Scan(&ev.ScopedKey, &ev.SetID, &ev.Project, &at, &ev.BaseRef, &ev.BranchSHA); err != nil {
			return nil, err
		}
		ev.IntegratedAt = parseTime(at)
		out = append(out, ev)
	}
	return out, rows.Err()
}
