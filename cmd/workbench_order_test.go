package cmd

import (
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// labelsOf returns the display Name of each item, the observable render order.
func labelsOf(items []ui.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func opt(label, path string) workbenchOption {
	return workbenchOption{Label: label, Item: ui.Item{Name: label, Path: path}}
}

func TestOrderWorkbenchOptions(t *testing.T) {
	// Default candidate set in default order: <empty>, Workbenches, <reset>.
	base := func() []workbenchOption {
		return []workbenchOption{
			opt("<empty>", preferredNonePath),
			opt("minimal", workbenchItemPathPrefix+"minimal"),
			opt("full-dev", workbenchItemPathPrefix+"full-dev"),
			opt("<reset>", preferredResetPath),
		}
	}

	tests := []struct {
		name  string
		order []string
		cands []workbenchOption
		want  []string
	}{
		{
			name:  "nil order yields default sequence",
			order: nil,
			cands: base(),
			want:  []string{"<empty>", "minimal", "full-dev", "<reset>"},
		},
		{
			name:  "empty order yields default sequence",
			order: []string{},
			cands: base(),
			want:  []string{"<empty>", "minimal", "full-dev", "<reset>"},
		},
		{
			// Task worked example: order=["minimal"], both Workbenches resolved.
			name:  "front-load one workbench, default tail follows",
			order: []string{"minimal"},
			cands: base(),
			want:  []string{"minimal", "<empty>", "full-dev", "<reset>"},
		},
		{
			name:  "special tokens are orderable like any label",
			order: []string{"<reset>", "<empty>"},
			cands: base(),
			want:  []string{"<reset>", "<empty>", "minimal", "full-dev"},
		},
		{
			name:  "unresolvable names are ignored without error",
			order: []string{"ghost", "minimal", "also-gone"},
			cands: base(),
			want:  []string{"minimal", "<empty>", "full-dev", "<reset>"},
		},
		{
			name:  "duplicate token consumed once",
			order: []string{"minimal", "minimal"},
			cands: base(),
			want:  []string{"minimal", "<empty>", "full-dev", "<reset>"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := labelsOf(orderWorkbenchOptions(tc.order, tc.cands))
			if !equalStrs(got, tc.want) {
				t.Errorf("orderWorkbenchOptions() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPromptWorkbenchForCreate_CandidateSetAndOrder asserts the create prompt's
// per-list candidate set (<empty> + Workbenches, never <reset>) and that order
// front-loads through the shared rule.
func TestPromptWorkbenchForCreate_CandidateSetAndOrder(t *testing.T) {
	wbs := []config.Workbench{{Name: "minimal"}, {Name: "full-dev"}}
	var seen []ui.Item
	d := &ProjectDeps{
		RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			seen = items
			return ui.Result{Action: ui.ActionCancel}, nil
		},
	}

	// A <reset> token in order must never conjure a <reset> row in the create prompt.
	if _, _, err := promptWorkbenchForCreate(d, []string{"minimal", "<reset>"}, wbs); err != nil {
		t.Fatalf("promptWorkbenchForCreate: %v", err)
	}
	got := labelsOf(seen)
	want := []string{"minimal", "<empty>", "full-dev"}
	if !equalStrs(got, want) {
		t.Errorf("create prompt sequence = %v, want %v", got, want)
	}
	for _, it := range seen {
		if it.Path == preferredResetPath || it.Name == preferredResetLabel {
			t.Error("create prompt must never contain a <reset> option")
		}
	}
}

// TestBothListsShareOrdering asserts both interactive lists route through the one
// shared assembly and render the same sequence for the same inputs (same
// Workbenches, same order, and a candidate set without <reset>).
func TestBothListsShareOrdering(t *testing.T) {
	wbs := []config.Workbench{{Name: "minimal"}, {Name: "full-dev"}}
	order := []string{"full-dev"}

	// Create prompt.
	var createItems []ui.Item
	pd := &ProjectDeps{
		RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			createItems = items
			return ui.Result{Action: ui.ActionCancel}, nil
		},
	}
	if _, _, err := promptWorkbenchForCreate(pd, order, wbs); err != nil {
		t.Fatalf("promptWorkbenchForCreate: %v", err)
	}

	// Preferred picker with no runtime entry (so no <reset> — matching candidate set).
	var prefItems []ui.Item
	rec := &preferredCall{}
	ppd := newPreferredDeps(wbs, false, "esc:cancel", rec, &prefItems)
	ppd.WorkbenchOrder = func() []string { return order }
	if err := setPreferredWorkbench(ppd, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench: %v", err)
	}

	gotCreate := labelsOf(createItems)
	gotPref := labelsOf(prefItems)
	want := []string{"full-dev", "<empty>", "minimal"}
	if !equalStrs(gotCreate, want) {
		t.Errorf("create sequence = %v, want %v", gotCreate, want)
	}
	if !equalStrs(gotPref, want) {
		t.Errorf("preferred sequence = %v, want %v", gotPref, want)
	}
	if !equalStrs(gotCreate, gotPref) {
		t.Errorf("lists diverge: create=%v preferred=%v", gotCreate, gotPref)
	}
}

// TestPreferredPicker_ResetPresenceAndOrder asserts <reset> appears only when a
// runtime entry exists and still obeys order placement (worked example).
func TestPreferredPicker_ResetPresenceAndOrder(t *testing.T) {
	wbs := []config.Workbench{{Name: "minimal"}, {Name: "full-dev"}}

	// Entry exists → <reset> present; order=["minimal"] front-loads it.
	var items []ui.Item
	rec := &preferredCall{}
	d := newPreferredDeps(wbs, true, "esc:cancel", rec, &items)
	d.WorkbenchOrder = func() []string { return []string{"minimal"} }
	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench: %v", err)
	}
	got := labelsOf(items)
	want := []string{"minimal", "<empty>", "full-dev", "<reset>"}
	if !equalStrs(got, want) {
		t.Errorf("preferred sequence (entry) = %v, want %v", got, want)
	}

	// No entry → <reset> absent even if named in order.
	items = nil
	rec = &preferredCall{}
	d = newPreferredDeps(wbs, false, "esc:cancel", rec, &items)
	d.WorkbenchOrder = func() []string { return []string{"<reset>", "minimal"} }
	if err := setPreferredWorkbench(d, "/repo/app"); err != nil {
		t.Fatalf("setPreferredWorkbench: %v", err)
	}
	if hasPath(items, preferredResetPath) {
		t.Error("<reset> must be absent when no runtime entry exists, even if named in order")
	}
	if labelsOf(items)[0] != "minimal" {
		t.Errorf("named minimal should still front-load; got %v", labelsOf(items))
	}
}
