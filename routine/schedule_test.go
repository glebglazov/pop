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
	loc := time.FixedZone("test", -5*3600)
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 9, 0, 0, 0, loc)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 18, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailyNextDay(t *testing.T) {
	loc := time.FixedZone("test", -5*3600)
	s, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 11, 0, 0, 0, loc)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 19, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailyMidnight(t *testing.T) {
	loc := time.FixedZone("test", 0)
	s, err := ParseSchedule("daily at 00:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 23, 30, 0, 0, loc)
	got := s.NextAfter(last)
	want := time.Date(2026, 7, 19, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}

func TestScheduleNextAfterDailyDSTAgnosticWallClock(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSchedule("daily at 02:30")
	if err != nil {
		t.Fatal(err)
	}
	// Day before US spring-forward 2026 (Mar 8): keep 02:30 wall time next day.
	last := time.Date(2026, 3, 7, 3, 0, 0, 0, loc)
	got := s.NextAfter(last)
	want := time.Date(2026, 3, 8, 2, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextAfter = %s, want %s", got, want)
	}
}
