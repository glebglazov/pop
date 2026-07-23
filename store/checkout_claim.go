package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrCheckoutClaimed reports that StartDrain refused because another set's live
// Checkout claim already holds the runtime checkout (ADR-0135). It is distinct
// from ErrDrainInProgress — which names a live *running Drain* on the same (repo,
// set) or checkout — because a claim can be held by a non-executing process (a
// quota-recovery waiter that will resume). A *CheckoutClaimedError carries the
// claiming set and claim kind and satisfies errors.Is(err, ErrCheckoutClaimed).
var ErrCheckoutClaimed = errors.New("checkout claimed by another set")

// CheckoutClaimKind names what holds a Checkout claim on a runtime path. The
// claim union is derived at read time (no table): a live running Drain, or a live
// Recovery waiter (the Checkout gate-hold arm lands in a later slice).
type CheckoutClaimKind string

const (
	// ClaimRunningDrain: a live running Drain is executing against the path.
	ClaimRunningDrain CheckoutClaimKind = "running_drain"
	// ClaimQuotaWaiter: a live Recovery waiter is parked on the path, waiting for
	// its preset's cooldown before resuming — an automatic process that will
	// resume, so it claims the checkout (ADR-0135).
	ClaimQuotaWaiter CheckoutClaimKind = "quota_waiter"
)

// CheckoutClaim is a live claim on a runtime checkout, derived at read time from
// the claim union (ADR-0135): the kind of claim and the set that owns it, plus
// the owner's PID and the instant the claim began so a caller can say how long it
// has been held.
type CheckoutClaim struct {
	Kind  CheckoutClaimKind
	SetID string
	PID   int
	Since time.Time
}

// Reason renders the claim kind as a short human phrase for status and refusal
// lines (e.g. "checkout claimed by set X (quota wait)").
func (c CheckoutClaim) Reason() string {
	switch c.Kind {
	case ClaimRunningDrain:
		return "running drain"
	case ClaimQuotaWaiter:
		return "quota wait"
	default:
		return string(c.Kind)
	}
}

// CheckoutClaimedError carries the claim that caused a StartDrain refusal so the
// caller can name the claiming set and claim kind. It unwraps to ErrCheckoutClaimed
// so errors.Is(err, ErrCheckoutClaimed) holds.
type CheckoutClaimedError struct {
	Claim CheckoutClaim
}

func (e *CheckoutClaimedError) Error() string {
	return fmt.Sprintf("checkout claimed by set %s (%s)", e.Claim.SetID, e.Claim.Reason())
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
	return s.liveWaiterClaim(s.db, runtimePath, "")
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
			return &CheckoutClaim{Kind: ClaimRunningDrain, SetID: c.setID, PID: c.pid, Since: c.since}, nil
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
			return &CheckoutClaim{Kind: ClaimQuotaWaiter, SetID: c.setID, PID: c.pid, Since: c.since}, nil
		}
	}
	return nil, nil
}
