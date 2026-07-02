package cmd

import (
	"errors"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// preferredCall records what setPreferredWorkbench wrote to the runtime store.
type preferredCall struct {
	setPath  string
	setName  string
	setCount int

	clearPath  string
	clearCount int
}

// newPreferredDeps builds preferredPickerDeps with recorded write/clear seams
// and a scripted picker that confirms the item whose Path == pickPath (or Esc
// when pickPath == "esc:cancel"). hasEntry drives whether "reset to default" is
// offered. lastItems captures the items handed to the picker.
func newPreferredDeps(workbenches []config.Workbench, hasEntry bool, pickPath string, rec *preferredCall, lastItems *[]ui.Item) *preferredPickerDeps {
	return &preferredPickerDeps{
		RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			*lastItems = items
			if pickPath == "esc:cancel" {
				return ui.Result{Action: ui.ActionCancel}, nil
			}
			for i := range items {
				if items[i].Path == pickPath {
					return ui.Result{Action: ui.ActionConfirm, Selected: &items[i]}, nil
				}
			}
			return ui.Result{Action: ui.ActionCancel}, errors.New("scripted pick path not found: " + pickPath)
		},
		ResolveWorkbenches: func(string) []config.Workbench { return workbenches },
		CurrentEntry:       func(string) (string, bool) { return "", hasEntry },
		SetPreferred: func(path, name string) error {
			rec.setPath, rec.setName = path, name
			rec.setCount++
			return nil
		},
		ClearPreferred: func(path string) error {
			rec.clearPath = path
			rec.clearCount++
			return nil
		},
	}
}

func TestSetPreferredWorkbench_WritesName(t *testing.T) {
	rec := &preferredCall{}
	var items []ui.Item
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d := newPreferredDeps(wbs, false, workbenchItemPathPrefix+"minimal", rec, &items)

	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if rec.setCount != 1 || rec.setPath != "/repo/app" || rec.setName != "minimal" {
		t.Fatalf("SetPreferred = %+v, want one call (/repo/app, minimal)", rec)
	}
	if rec.clearCount != 0 {
		t.Fatalf("ClearPreferred should not be called, got %d", rec.clearCount)
	}
}

func TestSetPreferredWorkbench_WritesExplicitNone(t *testing.T) {
	rec := &preferredCall{}
	var items []ui.Item
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d := newPreferredDeps(wbs, false, preferredNonePath, rec, &items)

	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if rec.setCount != 1 || rec.setPath != "/repo/app" || rec.setName != "" {
		t.Fatalf("SetPreferred = %+v, want one call (/repo/app, \"\")", rec)
	}
}

func TestSetPreferredWorkbench_ResetClearsWhenEntryExists(t *testing.T) {
	rec := &preferredCall{}
	var items []ui.Item
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d := newPreferredDeps(wbs, true, preferredResetPath, rec, &items)

	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if rec.clearCount != 1 || rec.clearPath != "/repo/app" {
		t.Fatalf("ClearPreferred = %+v, want one call (/repo/app)", rec)
	}
	if rec.setCount != 0 {
		t.Fatalf("SetPreferred should not be called on reset, got %d", rec.setCount)
	}
}

func TestSetPreferredWorkbench_ResetOfferedOnlyWithEntry(t *testing.T) {
	wbs := []config.Workbench{{Name: "gs-dev"}}

	// No entry: the reset item must be absent; the none item present.
	rec := &preferredCall{}
	var items []ui.Item
	d := newPreferredDeps(wbs, false, "esc:cancel", rec, &items)
	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if hasPath(items, preferredResetPath) {
		t.Error("reset item should NOT be offered when no entry exists")
	}
	if !hasPath(items, preferredNonePath) {
		t.Error("no-workbench item should always be offered")
	}

	// Entry exists: reset item present.
	rec = &preferredCall{}
	items = nil
	d = newPreferredDeps(wbs, true, "esc:cancel", rec, &items)
	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if !hasPath(items, preferredResetPath) {
		t.Error("reset item SHOULD be offered when an entry exists")
	}
}

func TestSetPreferredWorkbench_EscLeavesPreferenceUntouched(t *testing.T) {
	rec := &preferredCall{}
	var items []ui.Item
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d := newPreferredDeps(wbs, true, "esc:cancel", rec, &items)

	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench error: %v", err)
	}
	if rec.setCount != 0 || rec.clearCount != 0 {
		t.Fatalf("Esc must not write or clear; got set=%d clear=%d", rec.setCount, rec.clearCount)
	}
}

func hasPath(items []ui.Item, path string) bool {
	for _, it := range items {
		if it.Path == path {
			return true
		}
	}
	return false
}
