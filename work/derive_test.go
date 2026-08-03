package work

import "testing"

// These tests cover the derivation that stayed in the seam — the part that reads
// no kind's status vocabulary. The Done-inclusion filter, the sort order and the
// STATUS-cell composition moved with the Task-set kind and are tested there; the
// Map cell is tested with the Map kind. Note what this file cannot do: `work` is
// imported by every kind, so an in-package test here can never import one.

// TestAutoDrainWaiting pins the display predicate: a consented set counts while
// idle and is silenced once a live drain holds the checkout (ADR-0108).
func TestAutoDrainWaiting(t *testing.T) {
	if !AutoDrainWaiting(Container{AutoDrain: true}) {
		t.Errorf("idle auto-drain should be waiting")
	}
	if AutoDrainWaiting(Container{AutoDrain: true, LiveDrain: true}) {
		t.Errorf("live-drained auto-drain should be silenced")
	}
	if AutoDrainWaiting(Container{}) {
		t.Errorf("non-consenting set should not be waiting")
	}
}

// TestLiveIndicator confirms the plain indicator is the fixed-width glyph for a
// live-drained row and blank otherwise, regardless of STATUS (ADR-0111): the
// indicator is driven by LiveDrain alone.
func TestLiveIndicator(t *testing.T) {
	if got := LiveIndicator(Container{RawStatus: "READY"}); got != "" {
		t.Fatalf("idle indicator = %q, want blank", got)
	}
	for _, status := range []SetStatus{"READY", "AWAITING-APPROVAL", "NEEDS-VERIFY", "BLOCKED"} {
		row := Container{RawStatus: status, LiveDrain: true}
		if got := LiveIndicator(row); got != LiveDrainGlyph {
			t.Fatalf("status %s live indicator = %q, want %q", status, got, LiveDrainGlyph)
		}
	}
}

// TestWorktreeLabel pins the unstyled destination cell (ADR-0070/0072): bound
// shows the branch, a managed directive shows `[managed wt]`, no directive shows
// `needs bind`, and a Done managed-bound set shows `[managed wt <branch>]`.
func TestWorktreeLabel(t *testing.T) {
	cases := []struct {
		kind  DestKind
		label string
		want  string
	}{
		{DestBound, "feature", "feature"},
		{DestManagedDirective, "ignored", DestLabelManagedWt},
		{DestNeedsBind, "ignored", DestLabelNeedsBind},
		{DestDoneManagedBound, "main", "[managed wt main]"},
	}
	for _, c := range cases {
		if got := WorktreeLabel(c.kind, c.label); got != c.want {
			t.Errorf("WorktreeLabel(%d, %q) = %q, want %q", c.kind, c.label, got, c.want)
		}
	}
}
