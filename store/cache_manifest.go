package store

// cacheMigrationManifestEntries creates the persisted tier of the Manifest memo:
// one row per set directory, carrying the content key the row was derived under
// (ADR-0243 decision 2).
//
// Keyed by path rather than by the content key, which is what bounds the table
// by the machine's inventory instead of by its edit history: a task markdown
// edited ten times replaces one row ten times rather than minting ten. Nothing
// asks for an older content state — the question a reader has is always whether
// the directory as it stands right now is already validated — so nothing is lost
// by overwriting, and there is no pruning policy to maintain.
const cacheMigrationManifestEntries = `
CREATE TABLE manifest_entries (
	dir         TEXT PRIMARY KEY,
	content_key TEXT NOT NULL,
	manifest    BLOB NOT NULL
) WITHOUT ROWID
`

// ManifestEntry returns the manifest payload persisted for dir, and only when
// the row's stored content key is contentKey. The key comparison is the whole
// safety of this tier: it is recomputed from the directory on every serve, so an
// entry the directory no longer supports is not stale — it is unreachable.
//
// A missing row, a key that has moved on, or any database trouble at all is the
// same answer: no payload. The caller validates as if there were no cache.
func (c *Cache) ManifestEntry(dir, contentKey string) ([]byte, bool) {
	if c == nil || c.db == nil {
		return nil, false
	}
	var payload []byte
	err := c.db.QueryRow(
		`SELECT manifest FROM manifest_entries WHERE dir = ? AND content_key = ?`,
		dir, contentKey,
	).Scan(&payload)
	if err != nil {
		return nil, false
	}
	return payload, true
}

// PutManifestEntry records payload as dir's validated manifest under contentKey,
// replacing whatever dir held before, and reports whether the row landed.
// Failure — a lost WAL race, a full disk, no cache at all — is dropped: the next
// reader simply misses and re-validates, which is the behaviour of having no
// cache and never an error a human sees (ADR-0243 decision 4).
//
// The bool is not an error in disguise: no caller may act on why a cache write
// failed, only on whether the answer is on disk yet. A writer that skips rows it
// has already written needs that much, so it offers a dropped one again.
func (c *Cache) PutManifestEntry(dir, contentKey string, payload []byte) bool {
	if c == nil || c.db == nil {
		return false
	}
	_, err := c.db.Exec(
		`INSERT INTO manifest_entries (dir, content_key, manifest) VALUES (?, ?, ?)
		 ON CONFLICT(dir) DO UPDATE SET content_key = excluded.content_key, manifest = excluded.manifest`,
		dir, contentKey, payload,
	)
	return err == nil
}
