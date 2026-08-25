package tasks

import "io"

// TreeStableHold is a claim on a checkout taken by an operation that needs the
// tree to hold still for its duration but is not a drain (ADR-0238): the
// standalone Verifier and the standalone Reviewer. The Verifier runs tests
// another set's drain would break, and a Reviewer reading files that are moving
// reviews a state that never existed — so both are admitted exclusively, even
// though only one of them writes.
//
// It is the same acquisition a drain makes, without the drain's lifecycle: no
// terminal is recorded, no pending-spawn marker is consumed, and nothing of the
// hold survives its release. What it costs is one running Drain row for as long
// as the operation runs, which is what makes it visible to every admission
// chokepoint — the supervisor's dispatch, another command's Admission wait — for
// free.
type TreeStableHold struct {
	handle *DrainHandle
}

// AcquireTreeStable takes the checkout for a Tree-stable operation under the
// given Admission policy: AdmissionWait joins the checkout's Admission queue and
// blocks until a grant arrives, printing who holds it; AdmissionRefuse exits
// non-zero naming the claim. A caller that waited must look again at what it was
// going to do — the work may have moved while it waited.
func AcquireTreeStable(d *Deps, runtimePath, setID string, noticeOut io.Writer, policy AdmissionPolicy) (*TreeStableHold, error) {
	if noticeOut == nil {
		noticeOut = io.Discard
	}
	handle, _, err := admitDrainRow(d, runtimePath, setID, noticeOut, policy, false)
	if err != nil {
		return nil, err
	}
	return &TreeStableHold{handle: handle}, nil
}

// Release gives the checkout back. The row is removed rather than closed with a
// terminal: a Drain terminal is a statement about a drain's outcome, and this
// operation reports its own outcome (a verdict, a review document) through its
// own surface. Leaving one behind would put a verify in the set's drain history.
func (h *TreeStableHold) Release() error {
	if h == nil || h.handle == nil {
		return nil
	}
	return h.handle.Cancel()
}

// drainID is the row the hold inserted, for tests that assert the claim is
// really taken and really given back.
func (h *TreeStableHold) drainID() int64 {
	if h == nil || h.handle == nil {
		return 0
	}
	return h.handle.id
}
