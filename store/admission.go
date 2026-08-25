package store

import (
	"database/sql"
	"time"
)

// AdmissionWaiter is one place in a runtime path's Admission queue: a command
// that asked for the checkout, found it held, and is waiting for a window rather
// than refusing (ADR-0239). ID is assigned by the store and *is* the place in the
// line — the queue is strict registration FIFO, blind to task-set priority, so of
// two sets already waiting the one that asked first goes first.
//
// PID and ProcStart are the same owner pairing drains, gate holds and recovery
// waiters carry, so a waiter whose process died (a kill -9, a closed terminal)
// is swept by the opportunistic reconcile instead of holding the line forever.
type AdmissionWaiter struct {
	ID           int64
	RuntimePath  string
	Repo         string
	SetID        string
	PID          int
	ProcStart    string
	RegisteredAt time.Time
}

// AdmissionBlockKind names why an admission grant was withheld. It is the
// wait-line vocabulary: each kind carries the facts a human needs to go and
// unblock the checkout rather than poll it.
type AdmissionBlockKind string

const (
	// AdmissionBlockSetClaimed: a live Set claim holds (repo, set) — the same set
	// is already draining, in some checkout. Set carries the claiming drain.
	AdmissionBlockSetClaimed AdmissionBlockKind = "set_claimed"
	// AdmissionBlockCheckoutClaimed: a live Checkout claim holds the runtime path
	// — another set's running Drain, parked Recovery waiter, or claim-bearing
	// Failed-gate hold. Checkout carries the holder and the claim reason.
	AdmissionBlockCheckoutClaimed AdmissionBlockKind = "checkout_claimed"
	// AdmissionBlockBehindWaiter: nothing holds the checkout, but a waiter that
	// registered earlier is first in line. AheadSetID names it.
	AdmissionBlockBehindWaiter AdmissionBlockKind = "behind_waiter"
)

// AdmissionBlock is why one admission attempt was not granted, computed inside
// the grant transaction so there is no second query and no TOCTOU. Exactly one of
// Set / Checkout is populated for the two claim kinds; AheadSetID is populated
// for the queue-position kind.
type AdmissionBlock struct {
	Kind          AdmissionBlockKind
	Set           *SetClaim
	Checkout      *CheckoutClaim
	AheadSetID    string
	AheadWaiterID int64
}

// Err renders the block as the typed refusal a non-waiting caller gets, so
// StartDrain's two errors and the wait line describe the same block. A
// queue-position block has no refusal — a caller that never queued is never
// behind anyone — and yields nil.
func (b *AdmissionBlock) Err() error {
	if b == nil {
		return nil
	}
	switch b.Kind {
	case AdmissionBlockSetClaimed:
		if b.Set != nil {
			return &SetClaimedError{Claim: *b.Set}
		}
	case AdmissionBlockCheckoutClaimed:
		if b.Checkout != nil {
			return &CheckoutClaimedError{Claim: *b.Checkout}
		}
	}
	return nil
}

// RegisterAdmissionWaiter appends w to its runtime path's Admission queue and
// returns it carrying the assigned ID — its place in the line. Registration is
// append-only and never upserted: a second command from the same set is a second
// place, because it asked at a different time.
func (s *Store) RegisterAdmissionWaiter(w AdmissionWaiter) (AdmissionWaiter, error) {
	if w.RuntimePath == "" || w.SetID == "" {
		return AdmissionWaiter{}, nil
	}
	if w.RegisteredAt.IsZero() {
		w.RegisteredAt = time.Now().UTC()
	}
	res, err := s.db.Exec(
		`INSERT INTO admission_waiters (runtime_path, repo, set_id, pid, proc_start, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		w.RuntimePath, w.Repo, w.SetID, w.PID, nullString(w.ProcStart),
		w.RegisteredAt.UTC().Format(timeLayout))
	if err != nil {
		return AdmissionWaiter{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AdmissionWaiter{}, err
	}
	w.ID = id
	return w, nil
}

// DeleteAdmissionWaiter removes one waiter by its place in the line. A missing
// row is not an error: the wait loop deregisters on every exit path, including
// the one where the grant transaction already consumed the row.
func (s *Store) DeleteAdmissionWaiter(id int64) error {
	if id <= 0 {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM admission_waiters WHERE id = ?`, id)
	return err
}

// AdmissionWaitersOn returns the queue on one runtime path in registration
// order, dead owners included — callers that care about liveness (the grant's
// ordering check) apply it themselves.
func (s *Store) AdmissionWaitersOn(runtimePath string) ([]AdmissionWaiter, error) {
	if runtimePath == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT id, runtime_path, repo, set_id, pid, proc_start, registered_at
		 FROM admission_waiters WHERE runtime_path = ? ORDER BY id ASC`, runtimePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AdmissionWaiter
	for rows.Next() {
		var w AdmissionWaiter
		var procStart, registered sql.NullString
		if err := rows.Scan(&w.ID, &w.RuntimePath, &w.Repo, &w.SetID, &w.PID, &procStart, &registered); err != nil {
			return nil, err
		}
		w.ProcStart = procStart.String
		w.RegisteredAt = parseTime(registered.String)
		out = append(out, w)
	}
	return out, rows.Err()
}

// AllAdmissionWaiters returns every registered waiter across all runtime paths,
// in registration order.
func (s *Store) AllAdmissionWaiters() ([]AdmissionWaiter, error) {
	rows, err := s.db.Query(
		`SELECT id, runtime_path, repo, set_id, pid, proc_start, registered_at
		 FROM admission_waiters ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AdmissionWaiter
	for rows.Next() {
		var w AdmissionWaiter
		var procStart, registered sql.NullString
		if err := rows.Scan(&w.ID, &w.RuntimePath, &w.Repo, &w.SetID, &w.PID, &procStart, &registered); err != nil {
			return nil, err
		}
		w.ProcStart = procStart.String
		w.RegisteredAt = parseTime(registered.String)
		out = append(out, w)
	}
	return out, rows.Err()
}

// ReconcileAdmissionWaiters is the admission arm of the opportunistic reconcile
// pass: it deletes waiters whose registering process is no longer alive, using
// the same PID+start-token liveness every other sweep uses. A dead owner's waiter
// would otherwise sit at the head of a strict-FIFO queue and stall every command
// behind it — the one failure mode a never-refusing queue cannot tolerate. It
// returns the number of waiters swept.
//
// Rows are fully read and the cursor closed before any DELETE is issued so the
// store's single connection is never asked to run a follow-up with an open
// result set.
func (s *Store) ReconcileAdmissionWaiters() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT id, pid, proc_start FROM admission_waiters`)
	if err != nil {
		return 0, err
	}
	type waiter struct {
		id        int64
		pid       int
		procStart string
	}
	var waiters []waiter
	for rows.Next() {
		var w waiter
		var procStart sql.NullString
		if err := rows.Scan(&w.id, &w.pid, &procStart); err != nil {
			_ = rows.Close()
			return 0, err
		}
		w.procStart = procStart.String
		waiters = append(waiters, w)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	var swept int
	for _, w := range waiters {
		if s.alive(w.pid, w.procStart) {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM admission_waiters WHERE id = ?`, w.id); err != nil {
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

// TryAdmitDrain is the Admission grant: in one transaction it checks the Set
// claim, the Checkout claim union and (for a queued caller) its place in the
// line, and — when nothing stands in the way — inserts the running Drain row and
// consumes the waiter's registration. Inserting that one row *is* taking both
// claims, since each is derived from it, which is what makes the grant
// all-or-nothing: a waiter never comes away holding half its lock-set, so it
// cannot be a link in a deadlock cycle while it waits.
//
// waiterID is the caller's place in the Admission queue, or 0 for a caller that
// never queued (StartDrain's refusing path, and every machine caller). A queued
// caller is granted only when it is the first *live* waiter on the path, which
// is what makes the ordering strict registration FIFO; an unqueued caller is not
// held behind the queue, because it is not in it.
//
// When the grant lands, the returned block is nil and the Drain carries its
// assigned ID. When it does not, the Drain is zero and the block names the one
// thing that stood in the way — computed under the same write lock the insert
// would have taken, so there is no TOCTOU between the reason and the retry.
func (s *Store) TryAdmitDrain(d Drain, waiterID int64) (Drain, *AdmissionBlock, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Drain{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	block, err := s.admissionBlock(tx, d, waiterID)
	if err != nil {
		return Drain{}, nil, err
	}
	if block != nil {
		return Drain{}, block, nil
	}

	res, err := tx.Exec(
		`INSERT INTO drains (repo, set_id, runtime_path, pid, proc_start, started_at, state)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.Repo, d.SetID, d.RuntimePath, d.PID, nullString(d.ProcStart),
		d.StartedAt.UTC().Format(timeLayout), StateRunning)
	if err != nil {
		return Drain{}, nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Drain{}, nil, err
	}
	if waiterID > 0 {
		if _, err := tx.Exec(`DELETE FROM admission_waiters WHERE id = ?`, waiterID); err != nil {
			return Drain{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Drain{}, nil, err
	}
	d.ID = id
	d.State = StateRunning
	return d, nil, nil
}

// admissionBlock derives the first thing standing between d and the checkout, or
// nil when the way is clear. It runs on the grant's open transaction, under the
// write lock BEGIN IMMEDIATE already took, so what it reads cannot move before
// the insert.
//
// The order is the order a human needs the answers in: the Set claim first,
// because a set draining in another worktree is a different place to go than a
// busy tree and is the answer that matters when both are true; then the three
// Checkout claim arms; then the queue. A set's own recovery waiter and own gate
// hold are excluded so its resume or gate-launched re-acquire is never
// self-blocked.
func (s *Store) admissionBlock(tx *sql.Tx, d Drain, waiterID int64) (*AdmissionBlock, error) {
	setClaim, err := s.liveSetClaim(tx, d.Repo, d.SetID)
	if err != nil {
		return nil, err
	}
	if setClaim != nil {
		return &AdmissionBlock{Kind: AdmissionBlockSetClaimed, Set: setClaim}, nil
	}

	drainClaim, err := s.liveDrainClaim(tx, d.RuntimePath)
	if err != nil {
		return nil, err
	}
	if drainClaim != nil {
		return &AdmissionBlock{Kind: AdmissionBlockCheckoutClaimed, Checkout: drainClaim}, nil
	}

	waiterClaim, err := s.liveWaiterClaim(tx, d.RuntimePath, d.SetID)
	if err != nil {
		return nil, err
	}
	if waiterClaim != nil {
		return &AdmissionBlock{Kind: AdmissionBlockCheckoutClaimed, Checkout: waiterClaim}, nil
	}

	gateClaim, err := s.liveGateHoldClaim(tx, d.RuntimePath, d.SetID)
	if err != nil {
		return nil, err
	}
	if gateClaim != nil {
		return &AdmissionBlock{Kind: AdmissionBlockCheckoutClaimed, Checkout: gateClaim}, nil
	}

	if waiterID <= 0 {
		return nil, nil
	}
	return s.queuePositionBlock(tx, d.RuntimePath, waiterID)
}

// queuePositionBlock reports the live waiter ahead of waiterID on runtimePath, or
// nil when waiterID is first in line. Dead-owner waiters are stepped over rather
// than waited on: liveness is applied here as well as in the reconcile sweep, so
// a queue never stalls in the window between an owner dying and someone reading
// the store.
func (s *Store) queuePositionBlock(tx *sql.Tx, runtimePath string, waiterID int64) (*AdmissionBlock, error) {
	rows, err := tx.Query(
		`SELECT id, set_id, pid, proc_start FROM admission_waiters
		 WHERE runtime_path = ? ORDER BY id ASC`, runtimePath)
	if err != nil {
		return nil, err
	}
	type queued struct {
		id        int64
		setID     string
		pid       int
		procStart string
	}
	var line []queued
	for rows.Next() {
		var q queued
		var procStart sql.NullString
		if err := rows.Scan(&q.id, &q.setID, &q.pid, &procStart); err != nil {
			_ = rows.Close()
			return nil, err
		}
		q.procStart = procStart.String
		line = append(line, q)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, q := range line {
		if q.id == waiterID {
			return nil, nil
		}
		if !s.alive(q.pid, q.procStart) {
			continue
		}
		return &AdmissionBlock{
			Kind:          AdmissionBlockBehindWaiter,
			AheadSetID:    q.setID,
			AheadWaiterID: q.id,
		}, nil
	}
	return nil, nil
}
