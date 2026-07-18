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
exit ` + strconv.Itoa(exitCode) + `
`
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

func TestFakeClaudeOnPath(t *testing.T) {
	root := t.TempDir()
	installFakeClaude(t, root, 0)
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
}
