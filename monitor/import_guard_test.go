package monitor

import (
	"os/exec"
	"strings"
	"testing"
)

// TestMonitorImportsNoConfig enforces ADR 0001's boundary: the monitor package
// resolves no config of its own. Config-driven policy — the daemon address, the
// tmux socket and include (ADR-0199) — is decided in the command layer and
// handed to the monitor's primitives as already-decided values, which is what
// keeps the daemon, the store and the path derivations testable without a
// config file on disk. `go list -deps` prints the full transitive import graph,
// so the guard also catches config arriving through a helper package.
func TestMonitorImportsNoConfig(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/glebglazov/pop/monitor").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "github.com/glebglazov/pop/config" {
			t.Fatalf("monitor imports config — config-driven policy is resolved in the command layer and passed in as already-decided values (ADR 0001):\n%s", out)
		}
	}
}
