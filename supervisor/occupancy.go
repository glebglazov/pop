package supervisor

import (
	"fmt"
	"github.com/glebglazov/pop/tasks/drain"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// checkoutOccupancy is the one invariant the Work supervisor enforces itself:
// two pieces of Work never advance into the same checkout. Everything else about
// starting work — pane titles, session resolution, worktree routing, a Routine's
// fire — is irreducibly kind-specific and stays behind the seam.
//
// It is opt-in per adapter and enforced over Candidate.Checkout: an adapter
// whose advance mutates a tree names the tree, and one that mutates none leaves
// it empty and is never blocked. That is why this can be supervisor-level at all
// — a Map's grilling session and a Routine's fire occupy nothing, so a single
// mechanism does not have to model what "occupied" means per kind.
//
// It is a backstop, not the only check: the Task-set adapter keeps computing
// occupancy for its own deferral display, because the display needs the reason
// species and the earliest-eligible instant, which this ruling does not carry.
// The two never disagree because both are pure reads of the store's claim union
// through drain.Deps.CheckoutClaimAt. What this adds is that a *new* occupying kind
// cannot forget: it is blocked whether or not it checked.
type checkoutOccupancy struct {
	// claim reads the live Checkout claim on a path — the existing cross-process
	// mechanism (ADR-0135), not a new per-tick concept.
	claim checkoutClaimPathFunc
	// taken is the per-tick ledger: the checkouts this tick's dispatch has already
	// advanced into, and who took each. A just-dispatched drain has not yet
	// acquired anything the store can see, so the claim union alone would let a
	// second candidate follow it into the same tree in the same tick. This is what
	// makes dispatch order load-bearing, and so why dispatch is serial.
	taken map[string]ref.WorkRef
}

// checkoutClaimPathFunc reads the live Checkout claim on one runtime checkout.
// It is the path-keyed sibling of checkoutClaimFunc, which the Task-set adapter
// consults per Ready set; the supervisor already has the path on the candidate.
type checkoutClaimPathFunc func(runtimePath string) *store.CheckoutClaim

func newCheckoutOccupancy(d *drain.Deps) *checkoutOccupancy {
	return &checkoutOccupancy{claim: d.CheckoutClaimAt, taken: map[string]ref.WorkRef{}}
}

// refusal names why this candidate may not occupy its checkout right now, empty
// when it may. A candidate naming no checkout is never refused; neither is one
// whose checkout is held by itself — a set resuming past its own quota waiter is
// the ordinary case, not a collision.
func (o *checkoutOccupancy) refusal(c work.Candidate) string {
	if c.Checkout == "" {
		return ""
	}
	holder := c.Ref.Container()
	if taken, ok := o.taken[c.Checkout]; ok && taken != holder {
		return fmt.Sprintf("checkout %s already taken by %s this tick", c.Checkout, taken)
	}
	if claim := o.claim(c.Checkout); claim != nil && claim.Holder.Container() != holder {
		// The wording is the deferral's, not a second phrasing of the same fact: the
		// adapter's display and this backstop report one claim the same way.
		return drain.SpawnDeferral{Reason: drain.DeferCheckoutClaim, SetID: c.Ref.ContainerID, Claim: claim}.Message()
	}
	return ""
}

// occupy records that this candidate took its checkout for the rest of the tick.
// It runs after the kind advanced without error, including when the kind's own
// routing refused: the supervisor cannot see inside a message outcome, and
// holding a checkout a refused advance did not use costs at most one tick.
func (o *checkoutOccupancy) occupy(c work.Candidate) {
	if c.Checkout == "" {
		return
	}
	o.taken[c.Checkout] = c.Ref.Container()
}
