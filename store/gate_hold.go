package store

import (
	"database/sql"
	"time"
)

// CheckoutGateHold records that a drain is parked at a human-wait gate (Failed
// or HITL) on a runtime checkout. While active it blocks recovery turn
// acquisition on that path (ADR-0100). It carries the registering process's
// owner identity (PID plus start token, the same pairing drains use) so a hold
// whose owner died can be swept by the opportunistic reconcile pass instead of
// blocking that checkout forever.
type CheckoutGateHold struct {
	RuntimePath  string
	SetID        string
	PID          int
	ProcStart    string
	RegisteredAt time.Time
}

// PutCheckoutGateHold registers (or refreshes) a gate hold for one runtime path.
// The runtime_path is UNIQUE: at most one gate session per checkout at a time.
func (s *Store) PutCheckoutGateHold(h CheckoutGateHold) error {
	if h.RuntimePath == "" || h.SetID == "" {
		return nil
	}
	registeredAt := h.RegisteredAt
	if registeredAt.IsZero() {
		registeredAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO checkout_gate_holds (runtime_path, set_id, pid, proc_start, registered_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(runtime_path) DO UPDATE SET
		   set_id = excluded.set_id,
		   pid = excluded.pid,
		   proc_start = excluded.proc_start,
		   registered_at = excluded.registered_at`,
		h.RuntimePath,
		h.SetID,
		h.PID,
		nullString(h.ProcStart),
		registeredAt.UTC().Format(timeLayout))
	return err
}

// DeleteCheckoutGateHold removes the gate hold for one runtime path. A missing
// row is not an error.
func (s *Store) DeleteCheckoutGateHold(runtimePath string) error {
	if runtimePath == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM checkout_gate_holds WHERE runtime_path = ?`, runtimePath)
	return err
}

// GetCheckoutGateHold returns the active gate hold for one runtime path, or nil
// when no hold is registered.
func (s *Store) GetCheckoutGateHold(runtimePath string) (*CheckoutGateHold, error) {
	if runtimePath == "" {
		return nil, nil
	}
	row := s.db.QueryRow(
		`SELECT runtime_path, set_id, pid, proc_start, registered_at
		 FROM checkout_gate_holds WHERE runtime_path = ?`, runtimePath)
	var h CheckoutGateHold
	var procStart, registeredAt sql.NullString
	if err := row.Scan(&h.RuntimePath, &h.SetID, &h.PID, &procStart, &registeredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	h.ProcStart = procStart.String
	h.RegisteredAt = parseTime(registeredAt.String)
	return &h, nil
}

// ReconcileGateHolds is the gate-hold arm of the opportunistic reconcile pass:
// in one bounded transaction it deletes gate holds whose registering process is
// no longer alive, using the same PID+start-token liveness the drains sweep uses
// so a reused PID is not mistaken for the original owner. It returns the number
// of holds swept. A nil isAlive treats every hold as alive (a no-op).
//
// Rows are fully read and the cursor closed before any DELETE is issued so the
// store's single connection is never asked to run a follow-up query with an open
// result set.
func (s *Store) ReconcileGateHolds(isAlive func(pid int, procStart string) bool) (int, error) {
	if isAlive == nil {
		return 0, nil
	}
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
		if isAlive(h.pid, h.procStart) {
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
