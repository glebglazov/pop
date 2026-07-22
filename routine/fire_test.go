package routine

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func installFakeClaude(t *testing.T, dir string, exitCode int) {
	t.Helper()
	script := filepath.Join(dir, "claude")
	content := `#!/bin/sh
prompt=""
while [ $# -gt 1 ]; do
  shift
done
prompt=$1
if [ -n "$FAKE_PROMPT_FILE" ]; then
  printf '%s' "$prompt" > "$FAKE_PROMPT_FILE"
fi
case "$prompt" in
  *"write your report to "*)
    rest=${prompt#*write your report to }
    report=${rest%% and*}
    if [ -n "$report" ]; then
      mkdir -p "$(dirname "$report")"
      printf 'report\n' > "$report"
    fi
    ;;
esac
printf 'ROUTINE_COMPLETE\n'
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// installFakeClaudeSentinel installs a fake claude that emits the given trailing
// sentinel line (may be empty) and optionally writes the report file, so the
// sentinel-assessment ladder can be exercised end to end.
func installFakeClaudeSentinel(t *testing.T, dir, sentinel string, writeReport bool) {
	t.Helper()
	script := filepath.Join(dir, "claude")
	writeBlock := ""
	if writeReport {
		writeBlock = `case "$prompt" in
  *"write your report to "*)
    rest=${prompt#*write your report to }
    report=${rest%% and*}
    if [ -n "$report" ]; then
      mkdir -p "$(dirname "$report")"
      printf 'report\n' > "$report"
    fi
    ;;
esac
`
	}
	sentinelLine := ""
	if sentinel != "" {
		sentinelLine = "printf '%s\\n' '" + sentinel + "'\n"
	}
	content := `#!/bin/sh
prompt=""
while [ $# -gt 1 ]; do
  shift
done
prompt=$1
` + writeBlock + sentinelLine + "exit 0\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func fireDeps(t *testing.T, dataHome string) *Deps {
	t.Helper()
	d := routineDeps(t, dataHome)
	d.Tasks = tasks.DefaultDeps()
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Routines: &config.RoutinesConfig{Agents: []string{"claude"}},
		}, nil
	}
	d.Stdout = io.Discard
	d.IsInteractive = func() bool { return false }
	return d
}

func TestFireProducesReportAndWrappedPrompt(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	var capturedPrompt string
	promptFile := filepath.Join(root, "prompt-capture.txt")
	t.Setenv("FAKE_PROMPT_FILE", promptFile)
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dataHome, "pop", "routines", "daily", "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Assess the service."), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := FireWith(d, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if res.ReportPath == "" {
		t.Fatal("expected report path")
	}
	if _, err := os.Stat(res.ReportPath); err != nil {
		t.Fatalf("report file: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(res.ReportPath), "2026-07-18") {
		t.Fatalf("report name = %q", filepath.Base(res.ReportPath))
	}

	captured, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	capturedPrompt = string(captured)

	memoryDir := filepath.Join(dataHome, "pop", "routines", "daily", "memory")
	for _, want := range []string{memoryDir, res.ReportPath, "Assess the service."} {
		if !strings.Contains(capturedPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, capturedPrompt)
		}
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.LastRoutineRun("daily")
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunSucceeded {
		t.Fatalf("row = %+v", row)
	}
}

func TestFireAnchorsNeverFiredRoutine(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	// Created paused with zero runs: the daemon would never fire it.
	res, err := AddWith(d, "anchor", "every 6h", home)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Manifest.Paused {
		t.Fatal("routine should be created paused")
	}

	sched, err := ParseSchedule("every 6h")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	// A manual fire works on a never-fired routine and lays the anchor run row.
	if _, err := FireWith(d, "anchor"); err != nil {
		t.Fatal(err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	lastFired, err := LastFireTime(s, "anchor")
	if err != nil {
		t.Fatal(err)
	}
	if lastFired.IsZero() {
		t.Fatal("manual fire should record an anchor run row")
	}

	// Once anchored, the routine is due at the next slot (not immediately).
	if IsDue(sched, lastFired, now) {
		t.Fatal("should not be due at the anchoring instant")
	}
	if !IsDue(sched, lastFired, lastFired.Add(6*time.Hour)) {
		t.Fatal("should be due one interval after the anchor")
	}
}

func TestFireRunsUnscheduledRoutine(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	// An unscheduled routine is manual-fire-only (ADR-0134): a manual fire is
	// its whole point and runs normally, laying the anchor run row.
	if _, err := AddWith(d, "manual-only", "", home); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "manual-only"); err != nil {
		t.Fatalf("manual fire on an unscheduled routine should succeed: %v", err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	lastFired, err := LastFireTime(s, "manual-only")
	if err != nil {
		t.Fatal(err)
	}
	if lastFired.IsZero() {
		t.Fatal("manual fire should record a run row for an unscheduled routine")
	}
}

func TestFireRefusesConcurrentRun(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)
	d.ProcessAlive = func(pid int, procStart string) bool { return true }

	if _, err := AddWith(d, "serial", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "serial",
		FiredAt:   time.Now().UTC(),
		PID:       4242,
		ProcStart: "live",
	}, func(live store.RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	if _, err := FireWith(d, "serial"); err == nil {
		t.Fatal("expected concurrent fire to fail")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
}

func TestFireAgentFailureRecordsFailedRow(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 2)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "fail-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	_, err := FireWith(d, "fail-me")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 2") {
		t.Fatalf("err = %v", err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.LastRoutineRun("fail-me")
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunFailed {
		t.Fatalf("row = %+v", row)
	}
	if !strings.Contains(row.FailReason, "status 2") {
		t.Fatalf("fail reason = %q", row.FailReason)
	}
}

func TestFireCompleteSentinelWithReportSucceeds(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaudeSentinel(t, root, "ROUTINE_COMPLETE", true)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "ok", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "ok"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if row := lastRun(t, d, "ok"); row.Outcome != store.RoutineRunSucceeded {
		t.Fatalf("outcome = %q, reason = %q", row.Outcome, row.FailReason)
	}
}

func TestFireFailedSentinelReasonPersisted(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaudeSentinel(t, root, "ROUTINE_FAILED: source unreachable", true)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "boom", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "boom"); err == nil {
		t.Fatal("expected failure")
	}
	row := lastRun(t, d, "boom")
	if row.Outcome != store.RoutineRunFailed {
		t.Fatalf("outcome = %q", row.Outcome)
	}
	if row.FailReason != "source unreachable" {
		t.Fatalf("fail reason = %q", row.FailReason)
	}
}

func TestFireMissingSentinelRecordsDistinctFailure(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaudeSentinel(t, root, "", true)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "silent", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "silent"); err == nil {
		t.Fatal("expected failure")
	}
	row := lastRun(t, d, "silent")
	if row.Outcome != store.RoutineRunFailed {
		t.Fatalf("outcome = %q", row.Outcome)
	}
	if row.FailReason != "missing ROUTINE_COMPLETE sentinel" {
		t.Fatalf("fail reason = %q", row.FailReason)
	}
}

func TestFireMissingReportRecordsDistinctFailure(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaudeSentinel(t, root, "ROUTINE_COMPLETE", false)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "noreport", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "noreport"); err == nil {
		t.Fatal("expected failure")
	}
	row := lastRun(t, d, "noreport")
	if row.Outcome != store.RoutineRunFailed {
		t.Fatalf("outcome = %q", row.Outcome)
	}
	if row.FailReason != "missing report" {
		t.Fatalf("fail reason = %q", row.FailReason)
	}
}

func lastRun(t *testing.T, d *Deps, id string) *store.RoutineRun {
	t.Helper()
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.LastRoutineRun(id)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatal("no run row")
	}
	return row
}

func TestFakeClaudeOnPath(t *testing.T) {
	root := t.TempDir()
	installFakeClaude(t, root, 0)
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
}
