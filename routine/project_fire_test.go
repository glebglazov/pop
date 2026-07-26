package routine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// projectFireDeps wires a routine Deps that can fire from inside `checkout`: a
// fake `claude` on PATH, a claude-only config, and Project deps reporting
// `checkout` as the current git worktree. Stdout is a captured buffer so
// addressing warnings can be asserted.
func projectFireDeps(t *testing.T, dataHome, checkout string, out io.Writer) *Deps {
	t.Helper()
	d := checkoutDeps(t, dataHome, checkout)
	d.Tasks = tasks.DefaultDeps()
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{Routines: &config.RoutinesConfig{Agents: []string{"claude"}}}, nil
	}
	d.Stdout = out
	d.IsInteractive = func() bool { return false }
	return d
}

func TestFireProjectRoutineExplicitAndBare(t *testing.T) {
	for _, ref := range []string{"newrelic", "project:newrelic"} {
		t.Run(ref, func(t *testing.T) {
			root := t.TempDir()
			dataHome := filepath.Join(root, "data")
			checkout := filepath.Join(root, "checkout")
			installFakeClaude(t, root, 0)
			d := projectFireDeps(t, dataHome, checkout, io.Discard)
			writeProjectRoutine(t, checkout, "newrelic", "---\nagents:\n  - claude\n---\nResearch NewRelic bugs.\n")

			res, err := FireWith(d, ref)
			if err != nil {
				t.Fatalf("FireWith(%q): %v", ref, err)
			}
			if res.RoutineID != "project:newrelic" {
				t.Fatalf("RoutineID = %q, want project:newrelic", res.RoutineID)
			}
			// The report lands under the per-checkout project-routines root, never
			// under the daemon's routines/ registry.
			key := checkoutKey(checkout)
			wantRoot := filepath.Join(dataHome, "pop", projectRoutinesDataRoot, key, "newrelic")
			if !strings.HasPrefix(res.ReportPath, wantRoot) {
				t.Fatalf("report %q not under %q", res.ReportPath, wantRoot)
			}
			if _, err := os.Stat(res.ReportPath); err != nil {
				t.Fatalf("report file: %v", err)
			}
			// A run row is recorded on the synthetic per-checkout id.
			s, err := openExecutionStore(d)
			if err != nil {
				t.Fatal(err)
			}
			row, err := s.LastRoutineRun(projectStoreID(key, "newrelic"))
			if err != nil {
				t.Fatal(err)
			}
			if row == nil || row.Outcome != store.RoutineRunSucceeded {
				t.Fatalf("row = %+v", row)
			}
			// Nothing was written to the daemon's routines/ registry.
			if entries, err := os.ReadDir(filepath.Join(dataHome, "pop", "routines")); err == nil && len(entries) != 0 {
				t.Fatalf("project fire wrote to routines/ registry: %v", entries)
			}
		})
	}
}

func TestFireProjectRoutineWrapsPromptWithProjectPaths(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	promptFile := filepath.Join(root, "prompt-capture.txt")
	t.Setenv("FAKE_PROMPT_FILE", promptFile)
	installFakeClaude(t, root, 0)
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit the config.\n")

	res, err := FireWith(d, "project:audit")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	key := checkoutKey(checkout)
	memoryDir := filepath.Join(dataHome, "pop", projectRoutinesDataRoot, key, "audit", memoryDirName)
	for _, want := range []string{memoryDir, res.ReportPath, "Audit the config."} {
		if !strings.Contains(string(captured), want) {
			t.Fatalf("wrapped prompt missing %q:\n%s", want, captured)
		}
	}
}

func TestFireAuthoredWinsCollisionWithShadowWarning(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	var out bytes.Buffer
	d := projectFireDeps(t, dataHome, checkout, &out)

	if _, err := AddWith(d, "dup", "", home); err != nil {
		t.Fatal(err)
	}
	writeProjectRoutine(t, checkout, "dup", "---\n---\nProject prompt.\n")

	// Bare name: authored wins, with a shadow warning naming the escape hatch.
	res, err := FireWith(d, "dup")
	if err != nil {
		t.Fatal(err)
	}
	if res.RoutineID != "dup" {
		t.Fatalf("RoutineID = %q, want authored dup", res.RoutineID)
	}
	authoredRunsRoot := filepath.Join(dataHome, "pop", "routines", "dup")
	if !strings.HasPrefix(res.ReportPath, authoredRunsRoot) {
		t.Fatalf("report %q not under authored routine dir %q", res.ReportPath, authoredRunsRoot)
	}
	warn := out.String()
	if !strings.Contains(warn, "shadows") || !strings.Contains(warn, "project:dup") {
		t.Fatalf("expected shadow warning naming project:dup, got:\n%s", warn)
	}

	// The escape hatch reaches the Project routine directly.
	pres, err := FireWith(d, "project:dup")
	if err != nil {
		t.Fatal(err)
	}
	if pres.RoutineID != "project:dup" {
		t.Fatalf("RoutineID = %q, want project:dup", pres.RoutineID)
	}
	key := checkoutKey(checkout)
	projectRoot := filepath.Join(dataHome, "pop", projectRoutinesDataRoot, key, "dup")
	if !strings.HasPrefix(pres.ReportPath, projectRoot) {
		t.Fatalf("project fire report %q not under %q", pres.ReportPath, projectRoot)
	}
}

func TestFireProjectRoutineIndependentPerCheckout(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkoutA := filepath.Join(root, "wt-a")
	checkoutB := filepath.Join(root, "wt-b")
	installFakeClaude(t, root, 0)
	dA := projectFireDeps(t, dataHome, checkoutA, io.Discard)
	dB := projectFireDeps(t, dataHome, checkoutB, io.Discard)
	writeProjectRoutine(t, checkoutA, "shared", "---\n---\nDo shared work.\n")
	writeProjectRoutine(t, checkoutB, "shared", "---\n---\nDo shared work.\n")

	if _, err := FireWith(dA, "shared"); err != nil {
		t.Fatalf("fire in A: %v", err)
	}
	if _, err := FireWith(dB, "shared"); err != nil {
		t.Fatalf("fire in B: %v", err)
	}

	keyA := checkoutKey(checkoutA)
	keyB := checkoutKey(checkoutB)
	if keyA == keyB {
		t.Fatal("sibling checkouts must have distinct keys")
	}
	s, err := openExecutionStore(dA)
	if err != nil {
		t.Fatal(err)
	}
	rowsA, err := s.ListRoutineRuns(projectStoreID(keyA, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	rowsB, err := s.ListRoutineRuns(projectStoreID(keyB, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsA) != 1 || len(rowsB) != 1 {
		t.Fatalf("history not independent: A=%d B=%d, want 1 each", len(rowsA), len(rowsB))
	}
}

func TestFireProjectRoutineRefusesConcurrentSameCheckout(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 0)
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	d.ProcessAlive = func(pid int, procStart string) bool { return true }
	writeProjectRoutine(t, checkout, "serial", "---\n---\nWork.\n")

	key := checkoutKey(checkout)
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	// A live run in this checkout holds exclusivity.
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: projectStoreID(key, "serial"),
		FiredAt:   time.Now().UTC(),
		PID:       4242,
		ProcStart: "live",
	}, func(store.RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "serial"); err == nil {
		t.Fatal("expected concurrent fire in the same checkout to be refused")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}

	// This checkout's live run does NOT block a sibling worktree: its synthetic
	// id keys on a different checkout, so the sibling fires cleanly.
	sibling := filepath.Join(root, "sibling")
	dSibling := projectFireDeps(t, dataHome, sibling, io.Discard)
	dSibling.ProcessAlive = func(pid int, procStart string) bool { return true }
	writeProjectRoutine(t, sibling, "serial", "---\n---\nWork.\n")
	if _, err := FireWith(dSibling, "serial"); err != nil {
		t.Fatalf("a sibling checkout's live run must not block this one: %v", err)
	}
}

func TestRunsProjectRoutine(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 0)
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")

	if _, err := FireWith(d, "project:audit"); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"audit", "project:audit"} {
		var out bytes.Buffer
		if err := RunsWith(d, ref, &out); err != nil {
			t.Fatalf("RunsWith(%q): %v", ref, err)
		}
		if !strings.Contains(out.String(), store.RoutineRunSucceeded) {
			t.Fatalf("runs output for %q missing succeeded row:\n%s", ref, out.String())
		}
	}
}

func TestReconcileCrashedProjectRoutineRun(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	// Owning process is treated as dead so the crashed row is reconciled.
	d.ProcessAlive = func(pid int, procStart string) bool { return false }

	key := checkoutKey(checkout)
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: projectStoreID(key, "crashed"),
		FiredAt:   time.Now().UTC(),
		PID:       999999,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	n, err := ReconcileRunsWith(d)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d rows, want 1", n)
	}
	row, err := s.LastRoutineRun(projectStoreID(key, "crashed"))
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunFailed {
		t.Fatalf("row = %+v, want failed", row)
	}
}

func TestFireProjectRoutineFailureRecordsNoPauseState(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 2) // non-zero exit → failed run
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "flaky", "---\n---\nMight fail.\n")

	if _, err := FireWith(d, "project:flaky"); err == nil {
		t.Fatal("expected the failing run to error")
	}
	key := checkoutKey(checkout)
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.LastRoutineRun(projectStoreID(key, "flaky"))
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunFailed {
		t.Fatalf("row = %+v, want failed", row)
	}
	// Project routines have no pause state (ADR-0138): no state.json is written.
	stateFilePath := filepath.Join(projectRoutineDataDir(d, key, "flaky"), stateFileName)
	if _, err := os.Stat(stateFilePath); err == nil {
		t.Fatalf("project routine fire wrote pause state at %s; it should have none", stateFilePath)
	}
}

func TestEditScheduleRejectsProjectRoutine(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	writeProjectRoutine(t, checkout, "sched", "---\n---\nBody.\n")

	for _, ref := range []string{"sched", "project:sched"} {
		_, err := EditWith(d, ref, "every 6h", true)
		if err == nil {
			t.Fatalf("EditWith(%q, --schedule) should be rejected", ref)
		}
		if !strings.Contains(err.Error(), "Project routine") || !strings.Contains(err.Error(), "manual-fire-only") {
			t.Fatalf("error for %q should explain Project routines are manual-fire-only, got: %v", ref, err)
		}
	}
}
