package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrCheckoutBusy reports that an out-of-band mutation was refused because the
// runtime checkout was not quiescent — a live drain or a live gate hold owns it
// (ADR-0104). The refusal carries the occupant so the caller can name it.
var ErrCheckoutBusy = errors.New("checkout not quiescent")

// Occupant kinds returned on ErrCheckoutBusy.
const (
	OccupantDrain    = "drain"
	OccupantGateHold = "gate-hold"
	OccupantWaiter   = "waiter"
)

// CheckoutOccupant names what holds a runtime checkout when an out-of-band
// mutation is refused: a live running drain, a live Recovery waiter (quota
// recovery — a process that will resume), or a live Checkout gate hold. Since
// carries the drain's start / the waiter's or hold's registration time so the
// caller's error can say how long the occupant has held the checkout.
//
// NextInTurn is meaningful only for OccupantWaiter: it reports whether the
// waiting set is first under Recovery turn ordering for its checkout — true when
// resume is imminent (nothing ahead of it), false when it is queued behind
// another waiter that holds the turn. This lets the refusal tell the human how
// soon the waiter would resume and re-verify over an out-of-band verdict
// (ADR-0135).
type CheckoutOccupant struct {
	Kind       string
	SetID      string
	PID        int
	Since      time.Time
	NextInTurn bool
}

// Execer is the subset of *sql.DB / *sql.Conn / *sql.Tx used by the
// executor-scoped verdict writers, so a mutation can run either on the store's
// shared handle or inside an open transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// MutateIfCheckoutQuiescent enforces ADR-0104: it runs mutate only when the
// runtime checkout is quiescent — no live running drain and no live Checkout
// gate hold on runtimePath — refusing with ErrCheckoutBusy (and the naming
// occupant) otherwise. The quiescence check and the mutation share ONE
// transaction opened with BEGIN IMMEDIATE, so the write lock is held across both:
// a concurrent StartDrain cannot commit a running row between the check and the
// mutation, and a drain that became live is seen by the check. This is the
// StartDrain mutual-exclusion pattern applied to human out-of-band writes.
//
// The store's liveness policy reports whether a recorded owner's process is still
// running (checked against its PID and start token so a reused PID does not read
// as live); a dead-owner drain row or gate hold therefore does not block.
//
// mutate runs on the transaction's connection (an Execer) so its writes land
// inside the same transaction; it may also perform filesystem work (a manifest
// append) — any error rolls the whole transaction back. The single connection is
// held for the transaction's duration, so all reads fully drain their cursors
// before the next statement (the rows-close constraint).
func (s *Store) MutateIfCheckoutQuiescent(
	runtimePath string,
	mutate func(ctx context.Context, ex Execer) error,
) (*CheckoutOccupant, error) {
	if runtimePath == "" {
		return nil, errors.New("MutateIfCheckoutQuiescent: empty runtime path")
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	// BEGIN IMMEDIATE takes the write lock up front, so no concurrent writer can
	// slip a running drain in between the check below and the mutation.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	// Live drain on this checkout? Read every running row, close the cursor, then
	// evaluate liveness (a dead-owner row does not block).
	rows, err := conn.QueryContext(ctx,
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
		startedAt time.Time
	}
	var drains []candidate
	for rows.Next() {
		var c candidate
		var procStart sql.NullString
		var started string
		if err := rows.Scan(&c.setID, &c.pid, &procStart, &started); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.procStart = procStart.String
		c.startedAt = parseTime(started)
		drains = append(drains, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, c := range drains {
		if s.alive(c.pid, c.procStart) {
			return &CheckoutOccupant{Kind: OccupantDrain, SetID: c.setID, PID: c.pid, Since: c.startedAt}, ErrCheckoutBusy
		}
	}

	// Live Recovery waiter on this checkout? A quota-parked set is an automatic
	// process that will resume and re-run its verify phase, so accepting or
	// remediating out of band now would be overwritten when it resumes (ADR-0135).
	// Read every waiter on the path, close the cursor, then evaluate liveness (a
	// dead-owner waiter does not block — it is swept by the reconcile pass).
	rows, err = conn.QueryContext(ctx,
		`SELECT set_id, pid, proc_start, registered_at, priority
		 FROM recovery_waiters WHERE runtime_path = ?`, runtimePath)
	if err != nil {
		return nil, err
	}
	type waiterCand struct {
		setID        string
		pid          int
		procStart    string
		registeredAt time.Time
		priority     int
	}
	var waiters []waiterCand
	for rows.Next() {
		var w waiterCand
		var procStart, registered sql.NullString
		if err := rows.Scan(&w.setID, &w.pid, &procStart, &registered, &w.priority); err != nil {
			_ = rows.Close()
			return nil, err
		}
		w.procStart = procStart.String
		w.registeredAt = parseTime(registered.String)
		waiters = append(waiters, w)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	// Front-runner under Recovery turn ordering (priority DESC, then earliest
	// registration): the live waiter that resumes soonest is the threat to an
	// out-of-band verdict, so it is the occupant we name.
	var front *waiterCand
	for i := range waiters {
		w := &waiters[i]
		if !s.alive(w.pid, w.procStart) {
			continue
		}
		if front == nil ||
			w.priority > front.priority ||
			(w.priority == front.priority && w.registeredAt.Before(front.registeredAt)) {
			front = w
		}
	}
	if front != nil {
		// next-in-turn unless another set currently holds the recovery turn on the
		// path (that set — itself a former waiter — is ahead of the front-runner).
		var turnSet sql.NullString
		err = conn.QueryRowContext(ctx,
			`SELECT set_id FROM recovery_turns WHERE runtime_path = ?`, runtimePath).
			Scan(&turnSet)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		nextInTurn := !turnSet.Valid || turnSet.String == front.setID
		return &CheckoutOccupant{
			Kind: OccupantWaiter, SetID: front.setID, PID: front.pid,
			Since: front.registeredAt, NextInTurn: nextInTurn,
		}, ErrCheckoutBusy
	}

	// Live gate hold on this checkout? (runtime_path is UNIQUE, so at most one.)
	var holdSet string
	var holdPID int
	var holdProc, holdReg sql.NullString
	err = conn.QueryRowContext(ctx,
		`SELECT set_id, pid, proc_start, registered_at FROM checkout_gate_holds
		 WHERE runtime_path = ?`, runtimePath).
		Scan(&holdSet, &holdPID, &holdProc, &holdReg)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil && s.alive(holdPID, holdProc.String) {
		return &CheckoutOccupant{Kind: OccupantGateHold, SetID: holdSet, PID: holdPID, Since: parseTime(holdReg.String)}, ErrCheckoutBusy
	}

	if err := mutate(ctx, conn); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	committed = true
	return nil, nil
}
