package store

import "sort"

// SetReg is one Task set's registration metadata at the store layer: the
// machine-local priority, archived, and auto-drain bits ADR-0055 moves off the
// per-repository state.json into the global store. It is keyed by
// (DefPath, SetID), where DefPath identifies the repository's Task storage.
// Registration order — which the status table renders by — is carried by the
// table's autoincrement seq, not this struct.
type SetReg struct {
	DefPath   string
	SetID     string
	Priority  int
	Archived  bool
	AutoDrain bool
}

// AllSets returns every registration grouped by def_path, each slice ordered by
// registration order (the seq autoincrement).
func (s *Store) AllSets() (map[string][]SetReg, error) {
	rows, err := s.db.Query(
		`SELECT def_path, set_id, priority, archived, auto_drain
		 FROM sets ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]SetReg{}
	for rows.Next() {
		var r SetReg
		var archived, autoDrain int
		if err := rows.Scan(&r.DefPath, &r.SetID, &r.Priority, &archived, &autoDrain); err != nil {
			return nil, err
		}
		r.Archived = archived != 0
		r.AutoDrain = autoDrain != 0
		out[r.DefPath] = append(out[r.DefPath], r)
	}
	return out, rows.Err()
}

// PutSet upserts one registration row. On update the existing seq is kept, so a
// metadata change (priority, archived, auto-drain toggle) never reorders the
// set in the status table.
func (s *Store) PutSet(r SetReg) error {
	_, err := s.db.Exec(
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(def_path, set_id) DO UPDATE SET
		   priority=excluded.priority, archived=excluded.archived,
		   auto_drain=excluded.auto_drain`,
		r.DefPath, r.SetID, r.Priority, boolToInt(r.Archived), boolToInt(r.AutoDrain))
	return err
}

// ReplaceAllSets replaces the entire sets table with all in one transaction. It
// inserts def_paths in sorted order and each def_path's registrations in slice
// order, so the autoincrement seq deterministically preserves registration
// order across rewrites. It mirrors the whole-store rewrite the file-backed
// state did, kept atomic by the single writer.
func (s *Store) ReplaceAllSets(all map[string][]SetReg) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM sets`); err != nil {
		return err
	}
	defs := make([]string, 0, len(all))
	for def := range all {
		defs = append(defs, def)
	}
	sort.Strings(defs)
	for _, def := range defs {
		for _, r := range all[def] {
			if _, err := tx.Exec(
				`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain)
				 VALUES (?, ?, ?, ?, ?)`,
				def, r.SetID, r.Priority, boolToInt(r.Archived), boolToInt(r.AutoDrain)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
