package store

import (
	"database/sql"
	"time"
)

// CheckoutGateHold records that a drain is parked at a human-wait gate (Failed
// or HITL) on a runtime checkout. While active it blocks recovery turn
// acquisition on that path (ADR-0100).
type CheckoutGateHold struct {
	RuntimePath  string
	SetID        string
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
		`INSERT INTO checkout_gate_holds (runtime_path, set_id, registered_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(runtime_path) DO UPDATE SET
		   set_id = excluded.set_id,
		   registered_at = excluded.registered_at`,
		h.RuntimePath,
		h.SetID,
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
		`SELECT runtime_path, set_id, registered_at
		 FROM checkout_gate_holds WHERE runtime_path = ?`, runtimePath)
	var h CheckoutGateHold
	var registeredAt sql.NullString
	if err := row.Scan(&h.RuntimePath, &h.SetID, &registeredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	h.RegisteredAt = parseTime(registeredAt.String)
	return &h, nil
}
