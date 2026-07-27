package work

import (
	"reflect"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// TestShowRow pins the shared Done-inclusion row filter (ADR-0121): DONE sets are
// hidden by default and revealed under include-done; every other status always
// shows.
func TestShowRow(t *testing.T) {
	for _, status := range []tasks.TaskSetStatus{
		tasks.StatusReady, tasks.StatusFailed, tasks.StatusBlocked, tasks.StatusDeferred,
		tasks.StatusMissing, tasks.StatusMalformed, tasks.StatusAwaitingApproval, tasks.StatusNeedsVerify,
	} {
		row := tasks.Row{Status: status}
		if !ShowRow(row, false) {
			t.Errorf("%s hidden by default, want shown", status)
		}
		if !ShowRow(row, true) {
			t.Errorf("%s hidden under include-done, want shown", status)
		}
	}
	done := tasks.Row{Status: tasks.StatusDone}
	if ShowRow(done, false) {
		t.Errorf("DONE shown by default, want hidden")
	}
	if !ShowRow(done, true) {
		t.Errorf("DONE hidden under include-done, want shown")
	}
}

func TestSortOrder(t *testing.T) {
	rows := []Row{
		{Project: "zeta", SetRef: SetRef{SetID: "2026-01-01-old"}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-old"}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-06-18-new"}},
	}
	SortRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].SetID, rows[1].Project + "/" + rows[1].SetID, rows[2].Project + "/" + rows[2].SetID}
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
	rows := []Row{
		// Rest tier, rest band — alphabetically-early project with a needs-you status.
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-01-blk", RawStatus: tasks.StatusBlocked}},
		// Rest tier, READY band — floats above alpha's BLOCKED even though bravo sorts later.
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-02-rdy", RawStatus: tasks.StatusReady}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-03-rdy", RawStatus: tasks.StatusReady}},
		// Rest tier, IN PROGRESS band (started READY) — floats above the READY band.
		{Project: "bravo", Started: true, SetRef: SetRef{SetID: "2026-02-04-inp", RawStatus: tasks.StatusReady}},
		// Rest tier, rest band — DONE and AWAITING-APPROVAL, project-first then status order.
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-05-done", RawStatus: tasks.StatusDone}},
		{Project: "charlie", SetRef: SetRef{SetID: "2026-02-06-aa", RawStatus: tasks.StatusAwaitingApproval}},
		// Orphaned tier.
		{Project: "zoo", SetRef: SetRef{SetID: "2026-04-01-orph", RawStatus: tasks.StatusReady, Orphaned: true}},
		// Auto-drain tier — the orphaned+auto-drain set belongs here, not orphaned.
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-01-ad", RawStatus: tasks.StatusReady, AutoDrain: true}},
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-02-ado", RawStatus: tasks.StatusReady, AutoDrain: true, Orphaned: true}},
		// Running tier — highest precedence even over an auto-drain BLOCKED set.
		{Project: "delta", SetRef: SetRef{SetID: "2026-06-01-run", RawStatus: tasks.StatusBlocked, AutoDrain: true, LiveDrain: true}},
	}
	SortRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.SetID
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
	rows := []Row{
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-blk", RawStatus: tasks.StatusBlocked}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-01-02-rdy", RawStatus: tasks.StatusReady}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-03-rdy", RawStatus: tasks.StatusReady}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-01-04-blk", RawStatus: tasks.StatusBlocked}},
	}
	SortRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.SetID
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

// TestMapAndSetRowsInterleaveByProject proves the rest band groups per-project
// (ADR-0121/0130) across mixed map and task-set rows: a project's map and set
// rows sort together before the next project's, rather than all sets floating
// ahead of all maps.
func TestMapAndSetRowsInterleaveByProject(t *testing.T) {
	rows := []Row{
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-01-set", RawStatus: tasks.StatusBlocked}},
		{Project: "alpha", IsMap: true, SetRef: SetRef{SetID: "2026-02-01-map"}, MapOpen: 1, MapFrontier: 1},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-01-set", RawStatus: tasks.StatusBlocked}},
		{Project: "bravo", IsMap: true, SetRef: SetRef{SetID: "2026-02-01-map"}, MapOpen: 0, MapFrontier: 0},
	}
	SortRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		kind := "set"
		if r.IsMap {
			kind = "map"
		}
		got[i] = r.Project + "/" + kind + "/" + r.SetID
	}
	want := []string{
		"alpha/set/2026-02-01-set",
		"alpha/map/2026-02-01-map",
		"bravo/set/2026-02-01-set",
		"bravo/map/2026-02-01-map",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project interleave = %v, want %v", got, want)
	}
}

// TestBandKeysOnDisplayedLabel proves a row's band is keyed on its displayed
// label, not its raw status (ADR-0121): a started READY set renders as IN
// PROGRESS and sorts in the IN PROGRESS band, floating above a plain READY set
// even though both carry raw status READY and the IN PROGRESS row's project sorts
// later alphabetically.
func TestBandKeysOnDisplayedLabel(t *testing.T) {
	rows := []Row{
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-rdy", RawStatus: tasks.StatusReady}},
		{Project: "zeta", Started: true, SetRef: SetRef{SetID: "2026-01-02-inp", RawStatus: tasks.StatusReady}},
	}
	SortRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].SetID, rows[1].Project + "/" + rows[1].SetID}
	want := []string{"zeta/2026-01-02-inp", "alpha/2026-01-01-rdy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestStatusLabelRefinesReadyToInProgress covers ADR-0111's live-drain trigger
// for the STATUS label: a READY set held by a live drain reads "IN PROGRESS" even
// with zero done tasks; a started READY set (≥1 done, no live drain) still reads
// "IN PROGRESS"; an idle READY set stays "READY"; and a live drain coinciding
// with a non-READY status leaves that status' label untouched — needs-you
// outranks liveness.
func TestStatusLabelRefinesReadyToInProgress(t *testing.T) {
	liveReady := Row{SetRef: SetRef{RawStatus: tasks.StatusReady, LiveDrain: true}}
	if got := StatusLabel(liveReady); got != "IN PROGRESS" {
		t.Errorf("live READY label = %q, want IN PROGRESS", got)
	}
	startedReady := Row{SetRef: SetRef{RawStatus: tasks.StatusReady}, Started: true}
	if got := StatusLabel(startedReady); got != "IN PROGRESS" {
		t.Errorf("started READY label = %q, want IN PROGRESS", got)
	}
	idleReady := Row{SetRef: SetRef{RawStatus: tasks.StatusReady}}
	if got := StatusLabel(idleReady); got != string(tasks.StatusReady) {
		t.Errorf("idle READY label = %q, want READY", got)
	}
	// The refinement is READY-only: a live drain on a non-READY set keeps its real
	// label.
	for _, status := range []tasks.TaskSetStatus{tasks.StatusAwaitingApproval, tasks.StatusNeedsVerify, tasks.StatusBlocked} {
		row := Row{SetRef: SetRef{RawStatus: status, LiveDrain: true}}
		if got := StatusLabel(row); got != string(status) {
			t.Errorf("live %s label = %q, want %s (needs-you outranks liveness)", status, got, status)
		}
	}
}

// TestStatusLabelMirrorsTasksStatusLabel proves the dashboard's status label is
// the same derivation `pop tasks status` uses (ADR-0121) for every non-refined,
// non-map row: work.StatusLabel reproduces tasks.StatusLabel from the row's live
// fields rather than a separate scheme.
func TestStatusLabelMirrorsTasksStatusLabel(t *testing.T) {
	for _, status := range []tasks.TaskSetStatus{
		tasks.StatusFailed, tasks.StatusVerifyFailed, tasks.StatusBlocked, tasks.StatusDeferred,
		tasks.StatusDone, tasks.StatusMissing, tasks.StatusMalformed, tasks.StatusAwaitingApproval,
		tasks.StatusNeedsVerify,
	} {
		row := Row{SetRef: SetRef{RawStatus: status}}
		want := tasks.StatusLabel(tasks.Row{Status: status})
		if got := StatusLabel(row); got != want {
			t.Errorf("status %s label = %q, want %q (mirror of tasks.StatusLabel)", status, got, want)
		}
	}
}

// TestStatusCellComposition pins the unstyled STATUS cell composition (ADR-0108/
// 0111): the display label followed by the verified-at, auto-drain, orphaned,
// parked, and config-error suffixes in a fixed order, with no ANSI. Map rows show
// the WAYFINDING tally and skip the set-only suffixes (ADR-0130).
func TestStatusCellComposition(t *testing.T) {
	cases := []struct {
		name string
		row  Row
		want string
	}{
		{"plain", Row{SetRef: SetRef{RawStatus: tasks.StatusBlocked}}, "BLOCKED"},
		{"verified", Row{VerifiedAtSHA: "abc123", SetRef: SetRef{RawStatus: tasks.StatusAwaitingApproval}}, "AWAITING-APPROVAL · verified @ abc123"},
		{"auto-drain waiting", Row{SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true}}, "READY · auto-drain"},
		{"auto-drain silenced by live drain", Row{SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true, LiveDrain: true}}, "IN PROGRESS"},
		{"auto-drain then orphaned", Row{SetRef: SetRef{RawStatus: tasks.StatusBlocked, AutoDrain: true, Orphaned: true}}, "BLOCKED · auto-drain · orphaned"},
		{"parked alone", Row{SetRef: SetRef{RawStatus: tasks.StatusBlocked, Parked: true}}, "BLOCKED · parked"},
		{"config error alone", Row{SetRef: SetRef{RawStatus: tasks.StatusReady, ConfigError: "no trunk worktree configured"}}, "READY · config error: no trunk worktree configured"},
		{"orphaned then parked then config", Row{SetRef: SetRef{RawStatus: tasks.StatusBlocked, Orphaned: true, Parked: true, ConfigError: "no trunk"}}, "BLOCKED · orphaned · parked · config error: no trunk"},
		{
			"full suffix order",
			Row{VerifiedAtSHA: "abcdef123456", SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true, Orphaned: true, Parked: true, ConfigError: "no trunk"}},
			"READY · verified @ abcdef123456 · auto-drain · orphaned · parked · config error: no trunk",
		},
		{"map row", Row{IsMap: true, MapOpen: 3, MapFrontier: 1}, "WAYFINDING · 3 open / 1 frontier"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StatusCell(c.row); got != c.want {
				t.Fatalf("StatusCell = %q, want %q", got, c.want)
			}
			if got := StatusCell(c.row); containsANSI(got) {
				t.Fatalf("StatusCell carries ANSI: %q", got)
			}
		})
	}
}

// TestAutoDrainWaiting pins the display predicate: a consented set counts while
// idle and is silenced once a live drain holds the checkout (ADR-0108).
func TestAutoDrainWaiting(t *testing.T) {
	if !AutoDrainWaiting(Row{SetRef: SetRef{AutoDrain: true}}) {
		t.Errorf("idle auto-drain should be waiting")
	}
	if AutoDrainWaiting(Row{SetRef: SetRef{AutoDrain: true, LiveDrain: true}}) {
		t.Errorf("live-drained auto-drain should be silenced")
	}
	if AutoDrainWaiting(Row{SetRef: SetRef{}}) {
		t.Errorf("non-consenting set should not be waiting")
	}
}

// TestLiveIndicator confirms the plain indicator is the fixed-width glyph for a
// live-drained row and blank otherwise, regardless of STATUS (ADR-0111): the
// indicator is driven by LiveDrain alone.
func TestLiveIndicator(t *testing.T) {
	if got := LiveIndicator(Row{SetRef: SetRef{RawStatus: tasks.StatusReady}}); got != "" {
		t.Fatalf("idle indicator = %q, want blank", got)
	}
	for _, status := range []tasks.TaskSetStatus{
		tasks.StatusReady, tasks.StatusAwaitingApproval, tasks.StatusNeedsVerify, tasks.StatusBlocked,
	} {
		row := Row{SetRef: SetRef{RawStatus: status, LiveDrain: true}}
		if got := LiveIndicator(row); got != LiveDrainGlyph {
			t.Fatalf("status %s live indicator = %q, want %q", status, got, LiveDrainGlyph)
		}
		// The indicator never rewrites the status label (only READY refines).
		if status != tasks.StatusReady {
			if got := StatusLabel(row); got != string(status) {
				t.Fatalf("status %s label = %q, want unchanged (indicator does not refine)", status, got)
			}
		}
	}
}

// TestWorktreeLabel pins the unstyled destination cell (ADR-0070/0072): bound
// shows the branch, a managed directive shows `[managed wt]`, no directive shows
// `needs bind`, and a Done managed-bound set shows `[managed wt <branch>]`.
func TestWorktreeLabel(t *testing.T) {
	cases := []struct {
		kind  DestKind
		label string
		want  string
	}{
		{DestBound, "feature", "feature"},
		{DestManagedDirective, "ignored", DestLabelManagedWt},
		{DestNeedsBind, "ignored", DestLabelNeedsBind},
		{DestDoneManagedBound, "main", "[managed wt main]"},
	}
	for _, c := range cases {
		if got := WorktreeLabel(c.kind, c.label); got != c.want {
			t.Errorf("WorktreeLabel(%d, %q) = %q, want %q", c.kind, c.label, got, c.want)
		}
	}
}

func containsANSI(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			return true
		}
	}
	return false
}
