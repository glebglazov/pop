package routine

import (
	"regexp"
	"strings"
)

// Routine runs adopt the tasks completion-sentinel contract (ADR-0127) with
// routine-specific names so a routine prompt that discusses tasks can't
// false-positive on TASK_*.
const (
	routineCompleteSentinel = "ROUTINE_COMPLETE"
	routineFailedSentinel   = "ROUTINE_FAILED"
)

var (
	// routineCompleteRE matches the completion sentinel anywhere in the output.
	// Leading \b only (no trailing) so a sentinel glued to prose still counts,
	// mirroring tasks/assess.go's tolerance.
	routineCompleteRE = regexp.MustCompile(`\bROUTINE_COMPLETE`)
	// routineFailedRE captures the fail reason after ROUTINE_FAILED, tolerant of
	// the sentinel glued to prose and of a missing colon. `.` stops at newline, so
	// the reason is the remainder of the sentinel's line.
	routineFailedRE = regexp.MustCompile(`\bROUTINE_FAILED:?[ \t]*(.*)`)
)

// routineOutcome is the sentinel-derived verdict of one routine agent run.
type routineOutcome struct {
	Succeeded  bool
	FailReason string
}

// assessRoutineOutput derives a run verdict from captured agent output and
// whether the report file landed on disk. Callers gate this behind a clean
// exit; nonzero exit / exec error / crash-reconcile / quota-exhaustion keep
// their own fail reasons and never reach here. Ladder (ADR-0127):
//   - ROUTINE_FAILED present     → failed, sentinel reason is the fail reason
//   - no ROUTINE_COMPLETE        → failed, "missing ROUTINE_COMPLETE sentinel"
//   - sentinel but no report file → failed, "missing report"
//   - ROUTINE_COMPLETE + report  → succeeded
func assessRoutineOutput(output string, reportExists bool) routineOutcome {
	trimmed := strings.TrimRight(output, " \t\r\n")

	if m := routineFailedRE.FindStringSubmatch(trimmed); m != nil {
		reason := strings.TrimSpace(m[1])
		if reason == "" {
			reason = "agent reported failure"
		}
		return routineOutcome{FailReason: reason}
	}

	if !routineCompleteRE.MatchString(trimmed) {
		return routineOutcome{FailReason: "missing ROUTINE_COMPLETE sentinel"}
	}

	if !reportExists {
		return routineOutcome{FailReason: "missing report"}
	}

	return routineOutcome{Succeeded: true}
}
