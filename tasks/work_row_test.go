package tasks

import (
	"reflect"
	"testing"

	"github.com/glebglazov/pop/work"
)

// The Work row derivation tests, relocated with the derivation itself when the
// Work seam took shape: the filter, the order and the STATUS cell all read this
// kind's statuses, so they are this kind's tests.

// TestShowRow pins the shared Done-inclusion row filter (ADR-0121): DONE sets are
// hidden by default and revealed under include-done; every other status always
// shows.
func TestShowRow(t *testing.T) {
	for _, status := range []TaskSetStatus{
		StatusReady, StatusFailed, StatusBlocked, StatusDeferred,
		StatusMissing, StatusMalformed, StatusAwaitingApproval, StatusNeedsVerify,
	} {
		row := Row{Status: status}
		if !ShowRow(row, false) {
			t.Errorf("%s hidden by default, want shown", status)
		}
		if !ShowRow(row, true) {
			t.Errorf("%s hidden under include-done, want shown", status)
		}
	}
	done := Row{Status: StatusDone}
	if ShowRow(done, false) {
		t.Errorf("DONE shown by default, want hidden")
	}
	if !ShowRow(done, true) {
		t.Errorf("DONE hidden under include-done, want shown")
	}
}

func TestSortOrder(t *testing.T) {
	rows := []work.Container{
		{Project: "zeta", ID: "2026-01-01-old"},
		{Project: "alpha", ID: "2026-01-01-old"},
		{Project: "alpha", ID: "2026-06-18-new"},
	}
	SortWorkRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].ID, rows[1].Project + "/" + rows[1].ID, rows[2].Project + "/" + rows[2].ID}
	want := []string{"alpha/2026-06-18-new", "alpha/2026-01-01-old", "zeta/2026-01-01-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestTieredSortOrder drives the full Queue surface order (ADR-0121) across a
// mixed fixture that exercises every membership tier and the status scheme. Tier
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
	SortWorkRows(rows)
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
	SortWorkRows(rows)
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
	SortWorkRows(rows)
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
// (ADR-0108/0111): the display label followed by the verified-at, auto-drain,
// orphaned, parked, and config-error suffixes in a fixed order, with no ANSI.
func TestWorkRowStatusCellComposition(t *testing.T) {
	cases := []struct {
		name string
		row  work.Container
		want string
	}{
		{"plain", work.Container{RawStatus: StatusBlocked}, "BLOCKED"},
		{"verified drifted", work.Container{VerifiedAtSHA: "abc123", VerifiedAtDrifted: true, RawStatus: StatusAwaitingApproval}, "AWAITING-APPROVAL · verified @ abc123"},
		{"unverified", work.Container{RawStatus: StatusNeedsVerify}, "NEEDS-VERIFY · unverified"},
		{"auto-drain waiting", work.Container{RawStatus: StatusReady, AutoDrain: true}, "READY · auto-drain"},
		{"auto-drain silenced by live drain", work.Container{RawStatus: StatusReady, AutoDrain: true, LiveDrain: true}, "IN PROGRESS"},
		{"auto-drain then orphaned", work.Container{RawStatus: StatusBlocked, AutoDrain: true, Orphaned: true}, "BLOCKED · auto-drain · orphaned"},
		{"parked alone", work.Container{RawStatus: StatusBlocked, Parked: true}, "BLOCKED · parked"},
		{"config error alone", work.Container{RawStatus: StatusReady, ConfigError: "no trunk worktree configured"}, "READY · config error: no trunk worktree configured"},
		{"orphaned then parked then config", work.Container{RawStatus: StatusBlocked, Orphaned: true, Parked: true, ConfigError: "no trunk"}, "BLOCKED · orphaned · parked · config error: no trunk"},
		{
			"full suffix order",
			work.Container{VerifiedAtSHA: "abcdef123456", VerifiedAtDrifted: true, RawStatus: StatusAwaitingApproval, AutoDrain: true, Orphaned: true, Parked: true, ConfigError: "no trunk"},
			"AWAITING-APPROVAL · verified @ abcdef123456 · auto-drain · orphaned · parked · config error: no trunk",
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
