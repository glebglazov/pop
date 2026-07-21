package routine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// dailySchedulePattern is a permanent parse-only alias for the slot form
	// `at H[:MM][ utc]` (step 1d, all weekdays). Manifests store the raw
	// string unchanged — never rewrite aliases (ADR-0133).
	dailySchedulePattern = regexp.MustCompile(`(?i)^daily\s+at\s+(\d{1,2})(?::(\d{2}))?(\s+utc)?\s*$`)

	everyClauseRe = regexp.MustCompile(`(?i)^every\s+(\d+)([hmdw])(?:\s+|$)`)
	onClauseRe    = regexp.MustCompile(`(?i)^on\s+(\S+)(?:\s+|$)`)
	atClauseRe    = regexp.MustCompile(`(?i)^at\s+(\d{1,2})(?::(\d{2}))?(?:\s+|$)`)
	utcClauseRe   = regexp.MustCompile(`(?i)^utc\s*$`)
	clauseWordRe  = regexp.MustCompile(`(?i)\b(every|on|at|utc)\b`)
)

// ScheduleKind classifies a Routine schedule form.
type ScheduleKind int

const (
	// ScheduleEvery is a rolling schedule: next fire is lastFired + Interval.
	ScheduleEvery ScheduleKind = iota
	// ScheduleSlot is a calendar slot: day step, weekday mask, and time of day.
	ScheduleSlot
)

// ScheduleGrammar is the canonical user-facing description of the routine
// schedule clause production (ADR-0133). Every surface that advertises valid
// schedule syntax must read from this constant.
const ScheduleGrammar = "[every <N><unit>] [on <days>] [at H[:MM]] [utc] — at least one clause required; e.g. \"every 6h\", \"at 10:00\", \"on mon-fri at 09:00\", \"every 2d at 10:00\", \"every 2w on mon at 10:00\"; wall-clock forms use the machine's local time unless suffixed \"utc\""

// allWeekdays is the 7-bit mask with every weekday set (bit i = time.Weekday(i)).
const allWeekdays uint8 = 0x7f

// Schedule is a parsed Routine schedule.
type Schedule struct {
	Raw      string
	Kind     ScheduleKind
	Interval time.Duration // ScheduleEvery only
	StepDays int           // ScheduleSlot: calendar step in days
	Weekdays uint8         // ScheduleSlot: 7-bit mask, bit i = time.Weekday(i)
	Hour     int
	Minute   int
	// UTC reports whether a slot schedule's slot is computed in UTC. When
	// false (the default), slots use machine-local wall clock. See ADR-0126.
	UTC bool
}

// zone returns the location a slot schedule computes its slots in.
func (s Schedule) zone() *time.Location {
	if s.UTC {
		return time.UTC
	}
	return time.Local
}

// ParseSchedule parses the Routine schedule clause production
// [every <N><unit>] [on <days>] [at H[:MM]] [utc], or the permanent
// parse-only alias daily at H[:MM][ utc].
func ParseSchedule(raw string) (Schedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Schedule{}, scheduleFormatError(raw)
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
			Raw:      raw,
			Kind:     ScheduleSlot,
			StepDays: 1,
			Weekdays: allWeekdays,
			Hour:     hour,
			Minute:   minute,
			UTC:      m[3] != "",
		}, nil
	}

	return parseClauseProduction(raw)
}

func parseClauseProduction(raw string) (Schedule, error) {
	rest := raw

	var (
		hasEvery, hasOn, hasAt, hasUTC bool
		everyN                         int
		everyUnit                      byte
		daysRaw                        string
		hour, minute                   int
	)

	if m := everyClauseRe.FindStringSubmatch(rest); m != nil {
		if _, err := fmt.Sscanf(m[1], "%d", &everyN); err != nil || everyN <= 0 {
			return Schedule{}, fmt.Errorf("invalid schedule %q: duration after \"every\" must be positive (e.g. \"6h\", \"30m\")", raw)
		}
		everyUnit = strings.ToLower(m[2])[0]
		hasEvery = true
		rest = strings.TrimSpace(rest[len(m[0]):])
	}

	if m := onClauseRe.FindStringSubmatch(rest); m != nil {
		daysRaw = m[1]
		hasOn = true
		rest = strings.TrimSpace(rest[len(m[0]):])
	}

	if m := atClauseRe.FindStringSubmatch(rest); m != nil {
		var errH, errM error
		hour, errH = parseClockComponent(m[1], 23)
		if m[2] != "" {
			minute, errM = parseClockComponent(m[2], 59)
		}
		if errH != nil || errM != nil {
			return Schedule{}, scheduleFormatError(raw)
		}
		hasAt = true
		rest = strings.TrimSpace(rest[len(m[0]):])
	}

	if m := utcClauseRe.FindStringSubmatch(rest); m != nil {
		hasUTC = true
		rest = ""
	}

	if rest != "" {
		if clausesOutOfOrder(raw) {
			return Schedule{}, outOfOrderScheduleError(raw)
		}
		return Schedule{}, scheduleFormatError(raw)
	}

	if !hasEvery && !hasOn && !hasAt {
		return Schedule{}, scheduleFormatError(raw)
	}

	// Rolling: every <N>h|m with no other clauses.
	if hasEvery && (everyUnit == 'h' || everyUnit == 'm') {
		if hasOn || hasAt {
			return Schedule{}, hmWithClauseError(raw)
		}
		if hasUTC {
			return Schedule{}, scheduleFormatError(raw)
		}
		var interval time.Duration
		if everyUnit == 'h' {
			interval = time.Duration(everyN) * time.Hour
		} else {
			interval = time.Duration(everyN) * time.Minute
		}
		return Schedule{
			Raw:      raw,
			Kind:     ScheduleEvery,
			Interval: interval,
		}, nil
	}

	step := 1
	if hasEvery {
		switch everyUnit {
		case 'd':
			step = everyN
		case 'w':
			step = everyN * 7
		default:
			return Schedule{}, scheduleFormatError(raw)
		}
	}

	mask := allWeekdays
	if hasOn {
		var err error
		mask, err = parseDayMask(raw, daysRaw)
		if err != nil {
			return Schedule{}, err
		}
	}

	return Schedule{
		Raw:      raw,
		Kind:     ScheduleSlot,
		StepDays: step,
		Weekdays: mask,
		Hour:     hour,
		Minute:   minute,
		UTC:      hasUTC,
	}, nil
}

func clausesOutOfOrder(raw string) bool {
	rank := map[string]int{"every": 0, "on": 1, "at": 2, "utc": 3}
	last := -1
	for _, m := range clauseWordRe.FindAllStringSubmatchIndex(raw, -1) {
		word := strings.ToLower(raw[m[2]:m[3]])
		r := rank[word]
		if r < last {
			return true
		}
		last = r
	}
	return false
}

func parseDayMask(raw, days string) (uint8, error) {
	lower := strings.ToLower(strings.TrimSpace(days))
	if lower == "weekdays" {
		return weekdayMask(time.Monday, time.Friday), nil
	}
	if lower == "weekends" {
		return (1 << time.Saturday) | (1 << time.Sunday), nil
	}

	parts := strings.Split(lower, ",")
	if len(parts) > 1 {
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "weekdays" || p == "weekends" {
				return 0, sugarInListError(raw)
			}
		}
	}

	var mask uint8
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return 0, scheduleFormatError(raw)
		}
		if strings.Contains(p, "-") {
			a, b, ok := strings.Cut(p, "-")
			if !ok || a == "" || b == "" || strings.Contains(b, "-") {
				return 0, scheduleFormatError(raw)
			}
			start, err := parseWeekdayName(a)
			if err != nil {
				return 0, scheduleFormatError(raw)
			}
			end, err := parseWeekdayName(b)
			if err != nil {
				return 0, scheduleFormatError(raw)
			}
			if start > end {
				return 0, wrappingRangeError(raw)
			}
			for d := start; d <= end; d++ {
				mask |= 1 << d
			}
			continue
		}
		day, err := parseWeekdayName(p)
		if err != nil {
			return 0, scheduleFormatError(raw)
		}
		mask |= 1 << day
	}
	if mask == 0 {
		return 0, scheduleFormatError(raw)
	}
	return mask, nil
}

func weekdayMask(from, to time.Weekday) uint8 {
	var mask uint8
	for d := from; d <= to; d++ {
		mask |= 1 << d
	}
	return mask
}

func parseWeekdayName(name string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sun", "sunday":
		return time.Sunday, nil
	case "mon", "monday":
		return time.Monday, nil
	case "tue", "tues", "tuesday":
		return time.Tuesday, nil
	case "wed", "wednesday":
		return time.Wednesday, nil
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, nil
	case "fri", "friday":
		return time.Friday, nil
	case "sat", "saturday":
		return time.Saturday, nil
	default:
		return 0, fmt.Errorf("unknown weekday")
	}
}

func parseClockComponent(s string, max int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 0 || n > max {
		return 0, fmt.Errorf("out of range")
	}
	return n, nil
}

func scheduleFormatError(raw string) error {
	return fmt.Errorf("invalid schedule %q: expected %s", raw, ScheduleGrammar)
}

func hmWithClauseError(raw string) error {
	return fmt.Errorf("invalid schedule %q: \"h\"/\"m\" intervals cannot carry \"on\"/\"at\" clauses; use a day-step form (e.g. \"every 2d at 10:00\") or a rolling form without those clauses (e.g. \"every 6h\")", raw)
}

func wrappingRangeError(raw string) error {
	return fmt.Errorf("invalid schedule %q: weekday ranges do not wrap (e.g. \"on fri-mon\"); write a comma list instead (e.g. \"on fri,sat,sun,mon\")", raw)
}

func sugarInListError(raw string) error {
	return fmt.Errorf("invalid schedule %q: \"weekdays\"/\"weekends\" cannot mix into a list; write \"on mon-fri,sun\" instead of \"on weekdays,sun\"", raw)
}

func outOfOrderScheduleError(raw string) error {
	return fmt.Errorf("invalid schedule %q: clauses must appear in order: [every <N><unit>] [on <days>] [at H[:MM]] [utc]", raw)
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
	case ScheduleSlot:
		loc := s.zone()
		if lastFired.IsZero() {
			lastFired = time.Now().In(loc)
		} else {
			lastFired = lastFired.In(loc)
		}
		candidate := time.Date(lastFired.Year(), lastFired.Month(), lastFired.Day(), s.Hour, s.Minute, 0, 0, loc)
		if !candidate.After(lastFired) {
			candidate = candidate.AddDate(0, 0, s.StepDays)
		}
		for i := 0; i < 7; i++ {
			if s.Weekdays&(1<<uint(candidate.Weekday())) != 0 {
				return candidate
			}
			candidate = candidate.AddDate(0, 0, 1)
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}
