package tasks

import (
	"time"
)

// SetBackoffInfo mirrors store.SetBackoff at the tasks boundary: the recent
// abnormal-terminal history the Queue derives backoff and parking from
// (ADR-0055). It carries the count of consecutive abnormal terminals since the
// last clean one, the instant of the most recent abnormal terminal, and the
// latest park-clear (unpark) event.
type SetBackoffInfo struct {
	ConsecutiveAbnormal int
	LastAbnormalAt      time.Time
	ParkClearedAt       time.Time
}

// ReadSetBackoff projects a (repo, set)'s abnormal-terminal history from the
// store. repo is the repository's canonical common dir — the Drain row's repo
// key, as set by BeginDrain. It opens the store only when it already exists, so
// a pure reader never materialises an empty database; a missing store yields a
// zero value (no history, spawnable).
func ReadSetBackoff(d *Deps, repo, setID string) (SetBackoffInfo, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return SetBackoffInfo{}, err
	}
	defer func() { _ = s.Close() }()
	info, err := s.ReadSetBackoff(repo, setID)
	if err != nil {
		return SetBackoffInfo{}, err
	}
	return SetBackoffInfo(info), nil
}

// RecordParkClear writes a durable park-clear (unpark) event for (repo, set),
// creating the store if needed. A clear newer than the set's latest abnormal
// Drain lifts the derived park, making the set spawnable again (ADR-0055).
func RecordParkClear(d *Deps, repo, setID string) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.RecordParkClear(repo, setID, time.Now().UTC())
}
