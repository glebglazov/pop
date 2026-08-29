package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// The Cache database has to survive a daemon, a dashboard and a CLI writing it
// at once, which is WAL plus a busy timeout and nothing else.
func TestOpenCacheUsesWALAndABusyTimeout(t *testing.T) {
	c, err := OpenCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}
	defer func() { _ = c.Close() }()

	var mode string
	if err := c.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var busy int
	if err := c.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busy)
	}
}

// A file written by a newer pop carries tables this build cannot read. Refusing
// it is what turns "schema from the future" into a miss at the caller rather
// than a misread row.
func TestOpenCacheRefusesASchemaFromTheFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := OpenCache(path)
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 9999`); err != nil {
		t.Fatalf("stamp future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	if _, err := OpenCache(path); err == nil {
		t.Fatal("OpenCache accepted a schema version this build does not know")
	}
}

// Every method is nil-safe, because a caller that failed to open the cache goes
// on calling it.
func TestNilCacheCloses(t *testing.T) {
	var c *Cache
	if err := c.Close(); err != nil {
		t.Fatalf("Close on a nil cache: %v", err)
	}
}

// The manifest table is keyed by set directory and serves only under the content
// key it was written with: re-recording a directory replaces its one row rather
// than adding a second, which is what bounds the table by the machine's inventory
// instead of by its edit history.
func TestManifestEntryServesOnlyUnderItsContentKey(t *testing.T) {
	c, err := OpenCache(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}
	defer func() { _ = c.Close() }()

	const dir = "/sets/demo"
	c.PutManifestEntry(dir, "key-1", []byte("first"))
	if payload, ok := c.ManifestEntry(dir, "key-1"); !ok || string(payload) != "first" {
		t.Fatalf("entry under its own key = %q, %v", payload, ok)
	}
	if _, ok := c.ManifestEntry(dir, "key-2"); ok {
		t.Fatal("an entry was served under a content key it was not written with")
	}
	if _, ok := c.ManifestEntry("/sets/other", "key-1"); ok {
		t.Fatal("a directory with no row was served one")
	}

	c.PutManifestEntry(dir, "key-2", []byte("second"))
	if payload, ok := c.ManifestEntry(dir, "key-2"); !ok || string(payload) != "second" {
		t.Fatalf("entry after the directory moved on = %q, %v", payload, ok)
	}
	if _, ok := c.ManifestEntry(dir, "key-1"); ok {
		t.Fatal("the superseded content state is still reachable")
	}
	var rows int
	if err := c.db.QueryRow(`SELECT count(*) FROM manifest_entries`).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows for one directory recorded twice = %d, want 1", rows)
	}
}

// A caller that failed to open the cache goes on calling it: reads miss and
// writes drop, and neither reaches the human as an error.
func TestNilCacheMissesAndDropsManifestEntries(t *testing.T) {
	var c *Cache
	c.PutManifestEntry("/sets/demo", "key", []byte("payload"))
	if _, ok := c.ManifestEntry("/sets/demo", "key"); ok {
		t.Fatal("a nil cache served an entry")
	}
}
