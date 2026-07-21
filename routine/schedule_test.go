package routine

import (
	"strings"
	"testing"
	"time"
)

func TestScheduleGrammarAdvertisesProduction(t *testing.T) {
	for _, ex := range []string{
		"every 6h",
		"at 10:00",
		"on mon-fri at 09:00",
		"every 2d at 10:00",
		"every 2w on mon at 10:00",
	} {
		if !strings.Contains(ScheduleGrammar, ex) {
			t.Fatalf("ScheduleGrammar missing example %q", ex)
		}
	}
	if !strings.Contains(ScheduleGrammar, "[every <N><unit>]") {
		t.Fatal("ScheduleGrammar missing production")
	}
	if !strings.Contains(ScheduleGrammar, "utc") {
		t.Fatal("ScheduleGrammar missing utc rule")
	}
	if strings.Contains(strings.ToLower(ScheduleGrammar), "daily at") {
		t.Fatal("ScheduleGrammar must not advertise daily at alias")
	}
}

func TestParseScheduleEvery(t *testing.T) {
	s, err := ParseSchedule("every 6h")
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != ScheduleEvery || s.Interval != 6*time.Hour {
		t.Fatalf("got %+v", s)
	}
}

func TestParseScheduleEveryCaseInsensitive(t *testing.T) {
	s, err := ParseSchedule("EVERY 30m")
	if err != nil {
		t.Fatal(err)
	}
	if s.Interval != 30*time.Minute {
		t.Fatalf("interval = %s", s.Interval)
	}
}

func TestParseScheduleDailyAlias(t *testing.T) {
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != ScheduleSlot || s.StepDays != 1 || s.Weekdays != allWeekdays || s.Hour != 10 || s.Minute != 0 {
		t.Fatalf("got %+v", s)
	}
	if s.Raw != "daily at 10:00" {
		t.Fatalf("Raw = %q, want unchanged alias spelling", s.Raw)
	}
}

func TestParseScheduleDailyMidnight(t *testing.T) {
	s, err := ParseSchedule("daily at 00:00")
	if err != nil {
		t.Fatal(err)
	}
	if s.Hour != 0 || s.Minute != 0 {
		t.Fatalf("got %+v", s)
	}
}

func TestParseScheduleRejectsInvalidForms(t *testing.T) {
	cases := []string{
		"",
		"cron */5 * * * *",
		"every",
		"every 0h",
		"every -1h",
		"daily at 25:00",
		"daily at 10:60",
		"weekly on monday",
	}
	for _, raw := range cases {
		if _, err := ParseSchedule(raw); err == nil {
			t.Fatalf("ParseSchedule(%q) expected error", raw)
		} else if !strings.Contains(err.Error(), "invalid schedule") {
			t.Fatalf("ParseSchedule(%q) error = %v", raw, err)
		}
	}
}

func TestParseScheduleClauseProduction(t *testing.T) {
	mon := uint8(1 << time.Monday)
	monFri := weekdayMask(time.Monday, time.Friday)
	satSun := uint8((1 << time.Saturday) | (1 << time.Sunday))

	cases := []struct {
		raw      string
		kind     ScheduleKind
		interval time.Duration
		step     int
		mask     uint8
		hour     int
		minute   int
		utc      bool
	}{
		{raw: "every 6h", kind: ScheduleEvery, interval: 6 * time.Hour},
		{raw: "at 10:00", kind: ScheduleSlot, step: 1, mask: allWeekdays, hour: 10},
		{raw: "at 10", kind: ScheduleSlot, step: 1, mask: allWeekdays, hour: 10},
		{raw: "on mon", kind: ScheduleSlot, step: 1, mask: mon},
		{raw: "on Monday", kind: ScheduleSlot, step: 1, mask: mon},
		{raw: "on mon-fri at 09:00", kind: ScheduleSlot, step: 1, mask: monFri, hour: 9},
		{raw: "on MON-FRI at 09:00", kind: ScheduleSlot, step: 1, mask: monFri, hour: 9},
		{raw: "on weekdays", kind: ScheduleSlot, step: 1, mask: monFri},
		{raw: "on weekends", kind: ScheduleSlot, step: 1, mask: satSun},
		{raw: "on mon,wed,fri", kind: ScheduleSlot, step: 1, mask: mon | (1 << time.Wednesday) | (1 << time.Friday)},
		{raw: "every 2d at 10:00", kind: ScheduleSlot, step: 2, mask: allWeekdays, hour: 10},
		{raw: "every 2w on mon at 10:00", kind: ScheduleSlot, step: 14, mask: mon, hour: 10},
		{raw: "every 2d", kind: ScheduleSlot, step: 2, mask: allWeekdays},
		{raw: "at 11:00 utc", kind: ScheduleSlot, step: 1, mask: allWeekdays, hour: 11, utc: true},
		{raw: "on fri at 08:30 utc", kind: ScheduleSlot, step: 1, mask: 1 << time.Friday, hour: 8, minute: 30, utc: true},
	}
	for _, tc := range cases {
		s, err := ParseSchedule(tc.raw)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", tc.raw, err)
		}
		if s.Kind != tc.kind {
			t.Fatalf("ParseSchedule(%q).Kind = %v, want %v", tc.raw, s.Kind, tc.kind)
		}
		if s.Raw != tc.raw {
			t.Fatalf("ParseSchedule(%q).Raw = %q, want unchanged", tc.raw, s.Raw)
		}
		if tc.kind == ScheduleEvery {
			if s.Interval != tc.interval {
				t.Fatalf("ParseSchedule(%q).Interval = %s, want %s", tc.raw, s.Interval, tc.interval)
			}
			continue
		}
		if s.StepDays != tc.step || s.Weekdays != tc.mask || s.Hour != tc.hour || s.Minute != tc.minute || s.UTC != tc.utc {
			t.Fatalf("ParseSchedule(%q) = %+v, want step=%d mask=%07b hour=%d minute=%d utc=%v",
				tc.raw, s, tc.step, tc.mask, tc.hour, tc.minute, tc.utc)
		}
	}
}

func TestParseScheduleTargetedErrors(t *testing.T) {
	cases := []struct {
		raw     string
		wantSub string
	}{
		{raw: "every 6h at 10:00", wantSub: `"h"/"m" intervals cannot carry "on"/"at"`},
		{raw: "every 30m on mon", wantSub: `"h"/"m" intervals cannot carry "on"/"at"`},
		{raw: "on fri-mon", wantSub: "weekday ranges do not wrap"},
		{raw: "on weekdays,sun", wantSub: `"weekdays"/"weekends" cannot mix into a list`},
		{raw: "at 10:00 every 2d", wantSub: "clauses must appear in order"},
	}
	for _, tc := range cases {
		_, err := ParseSchedule(tc.raw)
		if err == nil {
			t.Fatalf("ParseSchedule(%q) expected error", tc.raw)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("ParseSchedule(%q) error = %v, want substring %q", tc.raw, err, tc.wantSub)
		}
		if strings.Contains(err.Error(), ScheduleGrammar) {
			t.Fatalf("ParseSchedule(%q) should be targeted, got generic blurb: %v", tc.raw, err)
		}
	}
}

func TestScheduleNextAfterEvery(t *testing.T) {
	s, err := ParseSchedule("every 6h")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	got := s.NextAfter(last)
	want := last.Add(6 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailySameDay(t *testing.T) {
	// Suffix-less daily computes in machine-local wall clock (ADR-0126).
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 9, 0, 0, 0, time.Local)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 18, 10, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailyNextDay(t *testing.T) {
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 11, 0, 0, 0, time.Local)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 19, 10, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailyMidnight(t *testing.T) {
	s, err := ParseSchedule("daily at 00:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 23, 30, 0, 0, time.Local)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 19, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestParseScheduleDailyBareHour(t *testing.T) {
	s, err := ParseSchedule("daily at 11")
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != ScheduleSlot || s.Hour != 11 || s.Minute != 0 || s.UTC {
		t.Fatalf("got %+v", s)
	}
}

func TestParseScheduleDailyUTCSuffix(t *testing.T) {
	for _, raw := range []string{"daily at 11:00 utc", "daily at 11 UTC"} {
		s, err := ParseSchedule(raw)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", raw, err)
		}
		if s.Kind != ScheduleSlot || s.Hour != 11 || !s.UTC {
			t.Fatalf("ParseSchedule(%q) = %+v", raw, s)
		}
	}
}

func TestScheduleNextAfterDailyUTCSuffix(t *testing.T) {
	s, err := ParseSchedule("daily at 11:00 utc")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

// A UTC-parsed last-fired instant (as stored in run rows) must still yield a
// machine-local wall-clock slot for a suffix-less daily schedule — slot zone
// comes from the schedule, never the last-fired instant's location (ADR-0126).
func TestScheduleNextAfterDailyIgnoresLastFiredZone(t *testing.T) {
	s, err := ParseSchedule("daily at 11:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	got := s.NextAfter(last)
	if got.Location() != time.Local {
		t.Fatalf("slot location = %s, want Local", got.Location())
	}
	if got.Hour() != 11 || got.Minute() != 0 {
		t.Fatalf("slot wall clock = %02d:%02d, want 11:00 local", got.Hour(), got.Minute())
	}
}

func TestScheduleNextAfterSlotAnchorSameDay(t *testing.T) {
	// Manual anchor at 08:00 under every 2d at 10:00 → today 10:00, then 2-day cadence.
	s, err := ParseSchedule("every 2d at 10:00 utc")
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	first := s.NextAfter(anchor)
	wantFirst := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if !first.Equal(wantFirst) {
		t.Fatalf("first NextAfter = %s, want %s", first, wantFirst)
	}
	second := s.NextAfter(first)
	wantSecond := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	if !second.Equal(wantSecond) {
		t.Fatalf("second NextAfter = %s, want %s", second, wantSecond)
	}
}

func TestScheduleNextAfterSlotLateFireNoDrift(t *testing.T) {
	s, err := ParseSchedule("every 2d at 10:00 utc")
	if err != nil {
		t.Fatal(err)
	}
	late := time.Date(2026, 7, 18, 10, 7, 0, 0, time.UTC)
	got := s.NextAfter(late)
	want := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextAfter after late fire = %s, want %s (slot time, no drift)", got, want)
	}
}

func TestScheduleNextAfterBiweeklyWeekdaySnap(t *testing.T) {
	// every 2w on mon at 10:00 anchored Wednesday → next Monday, then 14-day cadence.
	s, err := ParseSchedule("every 2w on mon at 10:00 utc")
	if err != nil {
		t.Fatal(err)
	}
	wed := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC) // Wednesday
	if wed.Weekday() != time.Wednesday {
		t.Fatalf("fixture weekday = %s, want Wednesday", wed.Weekday())
	}
	first := s.NextAfter(wed)
	wantFirst := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC) // Monday
	if !first.Equal(wantFirst) {
		t.Fatalf("snap NextAfter = %s, want %s", first, wantFirst)
	}
	second := s.NextAfter(first)
	wantSecond := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	if !second.Equal(wantSecond) {
		t.Fatalf("cadence NextAfter = %s, want %s", second, wantSecond)
	}
}

func TestScheduleNextAfterSlotDSTBoundary(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	old := time.Local
	time.Local = loc
	defer func() { time.Local = old }()

	s, err := ParseSchedule("at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08 is US spring-forward in America/New_York.
	last := time.Date(2026, 3, 7, 10, 0, 0, 0, loc)
	got := s.NextAfter(last)
	want := time.Date(2026, 3, 8, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextAfter across DST = %s, want %s", got, want)
	}
	if got.Hour() != 10 || got.Minute() != 0 {
		t.Fatalf("wall clock = %02d:%02d, want 10:00 local", got.Hour(), got.Minute())
	}
	_, offBefore := last.Zone()
	_, offAfter := got.Zone()
	if offBefore == offAfter {
		t.Fatalf("expected DST offset change across boundary; both %d", offBefore)
	}
}
