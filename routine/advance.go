package routine

import (
	"fmt"
	"io"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// Advancer is the Routine half of the Work advance seam: the whole of the
// daemon's relationship with Routines, expressed as a reconcile, a pure
// candidate read and a dispatch. It replaces the single inline pass that checked
// schedule, pause, fingerprint and liveness and fired, with no decision data in
// between — every one of those checks is now a verdict a caller can see.
//
// It implements work.Advancer and not yet work.Kind: the read seam arrives with
// the Routine dashboard. The two writes the old pass performed mid-scan — the
// fingerprint-drift pause and the overlap skip record — are refusal verdicts
// here whose writes happen in Advance, which is what makes the candidate read
// pure.
//
// Candidates are global and cwd-independent: the Routines it rules on are every
// Routine in the data dir, in id order. Project routines (discovered from the
// current checkout) are deliberately absent — they carry no schedule, so they
// can never consent to being fired unattended.
type Advancer struct {
	d   *Deps
	out io.Writer

	// pending holds what dispatch must do for each candidate this pass surfaced.
	// The candidate read already resolved every question — consent, schedule,
	// fingerprint, liveness — so Advance never re-derives one and never races the
	// reads its own verdict was formed from.
	pending map[ref.WorkRef]pendingAdvance
	// store is the handle the candidate read borrowed, reused by the writes its
	// refusals carry. Borrowed through the tasks accessor and never closed
	// (ADR-0140).
	store *store.Store
}

// pendingAdvance is one candidate's dispatch instruction: what to do, and the
// instant the tick ruled at — the fired_at a skipped run is recorded under, so
// the record dates from the fire it replaced rather than from dispatch.
type pendingAdvance struct {
	action  advanceAction
	firedAt time.Time
}

// advanceAction is what dispatch does for one candidate. It exists because a
// Routine's refusals are not alike — one is a store record, one is a pause plus
// a record, and one is only a line — and neither the supervisor nor the verdict
// itself should have to carry which.
type advanceAction int

const (
	// actionFire spawns the Routine's fire pane.
	actionFire advanceAction = iota
	// actionReport has nothing to write: the refusal's reason is the whole report.
	actionReport
	// actionRecordSkip records a skipped run, so the skip lands in the journal
	// beside the fire it replaced.
	actionRecordSkip
	// actionPauseChanged pauses the Routine with reason `changed` and records the
	// skip that pause caused.
	actionPauseChanged
)

// NewAdvancer returns the Routine advance seam over d. out receives the
// reconcile phase's own narration — a count of healed rows is a statement about
// the pass, not about a candidate, so it has no place in the supervisor's
// per-advance event stream.
func NewAdvancer(d *Deps, out io.Writer) *Advancer {
	if d == nil {
		d = defaultDeps
	}
	if out == nil {
		out = io.Discard
	}
	return &Advancer{d: d, out: out, pending: map[ref.WorkRef]pendingAdvance{}}
}

// ID is the closed enum's routine member.
func (a *Advancer) ID() work.KindID { return ref.KindRoutine }

// Reconcile heals run rows whose owning process died, mirroring the Drain
// reconcile. The failure is narrated and also returned: reconciliation is
// opportunistic, so the supervisor reads the pre-reconcile snapshot rather than
// abandoning the tick.
func (a *Advancer) Reconcile() error {
	n, err := ReconcileRunsWith(a.d)
	if err != nil {
		fmt.Fprintf(a.out, "queue: reconcile routines: %v\n", err)
		return err
	}
	if n > 0 {
		fmt.Fprintf(a.out, "queue: reconciled %d stale routine run(s)\n", n)
	}
	return nil
}

// Candidates rules on every discovered Routine and writes nothing. Consent is
// applied here and nowhere else: a paused Routine and an unscheduled one
// (durable manual-fire-only, ADR-0134) are no candidate at all, so the
// supervisor never learns they exist.
func (a *Advancer) Candidates() ([]work.Candidate, error) {
	routines, warnings, err := ListRoutines(a.d)
	if err != nil {
		// The wording is the kind's because it is the line the daemon prints; the
		// supervisor renders what it is handed.
		return nil, fmt.Errorf("queue: routines: %w", err)
	}
	a.pending = map[ref.WorkRef]pendingAdvance{}
	a.store = nil
	now := nowUTC(a.d)

	var candidates []work.Candidate
	// A Routine whose definition will not load is a refusal rather than a warning
	// on a side channel: the daemon did rule on it, and the ruling is "cannot".
	for _, w := range warnings {
		candidates = append(candidates, a.refuse(w.ID, fmt.Sprintf("load failed: %v", w.Err), actionReport, now))
	}
	if len(routines) == 0 {
		return candidates, nil
	}
	// If-exists mode: a machine with no store has fired nothing, and finding that
	// out must not materialise the database (ADR-0140).
	s, ok, err := openExecutionStoreIfExists(a.d)
	if err != nil {
		return nil, fmt.Errorf("queue: routines: %w", err)
	}
	if !ok {
		return candidates, nil
	}
	a.store = s
	for _, r := range routines {
		if c, ok := a.candidateFor(s, r, now); ok {
			candidates = append(candidates, c)
		}
	}
	return candidates, nil
}

// candidateFor rules on one Routine. A Routine that has not consented, or whose
// schedule is not due, is no candidate at all — the daemon has nothing to say
// about it. Everything past that point is either a fire or a refusal naming why
// not, because a due Routine that does not fire is exactly what the operator
// needs told.
func (a *Advancer) candidateFor(s *store.Store, r *Routine, now time.Time) (work.Candidate, bool) {
	if r.Manifest.Paused || !r.Manifest.IsScheduled() {
		return work.Candidate{}, false
	}
	lastFired, err := LastFireTime(s, r.ID)
	if err != nil {
		return a.refuse(r.ID, fmt.Sprintf("last fire: %v", err), actionReport, now), true
	}
	if !IsDue(r.Schedule, lastFired, now) {
		return work.Candidate{}, false
	}
	// Run-affecting drift safety net (ADR-0128): if the current fingerprint no
	// longer matches the last non-skipped run's, a prompt.md edit no CLI
	// chokepoint saw slipped in. The verdict refuses and dispatch pauses with
	// reason `changed`; a human re-proves it with a manual fire. An empty last
	// fingerprint (pre-migration or first fire) is never a mismatch.
	current, err := Fingerprint(a.d, r)
	if err != nil {
		return a.refuse(r.ID, fmt.Sprintf("fingerprint: %v", err), actionReport, now), true
	}
	last, err := LastFingerprint(s, r.ID)
	if err != nil {
		return a.refuse(r.ID, fmt.Sprintf("last fingerprint: %v", err), actionReport, now), true
	}
	if last != "" && last != current {
		return a.refuse(r.ID, SkipReasonChanged, actionPauseChanged, now), true
	}
	live, err := s.LiveRoutineRun(r.ID, a.runAlive)
	if err != nil {
		return a.refuse(r.ID, fmt.Sprintf("live run: %v", err), actionReport, now), true
	}
	if live != nil {
		return a.refuse(r.ID, SkipReasonOverlap, actionRecordSkip, now), true
	}
	return a.candidate(r.ID, work.Advance(), pendingAdvance{action: actionFire, firedAt: now}), true
}

// candidate builds one candidate and records its dispatch instruction.
//
// The ref is container-level with an empty item id, because a candidate exists
// before a run does: the run id is minted during the fire itself. Checkout is
// left empty on purpose — a fire occupies no checkout as far as the supervisor's
// one invariant is concerned, so a read-only Routine is never queued behind a
// drain.
func (a *Advancer) candidate(id string, verdict work.Verdict, p pendingAdvance) work.Candidate {
	c := work.Candidate{
		Ref:     ref.WorkRef{Kind: ref.KindRoutine, ContainerID: id},
		Label:   "routine " + id,
		Verdict: verdict,
	}
	a.pending[c.Ref] = p
	return c
}

func (a *Advancer) refuse(id, reason string, action advanceAction, now time.Time) work.Candidate {
	return a.candidate(id, work.Refuse(reason), pendingAdvance{action: action, firedAt: now})
}

// Advance dispatches one candidate: the fire on an advance verdict, and on a
// refusal the write that refusal carries. Both cross the same call because a
// refusal a kind must persist is as much a dispatch-phase write as a start is —
// a Routine is the reason the seam is shaped that way.
func (a *Advancer) Advance(c work.Candidate) (work.Outcome, error) {
	p, ok := a.pending[c.Ref]
	if !ok {
		return work.Outcome{}, fmt.Errorf("queue: %s is not a candidate of this pass", c.Ref)
	}
	id := c.Ref.ContainerID
	switch p.action {
	case actionFire:
		if _, err := FirePaneWith(a.d, id); err != nil {
			return work.Outcome{}, fmt.Errorf("queue: %s: spawn: %w", c.Label, err)
		}
		return advanceMessage("queue: %s: spawned fire", c.Label), nil
	case actionPauseChanged:
		if err := PauseChangedWith(a.d, id); err != nil {
			return work.Outcome{}, fmt.Errorf("queue: %s: pause on change: %w", c.Label, err)
		}
		if err := a.recordSkip(id, c.Verdict.Reason, p.firedAt); err != nil {
			return work.Outcome{}, fmt.Errorf("queue: %s: record skip: %w", c.Label, err)
		}
		return advanceMessage("queue: %s: paused (changed): %s", c.Label, c.Verdict.Reason), nil
	case actionRecordSkip:
		if err := a.recordSkip(id, c.Verdict.Reason, p.firedAt); err != nil {
			return work.Outcome{}, fmt.Errorf("queue: %s: record skip: %w", c.Label, err)
		}
		return advanceMessage("queue: %s: skipped fire (%s)", c.Label, c.Verdict.Reason), nil
	}
	return advanceMessage("queue: %s: %s", c.Label, c.Verdict.Reason), nil
}

// recordSkip writes the skipped run a refusal stands for, dated from the fire it
// replaced. Skipped rows are inert to scheduling — neither the last fire time
// nor the last fingerprint is read from one — so the record buys the journal
// entry and nothing else.
func (a *Advancer) recordSkip(id, reason string, firedAt time.Time) error {
	if a.store == nil {
		return fmt.Errorf("no execution store")
	}
	_, err := a.store.InsertSkippedRoutineRun(store.RoutineRun{
		RoutineID:  id,
		FiredAt:    firedAt,
		SkipReason: reason,
	})
	return err
}

func (a *Advancer) runAlive(run store.RoutineRun) bool {
	return routineProcessAlive(a.d, run.PID, run.ProcStart)
}

func advanceMessage(format string, args ...any) work.Outcome {
	return work.Outcome{Kind: work.OutcomeMessage, Message: fmt.Sprintf(format, args...)}
}
