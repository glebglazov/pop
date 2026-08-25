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
	"time"

	_ "modernc.org/sqlite"
)

// Liveness reports whether the process identified by pid, paired with the opaque
// start-time token procStart, is still running. It is the store's crash-healing
// policy: the reconcile sweeps and the mutual-exclusion / quiescence gates
// consult it to tell a live owner from a dead one, pairing pid with procStart so
// a reused PID is not mistaken for the original owner. It is fixed at Open
// (ADR-0118) rather than threaded through each call, so a missing predicate fails
// at construction instead of silently disabling crash healing.
type Liveness func(pid int, procStart string) bool

// Store is an open handle to the global execution-state database.
type Store struct {
	db    *sql.DB
	alive Liveness
}

// Open opens (creating if absent) the SQLite database at path in WAL mode and
// applies any outstanding schema migrations. The containing directory must
// already exist. alive is the required liveness policy every crash-healing path
// consults (ADR-0118); Open refuses a nil predicate rather than run with crash
// detection silently disabled.
func Open(path string, alive Liveness) (*Store, error) {
	if alive == nil {
		return nil, errors.New("store.Open: a liveness policy is required")
	}
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
	s := &Store{db: db, alive: alive}
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
	// only this machine-local registration moves. Tombstoned by #28, which copies
	// these rows onto the cross-kind registry: read-dead and write-dead from there
	// on, kept only so a pre-cut binary still boots.
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
	// 14: verify_verdicts.scope — the count of AFK tasks the verdict certified
	// (ADR-0101). A PASS certifies a set as verified *as scoped*; recording the
	// scope lets a reader tell an incidental SHA move (scope unchanged, coast on
	// the immunizing PASS per ADR-0096) apart from a scope increase (a new AFK
	// task added by a direct manifest edit), which ends the episode so the
	// Verifier re-fires against the enlarged set. Legacy rows and verdicts written
	// before the scope was known default to 0, read as "unknown" (no growth check).
	`ALTER TABLE verify_verdicts ADD COLUMN scope INTEGER NOT NULL DEFAULT 0;`,
	// 15: verify_verdicts human-authored provenance + note (ADR-0103). An
	// Accepted verdict is a human overriding a non-PASS Verifier judgment: an
	// ordinary PASS row flagged human_authored=1 and carrying the human's note.
	// Because it is a plain PASS it inherits PASS idempotency and the scope-growth
	// invalidation (ADR-0101), so status derivation needs no change. The note
	// feeds forward as context into later Verifier prompts so the known non-issue
	// is not re-flagged, without ever suppressing a fresh judgment. Existing rows
	// default to human_authored=0 / note='' — the agent-authored verdict shape.
	`ALTER TABLE verify_verdicts ADD COLUMN human_authored INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE verify_verdicts ADD COLUMN note TEXT NOT NULL DEFAULT '';`,
	// 16: drain_panes — the tmux pane the queue supervisor associates with a Task
	// set drain, surfaced in the dashboard preview. ADR-0055 completes the daemon
	// state consolidation by moving this last live state.json payload into the
	// store; it is transient preview data (no migration of the retired file — an
	// empty start is fine), keyed by the caller-built scoped key (repository
	// identity plus set id). The latest write for a set wins.
	`CREATE TABLE drain_panes (
		scoped_key   TEXT PRIMARY KEY,
		project      TEXT NOT NULL DEFAULT '',
		runtime_path TEXT NOT NULL DEFAULT '',
		set_id       TEXT NOT NULL,
		pane_id      TEXT NOT NULL,
		recorded_at  TEXT NOT NULL DEFAULT '',
		source       TEXT NOT NULL DEFAULT ''
	);`,
	// 17: checkout_gate_holds owner identity — the PID and process start token of
	// the process that registered the hold, mirroring the drains table so the same
	// opportunistic reconcile pass can sweep a hold whose owner died (a crash while
	// a human sat at a Failed/HITL gate would otherwise leave an orphan row that
	// blocks Recovery-turn acquisition on that checkout forever). proc_start is
	// nullable and pid defaults to 0: a legacy row (written before these columns
	// existed) carries no owner and reads dead, so the first reconcile sweeps it —
	// the correct outcome, since its registering process is long gone.
	`ALTER TABLE checkout_gate_holds ADD COLUMN pid INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE checkout_gate_holds ADD COLUMN proc_start TEXT;`,
	// 18: spawn_intents — a durable pending-spawn marker closing the supervisor
	// double-spawn window (item 1 of the recovery-quiescence hardening). Between
	// the supervisor sending `pop tasks implement` into a pane and that drain
	// reaching BeginDrain the store has no running row, so a fast re-poll would
	// re-select the same set and send a second implement (bouncing off
	// ErrDrainInProgress — safe but noisy). The dispatcher now records an intent
	// before the send and treats a set with a live intent as busy, so correctness
	// no longer depends on in-memory view seeding surviving across polls. The row
	// carries the recording process's owner identity (PID + start token, mirroring
	// drains) so it can be reconciled if that process dies, and created_at so it
	// expires: BeginDrain deletes it on success, and the opportunistic reconcile
	// pass sweeps any that never reached BeginDrain. Keyed by (repo, set_id).
	`CREATE TABLE spawn_intents (
		repo         TEXT NOT NULL,
		set_id       TEXT NOT NULL,
		runtime_path TEXT NOT NULL DEFAULT '',
		pid          INTEGER NOT NULL DEFAULT 0,
		proc_start   TEXT,
		created_at   TEXT NOT NULL,
		PRIMARY KEY (repo, set_id)
	);`,
	// 19: verify_forwarded_notes — a human Accept note (ADR-0103) captured across a
	// Verify verdict invalidation, so it survives even when the human-authored row
	// that carried it is deleted (a scope-growth invalidation, or any remediation
	// spawn — auto or human origin, ADR-0105). Previously only the scope-growth
	// path preserved the note, by threading it as a local variable straight into
	// the immediately-following re-verify; a remediation spawn's re-verify happens
	// much later (after the Remediation task drains, often a different process),
	// so the note needs a durable home to survive that gap. One row per (repo,
	// set_id): CaptureNoteThenInvalidate upserts it right before deleting the
	// verdicts, and TakeForwardedNote reads-then-deletes it (one-shot) so it
	// forward-feeds into exactly the next Verifier run before disappearing, the
	// same way the live human-authored row would have.
	`CREATE TABLE verify_forwarded_notes (
		repo   TEXT NOT NULL,
		set_id TEXT NOT NULL,
		note   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (repo, set_id)
	);`,
	// 20: routine_runs — one row per Routine run lifecycle. Per-routine exclusivity
	// is enforced on start: a live running row for the same routine_id blocks
	// another fire until the owning process finishes or is reconciled dead.
	`CREATE TABLE routine_runs (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		routine_id   TEXT    NOT NULL,
		fired_at     TEXT    NOT NULL,
		outcome      TEXT    NOT NULL,
		skip_reason  TEXT    NOT NULL DEFAULT '',
		fail_reason  TEXT    NOT NULL DEFAULT '',
		report_path  TEXT    NOT NULL DEFAULT '',
		pid          INTEGER NOT NULL DEFAULT 0,
		proc_start   TEXT,
		finished_at  TEXT
	);
	CREATE INDEX idx_routine_runs_routine ON routine_runs(routine_id);`,
	// 21: fingerprint — canonical hash of a Routine's explicitly-set run-affecting
	// inputs in effect when the run fired (ADR-0128). Pre-migration rows default to
	// the empty string, which the daemon reads as "no fingerprint recorded" and
	// never treats as a mismatch.
	`ALTER TABLE routine_runs ADD COLUMN fingerprint TEXT NOT NULL DEFAULT '';`,
	// 22: recovery_waiters owner identity — the PID and process start token of the
	// process that registered the waiter, mirroring the drains and gate-hold tables
	// so the same opportunistic reconcile pass can sweep a waiter whose owner died
	// (ADR-0135). A kill -9 or terminal close would otherwise leak the waiter row
	// forever, permanently deferring that set in the Queue and blocking the
	// checkout claim union. proc_start is nullable and pid defaults to 0: a legacy
	// row (written before these columns existed) carries no owner and reads dead, so
	// the first reconcile sweeps it — the correct outcome, since its registering
	// process is long gone.
	`ALTER TABLE recovery_waiters ADD COLUMN pid INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE recovery_waiters ADD COLUMN proc_start TEXT;`,
	// 23: checkout_gate_holds claim flag — whether the hold claims the checkout for
	// admission (ADR-0135), distinct from mere quiescence occupancy. Only a Failed
	// gate parked over a dirty tree (uncommitted work, snapshotted at park time and
	// never re-evaluated live) claims; the HITL gate, the verify-fail gate, and a
	// clean-tree Failed gate register non-claiming holds. A live claim-bearing hold
	// blocks another set's StartDrain via the checkout claim union. Legacy rows
	// default to 0 (non-claiming) — the historical occupancy-only behaviour.
	`ALTER TABLE checkout_gate_holds ADD COLUMN claim INTEGER NOT NULL DEFAULT 0;`,
	// 24: verify_verdicts.summary — the Verifier's optional one-line SUMMARY for
	// Remediation task titles. Persisted with the verdict so a cache-hit FIXABLE
	// spawn (re-drain at the same work SHA) still names what needs fixing.
	`ALTER TABLE verify_verdicts ADD COLUMN summary TEXT NOT NULL DEFAULT '';`,
	// 25: agent_model_cooldowns — the machine-global Effort model skip (ADR-0168):
	// a `Scope=Model` proceed verdict names one model of one preset as spent
	// rather than the whole preset, so it is a table of its own rather than a
	// column on agent_cooldowns — a spent model must never render as a paused
	// preset (queue's blockedItemsFromAgentCooldowns reads only agent_cooldowns
	// and so never sees these rows). until is NULL for a Permanent skip (never
	// expires); otherwise it is the adapter's parsed reset instant or a one hour
	// default, via the same policy preset cooldowns use. Keyed by (preset,
	// model); the latest write wins. A prefactor — nothing writes here until the
	// task that consumes the Effort model skip lands.
	`CREATE TABLE agent_model_cooldowns (
		preset TEXT NOT NULL,
		model  TEXT NOT NULL,
		until  TEXT,
		PRIMARY KEY (preset, model)
	);`,
	// 26: work_containers — the one cross-kind Work registry, keyed (kind, id):
	// membership plus machine-local runtime for every Work kind, so a Map
	// registers the same way a Task set does. A `kind` column on `sets` was
	// rejected — a table named for one kind cannot be the cross-kind registry
	// without lying. It carries `archived`, the only genuinely cross-kind
	// registration bit (Maps archive through it too), and never a derived status:
	// derivation is cheap and a status cache is a second source of truth that
	// drifts (ADR-0006/0056). Kind-local registration (priority, auto_drain, the
	// worktree directive) stays on its kind's own table. The autoincrement seq
	// preserves registration order, which listings render by. This migration only
	// creates the table, so `pop map` can register against it first; #28 copies the
	// `sets` rows in as kind='task-set'.
	`CREATE TABLE work_containers (
		seq           INTEGER PRIMARY KEY AUTOINCREMENT,
		kind          TEXT    NOT NULL,
		id            TEXT    NOT NULL,
		archived      INTEGER NOT NULL DEFAULT 0,
		registered_at TEXT    NOT NULL DEFAULT '',
		UNIQUE(kind, id)
	);
	CREATE INDEX idx_work_containers_kind ON work_containers(kind);`,
	// 27: work_item_claims — who is working one item of one Work container right
	// now. A Map's Decision ticket claims here rather than in its markdown or its
	// manifest: a claim belongs to a live grilling pane, and a file-borne claim
	// outlives the window that took it with nothing able to release it. Keyed by
	// the whole item ref so the table is cross-kind from the start — a Task set's
	// item claims the same way. owner is a tmux pane id when the claimer runs
	// inside tmux and a pid otherwise, both opaque here; claimed_at is what a
	// reclaim reports, not a deadline — whether the owner still lives is a
	// question the caller answers (ADR-0193).
	`CREATE TABLE work_item_claims (
		kind         TEXT NOT NULL,
		container_id TEXT NOT NULL,
		item_id      TEXT NOT NULL,
		owner        TEXT NOT NULL,
		claimed_at   TEXT NOT NULL,
		PRIMARY KEY (kind, container_id, item_id)
	);`,
	// 28: task_set_registrations, and the `sets` copy that moves Task sets onto
	// the cross-kind registry. Every `sets` row becomes a work_containers row
	// keyed ('task-set', set_id) carrying the cross-kind `archived` bit, and its
	// kind-local registration — the repository's def_path, priority, auto_drain
	// and the ADR-0059 worktree directive — moves here, keyed by the registry
	// row's seq. Columns rather than a per-kind JSON blob on the registry:
	// auto_drain is a daemon candidate filter, and JSON in SQLite turns a column
	// read into a table scan plus a parse.
	//
	// `sets` is tombstoned, not dropped: read-dead and write-dead from here on and
	// never dual-written, but left in place, which buys one real property. A
	// pre-cut binary's migrate loop is bounded by its own migration count, so this
	// user_version reads as a no-op and that binary still boots and still reads
	// its own rows against a frozen snapshot — rolling back a bad release stays
	// survivable. CLEANUP.md carries the drop under the beta-tester sign-off gate.
	//
	// The copy is OR IGNORE and ordered by the old seq, so the registry's own seq
	// inherits registration order and, since the registry keys ('task-set', id)
	// with no def_path, one set id registered under two repositories collapses to
	// its earliest registration — the same machine-wide uniqueness of a set id
	// that recovery_waiters has assumed since ADR-0100. registered_at is stamped
	// at the fold: the source table never recorded one, and the fold instant is
	// the earliest moment this machine can actually name.
	`CREATE TABLE task_set_registrations (
		container_seq    INTEGER PRIMARY KEY REFERENCES work_containers(seq) ON DELETE CASCADE,
		def_path         TEXT    NOT NULL,
		priority         INTEGER NOT NULL DEFAULT 0,
		auto_drain       INTEGER NOT NULL DEFAULT 0,
		worktree_managed INTEGER NOT NULL DEFAULT 0,
		worktree_name    TEXT    NOT NULL DEFAULT ''
	);
	CREATE INDEX idx_task_set_registrations_def ON task_set_registrations(def_path);
	INSERT OR IGNORE INTO work_containers (kind, id, archived, registered_at)
		SELECT 'task-set', set_id, archived, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		  FROM sets ORDER BY seq;
	INSERT OR IGNORE INTO task_set_registrations
		(container_seq, def_path, priority, auto_drain, worktree_managed, worktree_name)
		SELECT c.seq, s.def_path, s.priority, s.auto_drain, s.worktree_managed, s.worktree_name
		  FROM sets s JOIN work_containers c ON c.kind = 'task-set' AND c.id = s.set_id
		 ORDER BY s.seq;`,
	// 29: history_entries + history_folds — History moves off the standalone
	// history.json into the store (ADR-0188). One row per path holding the last
	// instant a human landed there, which is what the project picker, the worktree
	// picker and the dashboard's session-last-visit key all order by. Recording is
	// a single-row upsert in a transaction, so two recorders can no longer lose
	// each other's writes the way the lock-free whole-file rewrite could.
	//
	// history_folds is the once-only gate on the legacy file's fold. Unlike the
	// bindings fold this one must not delete its source — history.json is the only
	// rollback for a file with no other copy — so "already folded" cannot be read
	// off the file's absence and needs a marker of its own. Without it, a path the
	// human reset in the picker would be resurrected from the surviving file on
	// the very next read.
	`CREATE TABLE history_entries (
		path        TEXT PRIMARY KEY,
		last_access TEXT NOT NULL
	);
	CREATE TABLE history_folds (
		source    TEXT PRIMARY KEY,
		folded_at TEXT NOT NULL
	);`,
	// 30: agent_model_cooldowns.stated_until — what the provider's refusal said,
	// kept beside the expiry pop actually enforces. An Effort model skip is capped
	// at 24 hours however distant the stated reset is (ADR-0168), because a
	// spent-allowance probe costs seconds and a month-long park would sit blind
	// through a top-up or a plan change. The two instants therefore disagree by
	// design, and a read surface that showed only the capped one would misreport
	// the refusal; NULL when the message named no reset.
	`ALTER TABLE agent_model_cooldowns ADD COLUMN stated_until TEXT;`,
	// 31: work_containers.muted_until + mute_secret — a human's Mute, beside the
	// archived bit for the same reason (ADR-0200 decision 1): both are
	// registration-level, both mean the same thing for every kind that carries
	// them, and neither travels with the repository. There is no sweeper — a mute
	// ends by a read comparing muted_until against now, so nothing ever writes
	// when one expires and there is no state to reconcile.
	//
	// mute_secret marks the random default window, whose instant no read surface
	// discloses (decision 6). It is a column rather than a convention over the
	// instant because "the human picked this date off a list" is a fact about how
	// the mute was authored, and no arithmetic on the instant can recover it.
	//
	// Only a mutable kind may hold a value here — ref.Kind.Mutable is the rule and
	// MuteWorkContainer enforces it. SQLite has no partial CHECK worth the
	// migration here, so the guard is the accessor's.
	`ALTER TABLE work_containers ADD COLUMN muted_until TEXT NOT NULL DEFAULT '';
	 ALTER TABLE work_containers ADD COLUMN mute_secret INTEGER NOT NULL DEFAULT 0;`,
	// 32: verify_verdicts.commit_subject — the Planned commit subject the Verifier
	// rendered for the fix it is asking for (ADR-0207), under the set's commit
	// convention. It rides with the verdict for the same reason the summary does: a
	// FIXABLE spawn that reads a cached verdict (a re-drain at an unchanged work
	// SHA) must write the same subject the Verifier authored, and re-rendering it
	// would need a second Verifier run. Empty when the set carries no convention or
	// the Verifier rendered nothing usable — the commit then falls back to pop's
	// default format.
	`ALTER TABLE verify_verdicts ADD COLUMN commit_subject TEXT NOT NULL DEFAULT '';`,
	// 33: review_episodes — the Review episode for a Task set (ADR-0214): whether
	// automatic Code review is armed, keyed (repo, set_id) where repo is the
	// repository's git common dir, the same identity verify_verdicts uses. One row
	// per set, rewritten by every review: it records the done-AFK work composition
	// the review judged, and a drain re-arms only when the set's current
	// composition differs from it. Unlike a Verify verdict there is nothing to
	// invalidate and no verdict to cache — a review reaches none — so the row
	// carries only the fact that this composition has been reviewed, plus where
	// the resulting document went and which work SHA it was written against.
	`CREATE TABLE review_episodes (
		repo        TEXT NOT NULL,
		set_id      TEXT NOT NULL,
		work_sha    TEXT NOT NULL DEFAULT '',
		composition TEXT NOT NULL DEFAULT '',
		document    TEXT NOT NULL DEFAULT '',
		reviewed_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (repo, set_id)
	);`,
	// 34: drains.ending — the Agent fallback walk outcome behind a terminal, for
	// the walks whose exit reason cannot say it (ADR-0231). A drain that ran out
	// of agents finishes cleanly, and so does one that could not start a single
	// agent; both look exactly like a healthy drain in the `state` column, which
	// is what stopped the journal from ranking them. Empty on every ordinary
	// drain, including one where an agent stepped aside and the next finished the
	// work.
	`ALTER TABLE drains ADD COLUMN ending TEXT NOT NULL DEFAULT '';`,
	// 35: spent_retry_caps — one row per agent that burned its whole retry cap on
	// one piece of work without finishing it (ADR-0231). It is a table rather
	// than a second column beside `drains.ending` because the ending holds one
	// value per drain while the burn is per (agent, work): a single drain can
	// spend claude's cap on one task, codex's on the next, and still finish the
	// set, and the human who pays for those attempts has to see each of them.
	// The row is written when the cap runs out, not when the drain ends, so
	// whatever the drain went on to do — finish, park on a quota, run out of
	// agents — leaves the burn recorded.
	`CREATE TABLE spent_retry_caps (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		repo         TEXT    NOT NULL,
		set_id       TEXT    NOT NULL,
		runtime_path TEXT    NOT NULL DEFAULT '',
		phase        TEXT    NOT NULL DEFAULT '',
		task_id      TEXT    NOT NULL DEFAULT '',
		preset       TEXT    NOT NULL,
		attempts     INTEGER NOT NULL DEFAULT 0,
		reason       TEXT    NOT NULL DEFAULT '',
		spent_at     TEXT    NOT NULL
	);`,
	// 36: agent_cooldowns learns whether it read its instant or invented it
	// (ADR-0235). `exhausted_until` alone could not say, so four different
	// things wrote it identically — a provider's epoch, a provider's countdown,
	// a per-signal backoff pop invented, and a blind hour measured from pop's
	// disappointment. That last one compounded: an early retry earned a refusal
	// that wrote another full hour from the *later* moment, so an undershoot of
	// two minutes bought an overshoot of nearly one.
	//
	// stated_until is NULL exactly when pop guessed, mirroring the column
	// migration 30 gave agent_model_cooldowns. A guessed row's exhausted_until
	// is the ceiling of its Quota window class — the latest that window can
	// still run — dated at the *first* refusal and never re-derived from a later
	// one; class records which window, empty when the refusal named none.
	//
	// next_probe_at and probe_lease_until belong to the probe loop that ends a
	// guess by asking the agent rather than by waiting out the ceiling:
	// next_probe_at is when the exhausted preset is asked again, and
	// probe_lease_until is the short claim that keeps parallel checkouts from
	// each asking. Both are NULL on a stated row, which is never probed.
	`ALTER TABLE agent_cooldowns ADD COLUMN stated_until TEXT;
	 ALTER TABLE agent_cooldowns ADD COLUMN class TEXT NOT NULL DEFAULT '';
	 ALTER TABLE agent_cooldowns ADD COLUMN next_probe_at TEXT;
	 ALTER TABLE agent_cooldowns ADD COLUMN probe_lease_until TEXT;`,
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
