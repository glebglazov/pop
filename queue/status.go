package queue

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// PickedUpSet is a live in-flight drain derived from a runtime lock.
type PickedUpSet struct {
	Project     string
	SetID       string
	RuntimePath string
	PID         int
	StartedAt   time.Time
}

// IdleProject is a configured project with no live runtime lock.
type IdleProject struct {
	Project  string
	Waiting  string
	ReadySet string
	Reason   string
}

// StatusSnapshot is the pure data model for `pop queue status`.
type StatusSnapshot struct {
	PickedUp    []PickedUpSet
	Idle        []IdleProject
	DaemonState *DaemonState
}

// BuildStatus derives queue status from on-disk lock/state truth.
func BuildStatus(d *Deps, cfg *config.Config) (StatusSnapshot, error) {
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return StatusSnapshot{}, err
	}
	decisions, err := Scan(d, cfg)
	if err != nil {
		return StatusSnapshot{}, err
	}
	return statusFromDecisions(decisions, state), nil
}

func statusFromDecisions(decisions []Decision, state *DaemonState) StatusSnapshot {
	var snap StatusSnapshot
	snap.DaemonState = state
	for _, dec := range decisions {
		if dec.Busy {
			lock := dec.lockStatus
			picked := PickedUpSet{Project: dec.Project}
			if lock != nil {
				picked.RuntimePath = lock.RuntimePath
				if lock.Metadata != nil {
					picked.SetID = lock.Metadata.SetID
					picked.PID = lock.Metadata.PID
					picked.StartedAt = lock.Metadata.StartedAt
					picked.RuntimePath = lock.Metadata.RuntimePath
				}
			}
			snap.PickedUp = append(snap.PickedUp, picked)
			continue
		}
		if dec.Err != nil {
			snap.Idle = append(snap.Idle, IdleProject{Project: dec.Project, Waiting: "error", Reason: dec.Err.Error()})
			continue
		}
		idle := IdleProject{Project: dec.Project, Reason: dec.Reason}
		if dec.TaskSetID != "" {
			idle.Waiting = "ready"
			idle.ReadySet = dec.TaskSetID
		} else {
			idle.Waiting = "idle"
		}
		snap.Idle = append(snap.Idle, idle)
	}
	sort.SliceStable(snap.PickedUp, func(i, j int) bool { return snap.PickedUp[i].Project < snap.PickedUp[j].Project })
	sort.SliceStable(snap.Idle, func(i, j int) bool { return snap.Idle[i].Project < snap.Idle[j].Project })
	return snap
}

// RenderStatus prints a human-readable queue status snapshot.
func RenderStatus(out io.Writer, snap StatusSnapshot) {
	fmt.Fprintln(out, "Picked-up sets:")
	if len(snap.PickedUp) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, p := range snap.PickedUp {
			setID := p.SetID
			if setID == "" {
				setID = "(unknown set)"
			}
			started := ""
			if !p.StartedAt.IsZero() {
				started = " since " + p.StartedAt.UTC().Format(time.RFC3339)
			}
			pid := ""
			if p.PID > 0 {
				pid = fmt.Sprintf(" pid=%d", p.PID)
			}
			fmt.Fprintf(out, "  %s: %s%s%s\n", p.Project, setID, pid, started)
		}
	}

	fmt.Fprintln(out, "Idle/waiting projects:")
	if len(snap.Idle) == 0 {
		fmt.Fprintln(out, "  none")
	} else {
		for _, idle := range snap.Idle {
			switch {
			case idle.ReadySet != "":
				fmt.Fprintf(out, "  %s: waiting ready set %s\n", idle.Project, idle.ReadySet)
			case idle.Waiting == "error":
				fmt.Fprintf(out, "  %s: error: %s\n", idle.Project, idle.Reason)
			default:
				fmt.Fprintf(out, "  %s: idle (%s)\n", idle.Project, idle.Reason)
			}
		}
	}

	fmt.Fprintln(out, "Daemon state:")
	if snap.DaemonState == nil {
		fmt.Fprintln(out, "  null")
		return
	}
	payload, err := json.MarshalIndent(snap.DaemonState, "  ", "  ")
	if err != nil {
		fmt.Fprintf(out, "  error: %v\n", err)
		return
	}
	fmt.Fprintln(out, string(payload))
}

// RenderLog prints recent queue journal history.
func RenderLog(out io.Writer, entries []JournalEntry, limit int) {
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	start := len(entries) - limit
	if start < 0 {
		start = 0
	}
	if len(entries[start:]) == 0 {
		fmt.Fprintln(out, "No queue journal entries.")
		return
	}
	for _, entry := range entries[start:] {
		ts := entry.Timestamp.UTC().Format(time.RFC3339)
		switch entry.Event {
		case JournalEventOutcome:
			fmt.Fprintf(out, "%s %s %s outcome=%s\n", ts, entry.Project, entry.SetID, entry.Outcome)
		case JournalEventSpawn:
			source := ""
			if entry.Source != "" {
				source = " source=" + entry.Source
			}
			fmt.Fprintf(out, "%s %s %s spawned%s\n", ts, entry.Project, entry.SetID, source)
		default:
			fmt.Fprintf(out, "%s %s %s %s\n", ts, entry.Project, entry.SetID, entry.Event)
		}
	}
}

const DrainOutcomeCrashed tasks.DrainOutcome = "crashed"
