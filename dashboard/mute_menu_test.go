package dashboard

import (
	"math/rand"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// fixedRand is the roll seam pinned to one value, so a window derivation has one
// exact expected instant instead of a range.
type fixedRand struct {
	value int64
	spans []int64
}

func (r *fixedRand) Int63n(n int64) int64 {
	r.spans = append(r.spans, n)
	return r.value
}

// The week of 10 August 2026: Monday the 10th through Sunday the 16th, with
// Monday the 17th to Friday the 21st as the week after. It is the week ADR-0200's
// own examples (`Fri 14 Aug`, `Fri 21 Aug`) are written against.
func mondayOfExampleWeek() time.Time {
	return time.Date(2026, time.August, 10, 14, 23, 45, 0, time.UTC)
}

func TestMuteWindowsEveryWeekday(t *testing.T) {
	// ADR-0200 decision 4, copied rather than re-derived.
	cases := []struct {
		today  string
		dated  []string
		digits []int // day of August each dated entry lands on
	}{
		{"Mon", []string{"Tomorrow", "Wed 12 Aug", "Thu 13 Aug", "Fri 14 Aug", "Mon 17 Aug"}, []int{11, 12, 13, 14, 17}},
		{"Tue", []string{"Tomorrow", "Thu 13 Aug", "Fri 14 Aug", "Mon 17 Aug", "Wed 19 Aug"}, []int{12, 13, 14, 17, 19}},
		{"Wed", []string{"Tomorrow", "Fri 14 Aug", "Mon 17 Aug", "Wed 19 Aug", "Fri 21 Aug"}, []int{13, 14, 17, 19, 21}},
		{"Thu", []string{"Tomorrow", "Mon 17 Aug", "Tue 18 Aug", "Wed 19 Aug", "Fri 21 Aug"}, []int{14, 17, 18, 19, 21}},
		{"Fri", []string{"Tomorrow", "Mon 17 Aug", "Tue 18 Aug", "Wed 19 Aug", "Fri 21 Aug"}, []int{15, 17, 18, 19, 21}},
		{"Sat", []string{"Tomorrow", "Mon 17 Aug", "Tue 18 Aug", "Wed 19 Aug", "Fri 21 Aug"}, []int{16, 17, 18, 19, 21}},
		{"Sun", []string{"Tomorrow", "Tue 18 Aug", "Wed 19 Aug", "Thu 20 Aug", "Fri 21 Aug"}, []int{17, 18, 19, 20, 21}},
	}

	for offset, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			now := mondayOfExampleWeek().AddDate(0, 0, offset)
			td := &tasks.Deps{
				Clock: deps.FixedClock{Instant: now},
				Rand:  &fixedRand{value: 123456789012345},
			}

			windows := MuteWindows(td)
			if len(windows) != 6 {
				t.Fatalf("want 6 entries, got %d: %+v", len(windows), windows)
			}
			if !windows[0].Secret {
				t.Errorf("entry 1 must be the random default")
			}
			for i, want := range tc.dated {
				got := windows[i+1]
				if got.Label != want {
					t.Errorf("entry %d label: want %q, got %q", i+2, want, got.Label)
				}
				if got.Secret {
					t.Errorf("entry %d (%s) must not be secret", i+2, got.Label)
				}
				wantUntil := time.Date(2026, time.August, tc.digits[i], 9, 0, 0, 0, time.UTC)
				if !got.Until.Equal(wantUntil) {
					t.Errorf("entry %d instant: want %s, got %s", i+2, wantUntil, got.Until)
				}
			}

			// Tomorrow is the first dated entry every day, weekend included, and
			// next week always contributes at least one entry so "not this week at
			// all" can be said. Next week here is Monday the 17th onward.
			tomorrow := time.Date(2026, time.August, 10+offset+1, 9, 0, 0, 0, time.UTC)
			if !windows[1].Until.Equal(tomorrow) || windows[1].Label != "Tomorrow" {
				t.Errorf("tomorrow: want %s labelled Tomorrow, got %+v", tomorrow, windows[1])
			}
			nextWeek := 0
			for _, w := range windows[1:] {
				if w.Until.Day() >= 17 {
					nextWeek++
				}
			}
			if nextWeek == 0 {
				t.Errorf("no entry from next week: %+v", windows)
			}
		})
	}
}

func TestRandomMuteWindowIsAnUnroundedInstantThreeToSevenDaysOut(t *testing.T) {
	now := mondayOfExampleWeek()
	roll := &fixedRand{value: 123456789012345}
	td := &tasks.Deps{Clock: deps.FixedClock{Instant: now}, Rand: roll}

	got := MuteWindows(td)[0]
	// 72h + 34h17m36.789012345s past Monday 14:23:45.
	want := time.Date(2026, time.August, 15, 0, 41, 21, 789012345, time.UTC)
	if !got.Until.Equal(want) {
		t.Errorf("random window: want %s, got %s", want, got.Until)
	}
	if len(roll.spans) != 1 || roll.spans[0] != int64(4*24*time.Hour) {
		t.Errorf("roll must span exactly four days, got %v", roll.spans)
	}
	if h, m, s := got.Until.Clock(); h == 9 && m == 0 && s == 0 {
		t.Errorf("random window must not be rounded to a morning: %s", got.Until)
	}

	// The floor is the guarantee that matters, so hold it across a seeded source
	// on every weekday rather than on one lucky roll.
	seeded := rand.New(rand.NewSource(20260810))
	for offset := 0; offset < 7; offset++ {
		day := now.AddDate(0, 0, offset)
		td := &tasks.Deps{Clock: deps.FixedClock{Instant: day}, Rand: seeded}
		for i := 0; i < 200; i++ {
			out := MuteWindows(td)[0].Until.Sub(day)
			if out < 3*24*time.Hour || out >= 7*24*time.Hour {
				t.Fatalf("random window %s out of [3d, 7d)", out)
			}
		}
	}
}
