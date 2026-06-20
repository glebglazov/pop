package queue

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// PickedUpSet is a live in-flight drain derived from a runtime lock.
type PickedUpSet struct {
	Project            string
	SetID              string
	RuntimePath        string
	PID                int
	StartedAt          time.Time
	WorktreeReady      bool
	ProjectConfigError string
}

// IdleProject is a configured project with no live runtime lock.
type IdleProject struct {
	Project            string
	Waiting            string
	ReadySet           string
	Reason             string
	WorktreeReady      bool
	ProjectConfigError string
}

// AwaitingIntegrationSet is a DONE set whose branch has not been integrated by
// queue. Mergeability is advisory and recomputed when the DONE outcome is seen.
type AwaitingIntegrationSet struct {
	Project     string
	SetID       string
	RuntimePath string
	Status      string
	Target      string
	Source      string
	CheckedAt   time.Time
}

// SkippedRepo is a repository the Queue refused to schedule because it could
// resolve no representative checkout (a bare repo with no queue_base and no
// per-set Worktree binding). It is reported, never scheduled (ADR-0035).
type SkippedRepo struct {
	Project string
	Reason  string
}

// StatusSnapshot is the pure data model for `pop queue status`.
type StatusSnapshot struct {
	PickedUp             []PickedUpSet
	Idle                 []IdleProject
	Skipped              []SkippedRepo
	AwaitingIntegration  []AwaitingIntegrationSet
	DaemonState          *DaemonState
	ActiveAgentCooldowns map[string]time.Time
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
	snap := statusFromDecisions(decisions, state)
	cooldowns, err := tasks.ActiveAgentCooldownsWith(d.Tasks, d.now().UTC())
	if err != nil {
		return StatusSnapshot{}, err
	}
	snap.ActiveAgentCooldowns = cooldowns
	return snap, nil
}

func statusFromDecisions(decisions []Decision, state *DaemonState) StatusSnapshot {
	var snap StatusSnapshot
	snap.DaemonState = state
	for _, dec := range decisions {
		if dec.Busy {
			lock := dec.lockStatus
			picked := PickedUpSet{Project: dec.Project, WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError}
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
			snap.Idle = append(snap.Idle, IdleProject{Project: dec.Project, Waiting: "error", Reason: dec.Err.Error(), WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError})
			continue
		}
		if dec.TaskSetID == "" && dec.Reason == repoScanReason {
			snap.Skipped = append(snap.Skipped, SkippedRepo{Project: dec.Project, Reason: dec.Reason})
			continue
		}
		idle := IdleProject{Project: dec.Project, Reason: dec.Reason, WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError}
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
	sort.SliceStable(snap.Skipped, func(i, j int) bool { return snap.Skipped[i].Project < snap.Skipped[j].Project })
	if state != nil {
		for _, rec := range state.Mergeability {
			snap.AwaitingIntegration = append(snap.AwaitingIntegration, AwaitingIntegrationSet{
				Project:     rec.Project,
				SetID:       rec.SetID,
				RuntimePath: rec.RuntimePath,
				Status:      rec.Status,
				Target:      rec.Target,
				Source:      rec.Source,
				CheckedAt:   rec.CheckedAt,
			})
		}
		sort.SliceStable(snap.AwaitingIntegration, func(i, j int) bool {
			if snap.AwaitingIntegration[i].Project != snap.AwaitingIntegration[j].Project {
				return snap.AwaitingIntegration[i].Project < snap.AwaitingIntegration[j].Project
			}
			return snap.AwaitingIntegration[i].SetID < snap.AwaitingIntegration[j].SetID
		})
	}
	return snap
}

// RenderStatus prints a human-readable queue status snapshot.
func RenderStatus(out io.Writer, snap StatusSnapshot) {
	view := BuildRunView(snap, time.Now())
	RenderRunBaseline(out, view)
}

func statusProjectLabel(project string, worktreeReady bool, configError string) string {
	label := project
	if worktreeReady {
		label += " [worktree-ready]"
	}
	if configError != "" {
		label += " [.pop.toml error: " + configError + "]"
	}
	return label
}

func mergeabilityLabel(status string) string {
	switch status {
	case MergeabilityClean:
		return "merges clean"
	case MergeabilityConflicts:
		return "conflicts"
	default:
		return "mergeability unknown"
	}
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
		case JournalEventSpawnFailed:
			fmt.Fprintf(out, "%s %s %s spawn_failed reason=%s\n", ts, entry.Project, entry.SetID, entry.Reason)
		case JournalEventAgentSwitch:
			fmt.Fprintf(out, "%s %s %s default-agent=%s\n", ts, entry.Project, entry.SetID, entry.Agent)
		case JournalEventAgentCooldown:
			fmt.Fprintf(out, "%s %s %s cooldown agent=%s until=%s reason=%s\n", ts, entry.Project, entry.SetID, entry.Agent, entry.Until.UTC().Format(time.RFC3339), entry.Reason)
		case JournalEventAgentUnavailable:
			fmt.Fprintf(out, "%s %s %s unavailable agent=%s reason=%s\n", ts, entry.Project, entry.SetID, entry.Agent, entry.Reason)
		case JournalEventSetParked:
			fmt.Fprintf(out, "%s %s %s parked reason=%s\n", ts, entry.Project, entry.SetID, entry.Reason)
		case JournalEventMergeability:
			fmt.Fprintf(out, "%s %s %s mergeability=%s\n", ts, entry.Project, entry.SetID, entry.MergeStatus)
		case JournalEventIntegrated:
			fmt.Fprintf(out, "%s %s %s integrated\n", ts, entry.Project, entry.SetID)
		case JournalEventAbandoned:
			fmt.Fprintf(out, "%s %s %s abandoned branch=%s\n", ts, entry.Project, entry.SetID, entry.SourceRef)
		default:
			fmt.Fprintf(out, "%s %s %s %s\n", ts, entry.Project, entry.SetID, entry.Event)
		}
	}
}

const DrainOutcomeCrashed tasks.DrainOutcome = "crashed"
