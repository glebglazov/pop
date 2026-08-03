package drain

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TaskSetKind is the Task-set Work kind as the supervisor and every read surface
// see it: the setkind adapter for read-and-render, wearing the advance seam on
// top. Both wiring lists build it through here, so the invariant the supervisor
// depends on — the task-set entry of any list advances, the Map entry does not —
// holds wherever the list is assembled.
//
// The advance half lives in queue rather than beside the read half because the
// Task-set drain pipeline is here: the scan, the readiness selector's deferrals,
// worktree routing and the tmux spawn. Moving that pipeline out is the queue
// package split's job, and this composition is what lets the seam land first
// without moving any file twice.
func (d *Deps) TaskSetKind(cfg *config.Config, groups func() ([]repogroup.Group, error)) work.Kind {
	return &taskSetKind{Kind: setkind.New(d.SetKindDeps(cfg, groups)), d: d, cfg: cfg}
}

// taskSetKind carries one supervisor pass's state beside the kind: the decisions
// behind this pass's candidates, and the checkouts it has already dispatched
// into. A fresh kind list is built per tick, which is what keeps that state
// tick-scoped; one pass drives it at a time, so it takes no lock.
type taskSetKind struct {
	work.Kind
	d   *Deps
	cfg *config.Config

	// pending holds the Decision behind each advance candidate this pass
	// surfaced. Advance looks its candidate up here rather than re-deriving it,
	// which would both cost a second scan and race the one the candidate was read
	// from.
	pending map[ref.WorkRef]Decision
	// dispatched is the per-checkout dispatch ledger for this pass (ADR-0070/0072):
	// each drain routes to its own checkout, so several of a repo's Ready sets
	// dispatch in one tick, but two drains routed to the *same* checkout would race
	// two implements into it — the first wins and the rest defer to a later tick.
	dispatched map[string]bool
	spawned    []PickedUpSet
}

// Reconcile runs the crash-detection pass the read surfaces no longer run
// (ADR-0055): dead-PID running Drains become crashed, and the dead-owner gate
// holds, spawn intents and recovery waiters they orphaned are swept. The failure
// is reported to ReconcileOut kind-side and also returned, because reconciliation
// is opportunistic — the supervisor reads the pre-reconcile snapshot rather than
// abandoning the tick.
func (k *taskSetKind) Reconcile() error {
	return k.d.reconcile()
}

// Candidates scans every registered project and projects the resulting Decisions
// onto candidates. It writes nothing: routing (which provisions worktrees) and
// the spawn intent both happen in Advance, and the reconcile pass is the caller's
// explicit phase above.
//
// Consent is applied here and nowhere else: the readiness selector surfaces only
// Auto-drain Ready sets, so a set whose owner has not consented is never a
// candidate and the supervisor never learns it exists.
func (k *taskSetKind) Candidates() ([]work.Candidate, error) {
	decisions, err := Scan(k.d, k.cfg)
	if err != nil {
		// The wording is the kind's because it is the line the daemon prints; the
		// supervisor renders what it is handed.
		return nil, fmt.Errorf("queue: scan: %w", err)
	}
	k.pending = map[ref.WorkRef]Decision{}
	k.dispatched = map[string]bool{}
	k.spawned = nil
	var candidates []work.Candidate
	for _, dec := range decisions {
		c, ok := candidateFor(dec)
		if !ok {
			continue
		}
		if !c.Refused() {
			k.pending[c.Ref] = dec
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// candidateFor projects one scan Decision onto a candidate: a dispatchable Ready
// set advances, a Ready set the readiness selector deferred refuses with that
// deferral's own message (ADR-0106), and everything else — a busy project, a scan
// error, a repo with nothing ready — is no candidate at all, because the daemon
// has no ruling to make about it.
func candidateFor(dec Decision) (work.Candidate, bool) {
	repoLabel := repoLabelFromScan(dec.scan)
	switch {
	case dec.Actionable():
		return work.Candidate{
			Ref:      ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: dec.TaskSetID},
			Label:    repoLabel + "/" + dec.TaskSetID,
			Checkout: dec.boundCheckout,
			Verdict:  work.Advance(),
		}, true
	case dec.Err == nil && !dec.Busy && dec.Deferral.Deferred():
		return work.Candidate{
			Ref:     ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: dec.Deferral.SetID},
			Label:   repoLabel + "/" + dec.Deferral.SetID,
			Verdict: work.Refuse(dec.Deferral.Message()),
		}, true
	}
	return work.Candidate{}, false
}

// Advance dispatches one candidate. On an advance verdict it routes the drain to
// its checkout, claims that checkout for this pass, and spawns the drain into
// tmux, reporting what it did (and any pane-record failure alongside it) as the
// outcome's message. A spawn failure comes back as an error already worded as the
// daemon's line.
//
// A refusal has nothing for this kind to write. Every deferral the readiness
// selector produces is a pure read of store state, and the "why nothing ran" line
// is the run-output view diff's — it reports each one once, on change, rather
// than every tick, so repeating it here would double it. The call still crosses
// the seam because the verdict is the kind's to record: a Routine's overlap skip
// is a store write on exactly this path.
func (k *taskSetKind) Advance(c work.Candidate) (work.Outcome, error) {
	if c.Refused() {
		return work.Outcome{Kind: work.OutcomeMessage}, nil
	}
	dec, ok := k.pending[c.Ref]
	if !ok {
		return work.Outcome{}, fmt.Errorf("queue: %s is not a candidate of this pass", c.Ref)
	}
	repoLabel := repoLabelFromScan(dec.scan)
	dec, refusal := prepareWorktreeDrain(k.d, dec)
	if !dec.Actionable() {
		// Routing refused (invalid binding, provision failure): the refusal is the
		// report, and nothing was spawned.
		return work.Outcome{Kind: work.OutcomeMessage, Message: refusal}, nil
	}
	if path := dec.scan.RuntimePath; path != "" {
		if k.dispatched[path] {
			return work.Outcome{Kind: work.OutcomeMessage, Message: fmt.Sprintf("queue: %s: skip %s; another set already dispatched to %s this tick", repoLabel, dec.TaskSetID, path)}, nil
		}
		k.dispatched[path] = true
	}
	spawn, err := SpawnWithResult(k.d, dec)
	if err != nil {
		return work.Outcome{}, fmt.Errorf("queue: %s: spawn %s: %w", repoLabel, dec.TaskSetID, err)
	}
	var lines []string
	if err := recordDrainPane(k.d, dec, spawn.PaneID, "supervisor"); err != nil {
		lines = append(lines, fmt.Sprintf("queue: %s: record drain pane %s: %v", repoLabel, dec.TaskSetID, err))
	}
	lines = append(lines, fmt.Sprintf("queue: %s: spawned drain for %s", statusProjectLabel(repoLabel, dec.ProjectConfigError), dec.TaskSetID))
	k.spawned = append(k.spawned, PickedUpSet{Project: dec.Project, RepoLabel: repoLabel, SetID: dec.TaskSetID, ProjectConfigError: dec.ProjectConfigError})
	return work.Outcome{Kind: work.OutcomeMessage, Message: strings.Join(lines, "\n")}, nil
}

// SpawnedSets reports the sets this pass spawned, so the post-spawn view can seed
// them and next tick's diff does not re-announce work the dispatch loop already
// reported imperatively. It is a Task-set-local hook rather than part of the
// seam: the run-output diff is a Task-set snapshot type, and generalizing it
// would make every kind grow one.
func (k *taskSetKind) SpawnedSets() []PickedUpSet { return k.spawned }
