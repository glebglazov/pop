package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// ErrCheckoutClaimed reports that StartDrain refused because a live Checkout
// claim already holds the runtime checkout — the tree must hold still (ADR-0135).
// It is distinct from ErrSetClaimed, which names the *set* being drained
// somewhere else, so a refusal tells a human whether to look in this tree or in
// another worktree. The claim need not be an executing process: a quota-recovery
// waiter that will resume holds one too. A *CheckoutClaimedError carries the
// claiming holder and claim reason and satisfies errors.Is(err, ErrCheckoutClaimed).
var ErrCheckoutClaimed = errors.New("checkout claimed by another set")

// ClaimReason names why a Checkout claim holds a runtime path. It survives the
// move to a Work-ref holder because two of its three values are *states of a
// Task set*: with a ref holder all three would collapse to task-set:<id> and a
// deferral message would lose why the checkout is held. The claim union is
// derived at read time (no table): a live running Drain, a live Recovery waiter,
// or a live claim-bearing Checkout gate hold.
type ClaimReason string

const (
	// ClaimRunningDrain: a live running Drain is executing against the path.
	ClaimRunningDrain ClaimReason = "running_drain"
	// ClaimQuotaWaiter: a live Recovery waiter is parked on the path, waiting for
	// its preset's cooldown before resuming — an automatic process that will
	// resume, so it claims the checkout (ADR-0135).
	ClaimQuotaWaiter ClaimReason = "quota_waiter"
	// ClaimFailedGate: a live claim-bearing Checkout gate hold — a set parked at a
	// Failed gate over uncommitted work (dirtiness snapshotted at park time,
	// ADR-0135). Admitting another set would rewrite the dirty tree the human is
	// mid-review of. A non-claiming gate hold (HITL, verify-fail, clean Failed
	// gate) is not a claim.
	ClaimFailedGate ClaimReason = "failed_gate"
	// ClaimQueuedCommand: a live Admission waiter is queued for the path — a human
	// command that found the checkout held and is waiting for a window rather than
	// refusing (ADR-0239). It holds nothing yet, and it claims anyway: an automatic
	// dispatch that raced into the tree the moment the current holder finished
	// would jump a human who has been waiting, which is the ordering guarantee the
	// queue exists to make. Read-side only — the Admission grant derives its own
	// block, so waiters never claim against each other.
	ClaimQueuedCommand ClaimReason = "queued_command"
)

// Phrase renders the claim reason as a short human phrase for status and refusal
// lines (e.g. "checkout claimed by set X (quota wait)").
func (r ClaimReason) Phrase() string {
	switch r {
	case ClaimRunningDrain:
		return "running drain"
	case ClaimQuotaWaiter:
		return "quota wait"
	case ClaimFailedGate:
		return "failed gate, uncommitted changes"
	case ClaimQueuedCommand:
		return "queued command"
	default:
		return string(r)
	}
}

// CheckoutClaim is a live claim on a runtime checkout, derived at read time from
// the claim union (ADR-0135): the piece of Work that owns it and why, plus the
// owner's PID and the instant the claim began so a caller can say how long it has
// been held. Holder is a Work ref rather than a set id so a checkout can be held
// by something that is not a Task set; every source of the union derives from a
// Task set today, so every holder is currently task-set:<id>.
type CheckoutClaim struct {
	Holder ref.WorkRef
	Reason ClaimReason
	// RuntimePath is the checkout the claim holds, carried so a refusal names the
	// resource it is about without the caller re-deriving it.
	RuntimePath string
	PID         int
	Since       time.Time
}

// Sentence renders the Checkout claim as the refusal (and, later, wait) line:
// which tree is held, by which set, and why. It is the single wording for the
// checkout-keyed refusal, so BeginDrain and AcquireRuntimeLock say the same
// thing about the same claim.
func (c CheckoutClaim) Sentence() string {
	s := fmt.Sprintf("checkout %s is claimed by set %s (%s, PID %d",
		c.RuntimePath, c.Holder.ContainerID, c.Reason.Phrase(), c.PID)
	if !c.Since.IsZero() {
		s += " since " + c.Since.UTC().Format(time.RFC3339)
	}
	return s + ")"
}

// taskSetHolder names the Task set owning a claim. Each row the union reads
// (drains, recovery_waiters, checkout_gate_holds) is keyed by set id, so the
// kind is fixed here rather than stored.
func taskSetHolder(setID string) ref.WorkRef {
	return ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: setID}
}

// CheckoutClaimedError carries the claim that caused a StartDrain refusal so the
// caller can name the claiming holder and claim reason. It unwraps to
// ErrCheckoutClaimed so errors.Is(err, ErrCheckoutClaimed) holds.
type CheckoutClaimedError struct {
	Claim CheckoutClaim
}

func (e *CheckoutClaimedError) Error() string { return e.Claim.Sentence() }

func (e *CheckoutClaimedError) Is(target error) bool { return target == ErrCheckoutClaimed }

func (e *CheckoutClaimedError) Unwrap() error { return ErrCheckoutClaimed }

// claimQuerier is the subset of *sql.DB and *sql.Tx the claim derivation uses, so
// it can read either on the store's shared handle (ReadCheckoutClaim) or inside
// StartDrain's open transaction — the check-then-insert must consult claims under
// the same write lock it takes.
type claimQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// ReadCheckoutClaim derives the live Checkout claim on runtimePath, or nil when
// nothing live claims it (ADR-0135). A running Drain claim takes precedence over a
// quota-waiter claim, and a live holder of any species takes precedence over a
// queued command waiting for one to finish (ADR-0239). The store's liveness policy is applied to every candidate,
// so a dead-owner drain row or waiter (a crash or a kill -9) never claims — it is
// swept by the opportunistic reconcile, but the read filters it regardless.
func (s *Store) ReadCheckoutClaim(runtimePath string) (*CheckoutClaim, error) {
	if runtimePath == "" {
		return nil, nil
	}
	if claim, err := s.liveDrainClaim(s.db, runtimePath); err != nil || claim != nil {
		return claim, err
	}
	if claim, err := s.liveWaiterClaim(s.db, runtimePath, ""); err != nil || claim != nil {
		return claim, err
	}
	if claim, err := s.liveGateHoldClaim(s.db, runtimePath, ""); err != nil || claim != nil {
		return claim, err
	}
	return s.liveAdmissionClaim(s.db, runtimePath)
}

// liveAdmissionClaim returns the head of runtimePath's Admission queue — the
// first waiter whose registering process is still alive — as a Checkout claim,
// or nil when nobody is queued. It is the last arm of the union because a real
// holder is the better answer while one exists: a human reading "claimed by set
// A (running drain)" is told where to go, and only once nothing holds the tree
// does the queue itself become the reason the checkout is unavailable.
//
// This arm belongs to the read side alone. The Admission grant computes its own
// block (admissionBlock), which consults the queue by position rather than as a
// claim — were the grant to read this, every waiter would claim against every
// other and no one would ever be admitted.
func (s *Store) liveAdmissionClaim(q claimQuerier, runtimePath string) (*CheckoutClaim, error) {
	rows, err := q.Query(
		`SELECT set_id, pid, proc_start, registered_at FROM admission_waiters
		 WHERE runtime_path = ? ORDER BY id ASC`, runtimePath)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		setID     string
		pid       int
		procStart string
		since     time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart, registered sql.NullString
		if err := rows.Scan(&c.setID, &c.pid, &procStart, &registered); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.since = parseTime(registered.String)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if s.alive(c.pid, c.procStart) {
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimQueuedCommand, RuntimePath: runtimePath, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}

// liveGateHoldClaim returns the live claim-bearing Checkout gate hold on
// runtimePath (skipping excludeSet, empty excludes nothing) as a Checkout claim,
// or nil when none is live. Only holds flagged claim=1 are considered — a
// non-claiming hold (HITL, verify-fail, clean Failed gate) contributes quiescence
// occupancy but no admission claim (ADR-0135). excludeSet lets StartDrain admit a
// set's own gate-launched re-acquire past its own hold, mirroring the waiter arm.
// The runtime_path is UNIQUE so there is at most one row, but it is still read to
// completion before liveness is evaluated so the single connection is free.
func (s *Store) liveGateHoldClaim(q claimQuerier, runtimePath, excludeSet string) (*CheckoutClaim, error) {
	rows, err := q.Query(
		`SELECT set_id, pid, proc_start, registered_at FROM checkout_gate_holds
		 WHERE runtime_path = ? AND claim = 1`, runtimePath)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		setID     string
		pid       int
		procStart string
		since     time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart, registered sql.NullString
		if err := rows.Scan(&c.setID, &c.pid, &procStart, &registered); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.since = parseTime(registered.String)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if c.setID == excludeSet {
			continue
		}
		if s.alive(c.pid, c.procStart) {
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimFailedGate, RuntimePath: runtimePath, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}

// liveDrainClaim returns the first live running Drain on runtimePath as a Checkout
// claim, or nil when none is live. Rows are fully read and the cursor closed
// before liveness is evaluated so the store's single connection is free for the
// next statement.
func (s *Store) liveDrainClaim(q claimQuerier, runtimePath string) (*CheckoutClaim, error) {
	rows, err := q.Query(
		`SELECT set_id, pid, proc_start, started_at FROM drains
		 WHERE state = ? AND runtime_path = ?`,
		StateRunning, runtimePath)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		setID     string
		pid       int
		procStart string
		since     time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart sql.NullString
		var started string
		if err := rows.Scan(&c.setID, &c.pid, &procStart, &started); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.since = parseTime(started)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if s.alive(c.pid, c.procStart) {
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimRunningDrain, RuntimePath: runtimePath, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}

// liveWaiterClaim returns the first live Recovery waiter on runtimePath (skipping
// excludeSet, empty excludes nothing) as a Checkout claim, or nil when none is
// live. excludeSet lets StartDrain admit a set's own resume past its still-
// registered waiter (deregistration happens after the resume BeginDrain today).
// Rows are fully read and the cursor closed before liveness is evaluated so the
// single connection is free for the next statement.
func (s *Store) liveWaiterClaim(q claimQuerier, runtimePath, excludeSet string) (*CheckoutClaim, error) {
	rows, err := q.Query(
		`SELECT set_id, pid, proc_start, registered_at FROM recovery_waiters
		 WHERE runtime_path = ?`, runtimePath)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		setID     string
		pid       int
		procStart string
		since     time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart, registered sql.NullString
		if err := rows.Scan(&c.setID, &c.pid, &procStart, &registered); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.since = parseTime(registered.String)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if c.setID == excludeSet {
			continue
		}
		if s.alive(c.pid, c.procStart) {
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimQuotaWaiter, RuntimePath: runtimePath, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}

// ClaimHeldBy reports whether the live Checkout claim on runtimePath for setID
// is a running Drain row this very process inserted — the re-entrancy test a
// nested Tree-stable operation asks before acquiring (ADR-0238).
//
// A command that already holds the checkout for a set cannot take it a second
// time: the Set claim it holds refuses it, and waiting for that claim would be
// waiting on itself. Ownership is the same PID + start-token pairing
// checkout-quiescence exempts through ProcessOwner, and it is narrowed to the
// claim's own set so a process holding the tree for one set still faces the
// full admission check when it asks for another.
//
// Only the running-drain arm is consulted: it is the only claim species a
// process takes and then keeps holding while it calls further into pop.
func (s *Store) ClaimHeldBy(runtimePath, setID string, owner ProcessOwner) (bool, error) {
	if runtimePath == "" || setID == "" || owner.PID == 0 {
		return false, nil
	}
	rows, err := s.db.Query(
		`SELECT pid, proc_start FROM drains WHERE state = ? AND runtime_path = ? AND set_id = ?`,
		StateRunning, runtimePath, setID)
	if err != nil {
		return false, err
	}
	type candidate struct {
		pid       int
		procStart string
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		var procStart sql.NullString
		if err := rows.Scan(&c.pid, &procStart); err != nil {
			_ = rows.Close()
			return false, err
		}
		c.procStart = procStart.String
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	_ = rows.Close()

	for _, c := range cands {
		if owner.owns(c.pid, c.procStart) && s.alive(c.pid, c.procStart) {
			return true, nil
		}
	}
	return false, nil
}
