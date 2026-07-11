package tasks

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// DrainPane is the tmux pane record the queue supervisor writes for a Task set
// drain at the tasks boundary, surfaced in the dashboard preview. It mirrors
// store.DrainPane so the queue never imports the store directly; it is keyed
// (by the caller) per repository identity plus set id.
type DrainPane struct {
	ScopedKey   string
	Project     string
	RuntimePath string
	SetID       string
	PaneID      string
	RecordedAt  time.Time
	Source      string
}

// RecordDrainPane upserts one drain-pane record, creating the store on first
// write.
func RecordDrainPane(d *Deps, p DrainPane) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.PutDrainPane(storeDrainPane(p))
}

// AllDrainPanes returns every recorded drain pane keyed by its scoped key. It
// opens the store only when it already exists, so a pure reader (dashboard
// poll) never materialises an empty database.
func AllDrainPanes(d *Deps) (map[string]DrainPane, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return map[string]DrainPane{}, err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.AllDrainPanes()
	if err != nil {
		return nil, err
	}
	out := make(map[string]DrainPane, len(rows))
	for key, p := range rows {
		out[key] = drainPaneFromStore(p)
	}
	return out, nil
}

func storeDrainPane(p DrainPane) store.DrainPane {
	return store.DrainPane{
		ScopedKey:   p.ScopedKey,
		Project:     p.Project,
		RuntimePath: p.RuntimePath,
		SetID:       p.SetID,
		PaneID:      p.PaneID,
		RecordedAt:  p.RecordedAt,
		Source:      p.Source,
	}
}

func drainPaneFromStore(p store.DrainPane) DrainPane {
	return DrainPane{
		ScopedKey:   p.ScopedKey,
		Project:     p.Project,
		RuntimePath: p.RuntimePath,
		SetID:       p.SetID,
		PaneID:      p.PaneID,
		RecordedAt:  p.RecordedAt,
		Source:      p.Source,
	}
}
