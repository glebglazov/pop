package drain

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// Mute through the list the Work page really wires (ADR-0200 decisions 2 and 5).
// The Task-set kind's own mute tests drive the adapter the wiring composes over;
// these drive what the wiring hands the surface, which is the value whose seams a
// read surface asserts against.

const wiredMuteSetID = "2026-07-01-demo"

// wiredMuteFixture registers one Task set and returns the queue deps the wiring
// list is built from, plus the container a loaded row would carry.
func wiredMuteFixture(t *testing.T) (*Deps, *config.Config, work.Container) {
	t.Helper()
	td := queuetest.DataDeps(t)
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	statePath := tasks.StatePathFor(tasksDir)
	canon, err := tasks.CanonicalDefinitionPathWith(td, tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(td, statePath, func(state *tasks.GlobalState) error {
		if state.Tasks == nil {
			state.Tasks = map[string]*tasks.TaskEntry{}
		}
		state.Tasks[canon] = &tasks.TaskEntry{TaskSets: []tasks.RegisteredTaskSet{{ID: wiredMuteSetID}}}
		return nil
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	cfg := &config.Config{}
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	return d, cfg, work.Container{ID: wiredMuteSetID, DefPath: canon, StatePath: statePath}
}

// wiredMuter is the Task-set kind as the wiring list built it, obtained exactly
// as a read surface obtains it: by type assertion on the optional seam. A wrapper
// that swallowed the seam fails here, which is the whole point of asking through
// the list rather than through setkind.New.
func wiredMuter(t *testing.T, d *Deps, cfg *config.Config) work.Muter {
	t.Helper()
	for _, k := range d.WorkKinds(cfg) {
		if k.ID() != ref.KindTaskSet {
			continue
		}
		muter, ok := k.(work.Muter)
		if !ok {
			t.Fatalf("the wired Task-set kind (%T) does not satisfy work.Muter; every mute of a Task set dies before the store", k)
		}
		return muter
	}
	t.Fatal("the Work wiring list holds no Task-set kind")
	return nil
}

// wiredSetReg reads the set back out of the registry the mute is written to.
func wiredSetReg(t *testing.T, d *Deps, defPath string) store.SetReg {
	t.Helper()
	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.AllSets()
	if err != nil {
		t.Fatal(err)
	}
	for _, reg := range all[defPath] {
		if reg.SetID == wiredMuteSetID {
			return reg
		}
	}
	t.Fatalf("set %s missing from the registry under %s", wiredMuteSetID, defPath)
	return store.SetReg{}
}

// The pair, end to end through the wired kind: muting writes the window and
// destroys the Auto-drain consent in one gesture, and unmuting clears the window
// and gives nothing back (ADR-0200 decision 2).
func TestWiredTaskSetKindMutesAndUnmutesThroughToTheStore(t *testing.T) {
	d, cfg, c := wiredMuteFixture(t)
	muter := wiredMuter(t, d, cfg)
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	if _, err := tasks.SetTaskSetAutoDrain(d.Tasks, c.DefPath, wiredMuteSetID, true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}

	out, err := muter.Mute(c, until, false)
	if err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if out.Kind != work.OutcomeRefresh {
		t.Fatalf("mute outcome = %+v, want a refresh", out)
	}
	reg := wiredSetReg(t, d, c.DefPath)
	if !reg.MutedUntil.Equal(until) {
		t.Fatalf("stored mute = %v, want %v", reg.MutedUntil, until)
	}
	if reg.AutoDrain {
		t.Fatal("muting through the wired kind left auto-drain standing")
	}

	if _, err := muter.Unmute(c); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	reg = wiredSetReg(t, d, c.DefPath)
	if !reg.MutedUntil.IsZero() {
		t.Fatalf("stored mute after unmute = %v, want none", reg.MutedUntil)
	}
	if reg.AutoDrain {
		t.Fatal("unmuting restored auto-drain; standing consent is the human's to give back")
	}
}
