package tasks

import (
	"bytes"
	"strings"
	"testing"
)

// renderWithCheckout renders a one-row status table carrying cs and returns the
// output, so checkout-line assertions don't depend on real task discovery.
func renderWithCheckout(cs *CheckoutStatus) string {
	var buf bytes.Buffer
	Render(&buf, &RefreshResult{
		DefinitionPath: "/tmp/defs",
		Rows:           []Row{{ID: "set-1", Status: StatusReady, Progress: "0/1 done"}},
		Checkout:       cs,
	})
	return buf.String()
}

func TestRenderCheckoutTrunkWorktreeInline(t *testing.T) {
	out := renderWithCheckout(&CheckoutStatus{Path: "/repo", Worktree: false})
	if !strings.Contains(out, "Checkout: Trunk worktree — whole-set implement drains inline") {
		t.Fatalf("trunk worktree checkout line missing: %q", out)
	}
}

func TestRenderCheckoutWorktreeWithBranch(t *testing.T) {
	out := renderWithCheckout(&CheckoutStatus{Path: "/wt", Worktree: true, Branch: "pop/set-1"})
	if !strings.Contains(out, "Checkout: worktree (pop/set-1) — implement adopts it (integrateable)") {
		t.Fatalf("worktree checkout line missing: %q", out)
	}
}

func TestRenderCheckoutWorktreeDetached(t *testing.T) {
	out := renderWithCheckout(&CheckoutStatus{Path: "/wt", Worktree: true})
	if !strings.Contains(out, "Checkout: worktree (detached) — implement adopts it") {
		t.Fatalf("detached worktree checkout line missing: %q", out)
	}
}

func TestRenderCheckoutAbsentWhenNil(t *testing.T) {
	out := renderWithCheckout(nil)
	if strings.Contains(out, "Checkout:") {
		t.Fatalf("checkout line should be absent when status is nil: %q", out)
	}
}
