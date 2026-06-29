package queue

import (
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
	// UnverifiedSetID is the first Task-set in UNVERIFIED state (awaiting human
	// sign-off). Non-empty only when Reason is "awaiting verification".
	UnverifiedSetID    string
	Reason             string
	// BlockedSetID names the set whose abnormal backoff or parking produced
	// Reason; WaitUntil is when a backed-off set next becomes spawnable (zero for
	// a parked set). Both are derived from Drain history (ADR-0055).
	BlockedSetID       string
	WaitUntil          time.Time
	WorktreeReady      bool
	ProjectConfigError string
}

// SkippedRepo is a repository the Queue refused to schedule because it could
// resolve no representative checkout (a bare repo with no Trunk worktree and no
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
	DaemonState          *DaemonState
	ActiveAgentCooldowns map[string]time.Time
	Tasks                *tasks.Deps
	// CrashRetryDelays is the resolved abnormal-backoff escalation schedule (its
	// length is the park threshold). The run view derives each set's parked /
	// backed-off status from Drain history against it (ADR-0055).
	CrashRetryDelays []time.Duration
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
	snap, err := statusFromDecisions(d, decisions, state)
	if err != nil {
		return StatusSnapshot{}, err
	}
	cooldowns, err := tasks.ActiveAgentCooldownsWith(d.Tasks, d.now().UTC())
	if err != nil {
		return StatusSnapshot{}, err
	}
	snap.ActiveAgentCooldowns = cooldowns
	snap.Tasks = d.Tasks
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		snap.CrashRetryDelays = qcfg.CrashRetryDelays
	}
	return snap, nil
}

func statusFromDecisions(d *Deps, decisions []Decision, state *DaemonState) (StatusSnapshot, error) {
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
		idle := IdleProject{Project: dec.Project, Reason: dec.Reason, WorktreeReady: dec.WorktreeReady, ProjectConfigError: dec.ProjectConfigError, UnverifiedSetID: dec.UnverifiedSetID, BlockedSetID: dec.BlockedSetID, WaitUntil: dec.WaitUntil}
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
	return snap, nil
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

const DrainOutcomeCrashed tasks.DrainOutcome = "crashed"
