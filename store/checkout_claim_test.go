package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

func putWaiter(t *testing.T, s *Store, setID, path string, pid int, procStart string) {
	t.Helper()
	if err := s.PutRecoveryWaiter(RecoveryWaiter{
		SetID:        setID,
		Preset:       "claude",
		ResetAt:      time.Now().Add(-time.Hour).UTC(),
		RuntimePath:  path,
		PID:          pid,
		ProcStart:    procStart,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}
}

func putGateHold(t *testing.T, s *Store, setID, path string, pid int, procStart string, claim bool) {
	t.Helper()
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		SetID:        setID,
		RuntimePath:  path,
		PID:          pid,
		ProcStart:    procStart,
		Claim:        claim,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
}

func TestReadCheckoutClaimFailedGate(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Reason != ClaimFailedGate || claim.Holder.ContainerID != "set-a" {
		t.Fatalf("claim = %+v, want failed-gate claim by set-a", claim)
	}
	if claim.Reason.Phrase() != "failed gate, uncommitted changes" {
		t.Fatalf("reason = %q, want %q", claim.Reason.Phrase(), "failed gate, uncommitted changes")
	}
}

func TestReadCheckoutClaimNonClaimingGateHoldUnclaimed(t *testing.T) {
	// A non-claiming hold (HITL / verify-fail / clean Failed gate) contributes
	// quiescence occupancy but never a checkout claim.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", false)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (non-claiming hold does not claim)", claim)
	}
}

func TestReadCheckoutClaimDeadGateHoldUnclaimed(t *testing.T) {
	// A claim-bearing hold whose owner is dead does not claim (swept by reconcile,
	// filtered by the read regardless).
	s := openTestStore(t, aliveByToken()) // nothing alive
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (dead-owner gate hold does not claim)", claim)
	}
}

func TestStartDrainRefusesOtherSetClaimingGateHold(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	_, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed", err)
	}
	var claimed *CheckoutClaimedError
	if !errors.As(err, &claimed) {
		t.Fatalf("err = %v, want *CheckoutClaimedError", err)
	}
	if claimed.Claim.Holder.ContainerID != "set-a" || claimed.Claim.Reason != ClaimFailedGate {
		t.Fatalf("claim = %+v, want failed-gate claim by set-a", claimed.Claim)
	}
}

func TestStartDrainAdmittedAlongsideNonClaimingGateHold(t *testing.T) {
	// A non-claiming gate hold (a human at a HITL / clean Failed gate) must not
	// block another set's admission — queue liveness over gate-time safety.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", false)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("non-claiming gate hold wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started alongside a non-claiming hold: %+v", d)
	}
}

func TestStartDrainSameSetClaimingGateHoldDoesNotBlockReacquire(t *testing.T) {
	// A gate-launched, checkout-mutating action (e.g. reverify) re-acquires the
	// drain while the set's own claim-bearing hold is still registered; its own
	// hold must not self-block.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 3, ProcStart: "t3", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("same-set re-acquire refused by own gate hold: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("re-acquire drain not started: %+v", d)
	}
}

func TestStartDrainDeadClaimingGateHoldDoesNotBlock(t *testing.T) {
	s := openTestStore(t, aliveByToken()) // nothing alive
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("dead-owner gate hold wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started over dead-owner gate hold: %+v", d)
	}
}

func TestReadCheckoutClaimNoneWhenIdle(t *testing.T) {
	s := openTestStore(t)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil on an idle checkout", claim)
	}
}

func TestReadCheckoutClaimRunningDrain(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Reason != ClaimRunningDrain || claim.Holder.ContainerID != "set-a" {
		t.Fatalf("claim = %+v, want running-drain claim by set-a", claim)
	}
}

func TestReadCheckoutClaimQuotaWaiter(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Reason != ClaimQuotaWaiter || claim.Holder.ContainerID != "set-a" {
		t.Fatalf("claim = %+v, want quota-waiter claim by set-a", claim)
	}
}

// TestCheckoutClaimHoldersAreTaskSetRefs pins the property the ref holder buys
// and the one it must not cost: all three arms of the claim union name their
// holder as task-set:<id>, and each keeps its own reason phrase — the reason is
// the only thing distinguishing them once the holder collapses to one shape.
func TestCheckoutClaimHoldersAreTaskSetRefs(t *testing.T) {
	cases := []struct {
		name   string
		setUp  func(t *testing.T, s *Store)
		reason ClaimReason
		phrase string
	}{
		{
			name: "running drain",
			setUp: func(t *testing.T, s *Store) {
				if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}); err != nil {
					t.Fatalf("StartDrain: %v", err)
				}
			},
			reason: ClaimRunningDrain,
			phrase: "running drain",
		},
		{
			name:   "quota waiter",
			setUp:  func(t *testing.T, s *Store) { putWaiter(t, s, "set-a", "/rt", 100, "t1") },
			reason: ClaimQuotaWaiter,
			phrase: "quota wait",
		},
		{
			name:   "failed gate",
			setUp:  func(t *testing.T, s *Store) { putGateHold(t, s, "set-a", "/rt", 100, "t1", true) },
			reason: ClaimFailedGate,
			phrase: "failed gate, uncommitted changes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
			tc.setUp(t, s)
			claim, err := s.ReadCheckoutClaim("/rt")
			if err != nil {
				t.Fatalf("ReadCheckoutClaim: %v", err)
			}
			if claim == nil {
				t.Fatal("ReadCheckoutClaim = nil, want a live claim")
			}
			if claim.Holder != (ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}) {
				t.Fatalf("holder = %+v, want task-set:set-a", claim.Holder)
			}
			if claim.Holder.String() != "task-set:set-a" {
				t.Fatalf("holder renders %q, want %q", claim.Holder, "task-set:set-a")
			}
			if claim.Reason != tc.reason || claim.Reason.Phrase() != tc.phrase {
				t.Fatalf("reason = %q (%q), want %q (%q)", claim.Reason, claim.Reason.Phrase(), tc.reason, tc.phrase)
			}
			// The refusal names the bare set id, not the rendered ref: routing the
			// message through the holder's String() would silently reword every
			// refusal and deferral line to "claimed by set task-set:set-a".
			refusal := (&CheckoutClaimedError{Claim: *claim}).Error()
			if want := "checkout /rt is claimed by set set-a (" + tc.phrase + ", PID 100 since "; !strings.HasPrefix(refusal, want) {
				t.Fatalf("refusal = %q, want prefix %q", refusal, want)
			}
		})
	}
}

func TestReadCheckoutClaimDeadWaiterUnclaimed(t *testing.T) {
	// Owner reads dead → the waiter does not claim the checkout.
	s := openTestStore(t, aliveByToken())
	putWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (dead-owner waiter does not claim)", claim)
	}
}

func TestStartDrainRefusesOtherSetWaiter(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	// Set B tries to drain the checkout set A's live waiter is parked on.
	_, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed", err)
	}
	if errors.Is(err, ErrSetClaimed) {
		t.Fatalf("claim refusal must be distinguishable from ErrSetClaimed: %v", err)
	}
	var claimed *CheckoutClaimedError
	if !errors.As(err, &claimed) {
		t.Fatalf("err = %v, want *CheckoutClaimedError", err)
	}
	if claimed.Claim.Holder.ContainerID != "set-a" || claimed.Claim.Reason != ClaimQuotaWaiter {
		t.Fatalf("claim = %+v, want quota-waiter claim by set-a", claimed.Claim)
	}
}

func TestStartDrainSameSetWaiterDoesNotBlockResume(t *testing.T) {
	// A quota-parked set resumes by re-starting its drain past its own still-
	// registered waiter (deregistration happens after the resume start today).
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 3, ProcStart: "t3", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("same-set resume StartDrain refused: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("resume drain not started: %+v", d)
	}
}

func TestStartDrainDeadWaiterDoesNotBlock(t *testing.T) {
	// The waiter's owner reads dead (slice 01 would sweep it); it must not block
	// admission even before the sweep runs.
	s := openTestStore(t, aliveByToken()) // nothing alive
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("dead-owner waiter wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started over dead-owner waiter: %+v", d)
	}
}

func TestStartDrainAdmittedAfterWaiterDeregisters(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()}); !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed while set-a's waiter is live", err)
	}

	if err := s.DeleteRecoveryWaiter("set-a"); err != nil {
		t.Fatalf("DeleteRecoveryWaiter: %v", err)
	}
	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("StartDrain after deregister: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("set-b not admitted after set-a deregistered: %+v", d)
	}
}

func putAdmissionWaiter(t *testing.T, s *Store, setID, path string, pid int, procStart string) {
	t.Helper()
	if _, err := s.RegisterAdmissionWaiter(AdmissionWaiter{
		RuntimePath:  path,
		Repo:         "r",
		SetID:        setID,
		PID:          pid,
		ProcStart:    procStart,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterAdmissionWaiter: %v", err)
	}
}

// A command queued for a checkout claims it (ADR-0239): dispatch reads the claim
// union and must see the waiter there, or it spawns onto the tree the waiter has
// been queuing for the moment the current holder leaves.
func TestReadCheckoutClaimQueuedCommand(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putAdmissionWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Reason != ClaimQueuedCommand || claim.Holder.ContainerID != "set-a" {
		t.Fatalf("claim = %+v, want queued-command claim by set-a", claim)
	}
	if claim.Reason.Phrase() != "queued command" {
		t.Fatalf("reason = %q, want %q", claim.Reason.Phrase(), "queued command")
	}
	if claim.RuntimePath != "/rt" {
		t.Fatalf("runtime path = %q, want /rt", claim.RuntimePath)
	}
}

// A real holder is the better answer while there is one: the queue only becomes
// the reason a checkout is unavailable once nothing is executing in it.
func TestReadCheckoutClaimRunningDrainOutranksQueuedCommand(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}, Drain{PID: 101, ProcStart: "t2"}))
	if _, err := s.StartDrain(Drain{Repo: "r", SetID: "set-a", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	putAdmissionWaiter(t, s, "set-b", "/rt", 101, "t2")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Reason != ClaimRunningDrain || claim.Holder.ContainerID != "set-a" {
		t.Fatalf("claim = %+v, want the running drain by set-a", claim)
	}
}

// A waiter whose command died claims nothing — the sweep removes the row, and
// the read filters it regardless, so a closed terminal never freezes a checkout.
func TestReadCheckoutClaimDeadQueuedCommandUnclaimed(t *testing.T) {
	s := openTestStore(t, aliveByToken())
	putAdmissionWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want none for a dead waiter", claim)
	}
}

// Waiters do not claim against each other: the union's queued-command arm is
// read-side only, so a queue of two still grants its head.
func TestQueuedCommandClaimDoesNotBlockTheGrant(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}, Drain{PID: 101, ProcStart: "t2"}))
	putAdmissionWaiter(t, s, "set-a", "/rt", 100, "t1")
	putAdmissionWaiter(t, s, "set-b", "/rt", 101, "t2")
	head, err := s.AdmissionWaitersOn("/rt")
	if err != nil {
		t.Fatalf("AdmissionWaitersOn: %v", err)
	}
	drain, block, err := s.TryAdmitDrain(Drain{Repo: "r", SetID: "set-a", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}, head[0].ID)
	if err != nil {
		t.Fatalf("TryAdmitDrain: %v", err)
	}
	if block != nil || drain.ID == 0 {
		t.Fatalf("grant = (%+v, %+v), want the head of the queue admitted", drain, block)
	}
}
