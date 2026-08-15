package work

import (
	"github.com/glebglazov/pop/work/ref"
)

// Advancer is the second Work seam: the one a kind implements when the Work
// supervisor may start its items with no human in the loop. It is deliberately
// not part of Kind, which is read-and-render only — the Map kind implements Kind
// and not this, because every Decision ticket it holds is resolved in a session a
// human opens, and that asymmetry is what makes the split load-bearing rather
// than speculative. The supervisor obtains an advancer by type-asserting a kind
// it already holds, so a kind that cannot be advanced answers nothing and is
// never asked again.
//
// Consent is not in the seam. Both advanceable kinds already have a working
// consent model and the two mean different things (a Task set's auto-drain bit, a
// Routine's schedule-and-not-paused), so consent is an input to Candidates: a
// kind simply does not surface an item its own model withholds. A
// supervisor-level consent concept could only be a lowest common denominator.
type Advancer interface {
	// Reconcile heals the crash-shaped state this kind keeps before its candidates
	// are read. Making it an explicit phase is what took the opportunistic
	// reconcile off every read path (ADR-0055): the supervisor calls it, and a
	// status read no longer writes.
	Reconcile() error
	// Candidates lists what this kind would advance this tick. It performs no
	// writes: eligibility, consent and liveness are all pure reads, which is also
	// what makes it safe to run concurrently with other kinds'.
	Candidates() ([]Candidate, error)
	// Advance acts on one candidate — starting the work when the verdict says so,
	// recording the refusal when it does not. One call for both verdicts, because a
	// refusal a kind must persist (a Routine's overlap skip) is as much a
	// dispatch-phase write as a start is.
	//
	// The method is Advance rather than Perform, the name the Work supervisor
	// decision gave it, because a Go type cannot carry two methods of one name and
	// Kind.Perform(Container, *Item, Verb) is the verb seam every kind already
	// implements — an adapter satisfying both interfaces would need both. The
	// vocabulary difference is the useful half of the accident: a verb is
	// performed, a candidate is advanced.
	Advance(Candidate) (Outcome, error)
}

// Candidate is one item a Work advancer surfaces for a supervisor tick, carrying
// the kind's verdict on it. Produced by a pure read, it is a statement about the
// tick it was produced in and is not durable past it.
type Candidate struct {
	// Ref addresses the item. It is container-level with an empty item id for a
	// kind whose item does not exist until the advance mints one.
	Ref ref.WorkRef
	// Label is the human phrase the supervisor reports this candidate by.
	Label string
	// Checkout is the working tree this advance would mutate, empty when it
	// occupies none. Only an adapter whose advance touches a tree fills it — a
	// read-only kind leaves it blank and is never blocked — and the supervisor
	// enforces occupancy over it as the cross-kind backstop.
	Checkout string
	// Verdict is the kind's ruling: advance, or refuse with a reason.
	Verdict Verdict
}

// Refused reports a candidate the kind will not start.
func (c Candidate) Refused() bool { return !c.Verdict.Advance }

// Verdict is an advance-or-refuse ruling on one candidate. Refusals cross the
// seam rather than staying inside a kind because the supervisor's output is
// mostly why nothing ran.
type Verdict struct {
	// Advance is the kind asking for this item to be started now.
	Advance bool
	// Reason names why it will not be, in words a human reads. Empty on an
	// advance verdict.
	Reason string
}

// Advance is the verdict that starts the work.
func Advance() Verdict { return Verdict{Advance: true} }

// Refuse is the verdict that does not, naming why.
func Refuse(reason string) Verdict { return Verdict{Reason: reason} }

// AdvanceEvent is one dispatch decision a supervisor tick made, structured so
// that every kind reports through one path instead of each printing its own
// lines. It is what generalizes about supervisor reporting: the kind, the item,
// what the advance produced and what it failed with. What does *not* generalize
// rides beside it — the Task-set run-output view diff is a Task-set snapshot
// type, and folding it in here would make every kind grow one.
type AdvanceEvent struct {
	// Kind is the Work kind whose candidate this was.
	Kind KindID
	// Ref addresses the candidate.
	Ref ref.WorkRef
	// Label is the candidate's human phrase, the same one the kind reports by.
	Label string
	// Outcome is what the advance produced. It is the zero Outcome when the
	// supervisor ruled on the candidate itself and never asked the kind.
	Outcome Outcome
	// Err is the failure the kind reported, already worded as the line a human
	// reads — the supervisor renders what it is handed.
	Err error
}

// Line renders the event for the supervisor's output, empty when the decision
// has nothing to say. The render adds no prefix of its own: a kind's message and
// a kind's error already name themselves, and a uniform wrapper would double
// every "queue:" the existing lines carry.
func (e AdvanceEvent) Line() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Outcome.Message
}

// AdvancersInKindPrecedence returns the advanceable kinds of a wiring list in
// kind precedence order — the order dispatch runs in, because dispatch mutates
// the shared checkout ledger and "first wins, rest defer" needs a defined one.
//
// The order is in the name because scheduling is the one caller that may not
// take a Work read surface's ordering: read surfaces order by creation date now
// and a preset can flip that at will (ADR-0210), so a scheduler that inherited
// the display default would have the ledger's tiebreak moved under it by a
// config edit.
func AdvancersInKindPrecedence(kinds []Kind) []Advancer {
	var advancers []Advancer
	for _, k := range kindsInPrecedence(kinds) {
		if adv, ok := k.(Advancer); ok {
			advancers = append(advancers, adv)
		}
	}
	return advancers
}
