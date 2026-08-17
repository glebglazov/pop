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
// what a human pressing `a m 3` actually causes.

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
	actions := []work.Action{{Verb: work.VerbMute, Key: "m", Label: "mute ▸"}}
	if !c.MutedUntil.IsZero() {
		actions = append(actions, work.Action{Verb: work.VerbUnmute, Key: "u", Label: "unmute"})
	}
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
// names for a Monday.
func muteDashboard(t *testing.T, row DashboardRow) (QueueDashboard, *muteRecorder) {
	t.Helper()
	rec := &muteRecorder{}
	td := &tasks.Deps{
		Clock: deps.FixedClock{Instant: mondayOfExampleWeek()},
		Rand:  &fixedRand{value: 123456789012345},
	}
	m := newQueueDashboard(&drain.Deps{Tasks: td}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.kinds = newWorkKinds([]work.Kind{rec})
	m.menu = newDashboardMenu(m.kinds, row, false)
	return m, rec
}

// `a` then `m` nests the window list, and a digit picks one: the kind receives
// the instant behind that digit, never the digit and never a duration.
func TestMuteSubmenuOpensAndPicksAWindow(t *testing.T) {
	row := DashboardRow{ID: "demo", CursorKey: "pop\x00demo"}
	m, rec := muteDashboard(t, row)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.mute == nil {
		t.Fatal("m should nest the mute submenu under the action menu")
	}
	if n := len(got.menu.mute.list.Items()); n != 6 {
		t.Fatalf("mute submenu offered %d windows, want 6", n)
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

	for _, window := range got.menu.mute.list.Items() {
		if strings.Contains(window.Label, "UTC") || strings.Contains(window.Label, "09:00") {
			t.Errorf("entry %q repeats the invariant hour", window.Label)
		}
	}

	lines := dashboardMuteMenuLines(got.menu.mute, got.menu.pluralCount(), 200)
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

// Unmute is offered only where there is a mute to clear, and it reaches the
// kind through the Muter seam rather than through Perform.
func TestUnmuteIsOfferedOnlyOnAMutedRow(t *testing.T) {
	plain := DashboardRow{ID: "demo", CursorKey: "pop\x00demo"}
	muted := plain
	muted.MutedUntil = time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	m, _ := muteDashboard(t, plain)
	for _, item := range dashboardMenuItems(m.kinds, plain) {
		if item.verb == work.VerbUnmute {
			t.Fatalf("an unmuted row offers unmute: %+v", item)
		}
	}

	m, rec := muteDashboard(t, muted)
	offered := false
	for _, item := range dashboardMenuItems(m.kinds, muted) {
		if item.verb == work.VerbUnmute && item.key == "u" {
			offered = true
		}
	}
	if !offered {
		t.Fatal("a muted row must offer unmute on u")
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if cmd == nil {
		t.Fatal("u dispatched nothing on a muted row")
	}
	cmd()
	if rec.unmuted != 1 {
		t.Fatalf("kind unmuted %d times, want 1", rec.unmuted)
	}
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatal("unmute should close the one-shot menu")
	}
}

// esc backs out of the window list to the action menu, the way the status
// submenu does — the two nest identically.
func TestMuteSubmenuEscReturnsToTheActionMenu(t *testing.T) {
	m, rec := muteDashboard(t, DashboardRow{ID: "demo"})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	updated, _ = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.mute != nil {
		t.Fatal("esc should close the window list and leave the action menu open")
	}
	if len(rec.muted) != 0 {
		t.Fatalf("esc muted something: %v", rec.muted)
	}
}

// Routines are not mutable, and they say so by omission — the same way every
// other ineligible verb is declared. The durable half of the invariant is the
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
