package store

import "database/sql"

// Binding is one non-trunk Worktree binding: the durable 1:1 association between
// a Task set (within a repository identity) and the checkout it drains in. It is
// keyed by the caller-built scoped key. Provisioned is true when pop ran
// `git worktree add` to create the checkout (managed; torn down on
// integration/abandon) and false when the binding is adopted (a human pointed an
// existing checkout at the set; pop must never delete it).
type Binding struct {
	ScopedKey   string
	RuntimePath string
	Branch      string
	Project     string
	Provisioned bool
}

// AllBindings returns every binding row keyed by its scoped key.
func (s *Store) AllBindings() (map[string]Binding, error) {
	rows, err := s.db.Query(
		`SELECT scoped_key, runtime_path, branch, project, provisioned FROM bindings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Binding{}
	for rows.Next() {
		var b Binding
		var provisioned int
		if err := rows.Scan(&b.ScopedKey, &b.RuntimePath, &b.Branch, &b.Project, &provisioned); err != nil {
			return nil, err
		}
		b.Provisioned = provisioned != 0
		out[b.ScopedKey] = b
	}
	return out, rows.Err()
}

// LookupBinding returns the binding under scopedKey, ok=false when no row
// exists there. It is the keyed single-row read that replaces loading the whole
// table into a map just to index one key (ADR-0118).
func (s *Store) LookupBinding(scopedKey string) (Binding, bool, error) {
	row := s.db.QueryRow(
		`SELECT scoped_key, runtime_path, branch, project, provisioned FROM bindings WHERE scoped_key = ?`,
		scopedKey)
	b, provisioned := Binding{}, 0
	switch err := row.Scan(&b.ScopedKey, &b.RuntimePath, &b.Branch, &b.Project, &provisioned); err {
	case nil:
		b.Provisioned = provisioned != 0
		return b, true, nil
	case sql.ErrNoRows:
		return Binding{}, false, nil
	default:
		return Binding{}, false, err
	}
}

// PutBindingIfAbsent inserts b under its ScopedKey only when no row exists there
// yet, all inside one BEGIN IMMEDIATE transaction (the store opens with
// _txlock=immediate) so the check-then-insert is atomic — the same pattern
// StartDrain uses. It closes the read-modify-write race the whole-table façade
// left open: a concurrent Bind worktree and Queue provision can no longer clobber
// each other's rows (the race ADR-0055 existed to remove, ADR-0118). It returns
// (inserted, row): when it wrote the row, inserted is true and row is b; when a
// binding was already present, inserted is false and row is the existing binding,
// which is left untouched. Never overwrites.
func (s *Store) PutBindingIfAbsent(b Binding) (bool, Binding, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, Binding{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(
		`SELECT scoped_key, runtime_path, branch, project, provisioned FROM bindings WHERE scoped_key = ?`,
		b.ScopedKey)
	existing, provisioned := Binding{}, 0
	switch err := row.Scan(&existing.ScopedKey, &existing.RuntimePath, &existing.Branch, &existing.Project, &provisioned); err {
	case nil:
		existing.Provisioned = provisioned != 0
		return false, existing, nil
	case sql.ErrNoRows:
		// Absent — insert below.
	default:
		return false, Binding{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO bindings (scoped_key, runtime_path, branch, project, provisioned)
		 VALUES (?, ?, ?, ?, ?)`,
		b.ScopedKey, b.RuntimePath, b.Branch, b.Project, boolToInt(b.Provisioned)); err != nil {
		return false, Binding{}, err
	}
	if err := tx.Commit(); err != nil {
		return false, Binding{}, err
	}
	return true, b, nil
}

// PutBinding upserts a single binding row.
func (s *Store) PutBinding(b Binding) error {
	_, err := s.db.Exec(
		`INSERT INTO bindings (scoped_key, runtime_path, branch, project, provisioned)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(scoped_key) DO UPDATE SET
		   runtime_path=excluded.runtime_path, branch=excluded.branch,
		   project=excluded.project, provisioned=excluded.provisioned`,
		b.ScopedKey, b.RuntimePath, b.Branch, b.Project, boolToInt(b.Provisioned))
	return err
}

// DeleteBinding forgets the binding under scopedKey.
func (s *Store) DeleteBinding(scopedKey string) error {
	_, err := s.db.Exec(`DELETE FROM bindings WHERE scoped_key = ?`, scopedKey)
	return err
}
