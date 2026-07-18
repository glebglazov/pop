package queue

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
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
	// (finished/quota_paused/interrupted/crashed), "integrated", or "unparked".
	Kind   string
	Detail string
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
	routineRuns, err := tasks.AllRoutineRuns(td)
	if err != nil {
		return nil, err
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
		if dr.State == "quota_paused" && dr.ExhaustedPreset != "" {
			ev.Detail = "agent=" + dr.ExhaustedPreset
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

// RenderLog prints recent Queue journal events, most recent last.
func RenderLog(out io.Writer, events []LogEvent, limit int) {
	if limit <= 0 || limit > len(events) {
		limit = len(events)
	}
	start := len(events) - limit
	if start < 0 {
		start = 0
	}
	if len(events[start:]) == 0 {
		fmt.Fprintln(out, "No queue journal entries.")
		return
	}
	for _, ev := range events[start:] {
		ts := ev.Timestamp.UTC().Format(time.RFC3339)
		line := fmt.Sprintf("%s %s %s", ts, ev.SetID, ev.Kind)
		if ev.Detail != "" {
			line += " " + ev.Detail
		}
		fmt.Fprintln(out, line)
	}
}
