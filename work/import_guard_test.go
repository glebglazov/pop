package work

import (
	"os/exec"
	"strings"
	"testing"
)

// TestWorkImportsNoTUI enforces ADR-0143's boundary: the work data core must
// import neither bubbletea nor lipgloss, directly or transitively. The styled
// render layer lives queue-side; work stays pure so its comparator, filter, and
// cell derivation are testable without a terminal. `go list -deps` prints the
// full transitive import graph of this package — the guard fails if either
// forbidden dependency appears in it.
func TestWorkImportsNoTUI(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/glebglazov/pop/work").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, banned := range []string{"charm.land/bubbletea", "charm.land/lipgloss"} {
		if strings.Contains(string(out), banned) {
			t.Fatalf("work import graph includes forbidden TUI dependency %q — the styled render layer must stay queue-side (ADR-0143):\n%s", banned, out)
		}
	}
}
