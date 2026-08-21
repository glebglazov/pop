package supervisor

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// LogSeverity ranks a journal event by whether a human coming back to the
// machine has to see it. The journal is otherwise flat, which makes the one
// question worth asking after a night away — "what went wrong while I was
// gone?" — a read of the whole history (ADR-0231).
type LogSeverity int

const (
	// SeverityRoutine is pop working: a drain that finished, a park it comes back
	// from on its own, an agent that stepped aside for the next one. It is the
	// severity of every event that costs nothing.
	SeverityRoutine LogSeverity = iota
	// SeveritySevere is a drain that spent an agent list rather than using it: an
	// agent that burned its whole retry budget without finishing, or a walk that
	// could not start a single agent. Both leave the machine idle until a human
	// looks, which is the only reason to rank an event at all.
	SeveritySevere
)

// LogEvent is one entry in the Queue journal view. ADR-0055 retires the
// standalone append-only journal file: the journal is now a view derived from
// Drain transitions (each Drain contributes a spawn and, once terminal, an
// outcome) plus the integration and park-clear events already in the store.
type LogEvent struct {
	Timestamp   time.Time
	SetID       string
	RuntimePath string
	// Kind is the rendered event: "spawned", a terminal exit reason
	// (finished/quota_paused/interrupted/crashed), a Drain ending that the exit
	// reason cannot say on its own (agents_exhausted/no_agent_started),
	// "integrated", or "unparked".
	Kind string
	// Agent names the agent preset the event belongs to, where one does: the
	// preset a quota pause is waiting on, the one that spent the last retry cap,
	// or the one whose refusal ended a walk that started nothing. It is rendered
	// beside the set id so a severe entry says which agent and which task set
	// without the reader opening anything else.
	Agent    string
	Detail   string
	Severity LogSeverity
}

// BuildLog derives the Queue journal view from the store: every Drain's spawn
// and terminal, every integration event, and every park-clear (unpark) event,
// ordered oldest-first by timestamp.
func BuildLog(td *tasks.Deps) ([]LogEvent, error) {
	drains, err := tasks.AllDrains(td)
	if err != nil {
		return nil, err
	}
	integrations, err := tasks.AllIntegrationEvents(td)
	if err != nil {
		return nil, err
	}
	parkClears, err := tasks.AllParkClears(td)
	if err != nil {
		return nil, err
	}
	// Routine runs are read directly off the store handle borrowed through the
	// tasks accessor in if-exists mode: a machine without a store yields an empty
	// contribution without creating the database, and the shared handle is never
	// closed (closing it would poison the process cache for later store calls).
	var routineRuns []store.RoutineRun
	if s, ok, storeErr := td.Store(false); storeErr != nil {
		return nil, storeErr
	} else if ok {
		routineRuns, err = s.ListAllRoutineRuns()
		if err != nil {
			return nil, err
		}
	}

	var events []LogEvent
	for _, dr := range drains {
		events = append(events, LogEvent{
			Timestamp:   dr.StartedAt,
			SetID:       dr.SetID,
			RuntimePath: dr.RuntimePath,
			Kind:        "spawned",
		})
		if dr.Running() || dr.FinishedAt.IsZero() {
			continue
		}
		ev := LogEvent{
			Timestamp:   dr.FinishedAt,
			SetID:       dr.SetID,
			RuntimePath: dr.RuntimePath,
			Kind:        dr.State,
		}
		if dr.State == store.StateQuotaPaused {
			ev.Agent = dr.ExhaustedPreset
		}
		if dr.Ending != "" {
			// The Drain ending is the event: a walk that ran out of agents and one
			// that started none both stop on an ordinary clean-finish exit reason,
			// so reading the state column would file them beside a healthy drain
			// (ADR-0231).
			ev.Kind = dr.Ending
			ev.Agent = dr.ExhaustedPreset
			ev.Severity = SeveritySevere
		}
		events = append(events, ev)
	}
	for _, in := range integrations {
		detail := ""
		if in.BaseRef != "" {
			detail = "base=" + in.BaseRef
		}
		events = append(events, LogEvent{
			Timestamp: in.IntegratedAt,
			SetID:     in.SetID,
			Kind:      "integrated",
			Detail:    detail,
		})
	}
	for _, pc := range parkClears {
		events = append(events, LogEvent{
			Timestamp: pc.ClearedAt,
			SetID:     pc.SetID,
			Kind:      "unparked",
		})
	}
	for _, run := range routineRuns {
		appendRoutineLogEvents(&events, run)
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events, nil
}

func appendRoutineLogEvents(events *[]LogEvent, run store.RoutineRun) {
	if run.Outcome == store.RoutineRunSkipped {
		*events = append(*events, LogEvent{
			Timestamp: run.FiredAt,
			SetID:     run.RoutineID,
			Kind:      "skipped",
			Detail:    run.SkipReason,
		})
		return
	}
	*events = append(*events, LogEvent{
		Timestamp: run.FiredAt,
		SetID:     run.RoutineID,
		Kind:      "fired",
	})
	if run.Outcome == store.RoutineRunRunning || run.FinishedAt.IsZero() {
		return
	}
	*events = append(*events, LogEvent{
		Timestamp: run.FinishedAt,
		SetID:     run.RoutineID,
		Kind:      run.Outcome,
		Detail:    run.FailReason,
	})
}

// SevereEvents returns the severe events at or after cutoff, oldest-first — the
// short list behind "what went wrong while I was away?". An ordinary
// fall-through, where one agent stepped aside and the next completed the work,
// leaves no severe event to report (ADR-0231).
func SevereEvents(events []LogEvent, cutoff time.Time) []LogEvent {
	var out []LogEvent
	for _, ev := range events {
		if ev.Severity != SeveritySevere || ev.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// RenderSevereLog prints the severe events of the window ending at now, saying
// so plainly when there are none — an empty list is the answer a human wants
// most, and it must not read like a journal with nothing in it.
func RenderSevereLog(out io.Writer, events []LogEvent, window time.Duration, now time.Time) {
	severe := SevereEvents(events, now.Add(-window))
	if len(severe) == 0 {
		fmt.Fprintf(out, "No severe events in the last %s.\n", window)
		return
	}
	for _, ev := range severe {
		fmt.Fprintln(out, formatLogEvent(ev))
	}
}

// RenderLog prints recent Work journal events, most recent last.
func RenderLog(out io.Writer, events []LogEvent, limit int) {
	if limit <= 0 || limit > len(events) {
		limit = len(events)
	}
	start := len(events) - limit
	if start < 0 {
		start = 0
	}
	if len(events[start:]) == 0 {
		fmt.Fprintln(out, "No daemon journal entries.")
		return
	}
	for _, ev := range events[start:] {
		fmt.Fprintln(out, formatLogEvent(ev))
	}
}

// formatLogEvent renders one journal line. It is one format for both surfaces —
// the whole journal and the severe listing — so an entry read in either place
// names the same things: the instant, the severity where it is not routine, the
// task set, the event, and the agent it belongs to.
func formatLogEvent(ev LogEvent) string {
	line := ev.Timestamp.UTC().Format(time.RFC3339)
	if ev.Severity == SeveritySevere {
		line += " SEVERE"
	}
	line += fmt.Sprintf(" %s %s", ev.SetID, ev.Kind)
	if ev.Agent != "" {
		line += " agent=" + ev.Agent
	}
	if ev.Detail != "" {
		line += " " + ev.Detail
	}
	return line
}
