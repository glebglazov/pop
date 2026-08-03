package work

import (
	"os/exec"
	"strings"
	"testing"
)

// TestWorkImportsNoTUI enforces ADR-0143's boundary: the work data core must
// import neither bubbletea nor lipgloss, directly or transitively. The styled
// render layer lives TUI-side; work stays pure so its comparator, filter, and
// cell derivation are testable without a terminal. `go list -deps` prints the
// full transitive import graph of this package — the guard fails if either
// forbidden dependency appears in it.
func TestWorkImportsNoTUI(t *testing.T) {
	for _, banned := range []string{"charm.land/bubbletea", "charm.land/lipgloss"} {
		if deps := workDeps(t); strings.Contains(deps, banned) {
			t.Fatalf("work import graph includes forbidden TUI dependency %q — the styled render layer must stay TUI-side (ADR-0143):\n%s", banned, deps)
		}
	}
}

// TestWorkImportsNoKind is the property the seam exists for: kinds comply with
// `work.Kind` and import `work`, and `work` imports no kind. It is not a style
// rule — a kind adapter lives kind-side, so an import the other way is a cycle
// waiting to happen, and `work` growing an import per kind is exactly the hub
// this design rejected. The guard is transitive: reaching a kind through a
// helper package counts.
func TestWorkImportsNoKind(t *testing.T) {
	kinds := []string{
		"github.com/glebglazov/pop/tasks",
		"github.com/glebglazov/pop/wayfinder",
		"github.com/glebglazov/pop/routine",
		"github.com/glebglazov/pop/queue",
	}
	deps := strings.Fields(workDeps(t))
	for _, dep := range deps {
		for _, kind := range kinds {
			if dep == kind || strings.HasPrefix(dep, kind+"/") {
				t.Fatalf("work imports kind package %q — kinds comply with the seam, the seam knows no kind:\n%s", dep, strings.Join(deps, "\n"))
			}
		}
	}
}

// workDeps returns work's full transitive import graph.
func workDeps(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "github.com/glebglazov/pop/work").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	return string(out)
}
