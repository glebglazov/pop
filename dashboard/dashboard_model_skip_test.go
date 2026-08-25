package dashboard

import (
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// modelSkipDashboard builds a sized main-view dashboard carrying the given
// Effort model skips.
func modelSkipDashboard(height int, skips []work.ModelSkip) QueueDashboard {
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows, ModelSkips: skips})
	m.width = 120
	m.height = height
	return m
}

// TestDashboardFooterListsModelSkipsGroupedByPreset pins the read surface
// ADR-0168 specifies: one dim footer line naming every skipped ladder entry with
// its remaining time, each preset named once, permanent skips as ∞.
func TestDashboardFooterListsModelSkipsGroupedByPreset(t *testing.T) {
	now := time.Now()
	m := modelSkipDashboard(30, []work.ModelSkip{
		{Preset: "cursor", Model: "claude-opus-5-thinking-high", Until: now.Add(47*time.Minute + 30*time.Second)},
		{Preset: "cursor", Model: "claude-sonnet-5", Until: now.Add(2*time.Hour + 5*time.Minute + 30*time.Second)},
		{Preset: "kimi", Model: "k2.7-code-highspeed"},
	})

	got := m.View().Content
	want := "skipped: cursor/claude-opus-5-thinking-high 47m, claude-sonnet-5 2h5m · kimi/k2.7-code-highspeed ∞"
	if !strings.Contains(got, want) {
		t.Fatalf("view missing footer %q:\n%s", want, got)
	}
	// The line sits below the table, above the key hints.
	if strings.Index(got, want) > strings.Index(got, "r run") {
		t.Fatalf("skip footer should precede the key hints:\n%s", got)
	}
}

// TestDashboardFooterHiddenWithoutModelSkips pins the steady state: a machine
// with nothing skipped spends no line on the footer.
func TestDashboardFooterHiddenWithoutModelSkips(t *testing.T) {
	m := modelSkipDashboard(30, nil)
	if got := m.View().Content; strings.Contains(got, "skipped:") {
		t.Fatalf("view carries a skip footer with no skips recorded:\n%s", got)
	}
}

// TestDashboardFooterRespectsHeightGating pins that the footer obeys the same
// pane-height floor the two-line row rule uses (ADR-0107): in a short popup,
// visible-row density wins and the diagnostic stays on `pop tasks agents`.
func TestDashboardFooterRespectsHeightGating(t *testing.T) {
	skips := []work.ModelSkip{{Preset: "kimi", Model: "k2.7-code-highspeed"}}
	if got := modelSkipDashboard(dashboardTwoLineHeightFloor-1, skips).View().Content; strings.Contains(got, "skipped:") {
		t.Fatalf("short pane should not spend a line on the skip footer:\n%s", got)
	}
	if got := modelSkipDashboard(dashboardTwoLineHeightFloor, skips).View().Content; !strings.Contains(got, "skipped:") {
		t.Fatalf("roomy pane should carry the skip footer:\n%s", got)
	}
}

// TestDashboardFooterClipsToWidth pins width gating: a long skip list is
// truncated to the terminal width rather than wrapping the frame.
func TestDashboardFooterClipsToWidth(t *testing.T) {
	m := modelSkipDashboard(30, []work.ModelSkip{
		{Preset: "cursor", Model: strings.Repeat("m", 200)},
	})
	m.width = 60
	for _, line := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(line, "skipped:") && lipgloss.Width(line) > 60 {
			t.Fatalf("skip footer line exceeds the 60-column terminal: %q", line)
		}
	}
}

// TestDashboardFooterReservesItsLine pins that the footer is budgeted, not just
// drawn: the Frame reserves its row, so the table's visible rows shrink by one
// rather than the footer pushing the frame past the terminal height.
func TestDashboardFooterReservesItsLine(t *testing.T) {
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := string(rune('a' + i%26))
		rows[i] = DashboardRow{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	sized := func(skips []work.ModelSkip) QueueDashboard {
		m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows, ModelSkips: skips})
		m.width = 200
		m.height = 30
		return m
	}
	plain := len(strings.Split(sized(nil).View().Content, "\n"))
	withSkip := len(strings.Split(sized([]work.ModelSkip{{Preset: "kimi", Model: "k2.7"}}).View().Content, "\n"))
	if plain != withSkip {
		t.Fatalf("view height with footer = %d lines, without = %d; the footer must be budgeted", withSkip, plain)
	}
}

func TestFormatModelSkipRemaining(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		until time.Time
		want  string
	}{
		{"permanent", time.Time{}, "∞"},
		{"about to lift", now.Add(30 * time.Second), "<1m"},
		{"lapsed", now.Add(-time.Hour), "<1m"},
		{"minutes", now.Add(47*time.Minute + 30*time.Second), "47m"},
		{"hours", now.Add(2*time.Hour + 5*time.Minute), "2h5m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tasks.FormatModelSkipRemaining(tc.until, now); got != tc.want {
				t.Fatalf("FormatModelSkipRemaining = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDashboardFooterShowsStatedResetBesideCappedExpiry pins that a skip whose
// stated reset outruns the 24 hour cap (ADR-0168) reports both numbers: the
// footer would otherwise misreport what the provider actually said.
func TestDashboardFooterShowsStatedResetBesideCappedExpiry(t *testing.T) {
	now := time.Now()
	m := modelSkipDashboard(30, []work.ModelSkip{
		{Preset: "cursor", Model: "claude-opus-5-thinking-medium", Until: now.Add(24*time.Hour + time.Minute), StatedUntil: now.Add(26*24*time.Hour + time.Minute)},
	})

	got := m.View().Content
	want := "skipped: cursor/claude-opus-5-thinking-medium 24h0m (stated 26d0h)"
	if !strings.Contains(got, want) {
		t.Fatalf("view missing footer %q:\n%s", want, got)
	}
}
