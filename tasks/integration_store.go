package tasks

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// IntegrationEvent is the durable record that a set's branch was integrated, at
// the tasks boundary. ADR-0055 makes integration an explicit appended event so
// "integrated" is never inferred from a vanished binding. BaseRef is the base
// the branch merged into and BranchSHA the integrated branch's HEAD.
type IntegrationEvent struct {
	ScopedKey    string
	SetID        string
	Project      string
	IntegratedAt time.Time
	BaseRef      string
	BranchSHA    string
}

// RecordIntegrationEvent appends one integration event, creating the store on
// first write.
func RecordIntegrationEvent(d *Deps, ev IntegrationEvent) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if ev.IntegratedAt.IsZero() {
		ev.IntegratedAt = time.Now().UTC()
	}
	return s.RecordIntegration(store.Integration{
		ScopedKey:    ev.ScopedKey,
		SetID:        ev.SetID,
		Project:      ev.Project,
		IntegratedAt: ev.IntegratedAt,
		BaseRef:      ev.BaseRef,
		BranchSHA:    ev.BranchSHA,
	})
}

// IntegrationEventsForSet returns every integration event for setID, newest
// first. It opens the store only when it already exists.
func IntegrationEventsForSet(d *Deps, setID string) ([]IntegrationEvent, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.IntegrationsForSet(setID)
	if err != nil {
		return nil, err
	}
	return integrationEventsFromStore(rows), nil
}

// AllIntegrationEvents returns every integration event, newest first. It opens
// the store only when it already exists.
func AllIntegrationEvents(d *Deps) ([]IntegrationEvent, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.AllIntegrations()
	if err != nil {
		return nil, err
	}
	return integrationEventsFromStore(rows), nil
}

func integrationEventsFromStore(rows []store.Integration) []IntegrationEvent {
	out := make([]IntegrationEvent, 0, len(rows))
	for _, ev := range rows {
		out = append(out, IntegrationEvent{
			ScopedKey:    ev.ScopedKey,
			SetID:        ev.SetID,
			Project:      ev.Project,
			IntegratedAt: ev.IntegratedAt,
			BaseRef:      ev.BaseRef,
			BranchSHA:    ev.BranchSHA,
		})
	}
	return out
}
