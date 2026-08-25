package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The dashboard's half of Mute (ADR-0200 decisions 4 and 5): the surface derives
// the six windows and hands one instant to the row's own kind. These drive the
// model through real keypresses over a recording kind, so what is asserted is
// what a human pressing `m 3` actually causes.

// muteRecorder is a Work kind that records the mute it was asked for and nothing
// else. It stands in for a real kind because the question here is what the
// surface sends, not what a task set does with it.
type muteRecorder struct {
	muted   []time.Time
	secrets []bool
	unmuted int
	muteRow work.Container
}

func (r *muteRecorder) ID() work.KindID                 { return ref.KindTaskSet }
func (r *muteRecorder) Load() ([]work.Container, error) { return nil, nil }
func (r *muteRecorder) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (r *muteRecorder) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
}

func (r *muteRecorder) Actions(c work.Container) []work.Action {
	// Neither half of mute is here: the Mute menu opens from the row list and holds
	// both (ADR-0236 decision 5). The kind's contribution is the work.Muter seam
	// below and nothing else.
	actions := []work.Action{{Verb: work.VerbShell, Key: "O", Label: "shell"}}
	return actions
}

func (r *muteRecorder) StatusActions(work.Container) []work.Action          { return nil }
func (r *muteRecorder) ItemActions(work.Container, work.Item) []work.Action { return nil }
func (r *muteRecorder) Summary([]work.Container) []string                   { return nil }
func (r *muteRecorder) Columns() []string                                   { return nil }
func (r *muteRecorder) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}

func (r *muteRecorder) Mute(c work.Container, until time.Time, secret bool) (work.Outcome, error) {
	r.muteRow = c
	r.muted = append(r.muted, until)
	r.secrets = append(r.secrets, secret)
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "muted " + c.ID}, nil
}

func (r *muteRecorder) Unmute(work.Container) (work.Outcome, error) {
	r.unmuted++
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "unmuted"}, nil
}

// muteDashboard builds a dashboard whose only kind is the recorder, with the
// clock and the roll pinned so the six windows are the ones ADR-0200's table
// names for a Monday. No menu is opened: `m` reaches the Mute menu from the row
// list (ADR-0236 decision 1).
func muteDashboard(t *testing.T, row DashboardRow) (QueueDashboard, *muteRecorder) {
	t.Helper()
	rec := &muteRecorder{}
	td := &tasks.Deps{
		Clock: deps.FixedClock{Instant: mondayOfExampleWeek()},
		Rand:  &fixedRand{value: 123456789012345},
	}
	m := newQueueDashboard(&drain.Deps{Tasks: td}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.kinds = newWorkKinds([]work.Kind{rec})
	return m, rec
}

// `m` on a row opens the window list straight from the row list, and a digit
// picks one: the kind receives the instant behind that digit, never the digit and
// never a duration.
func TestMuteSubmenuOpensAndPicksAWindow(t *testing.T) {
	row := DashboardRow{ID: "demo", CursorKey: "pop\x00demo"}
	m, rec := muteDashboard(t, row)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.mute == nil {
		t.Fatal("m should open the Mute menu from the row list")
	}
	if n := len(got.menu.mute.list.Items()); n != 6 {
		t.Fatalf("the Mute menu over an unmuted row offered %d entries, want the 6 windows alone", n)
	}

	// Digit 3 on a Monday is `Wed 12 Aug` — the second dated entry, since the
	// random default holds digit 1 and tomorrow digit 2.
	updated, cmd := got.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("picking a window should close the menu")
	}
	if cmd == nil {
		t.Fatal("picking a window dispatched nothing")
	}
	cmd()

	if len(rec.muted) != 1 {
		t.Fatalf("kind received %d mutes, want 1", len(rec.muted))
	}
	want := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	if !rec.muted[0].Equal(want) {
		t.Fatalf("muted until %s, want %s", rec.muted[0], want)
	}
	if rec.secrets[0] {
		t.Fatal("a dated window must not be marked secret")
	}
	if rec.muteRow.ID != "demo" {
		t.Fatalf("mute landed on %q, want the row the menu was opened over", rec.muteRow.ID)
	}
}

// Digit 1 is the random default, and it is the one window that crosses the seam
// marked secret — which is what makes every read surface withhold its instant.
func TestMuteSubmenuFirstDigitIsTheSecretDefault(t *testing.T) {
	m, rec := muteDashboard(t, DashboardRow{ID: "demo", CursorKey: "pop\x00demo"})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, cmd := updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if cmd == nil {
		t.Fatal("digit 1 dispatched nothing")
	}
	cmd()

	if len(rec.secrets) != 1 || !rec.secrets[0] {
		t.Fatalf("digit 1 must mute with the secret window, got %v", rec.secrets)
	}
	if len(rec.muted) != 1 {
		t.Fatalf("digit 1 recorded %d mutes, want 1", len(rec.muted))
	}
}

// The invariant hour is stated once, in the footer, and no entry repeats it.
// Five entries each ending in "09:00 UTC" was the alternative.
func TestMuteSubmenuStatesTheHourOnlyInItsFooter(t *testing.T) {
	m, _ := muteDashboard(t, DashboardRow{ID: "demo"})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	got := updated.(QueueDashboard)

	for _, entry := range got.menu.mute.list.Items() {
		if strings.Contains(entry.label, "UTC") || strings.Contains(entry.label, "09:00") {
			t.Errorf("entry %q repeats the invariant hour", entry.label)
		}
	}

	lines := dashboardMuteMenuLines(got.menu.mute, got.menu.target(), 200)
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "09:00 UTC") {
		t.Fatalf("footer = %q, want the invariant hour stated once", footer)
	}
	hourLines := 0
	for _, line := range lines {
		if strings.Contains(line, "09:00 UTC") {
			hourLines++
		}
	}
	if hourLines != 1 {
		t.Fatalf("the hour appears on %d lines, want exactly the footer", hourLines)
	}
}

// Clearing a mute lives in the Mute menu, on `u`, beside the windows that set
// one (ADR-0236 decision 5) — and only where there is a mute to clear. It reaches
// the kind through the Muter seam rather than through Perform.
func TestUnmuteIsOfferedOnlyOnAMutedRow(t *testing.T) {
	plain := DashboardRow{ID: "demo", CursorKey: "pop\x00demo"}
	muted := plain
	muted.MutedUntil = time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	m, _ := muteDashboard(t, plain)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	for _, entry := range updated.(QueueDashboard).menu.mute.list.Items() {
		if entry.clear {
			t.Fatalf("an unmuted row's Mute menu offers a clear entry: %+v", entry)
		}
	}

	m, rec := muteDashboard(t, muted)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	got := updated.(QueueDashboard)
	offered := false
	for _, entry := range got.menu.mute.list.Items() {
		if entry.clear && entry.key == "u" {
			offered = true
		}
	}
	if !offered {
		t.Fatal("a muted row's Mute menu must offer clear on u")
	}
	// Every window keeps the digit it had: the clear entry is not one of them, so a
	// digit means the same date whether or not the row is muted.
	for i, entry := range got.menu.mute.list.Items() {
		if !entry.clear && entry.key != muteWindowKey(i) {
			t.Fatalf("window %d answers to %q, want %q", i, entry.key, muteWindowKey(i))
		}
	}

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if cmd == nil {
		t.Fatal("u cleared nothing on a muted row")
	}
	cmd()
	if rec.unmuted != 1 {
		t.Fatalf("kind unmuted %d times, want 1", rec.unmuted)
	}
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatal("clearing a mute should close the menu")
	}
}

// A kind with no mute answers `m` in a flash: at top level the key reaches the
// whole surface, and one that does nothing reads as broken (ADR-0236 decision 7).
func TestMuteMenuFlashesOnAKindThatCannotBeMuted(t *testing.T) {
	row := DashboardRow{Kind: ref.KindRoutine, ID: "daily", CursorKey: "pop\x00daily"}
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.kinds = testRoutineKinds()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("m opened a Mute menu over a kind that cannot be muted")
	}
	if want := "a Routine cannot be muted"; got.flash.Text() != want {
		t.Fatalf("flash = %q, want %q", got.flash.Text(), want)
	}
}

// The esc out of the Mute menu goes back to the rows, there being no menu under
// it any more.
func TestMuteMenuEscClosesToTheRowList(t *testing.T) {
	m, rec := muteDashboard(t, DashboardRow{ID: "demo", CursorKey: "pop\x00demo"})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	updated, _ = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatalf("esc left a menu open: %+v", got.menu)
	}
	if len(rec.muted) != 0 {
		t.Fatalf("esc muted something: %v", rec.muted)
	}
}

// Routines are not mutable: no verb of theirs names a mute and no seam of theirs
// takes one, which is what turns `m` on a Routine row into the flash above. The
// durable half of the invariant is the
// store's (ADR-0200 decision 7): a Routine already carries an indefinite pause
// bit, and a second human-set suppression beside it would be two vocabularies
// for one intent.
func TestRoutineOffersNoMuteVerb(t *testing.T) {
	row := DashboardRow{Kind: ref.KindRoutine, ID: "nightly"}
	for _, item := range dashboardMenuItems(testRoutineKinds(), row) {
		if item.verb == work.VerbMute || item.verb == work.VerbUnmute {
			t.Fatalf("routine row offers a mute verb: %+v", item)
		}
	}
	if muter := testRoutineKinds().muterFor(row); muter != nil {
		t.Fatalf("routine kind implements the Muter seam: %T", muter)
	}
}
