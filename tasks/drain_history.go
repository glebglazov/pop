package tasks

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// DrainRecord is one Drain projected at the tasks boundary for the Queue journal
// view (ADR-0055): the durable record of one supervised execution, carrying its
// start, its terminal exit reason (empty State while still running), and the
// agent-quota fields a quota_paused terminal recorded.
type DrainRecord struct {
	Repo             string
	SetID            string
	RuntimePath      string
	PID              int
	StartedAt        time.Time
	State            string
	FinishedAt       time.Time
	ExhaustedPreset  string
	ExhaustedPinned  bool
	ExhaustedResetAt time.Time
}

// Running reports whether the drain has not yet reached a terminal.
func (r DrainRecord) Running() bool { return r.State == store.StateRunning }

// RunningDrain is a live (process-alive) running Drain, projected for the queue's
// multi-checkout busy detection. It replaces the journal's open-spawn tracking:
// a running Drain row whose process is alive IS the live execution claim
// (ADR-0055).
type RunningDrain struct {
	Repo        string
	SetID       string
	RuntimePath string
	PID         int
	StartedAt   time.Time
}

// ParkClearRecord is one park-clear (unpark) event at the tasks boundary.
type ParkClearRecord struct {
	Repo      string
	SetID     string
	ClearedAt time.Time
}

// AllDrains returns every Drain (running and terminal), oldest-first. It opens
// the store only when it already exists, so a pure reader never materialises an
// empty database.
func AllDrains(d *Deps) ([]DrainRecord, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.AllDrains()
	if err != nil {
		return nil, err
	}
	out := make([]DrainRecord, 0, len(rows))
	for _, dr := range rows {
		out = append(out, DrainRecord{
			Repo:             dr.Repo,
			SetID:            dr.SetID,
			RuntimePath:      dr.RuntimePath,
			PID:              dr.PID,
			StartedAt:        dr.StartedAt,
			State:            dr.State,
			FinishedAt:       dr.FinishedAt,
			ExhaustedPreset:  dr.ExhaustedPreset,
			ExhaustedPinned:  dr.ExhaustedPinned,
			ExhaustedResetAt: dr.ExhaustedResetAt,
		})
	}
	return out, nil
}

// LiveRunningDrains returns every running Drain whose owning process is still
// alive, oldest-first. PID reuse is defeated by the recorded start token, the
// same liveness logic ReadRuntimeLockStatus uses. It opens the store only when
// it already exists.
func LiveRunningDrains(d *Deps) ([]RunningDrain, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.RunningDrains()
	if err != nil {
		return nil, err
	}
	var out []RunningDrain
	for _, dr := range rows {
		if !drainProcessAlive(d, dr.PID, dr.ProcStart) {
			continue
		}
		out = append(out, RunningDrain{
			Repo:        dr.Repo,
			SetID:       dr.SetID,
			RuntimePath: dr.RuntimePath,
			PID:         dr.PID,
			StartedAt:   dr.StartedAt,
		})
	}
	return out, nil
}

// AllParkClears returns every park-clear (unpark) event, oldest-first. It opens
// the store only when it already exists.
func AllParkClears(d *Deps) ([]ParkClearRecord, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.AllParkClears()
	if err != nil {
		return nil, err
	}
	out := make([]ParkClearRecord, 0, len(rows))
	for _, pc := range rows {
		out = append(out, ParkClearRecord{Repo: pc.Repo, SetID: pc.SetID, ClearedAt: pc.ClearedAt})
	}
	return out, nil
}
