package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// ErrCheckoutClaimed reports that StartDrain refused because another set's live
// Checkout claim already holds the runtime checkout (ADR-0135). It is distinct
// from ErrDrainInProgress — which names a live *running Drain* on the same (repo,
// set) or checkout — because a claim can be held by a non-executing process (a
// quota-recovery waiter that will resume). A *CheckoutClaimedError carries the
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
	PID    int
	Since  time.Time
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

func (e *CheckoutClaimedError) Error() string {
	return fmt.Sprintf("checkout claimed by set %s (%s)", e.Claim.Holder.ContainerID, e.Claim.Reason.Phrase())
}

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
// quota-waiter claim. The store's liveness policy is applied to every candidate,
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
	return s.liveGateHoldClaim(s.db, runtimePath, "")
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
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimFailedGate, PID: c.pid, Since: c.since}, nil
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
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimRunningDrain, PID: c.pid, Since: c.since}, nil
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
			return &CheckoutClaim{Holder: taskSetHolder(c.setID), Reason: ClaimQuotaWaiter, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}
