package routine

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// A Routine's read-surface status: the one label every surface shows for it,
// derived from the execution-state store and the pause bit. It is derivation
// only — no renderer, no table — because the surfaces that show it are the Work
// dashboard's page B and `pop routine list`, neither of which lives here.

// statusFor derives a Routine's STATUS label alongside the absolute path of its
// latest run's report (empty when never-fired, skipped, or still running — the
// running row itself has no report yet). It keys the store lookup on storeID and
// derives the idle label from m, so a Project routine (empty manifest) shares the
// same seam yet never surfaces a paused status.
func statusFor(d *Deps, s *store.Store, storeID string, m Manifest) (string, string) {
	last, lastErr := s.LastRoutineRun(storeID)
	reportPath := ""
	if lastErr == nil && last != nil {
		reportPath = last.ReportPath
	}
	live, err := s.LiveRoutineRun(storeID, func(run store.RoutineRun) bool {
		return routineProcessAlive(d, run.PID, run.ProcStart)
	})
	if err != nil {
		return idleStatus(m, ""), reportPath
	}
	if live != nil {
		return "running", reportPath
	}
	outcome := ""
	if lastErr == nil && last != nil {
		outcome = last.Outcome
	}
	return idleStatus(m, outcome), reportPath
}

// idleStatus renders the STATUS label for a Routine that is not live. A pause
// wins over everything; otherwise an idle Routine surfaces its last run's
// terminal outcome (ok/failed) and a never-fired Routine stays plain idle.
func idleStatus(m Manifest, lastOutcome string) string {
	if m.Paused {
		return pausedStatusLabel(m.PauseReason)
	}
	switch lastOutcome {
	case store.RoutineRunSucceeded:
		return "ok"
	case store.RoutineRunFailed:
		return "failed"
	}
	return "idle"
}

// formatLastRun renders a fire instant for a LAST RUN cell, or `never` for a
// Routine that has not fired.
func formatLastRun(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format("2006-01-02 15:04")
}
