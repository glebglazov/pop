// Package store is pop's single machine-global SQLite database for layer-2
// execution state — the non-derivable facts about how a drain ran (running,
// terminal exit reason, the agent it exhausted) that ADR-0055 moves off the
// filesystem and into one transactional store. Layer-1 Task set status stays
// manifest-derived on disk; nothing here restates it (ADR-0056).
//
// The store is a thin wrapper over database/sql backed by the pure-Go
// modernc.org/sqlite driver: it opens in WAL mode, runs a forward-only
// schema-migration step on open, and serialises writes through a single
// connection so a check-then-insert (drain mutual exclusion) is atomic across
// processes.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrDrainInProgress reports that a live running Drain already holds the
// (repository, set) or runtime checkout a StartDrain tried to claim.
var ErrDrainInProgress = errors.New("drain already in progress")

// Store is an open handle to the global execution-state database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at path in WAL mode and
// applies any outstanding schema migrations. The containing directory must
// already exist.
func Open(path string) (*Store, error) {
	// _txlock=immediate makes every transaction BEGIN IMMEDIATE so the
	// check-then-insert in StartDrain takes the write lock up front and a
	// competing starter blocks (then sees the inserted row) rather than racing.
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(on)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open execution-state store: %w", err)
	}
	// A single connection serialises writers in-process; WAL plus busy_timeout
	// serialise them across processes. pop's scale (a handful of concurrent
	// drains) makes this negligible (ADR-0055).
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrations is the forward-only, append-only list of schema steps. The
// database's PRAGMA user_version records how many have been applied; Open runs
// the remainder in order. Never edit a shipped entry — only append.
var migrations = []string{
	// 1: drains — one row per supervised execution of draining a Task set.
	`CREATE TABLE drains (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		repo               TEXT    NOT NULL,
		set_id             TEXT    NOT NULL,
		runtime_path       TEXT    NOT NULL,
		pid                INTEGER NOT NULL,
		started_at         TEXT    NOT NULL,
		state              TEXT    NOT NULL,
		finished_at        TEXT,
		exhausted_preset   TEXT,
		exhausted_pinned   INTEGER NOT NULL DEFAULT 0,
		exhausted_reset_at TEXT
	);
	CREATE INDEX idx_drains_repo_set ON drains(repo, set_id);
	CREATE INDEX idx_drains_runtime  ON drains(runtime_path);`,
	// 2: proc_start — an opaque token capturing the owning process's start
	// instant, recorded alongside pid so liveness can tell a still-running drain
	// from a reused PID (ADR-0055). Nullable: a row written before this column
	// existed, or by a platform that cannot read process start-time, carries no
	// token and falls back to bare PID liveness.
	`ALTER TABLE drains ADD COLUMN proc_start TEXT;`,
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for version < len(migrations) {
		if _, err := s.db.Exec(migrations[version]); err != nil {
			return fmt.Errorf("apply migration %d: %w", version+1, err)
		}
		version++
		// user_version cannot be parameterised; the value is a trusted int.
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			return fmt.Errorf("record schema version %d: %w", version, err)
		}
	}
	return nil
}
