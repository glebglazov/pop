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
