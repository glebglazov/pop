package routine

import (
	"strings"
	"testing"
	"time"
)

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

func TestParseScheduleDaily(t *testing.T) {
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != ScheduleDaily || s.Hour != 10 || s.Minute != 0 {
		t.Fatalf("got %+v", s)
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
	if s.Kind != ScheduleDaily || s.Hour != 11 || s.Minute != 0 || s.UTC {
		t.Fatalf("got %+v", s)
	}
}

func TestParseScheduleDailyUTCSuffix(t *testing.T) {
	for _, raw := range []string{"daily at 11:00 utc", "daily at 11 UTC"} {
		s, err := ParseSchedule(raw)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", raw, err)
		}
		if s.Kind != ScheduleDaily || s.Hour != 11 || !s.UTC {
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
