package cmd

import (
	"errors"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// newWorkbenchPreferDeps builds workbenchPreferDeps whose picker records
// write/clear seams and resolves a fixed workbench set. checkoutErr, when
// non-nil, makes CurrentCheckout fail. pickPath scripts the interactive picker
// (used only for the no-arg path).
func newWorkbenchPreferDeps(checkout string, checkoutErr error, workbenches []config.Workbench, pickPath string, rec *preferredCall) *workbenchPreferDeps {
	picker := &preferredPickerDeps{
		RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
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
		CurrentEntry:       func(string) (string, bool) { return "", false },
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
	return &workbenchPreferDeps{
		Picker:          picker,
		CurrentCheckout: func() (string, error) { return checkout, checkoutErr },
	}
}

func TestRunWorkbenchPrefer_SetsName(t *testing.T) {
	rec := &preferredCall{}
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d := newWorkbenchPreferDeps("/repo/app", nil, wbs, "", rec)

	if err := runWorkbenchPreferWith(d, "minimal", false, false); err != nil {
		t.Fatalf("runWorkbenchPreferWith error: %v", err)
	}
	if rec.setCount != 1 || rec.setPath != "/repo/app" || rec.setName != "minimal" {
		t.Fatalf("SetPreferred = %+v, want one call (/repo/app, minimal)", rec)
	}
	if rec.clearCount != 0 {
		t.Fatalf("ClearPreferred should not be called, got %d", rec.clearCount)
	}
}

func TestRunWorkbenchPrefer_BadNameErrorsWithoutWriting(t *testing.T) {
	rec := &preferredCall{}
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d := newWorkbenchPreferDeps("/repo/app", nil, wbs, "", rec)

	err := runWorkbenchPreferWith(d, "nope", false, false)
	if err == nil {
		t.Fatal("expected error for unresolvable name, got nil")
	}
	if rec.setCount != 0 || rec.clearCount != 0 {
		t.Fatalf("bad name must not write; got set=%d clear=%d", rec.setCount, rec.clearCount)
	}
}

func TestRunWorkbenchPrefer_Clear(t *testing.T) {
	rec := &preferredCall{}
	d := newWorkbenchPreferDeps("/repo/app", nil, nil, "", rec)

	if err := runWorkbenchPreferWith(d, "", true, false); err != nil {
		t.Fatalf("runWorkbenchPreferWith error: %v", err)
	}
	if rec.clearCount != 1 || rec.clearPath != "/repo/app" {
		t.Fatalf("ClearPreferred = %+v, want one call (/repo/app)", rec)
	}
	if rec.setCount != 0 {
		t.Fatalf("SetPreferred should not be called on --clear, got %d", rec.setCount)
	}
}

func TestRunWorkbenchPrefer_None(t *testing.T) {
	rec := &preferredCall{}
	d := newWorkbenchPreferDeps("/repo/app", nil, nil, "", rec)

	if err := runWorkbenchPreferWith(d, "", false, true); err != nil {
		t.Fatalf("runWorkbenchPreferWith error: %v", err)
	}
	if rec.setCount != 1 || rec.setPath != "/repo/app" || rec.setName != "" {
		t.Fatalf("SetPreferred = %+v, want one call (/repo/app, \"\")", rec)
	}
	if rec.clearCount != 0 {
		t.Fatalf("ClearPreferred should not be called on --none, got %d", rec.clearCount)
	}
}

func TestRunWorkbenchPrefer_MutuallyExclusiveFlags(t *testing.T) {
	rec := &preferredCall{}
	d := newWorkbenchPreferDeps("/repo/app", nil, nil, "", rec)

	if err := runWorkbenchPreferWith(d, "", true, true); err == nil {
		t.Fatal("expected error for --clear + --none, got nil")
	}
	if rec.setCount != 0 || rec.clearCount != 0 {
		t.Fatalf("conflicting flags must not write; got set=%d clear=%d", rec.setCount, rec.clearCount)
	}
}

func TestRunWorkbenchPrefer_NameWithFlagRejected(t *testing.T) {
	rec := &preferredCall{}
	wbs := []config.Workbench{{Name: "gs-dev"}}
	d := newWorkbenchPreferDeps("/repo/app", nil, wbs, "", rec)

	if err := runWorkbenchPreferWith(d, "gs-dev", true, false); err == nil {
		t.Fatal("expected error combining a name with --clear, got nil")
	}
	if rec.setCount != 0 || rec.clearCount != 0 {
		t.Fatalf("name+flag must not write; got set=%d clear=%d", rec.setCount, rec.clearCount)
	}
}

func TestRunWorkbenchPrefer_NoArgsOpensPicker(t *testing.T) {
	rec := &preferredCall{}
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d := newWorkbenchPreferDeps("/repo/app", nil, wbs, workbenchItemPathPrefix+"gs-dev", rec)

	if err := runWorkbenchPreferWith(d, "", false, false); err != nil {
		t.Fatalf("runWorkbenchPreferWith error: %v", err)
	}
	if rec.setCount != 1 || rec.setPath != "/repo/app" || rec.setName != "gs-dev" {
		t.Fatalf("SetPreferred = %+v, want one call (/repo/app, gs-dev) via picker", rec)
	}
}

func TestRunWorkbenchPrefer_CheckoutErrorAborts(t *testing.T) {
	rec := &preferredCall{}
	d := newWorkbenchPreferDeps("", errors.New("not in a git worktree"), nil, "", rec)

	if err := runWorkbenchPreferWith(d, "minimal", false, false); err == nil {
		t.Fatal("expected error when the current checkout can't be detected, got nil")
	}
	if rec.setCount != 0 || rec.clearCount != 0 {
		t.Fatalf("failed checkout detection must not write; got set=%d clear=%d", rec.setCount, rec.clearCount)
	}
}

func TestWorkbenchPreferNames_CompletionSource(t *testing.T) {
	rec := &preferredCall{}
	wbs := []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}}
	d := newWorkbenchPreferDeps("/repo/app", nil, wbs, "", rec)

	names, err := workbenchPreferNames(d)
	if err != nil {
		t.Fatalf("workbenchPreferNames error: %v", err)
	}
	if len(names) != 2 || names[0] != "gs-dev" || names[1] != "minimal" {
		t.Fatalf("workbenchPreferNames = %v, want [gs-dev minimal]", names)
	}
}

func TestWorkbenchPreferNames_CheckoutErrorPropagates(t *testing.T) {
	rec := &preferredCall{}
	d := newWorkbenchPreferDeps("", errors.New("not in a git worktree"), nil, "", rec)

	if _, err := workbenchPreferNames(d); err == nil {
		t.Fatal("expected error from completion source when checkout detection fails, got nil")
	}
}
