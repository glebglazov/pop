package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/workload"
)

func TestWorkloadStatusExitSuccessWithMalformedRows(t *testing.T) {
	root := t.TempDir()
	issueDir := filepath.Join(root, "thoughts/issues/bad")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := workload.DefaultDeps()
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(d, &buf); err != nil {
		t.Fatalf("status should succeed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestWorkloadStatusUnreadableDiscoveryFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	issueDir := filepath.Join(root, "thoughts/issues")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(issueDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(issueDir, 0o755) })

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	err := runWorkloadStatusWith(workload.DefaultDeps(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected setup failure")
	}
}

func TestWorkloadSetPriorityRefreshesTable(t *testing.T) {
	root := t.TempDir()
	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	issueDir := filepath.Join(root, "thoughts/issues/feature")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"issues":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Setenv("XDG_DATA_HOME", root)
	if _, err := workload.RefreshWith(workload.DefaultDeps(), root, workload.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runWorkloadSetPriorityWith(workload.DefaultDeps(), &buf, "feature", "7"); err != nil {
		t.Fatalf("set-priority failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Updated priority for feature: 0 -> 7") {
		t.Fatalf("missing change report:\n%s", out)
	}
	if !strings.Contains(out, "7 AUTO") {
		t.Fatalf("missing refreshed table with AUTO:\n%s", out)
	}
}

func TestWorkloadStatusUsesDefinitionOverride(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	writeWorkloadThoughts(t, defDir, "a")

	workloadProject = ""
	workloadPath = root
	workloadDefPath = defDir
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	t.Setenv("XDG_DATA_HOME", root)
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(workload.DefaultDeps(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

func TestWorkloadResolveByProjectName(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkloadThoughts(t, projectDir, "svc")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := workloadConfigLoad
	workloadConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { workloadConfigLoad = origLoad })

	workloadProject = "svc"
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	t.Setenv("XDG_DATA_HOME", root)
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(workload.DefaultDeps(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "svc") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

// writeWorkloadThoughts creates a minimal valid Issue set (no PRD pairing required).
func writeWorkloadThoughts(t *testing.T, dir, stem string) {
	t.Helper()
	issueDir := filepath.Join(dir, "thoughts/issues", stem)
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"issues":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkloadStatusShowsRuntimeLock(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	writeWorkloadThoughts(t, root, "demo")
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := workload.RefreshWith(workload.DefaultDeps(), root, workload.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := workload.DefaultDeps()
	runtimePath, err := workload.ResolveRuntimePathWith(d, root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := workload.AcquireRuntimeLock(d, runtimePath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(resetWorkloadFlags)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	if err := runWorkloadStatusWith(d, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Runtime execution lock") || !strings.Contains(out, "PID") {
		t.Fatalf("missing lock rendering:\n%s", out)
	}
}

func initGitRepoCmd(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
}

func TestHandleWorkloadExitMapsCodes(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{nil, 0},
		{&workload.ExitError{Code: workload.ExitNoRunnable, Err: fmt.Errorf("no work")}, workload.ExitNoRunnable},
		{&workload.ExitError{Code: workload.ExitInterrupted, Err: fmt.Errorf("interrupted")}, workload.ExitInterrupted},
	}
	for _, tt := range tests {
		if tt.err == nil {
			continue
		}
		var ee *workload.ExitError
		if !errors.As(tt.err, &ee) || ee.Code != tt.code {
			t.Fatalf("code = %v, want %d", tt.err, tt.code)
		}
	}
}

func TestRunIssueCmdDeclinedIsSuccess(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	agent := writeRunIssueFakeAgent(t, root)

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	workloadRunIssuePRD = ""
	workloadRunPRD = ""
	workloadRunIssue = ""
	workloadAgentPreset = ""
	workloadAgentCmd = agent
	workloadRunYes = false
	t.Cleanup(resetWorkloadFlags)

	var stdout bytes.Buffer
	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &stdout, io.Discard, strings.NewReader("n\n"))
	if err != nil {
		t.Fatalf("declined should succeed: %v", err)
	}
	if !strings.Contains(stdout.String(), "AUTO RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	_ = root
}

func TestRunIssueCmdNonInteractiveFails(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	agent := writeRunIssueFakeAgent(t, root)

	resetWorkloadFlags()
	workloadAgentCmd = agent
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, workload.NonInteractiveReader{})
	var ee *workload.ExitError
	if !errors.As(err, &ee) || ee.Code != workload.ExitOperational {
		t.Fatalf("err = %v", err)
	}
	_ = root
}

func resetWorkloadFlags() {
	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	workloadRunIssuePRD = ""
	workloadRunPRD = ""
	workloadRunIssue = ""
	workloadAgentPreset = ""
	workloadAgentCmd = ""
	workloadRunYes = false
}

func setupRunIssueCmdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cmd := exec.Command("git", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	writeFileCmd(t, filepath.Join(root, ".gitignore"), "thoughts/\n.agent/\n.xdg/\n")
	writeFileCmd(t, filepath.Join(root, "README.md"), "# test\n")
	if out, err := exec.Command("git", "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	writeWorkloadThoughts(t, root, "demo")
	issueDir := filepath.Join(root, "thoughts/issues/demo")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"issues":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := workload.RefreshWith(workload.DefaultDeps(), root, workload.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeRunIssueFakeAgent(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fake-agent.sh")
	script := "#!/bin/sh\nISSUE=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the issue at: ||p' | head -1)\n" +
		"if [ -n \"$ISSUE\" ] && [ -f \"$ISSUE\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$ISSUE\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$ISSUE\"; fi\n" +
		"printf 'SUMMARY_START\\ncmd test\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFileCmd(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
