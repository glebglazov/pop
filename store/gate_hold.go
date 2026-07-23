package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrGateHoldHeld reports that PutCheckoutGateHold refused to register because a
// different live owner already holds the gate on that runtime path (ADR-0135, no
// steal). A dead owner's hold is replaced silently; only a live foreign owner
// triggers this refusal.
var ErrGateHoldHeld = errors.New("checkout gate hold held by another live owner")

// CheckoutGateHold records that a drain is parked at a human-wait gate (Failed
// or HITL) on a runtime checkout. While active it blocks recovery turn
// acquisition on that path (ADR-0100). It carries the registering process's
// owner identity (PID plus start token, the same pairing drains use) so a hold
// whose owner died can be swept by the opportunistic reconcile pass instead of
// blocking that checkout forever.
//
// Claim distinguishes a claim-bearing hold — a Failed gate parked over a dirty
// tree (ADR-0135) — from a non-claiming hold (HITL gate, verify-fail gate,
// clean-tree Failed gate). A non-claiming hold contributes only quiescence
// occupancy; a claim-bearing one also blocks another set's admission via the
// checkout claim union. Dirtiness is snapshotted onto the row at park time and
// never re-evaluated, so cleaning the tree mid-gate does not release the claim.
type CheckoutGateHold struct {
	RuntimePath  string
	SetID        string
	PID          int
	ProcStart    string
	Claim        bool
	RegisteredAt time.Time
}

// PutCheckoutGateHold registers (or refreshes) a gate hold for one runtime path.
// The runtime_path is UNIQUE: at most one gate session per checkout at a time. It
// refuses (ErrGateHoldHeld) to replace a hold whose owner is a *different live*
// process — no steal — but replaces a dead owner's hold or refreshes the caller's
// own. The read-then-write runs in one BEGIN IMMEDIATE transaction so the liveness
// check and the upsert share the write lock.
func (s *Store) PutCheckoutGateHold(h CheckoutGateHold) error {
	if h.RuntimePath == "" || h.SetID == "" {
		return nil
	}
	registeredAt := h.RegisteredAt
	if registeredAt.IsZero() {
		registeredAt = time.Now().UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(
		`SELECT set_id, pid, proc_start FROM checkout_gate_holds WHERE runtime_path = ?`,
		h.RuntimePath)
	var exSet string
	var exPID int
	var exProc sql.NullString
	switch err := row.Scan(&exSet, &exPID, &exProc); err {
	case nil:
		// A different set's hold may only be replaced when its owner is dead; a live
		// foreign owner is never stolen from.
		if exSet != h.SetID && s.alive(exPID, exProc.String) {
			return ErrGateHoldHeld
		}
	case sql.ErrNoRows:
		// No existing hold — free to register.
	default:
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO checkout_gate_holds (runtime_path, set_id, pid, proc_start, claim, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(runtime_path) DO UPDATE SET
		   set_id = excluded.set_id,
		   pid = excluded.pid,
		   proc_start = excluded.proc_start,
		   claim = excluded.claim,
		   registered_at = excluded.registered_at`,
		h.RuntimePath,
		h.SetID,
		h.PID,
		nullString(h.ProcStart),
		boolToInt(h.Claim),
		registeredAt.UTC().Format(timeLayout)); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCheckoutGateHold removes the gate hold for one runtime path, but only when
// it belongs to setID — release deletes the caller's own hold, never another
// set's (ADR-0135). A missing row, or a row owned by a different set, is not an
// error.
func (s *Store) DeleteCheckoutGateHold(runtimePath, setID string) error {
	if runtimePath == "" || setID == "" {
		return nil
	}
	_, err := s.db.Exec(
		`DELETE FROM checkout_gate_holds WHERE runtime_path = ? AND set_id = ?`,
		runtimePath, setID)
	return err
}

// GetCheckoutGateHold returns the active gate hold for one runtime path, or nil
// when no hold is registered.
func (s *Store) GetCheckoutGateHold(runtimePath string) (*CheckoutGateHold, error) {
	if runtimePath == "" {
		return nil, nil
	}
	row := s.db.QueryRow(
		`SELECT runtime_path, set_id, pid, proc_start, claim, registered_at
		 FROM checkout_gate_holds WHERE runtime_path = ?`, runtimePath)
	var h CheckoutGateHold
	var procStart, registeredAt sql.NullString
	var claim int
	if err := row.Scan(&h.RuntimePath, &h.SetID, &h.PID, &procStart, &claim, &registeredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	h.ProcStart = procStart.String
	h.Claim = claim != 0
	h.RegisteredAt = parseTime(registeredAt.String)
	return &h, nil
}

// ReconcileGateHolds is the gate-hold arm of the opportunistic reconcile pass:
// in one bounded transaction it deletes gate holds whose registering process is
// no longer alive, using the same PID+start-token liveness the drains sweep uses
// so a reused PID is not mistaken for the original owner. It returns the number
// of holds swept.
//
// Rows are fully read and the cursor closed before any DELETE is issued so the
// store's single connection is never asked to run a follow-up query with an open
// result set.
func (s *Store) ReconcileGateHolds() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT runtime_path, pid, proc_start FROM checkout_gate_holds`)
	if err != nil {
		return 0, err
	}
	type held struct {
		runtimePath string
		pid         int
		procStart   string
	}
	var holds []held
	for rows.Next() {
		var h held
		var procStart sql.NullString
		if err := rows.Scan(&h.runtimePath, &h.pid, &procStart); err != nil {
			_ = rows.Close()
			return 0, err
		}
		h.procStart = procStart.String
		holds = append(holds, h)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	var swept int
	for _, h := range holds {
		if s.alive(h.pid, h.procStart) {
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM checkout_gate_holds WHERE runtime_path = ?`, h.runtimePath); err != nil {
			return 0, err
		}
		swept++
	}
	if swept == 0 {
		return 0, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return swept, nil
}
