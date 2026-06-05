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
	"github.com/spf13/cobra"
)

func TestWorkloadStatusExitSuccessWithMalformedRows(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issueDir := filepath.Join(cmdIssuesDir(t, root), "bad")
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
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issueDir := cmdIssuesDir(t, root)
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

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issuesDir := cmdIssuesDir(t, root)
	issueDir := filepath.Join(issuesDir, "feature")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := workload.RefreshWith(workload.DefaultDeps(), issuesDir, workload.DefaultStatePath()); err != nil {
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
	initGitRepoCmd(t, projectDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	writeWorkloadThoughts(t, cmdIssuesDir(t, projectDir), "svc")

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

	var buf bytes.Buffer
	if err := runWorkloadStatusWith(workload.DefaultDeps(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "svc") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

// cmdIssuesDir resolves the Workload storage issues directory for a repository checkout.
// XDG_DATA_HOME must already be set so the location is deterministic.
func cmdIssuesDir(t *testing.T, repoRoot string) string {
	t.Helper()
	id, err := workload.ResolveRepositoryIdentity(workload.DefaultDeps(), repoRoot)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	return id.TasksDir
}

// writeWorkloadThoughts creates a minimal valid Issue set under issuesDir/<stem>.
func writeWorkloadThoughts(t *testing.T, issuesDir, stem string) {
	t.Helper()
	issueDir := filepath.Join(issuesDir, stem)
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkloadStatusShowsRuntimeLock(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issuesDir := cmdIssuesDir(t, root)
	writeWorkloadThoughts(t, issuesDir, "demo")
	if _, err := workload.RefreshWith(workload.DefaultDeps(), issuesDir, workload.DefaultStatePath()); err != nil {
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
	workloadAgentPreset = ""
	workloadAgentCmd = agent
	workloadRunYes = false
	t.Cleanup(resetWorkloadFlags)

	var stdout bytes.Buffer
	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &stdout, io.Discard, strings.NewReader("n\n"), "")
	if err != nil {
		t.Fatalf("declined should succeed: %v", err)
	}
	if !strings.Contains(stdout.String(), "AUTO RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	_ = root
}

func TestRunIssuesCmdDeclinedIsSuccess(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	agent := writeRunIssueFakeAgent(t, root)

	resetWorkloadFlags()
	workloadAgentCmd = agent
	t.Cleanup(resetWorkloadFlags)

	var stdout bytes.Buffer
	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), &stdout, io.Discard, strings.NewReader("n\n"), "")
	if err != nil {
		t.Fatalf("declined should succeed: %v", err)
	}
	if !strings.Contains(stdout.String(), "AUTO RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	_ = root
}

func TestRunIssuesCmdRejectsRelativeIssueSetPath(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, runIssueCmdDemoDir(t, root)))
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative Issue set path error = %v", err)
	}
}

func TestRunIssueCmdRejectsRelativeIssuePath(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, filepath.Join(runIssueCmdDemoDir(t, root), "01-a.md")))
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative issue path error = %v", err)
	}
}

func TestRunIssueCmdTargetsIssueSetRelativeFile(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md")
	if err != nil {
		t.Fatalf("issue-set-relative file failed: %v", err)
	}
	_ = root
}

func TestRunIssueCmdTargetsIssueSetIdentifier(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo")
	if err != nil {
		t.Fatalf("Issue set identifier failed: %v", err)
	}
	_ = root
}

func TestRunIssueCmdRejectsInvalidIssueTargets(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a")
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("bare issue ID error = %v", err)
	}

	err = runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a.md")
	if err == nil || !strings.Contains(err.Error(), "bare filenames") {
		t.Fatalf("bare filename error = %v", err)
	}

	err = runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), filepath.Join(runIssueCmdDemoDir(t, root), "01-a.md"))
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestRunIssueCmdRejectsMoreThanOnePositional(t *testing.T) {
	err := workloadRunIssueCmd.Args(workloadRunIssueCmd, []string{"one", "two"})
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestResetIssueCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := workloadResetIssueCmd.Args(workloadResetIssueCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestResetIssueCmdTargetsIssueSetRelativeFile(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	manifestPath := filepath.Join(runIssueCmdDemoDir(t, root), "index.json")
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"failed","failed_after":2}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runWorkloadResetIssueWith(workload.DefaultDeps(), &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("issue-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Reset task demo/01-a to open") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	_ = root
}

func TestResetIssueCmdRejectsBareIdentifier(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadResetIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, "demo")
	if err == nil || !strings.Contains(err.Error(), "<task-set>/<file>.md") {
		t.Fatalf("bare identifier error = %v", err)
	}
	_ = root
}

func TestCompleteIssueCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := workloadCompleteIssueCmd.Args(workloadCompleteIssueCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestCompleteIssueCmdTargetsIssueSetRelativeFile(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	var stdout bytes.Buffer
	if err := runWorkloadCompleteIssueWith(workload.DefaultDeps(), &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("issue-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Completed task demo/01-a") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	manifestPath := filepath.Join(runIssueCmdDemoDir(t, root), "index.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("issue not marked done:\n%s", data)
	}
}

func TestRunIssuesCmdRejectsIssueSetRelativeFile(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md")
	if err == nil || !strings.Contains(err.Error(), "bare task set identifier") {
		t.Fatalf("file reference error = %v", err)
	}
	_ = root
}

func TestRunIssuesCmdTargetsIssueSetIdentifier(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo")
	if err != nil {
		t.Fatalf("Issue set identifier failed: %v", err)
	}
	_ = root
}

func TestRunIssuesCmdRejectsAbsoluteIssueSetPath(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	resetWorkloadFlags()
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), runIssueCmdDemoDir(t, root))
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestRunIssuesCmdRejectsMoreThanOnePositional(t *testing.T) {
	err := workloadRunIssuesCmd.Args(workloadRunIssuesCmd, []string{"one", "two"})
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestWorkloadCommandSurfaceUsesIssueSetVocabulary(t *testing.T) {
	names := map[string]*cobra.Command{}
	for _, c := range workloadCmd.Commands() {
		names[c.Name()] = c
	}

	if _, ok := names["drain"]; !ok {
		t.Fatal("drain command is not registered")
	}
	if _, ok := names["run-prd"]; ok {
		t.Fatal("removed run-prd alias is still registered")
	}

	if names["open"] == nil {
		t.Fatal("open command is not registered")
	}
	if names["open"].Flags().Lookup("issue-set") != nil {
		t.Fatal("open still exposes removed --issue-set flag")
	}
	if names["open"].Flags().Lookup("issue") != nil {
		t.Fatal("open still exposes removed --issue flag")
	}
	if names["run"].Flags().Lookup("issue-set") != nil {
		t.Fatal("run still exposes removed --issue-set flag")
	}
	if names["run"].Flags().Lookup("issue") != nil {
		t.Fatal("run still exposes removed --issue flag")
	}
	if names["drain"].Flags().Lookup("issue-set") != nil {
		t.Fatal("drain still exposes removed --issue-set flag")
	}
}

func TestWorkloadAllowDirtyFlagAcceptsOptionalStrategies(t *testing.T) {
	t.Cleanup(resetWorkloadFlags)
	for _, command := range []*cobra.Command{workloadRunIssueCmd, workloadRunIssuesCmd} {
		flag := command.Flags().Lookup("allow-dirty")
		if flag == nil {
			t.Fatalf("%s missing --allow-dirty", command.Name())
		}
		if flag.NoOptDefVal != string(workload.DirtyRuntimeContinue) {
			t.Fatalf("%s bare --allow-dirty = %q", command.Name(), flag.NoOptDefVal)
		}
		if err := command.Flags().Parse([]string{"--allow-dirty"}); err != nil {
			t.Fatalf("%s rejected bare --allow-dirty: %v", command.Name(), err)
		}
		if workloadAllowDirty != workload.DirtyRuntimeContinue {
			t.Fatalf("%s bare --allow-dirty parsed as %q", command.Name(), workloadAllowDirty)
		}
		for _, strategy := range workload.ValidDirtyRuntimeStrategies() {
			if err := command.Flags().Parse([]string{"--allow-dirty=" + strategy}); err != nil {
				t.Fatalf("%s rejected %q: %v", command.Name(), strategy, err)
			}
		}
		err := command.Flags().Parse([]string{"--allow-dirty=invalid"})
		if err == nil || !strings.Contains(err.Error(), "continue, commit-and-continue, stash-and-continue") {
			t.Fatalf("%s invalid strategy error = %v", command.Name(), err)
		}
	}
}

func TestRunIssueCmdNonInteractiveFails(t *testing.T) {
	root := setupRunIssueCmdFixture(t)
	agent := writeRunIssueFakeAgent(t, root)

	resetWorkloadFlags()
	workloadAgentCmd = agent
	t.Cleanup(resetWorkloadFlags)

	err := runWorkloadRunIssueWith(workload.DefaultDeps(), &bytes.Buffer{}, io.Discard, workload.NonInteractiveReader{}, "")
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
	workloadAgentPreset = ""
	workloadAgentCmd = ""
	workloadAgentOutput = ""
	workloadRunYes = false
	workloadAllowDirty = workload.DirtyRuntimeContinue
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
	writeFileCmd(t, filepath.Join(root, ".gitignore"), ".agent/\n.xdg/\n")
	writeFileCmd(t, filepath.Join(root, "README.md"), "# test\n")
	if out, err := exec.Command("git", "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	issuesDir := cmdIssuesDir(t, root)
	writeWorkloadThoughts(t, issuesDir, "demo")
	if _, err := workload.RefreshWith(workload.DefaultDeps(), issuesDir, workload.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root
}

// runIssueCmdDemoDir returns the storage directory of the fixture's "demo" Issue set.
func runIssueCmdDemoDir(t *testing.T, root string) string {
	t.Helper()
	return filepath.Join(cmdIssuesDir(t, root), "demo")
}

// relTo returns a relative path from base to target, failing the test on error.
func relTo(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return rel
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
