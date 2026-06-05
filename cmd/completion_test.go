package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/spf13/cobra"
)

func TestCompletionSubcommandsGenerateScripts(t *testing.T) {
	generators := []struct {
		name string
		gen  func(*bytes.Buffer) error
		need string
	}{
		{"bash", func(buf *bytes.Buffer) error { return rootCmd.GenBashCompletionV2(buf, true) }, "# bash completion V2 for pop"},
		{"zsh", func(buf *bytes.Buffer) error { return rootCmd.GenZshCompletion(buf) }, "#compdef pop"},
		{"fish", func(buf *bytes.Buffer) error { return rootCmd.GenFishCompletion(buf, true) }, "complete -c pop"},
		{"powershell", func(buf *bytes.Buffer) error { return rootCmd.GenPowerShellCompletionWithDesc(buf) }, "Register-ArgumentCompleter -CommandName 'pop'"},
	}

	for _, tc := range generators {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tc.gen(&buf); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if !strings.Contains(out, tc.need) {
				t.Fatalf("missing %q in generated script:\n%s", tc.need, out)
			}
			if !strings.Contains(out, "__complete") {
				t.Fatalf("generated script missing dynamic completion hook:\n%s", out)
			}
		})
	}
}

func TestWorkloadShellCompletionCandidates(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoCmd(t, projectDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	writeCompletionThoughts(t, cmdIssuesDir(t, projectDir), "svc", []string{"01-a", "02-b"})

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := workloadCompletionConfigLoad
	workloadCompletionConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { workloadCompletionConfigLoad = origLoad })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Run("project", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "status", "--project")
		assertShellCompContains(t, out, "svc")
	})

	t.Run("run issue positional defaults to Issue set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "run")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
		assertShellCompOmitsExact(t, out, "01-a")
	})

	t.Run("run issue positional issue set relative path", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "run", "svc/")
		assertShellCompContains(t, out, "svc/01-a.md", "svc/02-b.md")
	})

	t.Run("run issues positional defaults to Issue set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "drain")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
	})

	t.Run("reset issue positional defaults to Issue set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "open")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
		assertShellCompOmitsExact(t, out, "01-a")
	})

	t.Run("reset issue positional issue set relative file", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "open", "svc/")
		assertShellCompContains(t, out, "svc/01-a.md", "svc/02-b.md")
	})

	t.Run("set priority positional IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "set-priority")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
	})

	t.Run("agent presets", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "run", "--agent")
		for _, preset := range []string{"claude", "codex", "cursor", "opencode", "pi"} {
			assertShellCompContains(t, out, preset)
		}
	})

	t.Run("subcommands", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks")
		for _, sub := range []string{"status", "set-priority", "run", "drain", "open"} {
			assertShellCompContains(t, out, sub)
		}
	})
}

func TestWorkloadCompletionReadOnly(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	writeCompletionThoughts(t, cmdIssuesDir(t, root), "fresh", nil)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_ = shellCompNoDesc(t, "tasks", "run")

	statePath := filepath.Join(root, ".xdg", "pop", "workloads-state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("completion should not create workload state")
	}
}

func TestWorkloadPathFlagsRequestDirectoryCompletion(t *testing.T) {
	out := shellCompNoDesc(t, "tasks", "status", "--path")
	if !strings.Contains(out, ":16") {
		t.Fatalf("expected directory completion directive, got:\n%s", out)
	}
}

func shellCompNoDesc(t *testing.T, args ...string) string {
	t.Helper()
	allArgs := append([]string{cobra.ShellCompNoDescRequestCmd}, args...)
	allArgs = append(allArgs, "")
	return executeShellComp(t, allArgs)
}

func shellCompNoDescCompleting(t *testing.T, args ...string) string {
	t.Helper()
	return executeShellComp(t, append([]string{cobra.ShellCompNoDescRequestCmd}, args...))
}

func executeShellComp(t *testing.T, allArgs []string) string {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(allArgs)
	if _, err := rootCmd.ExecuteC(); err != nil {
		t.Fatalf("shell comp %v: %v", allArgs, err)
	}
	return buf.String()
}

func assertShellCompContains(t *testing.T, output string, items ...string) {
	t.Helper()
	body := shellCompBody(output)
	for _, item := range items {
		if !strings.Contains(body, item) {
			t.Fatalf("missing %q in completion output:\n%s", item, output)
		}
	}
}

func assertShellCompOmits(t *testing.T, output string, items ...string) {
	t.Helper()
	body := shellCompBody(output)
	for _, item := range items {
		if strings.Contains(body, item) {
			t.Fatalf("unexpected %q in completion output:\n%s", item, output)
		}
	}
}

func assertShellCompOmitsExact(t *testing.T, output string, items ...string) {
	t.Helper()
	lines := strings.Split(shellCompBody(output), "\n")
	for _, item := range items {
		for _, line := range lines {
			if line == item {
				t.Fatalf("unexpected %q in completion output:\n%s", item, output)
			}
		}
	}
}

func shellCompBody(output string) string {
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	last := lines[len(lines)-1]
	if strings.HasPrefix(last, ":") {
		return strings.Join(lines[:len(lines)-1], "\n")
	}
	return output
}

// writeCompletionThoughts creates a valid Issue set (no PRD pairing required)
// under the repository's Workload storage issues directory.
func writeCompletionThoughts(t *testing.T, issuesDir, stem string, issueIDs []string) {
	t.Helper()
	if len(issueIDs) == 0 {
		issueIDs = []string{"01-a"}
	}
	issueDir := filepath.Join(issuesDir, stem)
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var issues strings.Builder
	issues.WriteString(`{"tasks":[`)
	for i, id := range issueIDs {
		if i > 0 {
			issues.WriteByte(',')
		}
		file := id + ".md"
		issues.WriteString(`{"id":"`)
		issues.WriteString(id)
		issues.WriteString(`","file":"`)
		issues.WriteString(file)
		issues.WriteString(`","title":"`)
		issues.WriteString(id)
		issues.WriteString(`","type":"AFK","status":"open"}`)
		if err := os.WriteFile(filepath.Join(issueDir, file), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	issues.WriteString(`]}`)
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(issues.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
