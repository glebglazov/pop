package routine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	everySchedulePattern = regexp.MustCompile(`(?i)^every\s+(\S+)\s*$`)
	dailySchedulePattern = regexp.MustCompile(`(?i)^daily\s+at\s+(\d{1,2})(?::(\d{2}))?(\s+utc)?\s*$`)
)

// ScheduleKind classifies a Routine schedule form.
type ScheduleKind int

const (
	ScheduleEvery ScheduleKind = iota
	ScheduleDaily
)

// Schedule is a parsed Routine schedule.
type Schedule struct {
	Raw      string
	Kind     ScheduleKind
	Interval time.Duration
	Hour     int
	Minute   int
	// UTC reports whether a daily schedule's slot is computed in UTC. When
	// false (the default), daily slots use machine-local wall clock. See
	// ADR-0126.
	UTC bool
}

// zone returns the location a daily schedule computes its slots in.
func (s Schedule) zone() *time.Location {
	if s.UTC {
		return time.UTC
	}
	return time.Local
}

// ParseSchedule parses the two supported Routine schedule forms.
func ParseSchedule(raw string) (Schedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Schedule{}, scheduleFormatError(raw)
	}

	if m := everySchedulePattern.FindStringSubmatch(raw); m != nil {
		dur, err := time.ParseDuration(m[1])
		if err != nil || dur <= 0 {
			return Schedule{}, fmt.Errorf("invalid schedule %q: duration after \"every\" must be positive (e.g. \"6h\", \"30m\")", raw)
		}
		return Schedule{
			Raw:      raw,
			Kind:     ScheduleEvery,
			Interval: dur,
		}, nil
	}

	if m := dailySchedulePattern.FindStringSubmatch(raw); m != nil {
		hour, errH := parseClockComponent(m[1], 23)
		minute := 0
		var errM error
		if m[2] != "" {
			minute, errM = parseClockComponent(m[2], 59)
		}
		if errH != nil || errM != nil {
			return Schedule{}, scheduleFormatError(raw)
		}
		return Schedule{
			Raw:    raw,
			Kind:   ScheduleDaily,
			Hour:   hour,
			Minute: minute,
			UTC:    m[3] != "",
		}, nil
	}

	return Schedule{}, scheduleFormatError(raw)
}

func parseClockComponent(s string, max int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 0 || n > max {
		return 0, fmt.Errorf("out of range")
	}
	return n, nil
}

func scheduleFormatError(raw string) error {
	return fmt.Errorf("invalid schedule %q: expected \"every <duration>\" (e.g. \"every 6h\") or \"daily at H[:MM][ utc]\" — local wall clock unless a \"utc\" suffix is given (e.g. \"daily at 11\", \"daily at 10:00\", \"daily at 11:00 utc\")", raw)
}

// NextAfter returns the next fire instant strictly after lastFired. A zero
// lastFired means the routine has never fired and the next fire is immediate.
func (s Schedule) NextAfter(lastFired time.Time) time.Time {
	switch s.Kind {
	case ScheduleEvery:
		if lastFired.IsZero() {
			return time.Now()
		}
		return lastFired.Add(s.Interval)
	case ScheduleDaily:
		loc := s.zone()
		if lastFired.IsZero() {
			lastFired = time.Now().In(loc)
		} else {
			lastFired = lastFired.In(loc)
		}
		candidate := time.Date(lastFired.Year(), lastFired.Month(), lastFired.Day(), s.Hour, s.Minute, 0, 0, loc)
		if !candidate.After(lastFired) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate
	default:
		return time.Time{}
	}
}
