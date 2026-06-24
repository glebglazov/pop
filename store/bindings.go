package store

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

// ReplaceAllBindings replaces the entire bindings table with all in one
// transaction. It mirrors the whole-store rewrite the file-backed store did,
// kept atomic by the single writer.
func (s *Store) ReplaceAllBindings(all map[string]Binding) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM bindings`); err != nil {
		return err
	}
	for key, b := range all {
		b.ScopedKey = key
		if _, err := tx.Exec(
			`INSERT INTO bindings (scoped_key, runtime_path, branch, project, provisioned)
			 VALUES (?, ?, ?, ?, ?)`,
			b.ScopedKey, b.RuntimePath, b.Branch, b.Project, boolToInt(b.Provisioned)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
