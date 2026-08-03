package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/glebglazov/pop/work/ref"
)

// preCutMigrations is the migration count of the last binary that read the `sets`
// table — the version a rollback lands on. Tests build databases at it to drive
// the fold, and simulate that binary's bounded migrate loop against a database the
// new one has already migrated.
const preCutMigrations = 27

// writeDatabaseAtMigration creates a pop.db carrying the first n migrations and
// the given seed rows, at PRAGMA user_version = n: the state an installed pop
// leaves behind between releases.
func writeDatabaseAtMigration(t *testing.T, path string, n int, seed ...string) {
	t.Helper()
	if len(migrations) <= n {
		t.Fatalf("migrations has %d entries, want more than %d appended past it", len(migrations), n)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	for i, m := range migrations[:n] {
		if _, err := db.Exec(m); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
		t.Fatalf("record schema version: %v", err)
	}
	for _, stmt := range seed {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed pre-migration row: %v", err)
		}
	}
}

// legacySetRows reads the tombstoned `sets` table the way the pre-cut binary
// did, so a test can assert the tombstone is neither read from nor written to
// after the fold.
func legacySetRows(t *testing.T, db *sql.DB) []SetReg {
	t.Helper()
	rows, err := db.Query(
		`SELECT def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name
		   FROM sets ORDER BY seq`)
	if err != nil {
		t.Fatalf("read the tombstoned sets table: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SetReg
	for rows.Next() {
		var r SetReg
		var archived, autoDrain, managed int
		if err := rows.Scan(&r.DefPath, &r.SetID, &r.Priority, &archived, &autoDrain, &managed, &r.WorktreeName); err != nil {
			t.Fatalf("scan sets row: %v", err)
		}
		r.Archived, r.AutoDrain, r.WorktreeManaged = archived != 0, autoDrain != 0, managed != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("sets rows: %v", err)
	}
	return out
}

// TestMigration28CopiesEverySetOntoTheRegistry drives the fold a live pop.db
// takes: every `sets` row becomes a registry row plus a task-set-side row,
// preserving every bit and registration order across repositories, and the
// registration reads back through the ordinary accessor.
func TestMigration28CopiesEverySetOntoTheRegistry(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	writeDatabaseAtMigration(t, path, preCutMigrations,
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name)
		 VALUES ('/alpha/tasks', '2026-08-01-first', 7, 0, 1, 1, '')`,
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name)
		 VALUES ('/alpha/tasks', '2026-08-02-second', 0, 1, 0, 0, 'adopted-wt')`,
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name)
		 VALUES ('/beta/tasks', '2026-08-03-third', 3, 0, 0, 0, '')`)

	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open a pre-cut database: %v", err)
	}
	defer func() { _ = s.Close() }()

	all, err := s.AllSets()
	if err != nil {
		t.Fatalf("AllSets: %v", err)
	}
	wantAlpha := []SetReg{
		{DefPath: "/alpha/tasks", SetID: "2026-08-01-first", Priority: 7, AutoDrain: true, WorktreeManaged: true},
		{DefPath: "/alpha/tasks", SetID: "2026-08-02-second", Archived: true, WorktreeName: "adopted-wt"},
	}
	if got := all["/alpha/tasks"]; !reflect.DeepEqual(got, wantAlpha) {
		t.Fatalf("folded /alpha registrations = %#v, want %#v", got, wantAlpha)
	}
	wantBeta := []SetReg{{DefPath: "/beta/tasks", SetID: "2026-08-03-third", Priority: 3}}
	if got := all["/beta/tasks"]; !reflect.DeepEqual(got, wantBeta) {
		t.Fatalf("folded /beta registrations = %#v, want %#v", got, wantBeta)
	}

	// The archived bit landed on the cross-kind registry, in registration order,
	// with a registered_at the fold could actually name.
	rows, err := s.WorkContainersOfKind(ref.KindTaskSet)
	if err != nil {
		t.Fatalf("WorkContainersOfKind: %v", err)
	}
	var ids []string
	for _, row := range rows {
		ids = append(ids, row.Ref.ContainerID)
		if row.RegisteredAt.IsZero() {
			t.Fatalf("registry row %s has no registered_at", row.Ref)
		}
	}
	wantIDs := []string{"2026-08-01-first", "2026-08-02-second", "2026-08-03-third"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("registry ids = %v, want %v in registration order", ids, wantIDs)
	}
	if rows[0].Archived || !rows[1].Archived || rows[2].Archived {
		t.Fatalf("archived bits = %v/%v/%v, want false/true/false", rows[0].Archived, rows[1].Archived, rows[2].Archived)
	}
}

// TestTaskSetSideTableHoldsColumnsNotJSON pins the shape of the kind-local table:
// one column per registration fact, keyed by the registry row. A JSON blob would
// turn the daemon's auto_drain filter into a scan plus a parse.
func TestTaskSetSideTableHoldsColumnsNotJSON(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "pop.db"), allAlive(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	rows, err := s.db.Query(`PRAGMA table_info(task_set_registrations)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	sort.Strings(cols)
	want := "auto_drain container_seq def_path priority worktree_managed worktree_name"
	if got := fmt.Sprint(cols); got != "["+want+"]" {
		t.Fatalf("task_set_registrations columns = %v, want [%s]", cols, want)
	}
}

// TestRegistrationWritesNeverTouchTheTombstone is the no-dual-write property: a
// registration written, updated and unregistered after the fold leaves the `sets`
// table exactly as the fold found it. Two sources of truth that drift silently is
// the failure the registry exists to end.
func TestRegistrationWritesNeverTouchTheTombstone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	writeDatabaseAtMigration(t, path, preCutMigrations,
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name)
		 VALUES ('/alpha/tasks', 'folded', 7, 0, 1, 0, '')`)
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	before := legacySetRows(t, s.db)

	if err := s.PutSet(SetReg{DefPath: "/alpha/tasks", SetID: "fresh", Priority: 2, AutoDrain: true}); err != nil {
		t.Fatalf("PutSet: %v", err)
	}
	if err := s.PutSet(SetReg{DefPath: "/alpha/tasks", SetID: "folded", Priority: 9, Archived: true}); err != nil {
		t.Fatalf("PutSet update: %v", err)
	}
	if err := s.ReplaceAllSets(map[string][]SetReg{
		"/alpha/tasks": {{SetID: "fresh", Priority: 2, AutoDrain: true}},
	}); err != nil {
		t.Fatalf("ReplaceAllSets: %v", err)
	}
	if got := legacySetRows(t, s.db); !reflect.DeepEqual(got, before) {
		t.Fatalf("sets table after three writes = %#v, want it untouched at %#v", got, before)
	}
}

// TestAPreCutBinaryStillOpensAMigratedDatabase is the tombstone's whole point:
// after the new binary migrates, the previous release's migrate loop — bounded by
// its own migration count — applies nothing and still reads its own rows, so
// rolling a bad release back stays survivable.
func TestAPreCutBinaryStillOpensAMigratedDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	writeDatabaseAtMigration(t, path, preCutMigrations,
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain, worktree_managed, worktree_name)
		 VALUES ('/alpha/tasks', 'pre-cut', 4, 1, 1, 0, '')`)
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open with the new binary: %v", err)
	}
	// Registration made by the new binary after the cut: invisible to the old one,
	// which is the accepted cost of the frozen snapshot.
	if err := s.PutSet(SetReg{DefPath: "/alpha/tasks", SetID: "post-cut"}); err != nil {
		t.Fatalf("PutSet: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("reopen as the pre-cut binary: %v", err)
	}
	defer func() { _ = db.Close() }()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	// The pre-cut migrate loop is `for version < len(migrations)` over its own
	// shorter list, so a user_version past that count leaves it nothing to apply
	// and it never sees a statement it cannot run.
	if version <= preCutMigrations {
		t.Fatalf("user_version = %d, want the new binary to have moved past the pre-cut %d", version, preCutMigrations)
	}
	want := []SetReg{{DefPath: "/alpha/tasks", SetID: "pre-cut", Priority: 4, Archived: true, AutoDrain: true}}
	if got := legacySetRows(t, db); !reflect.DeepEqual(got, want) {
		t.Fatalf("pre-cut binary reads %#v, want its own frozen snapshot %#v", got, want)
	}
}

// TestReplaceAllSetsReconcilesWithoutReordering covers the write path the state
// view drives: an update keeps the set's place and its registration instant, a
// newly-registered set lands last, and one dropped from the view is unregistered
// on both tables.
func TestReplaceAllSetsReconcilesWithoutReordering(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "pop.db"), allAlive(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	first := map[string][]SetReg{
		"/zeta/tasks":  {{SetID: "z-one"}},
		"/alpha/tasks": {{SetID: "a-one"}, {SetID: "a-two"}},
	}
	if err := s.ReplaceAllSets(first); err != nil {
		t.Fatalf("ReplaceAllSets: %v", err)
	}
	registeredAt := map[string]string{}
	for _, row := range mustContainers(t, s) {
		registeredAt[row.Ref.ContainerID] = row.RegisteredAt.Format(timeLayout)
	}

	// a-one gains a priority, a-two is unregistered, a-three appears.
	second := map[string][]SetReg{
		"/zeta/tasks":  {{SetID: "z-one"}},
		"/alpha/tasks": {{SetID: "a-one", Priority: 5, AutoDrain: true}, {SetID: "a-three"}},
	}
	if err := s.ReplaceAllSets(second); err != nil {
		t.Fatalf("ReplaceAllSets again: %v", err)
	}
	var ids []string
	for _, row := range mustContainers(t, s) {
		ids = append(ids, row.Ref.ContainerID)
		if was, ok := registeredAt[row.Ref.ContainerID]; ok && was != row.RegisteredAt.Format(timeLayout) {
			t.Fatalf("%s registered_at moved from %s to %s on update", row.Ref, was, row.RegisteredAt.Format(timeLayout))
		}
	}
	if !reflect.DeepEqual(ids, []string{"a-one", "z-one", "a-three"}) {
		t.Fatalf("registry ids = %v, want the kept rows in place and a-three appended", ids)
	}

	all, err := s.AllSets()
	if err != nil {
		t.Fatalf("AllSets: %v", err)
	}
	want := []SetReg{
		{DefPath: "/alpha/tasks", SetID: "a-one", Priority: 5, AutoDrain: true},
		{DefPath: "/alpha/tasks", SetID: "a-three"},
	}
	if got := all["/alpha/tasks"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("/alpha registrations = %#v, want %#v", got, want)
	}
	var sideRows int
	if err := s.db.QueryRow(`SELECT count(*) FROM task_set_registrations`).Scan(&sideRows); err != nil {
		t.Fatalf("count task_set_registrations: %v", err)
	}
	if sideRows != 3 {
		t.Fatalf("task_set_registrations rows = %d, want 3 (the unregistered set's row is gone too)", sideRows)
	}
}

func mustContainers(t *testing.T, s *Store) []WorkContainer {
	t.Helper()
	rows, err := s.WorkContainersOfKind(ref.KindTaskSet)
	if err != nil {
		t.Fatalf("WorkContainersOfKind: %v", err)
	}
	return rows
}
