package tasks

import (
	"io"
	"os"

	"github.com/glebglazov/pop/store"
)

// TreeStableHold is a claim on a checkout taken by an operation that needs the
// tree to hold still for its duration but is not a drain (ADR-0238): the
// standalone Verifier and the standalone Refiner. The Verifier runs tests
// another set's drain would break, and a Refiner reading files that are moving
// judges a state that never existed — so both are admitted exclusively, even
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
//
// When this process already holds the checkout for this set the hold is a no-op
// (see heldHere): the tree is already still, and the outer claim outlives the
// inner release.
func AcquireTreeStable(d *Deps, runtimePath, setID string, noticeOut io.Writer, policy AdmissionPolicy) (*TreeStableHold, error) {
	if noticeOut == nil {
		noticeOut = io.Discard
	}
	if heldHere(d, runtimePath, setID) {
		return &TreeStableHold{}, nil
	}
	handle, _, err := admitDrainRow(d, runtimePath, setID, noticeOut, policy, false)
	if err != nil {
		return nil, err
	}
	return &TreeStableHold{handle: handle}, nil
}

// heldHere reports that this process already holds the checkout for this set,
// which makes the hold a no-op: the tree is already standing still, held by the
// caller itself. Without it a nested Tree-stable operation is refused by its own
// claim — an Assist session's Fold takes the checkout, hits a rebase conflict,
// and its "Verify set" would be told the set is already being drained by its own
// PID. It is the admission-path form of the exemption Fold's live-claim refusal
// and Checkout quiescence already make for the calling process.
//
// A store that does not exist yet holds nothing, and a read that fails falls
// through to the real acquisition rather than skipping it: the claim is what
// keeps the tree still, so the safe error is to ask for it.
func heldHere(d *Deps, runtimePath, setID string) bool {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return false
	}
	pid := os.Getpid()
	procStart, _ := procStartToken(d, pid)
	held, err := s.ClaimHeldBy(runtimePath, setID, store.ProcessOwner{PID: pid, ProcStart: procStart})
	return err == nil && held
}

// Release gives the checkout back. The row is removed rather than closed with a
// terminal: a Drain terminal is a statement about a drain's outcome, and this
// operation reports its own outcome (a verdict, a refine report) through its
// own surface. Leaving one behind would put a verify in the set's drain history.
// A no-op hold gives nothing back — what it found still held is not its to
// release.
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
