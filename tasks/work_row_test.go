package tasks

import (
	"reflect"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/work"
)

// The Work row derivation tests, relocated with the derivation itself when the
// Work seam took shape: the filter, the order and the STATUS cell all read this
// kind's statuses, so they are this kind's tests.

// TestMatchesPresetViaShowRowReplacement pins the Work view preset row filter
// (ADR-0197): the shipped active preset hides done-and-folded work and keeps
// every other status (including unfolded DONE); all reveals folded DONE too.
func TestMatchesPresetActiveAndAll(t *testing.T) {
	active, ok := config.ShippedWorkViewPreset("active")
	if !ok {
		t.Fatal("shipped active missing")
	}
	all, ok := config.ShippedWorkViewPreset("all")
	if !ok {
		t.Fatal("shipped all missing")
	}
	now := time.Now()
	for _, status := range []TaskSetStatus{
		StatusReady, StatusFailed, StatusBlocked, StatusDeferred,
		StatusMissing, StatusMalformed, StatusAwaitingApproval, StatusNeedsVerify,
	} {
		row := Row{Status: status}
		if !MatchesPreset(RowViewFacts(row), active, now) {
			t.Errorf("%s hidden by active, want shown", status)
		}
		if !MatchesPreset(RowViewFacts(row), all, now) {
			t.Errorf("%s hidden by all, want shown", status)
		}
	}
	folded := Row{Status: StatusDone, Bound: false}
	if MatchesPreset(RowViewFacts(folded), active, now) {
		t.Errorf("folded DONE shown by active, want hidden")
	}
	if !MatchesPreset(RowViewFacts(folded), all, now) {
		t.Errorf("folded DONE hidden by all, want shown")
	}
	adopted := Row{Status: StatusDone, Bound: true, Provisioned: false}
	if MatchesPreset(RowViewFacts(adopted), active, now) {
		t.Errorf("adopted DONE shown by active, want hidden (not unfolded)")
	}
	unfolded := Row{Status: StatusDone, Bound: true, Provisioned: true}
	if !MatchesPreset(RowViewFacts(unfolded), active, now) {
		t.Errorf("unfolded DONE hidden by active, want shown")
	}
}

// TestSortOrder pins the status scheme's project-then-id tiebreak, which is now
// what `sort = "status"` asks for rather than what an unset sort implies.
func TestSortOrder(t *testing.T) {
	rows := []work.Container{
		{Project: "zeta", ID: "2026-01-01-old"},
		{Project: "alpha", ID: "2026-01-01-old"},
		{Project: "alpha", ID: "2026-06-18-new"},
	}
	SortWorkRows(rows, config.PresetSortStatus)
	got := []string{rows[0].Project + "/" + rows[0].ID, rows[1].Project + "/" + rows[1].ID, rows[2].Project + "/" + rows[2].ID}
	want := []string{"alpha/2026-06-18-new", "alpha/2026-01-01-old", "zeta/2026-01-01-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestTieredSortOrder drives the full Queue surface order (ADR-0121) across a
// mixed fixture that exercises every membership tier and the status scheme,
// which `sort = "status"` is now the way to ask for (ADR-0210). Tier
// precedence is running → auto-drain → orphaned → the rest; the orphaned +
// auto-drain set lands in the auto-drain tier; within the rest tier the status
// scheme floats the IN PROGRESS band, then the READY band (both cross-project),
// then every remaining status per-project by the explicit status order; SetID
// descending is the tiebreak throughout.
func TestTieredSortOrder(t *testing.T) {
	rows := []work.Container{
		// Rest tier, rest band — alphabetically-early project with a needs-you status.
		{Project: "alpha", ID: "2026-02-01-blk", RawStatus: StatusBlocked},
		// Rest tier, READY band — floats above alpha's BLOCKED even though bravo sorts later.
		{Project: "bravo", ID: "2026-02-02-rdy", RawStatus: StatusReady},
		{Project: "alpha", ID: "2026-02-03-rdy", RawStatus: StatusReady},
		// Rest tier, IN PROGRESS band (started READY) — floats above the READY band.
		{Project: "bravo", Started: true, ID: "2026-02-04-inp", RawStatus: StatusReady},
		// Rest tier, rest band — DONE and AWAITING-APPROVAL, project-first then status order.
		{Project: "bravo", ID: "2026-02-05-done", RawStatus: StatusDone},
		{Project: "charlie", ID: "2026-02-06-aa", RawStatus: StatusAwaitingApproval},
		// Orphaned tier.
		{Project: "zoo", ID: "2026-04-01-orph", RawStatus: StatusReady, Orphaned: true},
		// Auto-drain tier — the orphaned+auto-drain set belongs here, not orphaned.
		{Project: "kilo", ID: "2026-05-01-ad", RawStatus: StatusReady, AutoDrain: true},
		{Project: "kilo", ID: "2026-05-02-ado", RawStatus: StatusReady, AutoDrain: true, Orphaned: true},
		// Running tier — highest precedence even over an auto-drain BLOCKED set.
		{Project: "delta", ID: "2026-06-01-run", RawStatus: StatusBlocked, AutoDrain: true, LiveDrain: true},
	}
	SortWorkRows(rows, config.PresetSortStatus)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.ID
	}
	want := []string{
		// Tier 1: running (floats above the whole status scheme, BLOCKED and all).
		"delta/2026-06-01-run",
		// Tier 2: auto-drain, SetID descending.
		"kilo/2026-05-02-ado",
		"kilo/2026-05-01-ad",
		// Tier 3: orphaned.
		"zoo/2026-04-01-orph",
		// Tier 4: the rest, status scheme.
		"bravo/2026-02-04-inp",  // IN PROGRESS band (started READY) floats first
		"alpha/2026-02-03-rdy",  // READY band, cross-project: alpha before bravo
		"bravo/2026-02-02-rdy",  // READY band
		"alpha/2026-02-01-blk",  // rest band, project-first: alpha BLOCKED
		"bravo/2026-02-05-done", // rest band: bravo DONE
		"charlie/2026-02-06-aa", // rest band: charlie AWAITING-APPROVAL
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestReadyBandInterleavesProjects proves the READY band reads cross-project
// (ADR-0121): every READY row floats above the rest band regardless of project,
// rather than each project's rows clustering together.
func TestReadyBandInterleavesProjects(t *testing.T) {
	rows := []work.Container{
		{Project: "alpha", ID: "2026-01-01-blk", RawStatus: StatusBlocked},
		{Project: "bravo", ID: "2026-01-02-rdy", RawStatus: StatusReady},
		{Project: "alpha", ID: "2026-01-03-rdy", RawStatus: StatusReady},
		{Project: "bravo", ID: "2026-01-04-blk", RawStatus: StatusBlocked},
	}
	SortWorkRows(rows, config.PresetSortStatus)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.ID
	}
	// READY band (cross-project, Project asc) first, then the rest band. If the
	// order were project-grouped it would read alpha/rdy, alpha/blk, bravo/rdy,
	// bravo/blk instead.
	want := []string{
		"alpha/2026-01-03-rdy",
		"bravo/2026-01-02-rdy",
		"alpha/2026-01-01-blk",
		"bravo/2026-01-04-blk",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestBandKeysOnDisplayedLabel proves a row's band is keyed on its displayed
// label, not its raw status (ADR-0121): a started READY set renders as IN
// PROGRESS and sorts in the IN PROGRESS band, floating above a plain READY set
// even though both carry raw status READY and the IN PROGRESS row's project sorts
// later alphabetically.
func TestBandKeysOnDisplayedLabel(t *testing.T) {
	rows := []work.Container{
		{Project: "alpha", ID: "2026-01-01-rdy", RawStatus: StatusReady},
		{Project: "zeta", Started: true, ID: "2026-01-02-inp", RawStatus: StatusReady},
	}
	SortWorkRows(rows, config.PresetSortStatus)
	got := []string{rows[0].Project + "/" + rows[0].ID, rows[1].Project + "/" + rows[1].ID}
	want := []string{"zeta/2026-01-02-inp", "alpha/2026-01-01-rdy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestWorkRowStatusLabelRefinesReadyToInProgress covers ADR-0111's live-drain
// trigger for the STATUS label: a READY set held by a live drain reads "IN
// PROGRESS" even with zero done tasks; a started READY set (≥1 done, no live
// drain) still reads "IN PROGRESS"; an idle READY set stays "READY"; and a live
// drain coinciding with a non-READY status leaves that status' label untouched —
// needs-you outranks liveness.
func TestWorkRowStatusLabelRefinesReadyToInProgress(t *testing.T) {
	liveReady := work.Container{RawStatus: StatusReady, LiveDrain: true}
	if got := WorkRowStatusLabel(liveReady); got != "IN PROGRESS" {
		t.Errorf("live READY label = %q, want IN PROGRESS", got)
	}
	startedReady := work.Container{RawStatus: StatusReady, Started: true}
	if got := WorkRowStatusLabel(startedReady); got != "IN PROGRESS" {
		t.Errorf("started READY label = %q, want IN PROGRESS", got)
	}
	idleReady := work.Container{RawStatus: StatusReady}
	if got := WorkRowStatusLabel(idleReady); got != string(StatusReady) {
		t.Errorf("idle READY label = %q, want READY", got)
	}
	// The refinement is READY-only: a live drain on a non-READY set keeps its real
	// label, and the live indicator never rewrites it either.
	for _, status := range []TaskSetStatus{StatusAwaitingApproval, StatusNeedsVerify, StatusBlocked} {
		row := work.Container{RawStatus: status, LiveDrain: true}
		if got := WorkRowStatusLabel(row); got != string(status) {
			t.Errorf("live %s label = %q, want %s (needs-you outranks liveness)", status, got, status)
		}
	}
}

// TestWorkRowStatusLabelMirrorsStatusLabel proves the dashboard's status label is
// the same derivation `pop tasks status` uses (ADR-0121) for every non-refined
// row: it reproduces StatusLabel from the row's live fields rather than a separate
// scheme.
func TestWorkRowStatusLabelMirrorsStatusLabel(t *testing.T) {
	for _, status := range []TaskSetStatus{
		StatusFailed, StatusVerifyFailed, StatusBlocked, StatusDeferred,
		StatusDone, StatusMissing, StatusMalformed, StatusAwaitingApproval,
		StatusNeedsVerify,
	} {
		row := work.Container{RawStatus: status}
		want := StatusLabel(Row{Status: status})
		if got := WorkRowStatusLabel(row); got != want {
			t.Errorf("status %s label = %q, want %q (mirror of StatusLabel)", status, got, want)
		}
	}
}

// TestWorkRowStatusCellComposition pins the unstyled STATUS cell composition
// (ADR-0108/0111/0197): the display label followed by the verified-at, unfolded,
// auto-drain, orphaned, parked, and config-error suffixes in a fixed order, with
// no ANSI.
func TestWorkRowStatusCellComposition(t *testing.T) {
	cases := []struct {
		name string
		row  work.Container
		want string
	}{
		{"plain", work.Container{RawStatus: StatusBlocked}, "BLOCKED"},
		{"verified drifted", work.Container{VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abc123", VerifiedAtDrifted: true, RawStatus: StatusAwaitingApproval}, "AWAITING-APPROVAL · verified @ abc123"},
		{"unverified", work.Container{RawStatus: StatusNeedsVerify}, "NEEDS-VERIFY · unverified"},
		// The badge follows the mark, not the status: a human-completed set reads
		// DONE and shows whichever verification outcome rides beside it, while a
		// terminal row with no mark at all (verification disabled) shows none.
		{"human-completed unverified", work.Container{RawStatus: StatusDone, VerifyMark: VerifyMarkUnverified}, "DONE · unverified"},
		{"human-completed verify-failed", work.Container{RawStatus: StatusDone, VerifyMark: VerifyMarkFailed}, "DONE · verify-failed"},
		{"terminal, verification off", work.Container{RawStatus: StatusDone}, "DONE"},
		{"managed DONE unfolded", work.Container{RawStatus: StatusDone, Bound: true, Provisioned: true}, "DONE · unfolded"},
		{"managed AWAITING-APPROVAL unfolded", work.Container{RawStatus: StatusAwaitingApproval, Bound: true, Provisioned: true}, "AWAITING-APPROVAL · unfolded"},
		{"adopted DONE not unfolded", work.Container{RawStatus: StatusDone, Bound: true, Provisioned: false}, "DONE"},
		{"unbound DONE not unfolded", work.Container{RawStatus: StatusDone, Bound: false}, "DONE"},
		{"bound READY not unfolded", work.Container{RawStatus: StatusReady, Bound: true, Provisioned: true}, "READY"},
		{"unfolded after verified badge", work.Container{VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abc123", VerifiedAtDrifted: true, RawStatus: StatusAwaitingApproval, Bound: true, Provisioned: true}, "AWAITING-APPROVAL · verified @ abc123 · unfolded"},
		{"auto-drain waiting", work.Container{RawStatus: StatusReady, AutoDrain: true}, "READY · auto-drain"},
		{"auto-drain silenced by live drain", work.Container{RawStatus: StatusReady, AutoDrain: true, LiveDrain: true}, "IN PROGRESS"},
		{"auto-drain then orphaned", work.Container{RawStatus: StatusBlocked, AutoDrain: true, Orphaned: true}, "BLOCKED · auto-drain · orphaned"},
		{"parked alone", work.Container{RawStatus: StatusBlocked, Parked: true}, "BLOCKED · parked"},
		{"config error alone", work.Container{RawStatus: StatusReady, ConfigError: "no trunk worktree configured"}, "READY · config error: no trunk worktree configured"},
		{"orphaned then parked then config", work.Container{RawStatus: StatusBlocked, Orphaned: true, Parked: true, ConfigError: "no trunk"}, "BLOCKED · orphaned · parked · config error: no trunk"},
		{
			"full suffix order",
			work.Container{VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abcdef123456", VerifiedAtDrifted: true, RawStatus: StatusAwaitingApproval, Bound: true, Provisioned: true, AutoDrain: true, Orphaned: true, Parked: true, ConfigError: "no trunk"},
			"AWAITING-APPROVAL · verified @ abcdef123456 · unfolded · auto-drain · orphaned · parked · config error: no trunk",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WorkRowStatusCell(c.row)
			if got != c.want {
				t.Fatalf("WorkRowStatusCell = %q, want %q", got, c.want)
			}
			for i := 0; i < len(got); i++ {
				if got[i] == 0x1b {
					t.Fatalf("WorkRowStatusCell carries ANSI: %q", got)
				}
			}
		})
	}
}

// TestPresetCreatedSortOrdersByIdentifierDate pins ADR-0197 decision 6: a
// preset's created_desc / created_asc replaces the status scheme under the
// membership tiers. recent-30d's shipped sort is created_desc (newest-first);
// created_asc is the reverse. Live-drain, auto-drain and orphaned rows still
// float above every dated rest-tier row.
func TestPresetCreatedSortOrdersByIdentifierDate(t *testing.T) {
	recent, ok := config.ShippedWorkViewPreset("recent-30d")
	if !ok {
		t.Fatal("shipped recent-30d missing")
	}
	if recent.Sort != config.PresetSortCreatedDesc {
		t.Fatalf("recent-30d sort = %q, want created_desc", recent.Sort)
	}

	rows := []work.Container{
		{Project: "alpha", ID: "2026-01-01-old", RawStatus: StatusBlocked},
		{Project: "bravo", ID: "2026-03-15-mid", RawStatus: StatusReady},
		{Project: "charlie", ID: "2026-06-01-new", RawStatus: StatusDone},
		{Project: "zoo", ID: "2026-02-01-orph", RawStatus: StatusReady, Orphaned: true},
		{Project: "kilo", ID: "2026-01-15-ad", RawStatus: StatusBlocked, AutoDrain: true},
		{Project: "delta", ID: "2026-01-10-run", RawStatus: StatusDone, LiveDrain: true},
	}

	desc := append([]work.Container(nil), rows...)
	SortWorkRows(desc, config.PresetSortCreatedDesc)
	gotDesc := idsOf(desc)
	wantDesc := []string{
		"2026-01-10-run", // live-drain tier
		"2026-01-15-ad",  // auto-drain tier
		"2026-02-01-orph", // orphaned tier
		"2026-06-01-new", // rest: newest first
		"2026-03-15-mid",
		"2026-01-01-old",
	}
	if !reflect.DeepEqual(gotDesc, wantDesc) {
		t.Fatalf("created_desc = %v, want %v", gotDesc, wantDesc)
	}

	asc := append([]work.Container(nil), rows...)
	SortWorkRows(asc, config.PresetSortCreatedAsc)
	gotAsc := idsOf(asc)
	wantAsc := []string{
		"2026-01-10-run",
		"2026-01-15-ad",
		"2026-02-01-orph",
		"2026-01-01-old", // rest: oldest first
		"2026-03-15-mid",
		"2026-06-01-new",
	}
	if !reflect.DeepEqual(gotAsc, wantAsc) {
		t.Fatalf("created_asc = %v, want %v", gotAsc, wantAsc)
	}
}

// TestPresetCreatedSortUndatedPosition pins a defined place for identifiers
// with no parseable date: after every dated rest-tier row under both
// created_desc and created_asc, then ID descending among themselves.
func TestPresetCreatedSortUndatedPosition(t *testing.T) {
	rows := []work.Container{
		{Project: "a", ID: "no-prefix-z"},
		{Project: "b", ID: "2026-05-01-dated"},
		{Project: "c", ID: "no-prefix-a"},
		{Project: "d", ID: "2026-01-01-older"},
	}

	desc := append([]work.Container(nil), rows...)
	SortWorkRows(desc, config.PresetSortCreatedDesc)
	if got := idsOf(desc); !reflect.DeepEqual(got, []string{
		"2026-05-01-dated",
		"2026-01-01-older",
		"no-prefix-z",
		"no-prefix-a",
	}) {
		t.Fatalf("created_desc undated = %v", got)
	}

	asc := append([]work.Container(nil), rows...)
	SortWorkRows(asc, config.PresetSortCreatedAsc)
	if got := idsOf(asc); !reflect.DeepEqual(got, []string{
		"2026-01-01-older",
		"2026-05-01-dated",
		"no-prefix-z",
		"no-prefix-a",
	}) {
		t.Fatalf("created_asc undated = %v", got)
	}
}

// TestAbsentSortMeansCreatedDescAndStatusIsTheOptIn pins the ADR-0210 flip on
// the shared comparator: a preset that declares no sort orders by creation date
// newest first, and the ADR-0121 status scheme is reached only by declaring
// `status`, where it reproduces the pre-change sequence exactly.
func TestAbsentSortMeansCreatedDescAndStatusIsTheOptIn(t *testing.T) {
	base := []work.Container{
		{Project: "alpha", ID: "2026-02-01-blk", RawStatus: StatusBlocked},
		{Project: "bravo", ID: "2026-02-02-rdy", RawStatus: StatusReady},
		{Project: "alpha", ID: "2026-02-03-rdy", RawStatus: StatusReady},
		{Project: "bravo", Started: true, ID: "2026-02-04-inp", RawStatus: StatusReady},
		{Project: "bravo", ID: "2026-02-05-done", RawStatus: StatusDone},
		{Project: "charlie", ID: "2026-02-06-aa", RawStatus: StatusAwaitingApproval},
		{Project: "zoo", ID: "2026-04-01-orph", RawStatus: StatusReady, Orphaned: true},
		{Project: "kilo", ID: "2026-05-01-ad", RawStatus: StatusReady, AutoDrain: true},
		{Project: "delta", ID: "2026-06-01-run", RawStatus: StatusBlocked, AutoDrain: true, LiveDrain: true},
	}

	viaAbsent := append([]work.Container(nil), base...)
	viaDeclared := append([]work.Container(nil), base...)
	SortWorkRows(viaAbsent, "")
	SortWorkRows(viaDeclared, config.PresetSortCreatedDesc)
	if !reflect.DeepEqual(idsOf(viaAbsent), idsOf(viaDeclared)) {
		t.Fatalf("absent sort %v differs from declared created_desc %v", idsOf(viaAbsent), idsOf(viaDeclared))
	}
	wantCreated := []string{
		// The membership tiers still float above the date key.
		"2026-06-01-run",
		"2026-05-01-ad",
		"2026-04-01-orph",
		// Then the rest tier, newest first, with no regard for status.
		"2026-02-06-aa",
		"2026-02-05-done",
		"2026-02-04-inp",
		"2026-02-03-rdy",
		"2026-02-02-rdy",
		"2026-02-01-blk",
	}
	if !reflect.DeepEqual(idsOf(viaAbsent), wantCreated) {
		t.Fatalf("absent sort = %v, want created_desc %v", idsOf(viaAbsent), wantCreated)
	}

	viaStatus := append([]work.Container(nil), base...)
	SortWorkRows(viaStatus, config.PresetSortStatus)
	wantStatus := []string{
		"2026-06-01-run",
		"2026-05-01-ad",
		"2026-04-01-orph",
		"2026-02-04-inp",
		"2026-02-03-rdy",
		"2026-02-02-rdy",
		"2026-02-01-blk",
		"2026-02-05-done",
		"2026-02-06-aa",
	}
	if !reflect.DeepEqual(idsOf(viaStatus), wantStatus) {
		t.Fatalf("status sort = %v, want the pre-change scheme %v", idsOf(viaStatus), wantStatus)
	}
}

func idsOf(rows []work.Container) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
