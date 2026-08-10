package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

func TestWorkContainerRegistrationSurvivesReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	at := time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC)
	set := ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "2026-08-02-foo"}
	mapRef := ref.WorkRef{Kind: ref.KindMap, ContainerID: "generalize-work"}

	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.RegisterWorkContainer(set, at); err != nil {
		t.Fatalf("RegisterWorkContainer(set): %v", err)
	}
	if err := s.RegisterWorkContainer(mapRef, at.Add(time.Minute)); err != nil {
		t.Fatalf("RegisterWorkContainer(map): %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got, ok, err := reopened.FindWorkContainer(mapRef)
	if err != nil || !ok {
		t.Fatalf("FindWorkContainer(map) = %+v, %v, %v", got, ok, err)
	}
	if got.Ref != mapRef || got.Archived || !got.RegisteredAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("FindWorkContainer(map) = %+v", got)
	}

	all, err := reopened.AllWorkContainers()
	if err != nil {
		t.Fatalf("AllWorkContainers: %v", err)
	}
	if len(all) != 2 || all[0].Ref != set || all[1].Ref != mapRef {
		t.Fatalf("AllWorkContainers = %+v, want registration order [task-set, map]", all)
	}

	maps, err := reopened.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		t.Fatalf("WorkContainersOfKind: %v", err)
	}
	if len(maps) != 1 || maps[0].Ref != mapRef {
		t.Fatalf("WorkContainersOfKind(map) = %+v", maps)
	}
	if routines, err := reopened.WorkContainersOfKind(ref.KindRoutine); err != nil || len(routines) != 0 {
		t.Fatalf("WorkContainersOfKind(routine) = %+v, %v, want empty", routines, err)
	}

	// A container nothing registered is an ordinary absent answer, not an error.
	if _, ok, err := reopened.FindWorkContainer(ref.WorkRef{Kind: ref.KindMap, ContainerID: "never-charted"}); err != nil || ok {
		t.Fatalf("FindWorkContainer(unknown) = %v, %v, want false, nil", ok, err)
	}
}

func TestReRegisterKeepsTimestampAndArchivedBit(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC)
	mapRef := ref.WorkRef{Kind: ref.KindMap, ContainerID: "generalize-work"}

	if err := s.RegisterWorkContainer(mapRef, at); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.ArchiveWorkContainer(mapRef); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := s.RegisterWorkContainer(mapRef, at.Add(48*time.Hour)); err != nil {
		t.Fatalf("re-register: %v", err)
	}

	got, ok, err := s.FindWorkContainer(mapRef)
	if err != nil || !ok {
		t.Fatalf("FindWorkContainer = %+v, %v, %v", got, ok, err)
	}
	if !got.RegisteredAt.Equal(at) {
		t.Fatalf("RegisteredAt = %v, want the original %v", got.RegisteredAt, at)
	}
	if !got.Archived {
		t.Fatal("re-registering cleared the archived bit")
	}
	if all, err := s.AllWorkContainers(); err != nil || len(all) != 1 {
		t.Fatalf("AllWorkContainers = %+v, %v, want one row", all, err)
	}
}

func TestArchiveAndUnarchiveByKindAndID(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mapRef := ref.WorkRef{Kind: ref.KindMap, ContainerID: "generalize-work"}
	if err := s.RegisterWorkContainer(mapRef, time.Now()); err != nil {
		t.Fatalf("register: %v", err)
	}

	archivedNow := func() bool {
		t.Helper()
		got, ok, err := s.FindWorkContainer(mapRef)
		if err != nil || !ok {
			t.Fatalf("FindWorkContainer = %+v, %v, %v", got, ok, err)
		}
		return got.Archived
	}

	if err := s.ArchiveWorkContainer(mapRef); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archivedNow() {
		t.Fatal("archive did not set the archived bit")
	}
	if err := s.UnarchiveWorkContainer(mapRef); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if archivedNow() {
		t.Fatal("unarchive did not clear the archived bit")
	}

	// Archival never registers on the caller's behalf.
	unknown := ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "typo-set"}
	if err := s.ArchiveWorkContainer(unknown); !errors.Is(err, ErrWorkContainerUnregistered) {
		t.Fatalf("archive unregistered: err = %v, want ErrWorkContainerUnregistered", err)
	}
	if all, err := s.AllWorkContainers(); err != nil || len(all) != 1 {
		t.Fatalf("AllWorkContainers = %+v, %v, want the archive of an unknown ref to have written nothing", all, err)
	}
}

func TestRegistryRefusesItemRefsAndUnknownKinds(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	bad := []ref.WorkRef{
		{Kind: ref.KindTaskSet, ContainerID: "2026-08-02-foo", ItemID: "03"},
		{Kind: ref.Kind("goal"), ContainerID: "ship-it"},
		{Kind: ref.KindMap},
	}
	for _, r := range bad {
		if err := s.RegisterWorkContainer(r, time.Now()); err == nil {
			t.Fatalf("RegisterWorkContainer(%+v) accepted a ref the registry cannot key", r)
		}
		if err := s.ArchiveWorkContainer(r); err == nil {
			t.Fatalf("ArchiveWorkContainer(%+v) accepted a ref the registry cannot key", r)
		}
		if _, _, err := s.FindWorkContainer(r); err == nil {
			t.Fatalf("FindWorkContainer(%+v) accepted a ref the registry cannot key", r)
		}
	}
	if _, err := s.WorkContainersOfKind(ref.Kind("goal")); err == nil {
		t.Fatal("WorkContainersOfKind accepted a kind outside the enum")
	}
}

// TestWorkContainersCachesNoDerivedStatus pins the registry's shape: membership,
// the one cross-kind bit, and a timestamp. A status column here would be a second
// source of truth against the file-derived status (ADR-0056), so the column set is
// asserted exactly rather than by absence of one name.
func TestWorkContainersCachesNoDerivedStatus(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rows, err := s.db.Query(`PRAGMA table_info(work_containers)`)
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
	want := "archived id kind mute_secret muted_until registered_at seq"
	if got := fmt.Sprint(cols); got != "["+want+"]" {
		t.Fatalf("work_containers columns = %v, want [%s]", cols, want)
	}
}

// TestMigration26AppliesToAPreExistingDatabase drives the fold every beta
// tester's pop.db takes: a database migrated only as far as #25, carrying real
// rows, gains the registry on the next open and loses nothing.
func TestMigration26AppliesToAPreExistingDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	writePreRegistryDatabase(t, path)

	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open a pre-#26 database: %v", err)
	}
	defer func() { _ = s.Close() }()

	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("user_version = %d, want %d", version, len(migrations))
	}

	// The new table is live...
	mapRef := ref.WorkRef{Kind: ref.KindMap, ContainerID: "generalize-work"}
	if err := s.RegisterWorkContainer(mapRef, time.Now()); err != nil {
		t.Fatalf("RegisterWorkContainer after fold: %v", err)
	}

	// ...and the tables that were already there are untouched.
	sets, err := s.AllSets()
	if err != nil {
		t.Fatalf("AllSets: %v", err)
	}
	regs := sets["/repo/tasks"]
	if len(regs) != 1 || regs[0].SetID != "2026-08-02-foo" || regs[0].Priority != 7 || !regs[0].AutoDrain {
		t.Fatalf("sets rows = %+v, want the pre-migration registration intact", regs)
	}
	var setID, verdict string
	if err := s.db.QueryRow(`SELECT set_id FROM drains`).Scan(&setID); err != nil {
		t.Fatalf("read pre-migration drain: %v", err)
	}
	if setID != "2026-08-02-foo" {
		t.Fatalf("drains.set_id = %q, want the pre-migration row", setID)
	}
	if err := s.db.QueryRow(`SELECT verdict FROM verify_verdicts`).Scan(&verdict); err != nil {
		t.Fatalf("read pre-migration verdict: %v", err)
	}
	if verdict != "PASS" {
		t.Fatalf("verify_verdicts.verdict = %q, want the pre-migration row", verdict)
	}
}

// writePreRegistryDatabase creates a pop.db carrying every migration up to #25
// and a row in three of its tables — the state an installed pop left behind
// before the registry existed.
func writePreRegistryDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	const preRegistry = 25
	if len(migrations) <= preRegistry {
		t.Fatalf("migrations has %d entries, want the registry appended past %d", len(migrations), preRegistry)
	}
	for i, m := range migrations[:preRegistry] {
		if _, err := db.Exec(m); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", preRegistry)); err != nil {
		t.Fatalf("record schema version: %v", err)
	}
	seed := []string{
		`INSERT INTO sets (def_path, set_id, priority, archived, auto_drain)
		 VALUES ('/repo/tasks', '2026-08-02-foo', 7, 0, 1)`,
		`INSERT INTO drains (repo, set_id, runtime_path, pid, started_at, state)
		 VALUES ('/repo/.git', '2026-08-02-foo', '/repo', 4242, '2026-08-02T10:00:00Z', 'done')`,
		`INSERT INTO verify_verdicts (repo, set_id, work_sha, verdict)
		 VALUES ('/repo/.git', '2026-08-02-foo', 'deadbeef', 'PASS')`,
	}
	for _, stmt := range seed {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed pre-migration row: %v", err)
		}
	}
}

// TestMuteRefusedAgainstARoutineReference is the durable half of ADR-0200
// decision 7. The Routine kind not offering the verb keeps the dashboard honest;
// this keeps every other writer honest, so no future CLI or test can leave a
// muted row that no verb is able to clear. It rides on the store rather than on
// a Kind member because eligibility here is a property of the ref, and the ref
// is all the SQL boundary has.
func TestMuteRefusedAgainstARoutineReference(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	at := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	until := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	routine := ref.WorkRef{Kind: ref.KindRoutine, ContainerID: "nightly"}
	if err := s.RegisterWorkContainer(routine, at); err != nil {
		t.Fatalf("RegisterWorkContainer(routine): %v", err)
	}
	err := s.MuteWorkContainer(routine, until, false)
	if !errors.Is(err, ErrWorkContainerNotMutable) {
		t.Fatalf("muting a routine = %v, want ErrWorkContainerNotMutable", err)
	}
	got, _, err := s.FindWorkContainer(routine)
	if err != nil {
		t.Fatalf("FindWorkContainer: %v", err)
	}
	if !got.MutedUntil.IsZero() {
		t.Fatalf("refused mute still wrote %s", got.MutedUntil)
	}

	// Both mutable kinds go through, and unmute clears the pair rather than only
	// the instant — a cleared mute that kept its secret bit would render the next
	// mute as `[?]`.
	for _, r := range []ref.WorkRef{
		{Kind: ref.KindTaskSet, ContainerID: "2026-08-02-foo"},
		{Kind: ref.KindMap, ContainerID: "generalize-work"},
	} {
		if err := s.RegisterWorkContainer(r, at); err != nil {
			t.Fatalf("RegisterWorkContainer(%s): %v", r, err)
		}
		if err := s.MuteWorkContainer(r, until, true); err != nil {
			t.Fatalf("MuteWorkContainer(%s): %v", r, err)
		}
		got, _, err := s.FindWorkContainer(r)
		if err != nil {
			t.Fatalf("FindWorkContainer(%s): %v", r, err)
		}
		if !got.MutedUntil.Equal(until) || !got.MuteSecret {
			t.Fatalf("%s reads back %s secret=%v, want %s secret=true", r, got.MutedUntil, got.MuteSecret, until)
		}
		if err := s.UnmuteWorkContainer(r); err != nil {
			t.Fatalf("UnmuteWorkContainer(%s): %v", r, err)
		}
		got, _, err = s.FindWorkContainer(r)
		if err != nil {
			t.Fatalf("FindWorkContainer(%s) after unmute: %v", r, err)
		}
		if !got.MutedUntil.IsZero() || got.MuteSecret {
			t.Fatalf("%s still muted after unmute: %s secret=%v", r, got.MutedUntil, got.MuteSecret)
		}
	}

	unregistered := ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "never-registered"}
	if err := s.MuteWorkContainer(unregistered, until, false); !errors.Is(err, ErrWorkContainerUnregistered) {
		t.Fatalf("muting an unregistered set = %v, want ErrWorkContainerUnregistered", err)
	}
}
