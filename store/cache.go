package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Cache is an open handle to the machine-local Cache database — pop's home for
// derived answers it would rather recompute than lose (ADR-0243 decision 1).
//
// It is emphatically not the execution state store. Nothing here is
// authoritative: every entry is re-validated against its source before it is
// served, so `rm cache.db` is always a valid repair. Keeping it in its own file
// is what stops read-path writes contending with authoritative ones on the
// execution state store's single process-cached connection.
//
// Every method is nil-safe. A caller that could not open the cache holds a nil
// *Cache and calls it anyway; reads then miss and writes drop. That is the whole
// error policy of decision 4 — a cache problem never reaches the human — and it
// only works because no tenant is allowed to treat a miss as unusual.
type Cache struct {
	db *sql.DB
}

// cacheMigrations is the forward-only, append-only schema of the Cache database,
// versioned by PRAGMA user_version exactly as the execution-state store's is.
// The two lists are independent: separate files, separate version counters.
//
// It is empty until a tenant lands one. Appending is the only legal edit — a
// database written by a newer pop carries a version past the end of this list,
// and OpenCache then refuses it (see errCacheSchemaAhead) so an older binary
// reads a miss instead of misreading a table it does not know.
var cacheMigrations = []string{}

// errCacheSchemaAhead reports a cache database written by a newer pop. It is
// returned rather than recovered from: the caller's response is to run with no
// cache at all, which is the same response it has to every other open failure.
var errCacheSchemaAhead = fmt.Errorf("cache database schema is newer than this pop")

// OpenCache opens (creating if absent) the Cache database at path, in WAL mode
// with a busy timeout, and applies any outstanding schema steps. The containing
// directory must already exist.
//
// It mirrors Open's pragmas so a daemon, a dashboard and a CLI can write the
// file at once, and returns an error for every unusable state — absent
// directory, corrupt file, schema from the future. Callers on the read path turn
// that error into "no cache" and never surface it.
func OpenCache(path string) (*Cache, error) {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	// One connection serialises writers in-process, WAL plus busy_timeout
	// serialises them across processes — the same bargain the execution-state
	// store strikes. A cache read costs a fraction of a millisecond, so the
	// queueing it imposes on a concurrent reader is not worth a pool.
	db.SetMaxOpenConns(1)
	c := &Cache{db: db}
	if err := c.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

// Close releases the underlying database handle.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// migrate brings the file up to len(cacheMigrations), retrying a lock error the
// way the execution-state store does: concurrent first-creates contend on WAL
// initialisation, and a loser should wait out the winner rather than fail the
// open (and cost its caller the cache for the run).
func (c *Cache) migrate() error {
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		if err = c.migrateOnce(); err == nil {
			return nil
		}
		if !isLockedErr(err) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

func (c *Cache) migrateOnce() error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin cache schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read cache schema version: %w", err)
	}
	if version > len(cacheMigrations) {
		return fmt.Errorf("%w (file is at %d, this build knows %d)", errCacheSchemaAhead, version, len(cacheMigrations))
	}
	for version < len(cacheMigrations) {
		if _, err := tx.Exec(cacheMigrations[version]); err != nil {
			return fmt.Errorf("apply cache migration %d: %w", version+1, err)
		}
		version++
		// user_version cannot be parameterised; the value is a trusted int.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			return fmt.Errorf("record cache schema version %d: %w", version, err)
		}
	}
	return tx.Commit()
}
