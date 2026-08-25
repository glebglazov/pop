package dashboard

import (
	"fmt"
	"sort"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// The mute submenu's content (ADR-0200 decision 4). A mute window is a date the
// human picks, so the list is derived from today rather than configured: six
// entries always — the random default, then tomorrow, then four weekday
// mornings. It is surface-owned because a date is not kind knowledge; three
// kinds returning the identical six actions would be the copy-paste the Work
// seam exists to prevent.

// muteWindowCount is the whole menu, the random default included.
const muteWindowCount = 6

// muteWindowHour is the invariant hour every dated entry lands at, in UTC. The
// list enumerates only instants that are already mornings, so nothing downstream
// normalizes a window forward.
const muteWindowHour = 9

// randomMuteFloor and randomMuteSpan bound the default window: an instant at
// least three days out and less than seven. The floor is the guarantee that
// matters; the hour deliberately is not one, because landing every secret mute
// at 09:00 UTC would hand back most of what the secret hides (decision 6).
const (
	randomMuteFloor = 3 * 24 * time.Hour
	randomMuteSpan  = 4 * 24 * time.Hour
)

// randomMuteLabel names the default window by its bounds, never its instant. It
// is the one entry whose date no read surface ever shows.
const randomMuteLabel = "Surprise (between 3 and 7 days)"

// muteWindowLabelFormat is the day-and-date form every dated entry reads as —
// `Fri 14 Aug`.
const muteWindowLabelFormat = "Mon 2 Jan"

// thisWeekLadder and nextWeekLadder decide which weekdays make the cut when
// there are more candidates than slots. Friday leads this week because "deal
// with it before the week ends" is the common intent, and Thursday trails
// because it is nearly the same answer as Friday; next week inverts to
// Monday-first because inside a week you are not pushing past anything, so the
// near days are the useful ones.
//
// Only the next-week ladder actually selects today: this week never offers more
// future weekdays than the slots available, so its order is inert. It is written
// down because it becomes load-bearing the moment muteWindowCount changes.
var (
	thisWeekLadder = []time.Weekday{time.Friday, time.Wednesday, time.Monday, time.Tuesday, time.Thursday}
	nextWeekLadder = []time.Weekday{time.Monday, time.Wednesday, time.Friday, time.Tuesday, time.Thursday}
)

// MuteWindow is one entry of the mute submenu: what the digit reads as, and the
// instant the container resurfaces at if the human picks it.
type MuteWindow struct {
	// Label is what the entry reads as — `Tomorrow`, or `Fri 14 Aug`.
	Label string
	// Until is the resurfacing instant. Dated entries land at 09:00 UTC; the
	// random default is an unrounded instant.
	Until time.Time
	// Secret marks the random default, the one entry whose instant a read
	// surface must never disclose (ADR-0200 decision 6).
	Secret bool
}

// MuteWindows is the mute submenu in the order it is shown: the random default
// pinned first, then the dates chronologically, so the digits stay monotonic in
// time. Both the clock and the roll come in through the dependency bag.
func MuteWindows(td *tasks.Deps) []MuteWindow {
	return muteWindowsAt(td.Now(), td.Int63n)
}

// muteWindowsAt is the derivation itself, with the two non-deterministic inputs
// already resolved: the instant the human opened the menu, and the roll behind
// the default window.
func muteWindowsAt(now time.Time, roll func(n int64) int64) []MuteWindow {
	dates := muteWindowDates(now)

	windows := make([]MuteWindow, 0, muteWindowCount)
	windows = append(windows, MuteWindow{
		Label:  randomMuteLabel,
		Until:  now.Add(randomMuteFloor + time.Duration(roll(int64(randomMuteSpan)))),
		Secret: true,
	})
	for i, date := range dates {
		label := date.Format(muteWindowLabelFormat)
		if i == 0 {
			// Tomorrow is by definition the earliest date, so it is also always
			// the first — the most-used entry is the one with a stable key.
			label = "Tomorrow"
		}
		windows = append(windows, MuteWindow{Label: label, Until: muteMorning(date)})
	}
	return windows
}

// muteWindowDates picks the five days the menu offers, chronologically. The
// first is always tomorrow, whatever day tomorrow is — a one-day postponement
// must not vanish two days a week, so it is the only weekend-capable entry.
func muteWindowDates(now time.Time) []time.Time {
	today := startOfDay(now)
	tomorrow := today.AddDate(0, 0, 1)

	thisMonday := today.AddDate(0, 0, -int((today.Weekday()+6)%7))
	nextMonday := thisMonday.AddDate(0, 0, 7)

	slots := muteWindowCount - 2 // minus the random default, minus tomorrow

	// This week's remaining weekdays take absolute precedence over next week's,
	// but one slot is held back so "not this week at all" can be said on any day.
	// The reservation never actually displaces a this-week entry: once tomorrow
	// has taken one, this week can offer at most slots-1 future weekdays anyway.
	weekdays := pickWeekdays(thisMonday, thisWeekLadder, today, tomorrow, slots-1)
	weekdays = append(weekdays, pickWeekdays(nextMonday, nextWeekLadder, today, tomorrow, slots-len(weekdays))...)
	sort.Slice(weekdays, func(i, j int) bool { return weekdays[i].Before(weekdays[j]) })

	return append([]time.Time{tomorrow}, weekdays...)
}

// pickWeekdays walks one ladder over the week starting at monday and takes up to
// limit days that are still ahead. Tomorrow is excluded because it already has
// its own entry — on a Sunday it is next week's Monday, which would otherwise
// appear twice.
func pickWeekdays(monday time.Time, ladder []time.Weekday, today, tomorrow time.Time, limit int) []time.Time {
	picked := make([]time.Time, 0, limit)
	for _, wd := range ladder {
		if len(picked) >= limit {
			break
		}
		date := monday.AddDate(0, 0, int((wd+6)%7))
		if !date.After(today) || date.Equal(tomorrow) {
			continue
		}
		picked = append(picked, date)
	}
	return picked
}

// muteMorning is a date's resurfacing instant: 09:00 UTC on that day. The date
// itself is read off the human's own calendar, so a menu opened on a Monday
// evening offers Tuesday whatever UTC thinks the day is.
func muteMorning(date time.Time) time.Time {
	y, m, d := date.Date()
	return time.Date(y, m, d, muteWindowHour, 0, 0, 0, time.UTC)
}

// dashboardMuteMenu is the Mute menu opened with `m` from the row list
// (ADR-0236 decision 1). Unlike the Status menu its items are the surface's
// own — a date is not kind knowledge — and what the kind receives is the instant
// behind the digit the human pressed, through work.Muter.
type dashboardMuteMenu struct {
	row  DashboardRow
	list *ui.List[muteMenuEntry]
}

// muteMenuEntry is one line of the Mute menu: a window that starts a mute, or
// the clear entry that ends one. Both halves of the gesture live on one menu
// because mute and unmute are one concept (ADR-0236 decision 5), and the clear
// entry keeps `u` rather than taking a digit — it is not a window, so a digit
// must not change which date it means depending on whether the row is muted.
type muteMenuEntry struct {
	key    string
	label  string
	window MuteWindow
	clear  bool
}

// taskDeps is the dependency bag the window derivation reads its clock and its
// roll from. A dashboard built without one still opens the menu: the bag's own
// accessors fall back to the wall clock and the global source.
func (m QueueDashboard) taskDeps() *tasks.Deps {
	if m.d == nil {
		return nil
	}
	return m.d.Tasks
}

// newDashboardMuteMenu opens the Mute menu over row with the windows derived
// from right now, and with `u` to clear when clearable says the target carries a
// mute. The windows are derived per opening rather than cached: the dates change
// with the day, and a menu built at launch would offer yesterday.
func newDashboardMuteMenu(td *tasks.Deps, row DashboardRow, clearable bool) *dashboardMuteMenu {
	windows := MuteWindows(td)
	entries := make([]muteMenuEntry, 0, len(windows)+1)
	for i, window := range windows {
		entries = append(entries, muteMenuEntry{key: muteWindowKey(i), label: window.Label, window: window})
	}
	if clearable {
		entries = append(entries, muteMenuEntry{key: "u", label: "clear mute", clear: true})
	}
	return &dashboardMuteMenu{
		row:  row,
		list: ui.NewList(entries, ui.Opts[muteMenuEntry]{Wrap: true}),
	}
}

// muteMenuClearable reports whether the Mute menu opened over rows offers the
// clear entry: every target must carry a mute. Over one row that is the
// eligibility the action menu expressed by omitting unmute; over a Selection it
// is the same intersection every plural verb answers to (ADR-0215) — one `u`
// that cleared some of the marked rows would be a partial answer.
func muteMenuClearable(rows []DashboardRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if row.MutedUntil.IsZero() {
			return false
		}
	}
	return true
}

// muteWindowKey is the digit one entry answers to. The digits are monotonic in
// time after the random default at 1, so `3` is always sooner than `5`.
func muteWindowKey(idx int) string { return fmt.Sprintf("%d", idx+1) }

// muteMenuFooter states the invariant hour once, which is why no dated entry
// repeats it: five entries each ending in "09:00 UTC" would spend a fifth of the
// menu's width on a constant.
func muteMenuFooter() string {
	return fmt.Sprintf("every date resurfaces at %02d:00 UTC", muteWindowHour)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
