package tasks

import "fmt"

// TaskStatus is the typed status of a single task within a manifest, mirroring
// the derived TaskSetStatus. The four constants are the only legal persisted
// task statuses; the transition chokepoint keys legality on them.
type TaskStatus string

const (
	TaskOpen    TaskStatus = "open"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
	TaskSkipped TaskStatus = "skipped"
)

// TransitionActor is the party driving a task transition. Legality is keyed by
// (from, to, actor): the Task executor may drive only agent-owned edges, the
// human may drive the manual-repair edges (ADR-0109, "Task transition").
type TransitionActor string

const (
	ActorExecutor TransitionActor = "executor"
	ActorHuman    TransitionActor = "human"
)

// transitionEdge is one (from, to, actor) key in the legality table.
type transitionEdge struct {
	From  TaskStatus
	To    TaskStatus
	Actor TransitionActor
}

// legalTransitions is the transition table: exactly the edges each actor may
// drive. The Task executor may drive only open→done and open→failed; the human
// — via Complete task, Skip, and Open task — may drive the manual-repair edges.
// Anything absent from this table is an illegal edge.
var legalTransitions = map[transitionEdge]bool{
	// Task executor
	{From: TaskOpen, To: TaskDone, Actor: ActorExecutor}:   true,
	{From: TaskOpen, To: TaskFailed, Actor: ActorExecutor}: true,
	// Human
	{From: TaskOpen, To: TaskDone, Actor: ActorHuman}:    true,
	{From: TaskFailed, To: TaskOpen, Actor: ActorHuman}:  true,
	{From: TaskFailed, To: TaskDone, Actor: ActorHuman}:  true,
	{From: TaskOpen, To: TaskSkipped, Actor: ActorHuman}: true,
	{From: TaskSkipped, To: TaskOpen, Actor: ActorHuman}: true,
	{From: TaskSkipped, To: TaskDone, Actor: ActorHuman}: true,
	{From: TaskDone, To: TaskOpen, Actor: ActorHuman}:    true,
}

// TransitionOp is one task status change requested against a manifest through
// the transition chokepoint.
type TransitionOp struct {
	// TaskID names the task within the manifest.
	TaskID string
	// To is the target status.
	To TaskStatus
	// Actor is the party driving the transition.
	Actor TransitionActor
	// Marker is the progress record outcome marker (e.g. "COMPLETE").
	Marker string
	// Summary is the progress record body.
	Summary string
	// AttemptCount is the recorded attempt count written when To == TaskFailed.
	// It is ignored for every other target status, which clear the count.
	AttemptCount int
}

// ApplyTransitions is the single Task-transition chokepoint through which
// task-status writes flow (ADR-0109). It validates every op's (from, to, actor)
// edge against the transition table, rejecting the whole batch — writing
// nothing — on the first illegal edge or unknown task. It then, in order:
// appends one progress record per op (marker + summary), applies each op's new
// status with attempt-count bookkeeping (the recorded attempt count is set when
// entering failed and cleared on every other target status), and performs
// exactly one atomic manifest write for the whole batch (a single op is a batch
// of one). Verb-level preconditions and verification invalidation stay at the
// caller; this owns only edge legality and the atomic write.
func ApplyTransitions(d *Deps, m *Manifest, ops []TransitionOp) error {
	if m == nil {
		return fmt.Errorf("apply transitions: nil manifest")
	}

	indexByID := make(map[string]int, len(m.Tasks))
	for i, task := range m.Tasks {
		indexByID[task.ID] = i
	}

	// Pass 1: resolve and validate every edge before any write.
	idxs := make([]int, len(ops))
	for i, op := range ops {
		idx, ok := indexByID[op.TaskID]
		if !ok {
			return fmt.Errorf("apply transitions: unknown task %q", op.TaskID)
		}
		idxs[i] = idx
		from := TaskStatus(m.Tasks[idx].Status)
		if !legalTransitions[transitionEdge{From: from, To: op.To, Actor: op.Actor}] {
			return fmt.Errorf("illegal task transition %s→%s by %s (task %q)", from, op.To, op.Actor, op.TaskID)
		}
	}

	// Pass 2: append progress records first, matching the single-verb ordering so
	// a crash between the records and the write leaves a recoverable trail.
	for i, op := range ops {
		idx := idxs[i]
		if err := AppendProgress(d, m.Dir, m.Tasks[idx].File, op.Marker, op.Summary); err != nil {
			return manualRepairErr(err)
		}
	}

	// Apply status + attempt bookkeeping, then one atomic manifest write.
	for i, op := range ops {
		idx := idxs[i]
		m.Tasks[idx].Status = string(op.To)
		if op.To == TaskFailed {
			count := op.AttemptCount
			m.Tasks[idx].FailedAfter = &count
		} else {
			m.Tasks[idx].FailedAfter = nil
		}
	}
	if err := WriteManifestAtomic(d, m); err != nil {
		return manualRepairErr(fmt.Errorf("update manifest after transition progress: %w", err))
	}
	return nil
}
