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
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// openCount counts every call to Open process-wide. It is test-only
// instrumentation — it lets a test assert that a caller (e.g. the queue
// dashboard build) opens the store a bounded number of times regardless of
// workload — and carries no production behaviour.
var openCount atomic.Int64

// OpenCount returns the number of times Open has been called process-wide. It
// exists so tests can assert a bounded number of store opens.
func OpenCount() int64 { return openCount.Load() }

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
	openCount.Add(1)
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
	// 3: mergeability — the kept-fresh merge verdict for a Done set's branch
	// against its working checkout, keyed per (repository identity, set id) via
	// the caller-built scoped key. It carries the two HEADs the verdict was
	// computed from so a reader can cheaply gate recomputation on a SHA change
	// (ADR-0051/0055): `unknown` is never stored as steady state, only the
	// transient gap between a set going Done and the next reconcile.
	`CREATE TABLE mergeability (
		scoped_key   TEXT PRIMARY KEY,
		project      TEXT NOT NULL DEFAULT '',
		runtime_path TEXT NOT NULL DEFAULT '',
		working_path TEXT NOT NULL DEFAULT '',
		set_id       TEXT NOT NULL,
		verdict      TEXT NOT NULL,
		base_sha     TEXT NOT NULL DEFAULT '',
		branch_sha   TEXT NOT NULL DEFAULT '',
		computed_at  TEXT NOT NULL DEFAULT ''
	);`,
	// 4: park_clears — the durable park-clear (unpark) event. Queue backoff and
	// parking are otherwise derived from Drain history (the run of abnormal
	// terminals); the only persisted addition is this event, appended when a
	// human clears a parked set. A clear newer than the set's latest abnormal
	// Drain lifts the derived park (ADR-0055). Append-only: the latest row wins.
	`CREATE TABLE park_clears (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		repo       TEXT NOT NULL,
		set_id     TEXT NOT NULL,
		cleared_at TEXT NOT NULL
	);
	CREATE INDEX idx_park_clears_repo_set ON park_clears(repo, set_id);`,
	// 5: bindings — the non-trunk Worktree bindings that are the Integration
	// backlog's source (ADR-0051), keyed per (repository identity, set id) via the
	// caller-built scoped key. The provisioned bit distinguishes a pop-provisioned
	// (managed) checkout — torn down on integration/abandon — from an adopted one a
	// human pointed at the set, which pop must never delete. This moves the binding
	// store off the standalone bindings.json file into the global store (ADR-0055);
	// the file's contents are migrated in at the tasks boundary on first read.
	`CREATE TABLE bindings (
		scoped_key   TEXT PRIMARY KEY,
		runtime_path TEXT NOT NULL DEFAULT '',
		branch       TEXT NOT NULL DEFAULT '',
		project      TEXT NOT NULL DEFAULT '',
		provisioned  INTEGER NOT NULL DEFAULT 0
	);`,
	// 6: integrations — the durable integration event {at, base_ref, branch_sha}.
	// ADR-0055 kills "integrated = binding released": integration is now an
	// explicit appended event, not inferred from a vanished binding. Append-only;
	// the latest row for a set is its integration of record.
	`CREATE TABLE integrations (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		scoped_key    TEXT NOT NULL,
		set_id        TEXT NOT NULL,
		project       TEXT NOT NULL DEFAULT '',
		integrated_at TEXT NOT NULL,
		base_ref      TEXT NOT NULL DEFAULT '',
		branch_sha    TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX idx_integrations_set    ON integrations(set_id);
	CREATE INDEX idx_integrations_scoped ON integrations(scoped_key);`,
	// 7: agent_cooldowns — the machine-global, per-agent-preset quota cooldown a
	// quota_paused Drain produces: the instant a subscription-level agent preset
	// may be tried again. ADR-0055 moves this off the standalone
	// agent-cooldowns.json file into the store; the queue's agent fallback reads it
	// to skip a preset still cooling down. Keyed by preset; the latest write wins.
	`CREATE TABLE agent_cooldowns (
		preset          TEXT PRIMARY KEY,
		exhausted_until TEXT NOT NULL
	);`,
	// 8: sets — per-repository Task set registration metadata (priority, archived,
	// auto-drain), keyed (def_path, set_id) where def_path identifies the
	// repository's Task storage. ADR-0055 completes the layer-2 consolidation by
	// moving registration off the per-repo state.json files into the global store;
	// the files are folded in at the tasks boundary on first read, then retired.
	// The autoincrement seq preserves registration order, which the status table
	// renders by. Layer-1 Task set status stays manifest-derived (ADR-0006/0056);
	// only this machine-local registration moves.
	`CREATE TABLE sets (
		seq        INTEGER PRIMARY KEY AUTOINCREMENT,
		def_path   TEXT    NOT NULL,
		set_id     TEXT    NOT NULL,
		priority   INTEGER NOT NULL DEFAULT 0,
		archived   INTEGER NOT NULL DEFAULT 0,
		auto_drain INTEGER NOT NULL DEFAULT 0,
		UNIQUE(def_path, set_id)
	);
	CREATE INDEX idx_sets_def ON sets(def_path);`,
	// 9: worktree-intent seed (ADR-0059) — the optional set-level worktree
	// directive read once at first registration alongside auto_drain. Two columns
	// carry the three states without a sentinel collision: worktree_managed=1
	// requests a pop-provisioned managed worktree; else a non-empty worktree_name
	// adopts the existing worktree of that name on this machine; else (the default
	// 0/'') there is no directive and the set drains in the current checkout.
	// Intent only — no provisioning happens at registration (lazy, per ADR-0059).
	`ALTER TABLE sets ADD COLUMN worktree_managed INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE sets ADD COLUMN worktree_name TEXT NOT NULL DEFAULT '';`,
	// 10: verify_verdicts — the SHA-gated Verify verdict for a Task set (ADR-0086):
	// an independent Verifier agent's PASS / FIXABLE / NEEDS-HUMAN judgment of the
	// set's completed AFK work, cached by the work SHA it was computed at, keyed
	// (repo, set_id, work_sha) where repo is the repository's git common dir (the
	// same identity the drains table uses). It is a cache, not a completion flag:
	// when the work SHA moves the verdict for the new SHA is simply absent, so the
	// set returns to needing verification. `pop tasks verify` always overwrites the
	// row for the current SHA (force). findings carries the Verifier's human-facing
	// text (the reasons behind a non-PASS, empty for PASS).
	`CREATE TABLE verify_verdicts (
		repo        TEXT NOT NULL,
		set_id      TEXT NOT NULL,
		work_sha    TEXT NOT NULL,
		verdict     TEXT NOT NULL,
		findings    TEXT NOT NULL DEFAULT '',
		computed_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (repo, set_id, work_sha)
	);
	CREATE INDEX idx_verify_verdicts_repo_set ON verify_verdicts(repo, set_id);`,
	// 11: recovery_waiters — the quota recovery wait registration (ADR-0100): when
	// agent quota detection exhausts the fallback chain on a task attempt, instead
	// of terminal-exit the drain parks, registers a waiter, and polls until the
	// cooldown elapses and a recovery turn is acquired. The waiter claims the set
	// against duplicate work (UNIQUE on set_id). Recovery turn ordering and the
	// per-checkout turn claim live in recovery_turns; this table records only the
	// wait registration so a crash can be reconciled (stale waiters cleared by
	// dead-PID check on the associated drain, or by explicit deregistration on
	// SIGINT). Priority mirrors the task-set registration priority for turn
	// ordering when multiple waiters contend.
	`CREATE TABLE recovery_waiters (
		set_id        TEXT PRIMARY KEY,
		preset        TEXT NOT NULL,
		reset_at      TEXT NOT NULL,
		runtime_path  TEXT NOT NULL,
		priority      INTEGER NOT NULL DEFAULT 0,
		registered_at TEXT NOT NULL
	);`,
	// 12: checkout_gate_holds — occupancy while a drain is parked at a Failed or
	// HITL gate (ADR-0100). The runtime lock is released per ADR-0067, but the
	// coordinator must still treat the checkout as busy so a quota recovery waiter
	// on another set cannot resume agent work on the same dirty tree while a human
	// sits at a gate. Keyed by runtime_path (one gate session per checkout).
	`CREATE TABLE checkout_gate_holds (
		runtime_path  TEXT PRIMARY KEY,
		set_id        TEXT NOT NULL,
		registered_at TEXT NOT NULL
	);`,
	// 13: recovery_turns — checkout-scoped recovery turn claim (ADR-0100). At most
	// one waiter may hold a turn per runtime path between grant and BeginDrain so
	// parallel poll loops cannot both resume on the same checkout. Released when
	// the owning process re-acquires the drain or abandons recovery.
	`CREATE TABLE recovery_turns (
		runtime_path TEXT PRIMARY KEY,
		set_id       TEXT NOT NULL,
		acquired_at  TEXT NOT NULL
	);`,
}

func (s *Store) migrate() error {
	// Concurrent first-creates (several processes opening a fresh database at
	// once) contend on WAL initialisation and the write lock; busy_timeout does
	// not always absorb the lock taken to switch journal modes on an empty file.
	// Retry the bounded migration transaction a few times on a lock error so the
	// losers wait out the winner rather than failing the open.
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		if err = s.migrateOnce(); err == nil {
			return nil
		}
		if !isLockedErr(err) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

func isLockedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}

func (s *Store) migrateOnce() error {
	// Run the check-and-apply inside one transaction so concurrent first-creates
	// (two processes opening a fresh database at once) cannot both read
	// user_version 0 and race the same CREATE TABLE. _txlock=immediate takes the
	// write lock up front, serialising migrators; the loser re-reads the version
	// after the winner commits and finds nothing left to apply.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for version < len(migrations) {
		if _, err := tx.Exec(migrations[version]); err != nil {
			return fmt.Errorf("apply migration %d: %w", version+1, err)
		}
		version++
		// user_version cannot be parameterised; the value is a trusted int.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			return fmt.Errorf("record schema version %d: %w", version, err)
		}
	}
	return tx.Commit()
}
