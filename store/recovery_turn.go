package store

import (
	"database/sql"
	"time"
)

// RecoveryBlockKind names why a post-cooldown recovery-turn acquisition was
// denied. The block itself is by design (ADR-0100); the kind explains it so the
// waiter's status line can name the actual blocker instead of claiming it is
// still waiting on quota.
type RecoveryBlockKind string

const (
	// RecoveryBlockClaimed: a live Checkout claim holds the path (ADR-0135) — a
	// running Drain of another set, or another set's claim-bearing Failed-gate
	// hold (a dirty tree under review). The Claim field carries the owning set and
	// claim kind so the status line can name it. A non-claiming gate hold (HITL,
	// verify-fail, clean Failed gate) and peer quota waiters are not blockers here:
	// peers are resolved by the waiter-ordering check below.
	RecoveryBlockClaimed RecoveryBlockKind = "claimed"
	// RecoveryBlockTurnHeld: another set already holds the recovery turn there.
	RecoveryBlockTurnHeld RecoveryBlockKind = "turn_held"
	// RecoveryBlockBehindWaiter: another waiter is first in the ordering.
	RecoveryBlockBehindWaiter RecoveryBlockKind = "behind_waiter"
)

// RecoveryBlock is the reason a recovery turn was denied after the waiter's
// cooldown elapsed: a kind plus the ID of the set that is blocking the path. For
// a RecoveryBlockClaimed block, Claim carries the live Checkout claim (owning set
// + claim kind) so the status line can render "claimed by set X — <reason>".
type RecoveryBlock struct {
	Kind  RecoveryBlockKind
	SetID string
	Claim *CheckoutClaim
}

// TryAcquireRecoveryTurn atomically grants a recovery turn to w when its preset
// cooldown has elapsed, no other set's Checkout claim blocks the path (ADR-0135),
// no other turn is held there, and w is first among eligible waiters on that path
// (priority descending, then registration time ascending).
//
// The claim union — a live running Drain or a live claim-bearing Failed-gate hold
// of another set — defers the turn; a non-claiming gate hold (HITL, approval,
// verify-fail, clean Failed gate) does not, so a quota-recovered waiter resumes
// past an open human-wait menu. The acquiring waiter's own registration is
// excluded from the claim check so a set never self-blocks, and peer quota waiters
// are left to the ordering check so two waiters never deadlock claiming each other.
//
// When the turn is granted the returned *RecoveryBlock is nil. When it is denied
// after the cooldown has elapsed, the block names the actual blocker, computed
// inside the same acquisition transaction so there is no second query and no
// TOCTOU. Before the cooldown elapses the block is nil.
func (s *Store) TryAcquireRecoveryTurn(w RecoveryWaiter, now time.Time) (bool, *RecoveryBlock, error) {
	if w.SetID == "" || w.RuntimePath == "" || now.Before(w.ResetAt.UTC()) {
		return false, nil, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if claim, err := s.liveDrainClaim(tx, w.RuntimePath); err != nil {
		return false, nil, err
	} else if claim != nil {
		return false, &RecoveryBlock{Kind: RecoveryBlockClaimed, SetID: claim.SetID, Claim: claim}, nil
	}
	if claim, err := s.liveGateHoldClaim(tx, w.RuntimePath, w.SetID); err != nil {
		return false, nil, err
	} else if claim != nil {
		return false, &RecoveryBlock{Kind: RecoveryBlockClaimed, SetID: claim.SetID, Claim: claim}, nil
	}

	var turnSetID sql.NullString
	err = tx.QueryRow(
		`SELECT set_id FROM recovery_turns WHERE runtime_path = ?`, w.RuntimePath).
		Scan(&turnSetID)
	if err != nil && err != sql.ErrNoRows {
		return false, nil, err
	}
	if turnSetID.Valid {
		return false, &RecoveryBlock{Kind: RecoveryBlockTurnHeld, SetID: turnSetID.String}, nil
	}

	nowStr := now.UTC().Format(timeLayout)
	var firstSetID string
	err = tx.QueryRow(
		`SELECT set_id FROM recovery_waiters
		 WHERE runtime_path = ? AND reset_at <= ?
		 ORDER BY priority DESC, registered_at ASC
		 LIMIT 1`,
		w.RuntimePath, nowStr).Scan(&firstSetID)
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if firstSetID != w.SetID {
		return false, &RecoveryBlock{Kind: RecoveryBlockBehindWaiter, SetID: firstSetID}, nil
	}

	_, err = tx.Exec(
		`INSERT INTO recovery_turns (runtime_path, set_id, acquired_at) VALUES (?, ?, ?)`,
		w.RuntimePath, w.SetID, now.UTC().Format(timeLayout))
	if err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, err
	}
	return true, nil, nil
}

// ReleaseRecoveryTurn drops the checkout-scoped recovery turn for one runtime
// path. A missing row is not an error.
func (s *Store) ReleaseRecoveryTurn(runtimePath string) error {
	if runtimePath == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM recovery_turns WHERE runtime_path = ?`, runtimePath)
	return err
}
