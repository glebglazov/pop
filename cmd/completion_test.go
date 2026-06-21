package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
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

func TestTaskShellCompletionCandidates(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoCmd(t, projectDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, projectDir)
	writeCompletionThoughts(t, tasksDir, "svc", []string{"01-a", "02-b"})
	writeCompletionThoughtsStatuses(t, tasksDir, "done", [][2]string{{"01-a", "done"}})
	writeCompletionThoughtsStatuses(t, tasksDir, "archived", [][2]string{{"01-a", "open"}})
	writeCompletionThoughtsStatuses(t, tasksDir, "mix", [][2]string{{"01-open", "open"}, {"02-done", "done"}})

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := taskCompletionConfigLoad
	taskCompletionConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { taskCompletionConfigLoad = origLoad })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{}, "archived"); err != nil {
		t.Fatal(err)
	}

	t.Run("project", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "status", "--project")
		assertShellCompContains(t, out, "svc")
	})

	t.Run("implement positional defaults to Task set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "implement")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
		assertShellCompOmitsExact(t, out, "01-a")
	})

	t.Run("implement positional task set relative path", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "implement", "svc/")
		assertShellCompContains(t, out, "svc/01-a.md", "svc/02-b.md")
	})

	t.Run("set stage drills with no-space directive", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "implement")
		assertShellCompContains(t, out, "svc/")
		assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoSpace|cobra.ShellCompDirectiveNoFileComp)
	})

	t.Run("file stage uses trailing space", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "implement", "svc/")
		assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)
	})

	t.Run("set-priority stays set-only without slash drill", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "set-priority")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmitsExact(t, out, "svc/")
		assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)
	})

	t.Run("show-path stays set-only without slash drill", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "show-path")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmitsExact(t, out, "archived")
		assertShellCompOmitsExact(t, out, "svc/")
		assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)
	})

	t.Run("export stays set-only without slash drill", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "transfer", "export")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmitsExact(t, out, "archived")
		assertShellCompOmitsExact(t, out, "svc/")
		assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)
	})

	t.Run("reset task positional defaults to Task set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "open")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
		assertShellCompOmitsExact(t, out, "01-a")
	})

	t.Run("reset task positional task set relative file", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "open", "svc/")
		assertShellCompContains(t, out, "svc/01-a.md", "svc/02-b.md")
	})

	t.Run("timings positional defaults to Task set IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "timings")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
		assertShellCompOmitsExact(t, out, "01-a")
	})

	t.Run("timings positional task set relative path", func(t *testing.T) {
		out := shellCompNoDescCompleting(t, "tasks", "timings", "svc/")
		assertShellCompContains(t, out, "svc/01-a.md", "svc/02-b.md")
	})

	t.Run("set priority positional IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "set-priority")
		assertShellCompContains(t, out, "svc")
		assertShellCompOmits(t, out, "thoughts/issues/svc")
	})

	t.Run("override verbs omit Done sets at set stage", func(t *testing.T) {
		for _, verb := range []string{"implement", "open", "complete", "skip"} {
			out := shellCompNoDesc(t, "tasks", verb)
			assertShellCompContains(t, out, "mix/")
			assertShellCompOmitsExact(t, out, "done/")
		}
	})

	t.Run("override verbs omit done tasks at file stage", func(t *testing.T) {
		for _, verb := range []string{"implement", "open", "complete", "skip"} {
			out := shellCompNoDescCompleting(t, "tasks", verb, "mix/")
			assertShellCompContains(t, out, "mix/01-open.md")
			assertShellCompOmitsExact(t, out, "mix/02-done.md")
		}
	})

	t.Run("timings keeps Done sets and done tasks", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "timings")
		assertShellCompContains(t, out, "done/")
		out = shellCompNoDescCompleting(t, "tasks", "timings", "mix/")
		assertShellCompContains(t, out, "mix/01-open.md", "mix/02-done.md")
	})

	t.Run("all task target completions omit archived sets", func(t *testing.T) {
		for _, verb := range []string{"implement", "open", "complete", "skip", "timings"} {
			out := shellCompNoDesc(t, "tasks", verb)
			assertShellCompOmitsExact(t, out, "archived/")
		}
		for _, verb := range []string{"status", "archive", "set-priority", "show-path"} {
			out := shellCompNoDesc(t, "tasks", verb)
			assertShellCompOmitsExact(t, out, "archived")
		}
		assertShellCompOmitsExact(t, shellCompNoDesc(t, "tasks", "transfer", "export"), "archived")
	})

	t.Run("unarchive offers only archived IDs", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "unarchive")
		assertShellCompContains(t, out, "archived")
		assertShellCompOmitsExact(t, out, "svc")
		assertShellCompOmitsExact(t, out, "done")
	})

	t.Run("agent presets", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks", "implement", "--agent")
		for _, preset := range []string{"claude", "codex", "cursor", "opencode", "pi"} {
			assertShellCompContains(t, out, preset)
		}
	})

	t.Run("subcommands", func(t *testing.T) {
		out := shellCompNoDesc(t, "tasks")
		for _, sub := range []string{"status", "set-priority", "implement", "open", "timings", "agents"} {
			assertShellCompContains(t, out, sub)
		}
	})
}

func TestTaskIntegrateShellCompletionCandidates(t *testing.T) {
	dir := t.TempDir()
	td := queueShellCompletionDeps(t, dir)
	store := &integration.Store{Records: map[string]integration.Record{
		"pop|set-ready": {
			Project: "pop",
			SetID:   "set-ready",
			Status:  queue.MergeabilityClean,
		},
		"pop|set-conflict": {
			Project: "pop",
			SetID:   "set-conflict",
			Status:  queue.MergeabilityConflicts,
		},
	}}
	if err := integration.Save(td, store); err != nil {
		t.Fatal(err)
	}

	prev := taskIntegrationCompletionDeps
	taskIntegrationCompletionDeps = func() *queue.Deps { return &queue.Deps{Tasks: td} }
	t.Cleanup(func() { taskIntegrationCompletionDeps = prev })

	out := shellCompNoDesc(t, "tasks", "integrate")
	assertShellCompContains(t, out, "set-ready", "set-conflict")
	assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)

	out = shellCompNoDescCompleting(t, "tasks", "integrate", "set-r")
	assertShellCompContains(t, out, "set-ready")
	assertShellCompOmitsExact(t, out, "set-conflict")
}

func TestTasksUnbindWorktreeShellCompletionCandidates(t *testing.T) {
	dir := t.TempDir()
	td := queueShellCompletionDeps(t, dir)
	if err := binding.Save(td, &binding.Store{Bindings: map[string]binding.Binding{
		"pop-deadbeef\x00set-bound": {
			RuntimePath: "/wt/bound",
			Project:     "pop",
		},
		"pop-deadbeef\x00set-other": {
			RuntimePath: "/wt/other",
			Project:     "pop",
		},
	}}); err != nil {
		t.Fatal(err)
	}

	prev := taskIntegrationCompletionDeps
	taskIntegrationCompletionDeps = func() *queue.Deps { return &queue.Deps{Tasks: td} }
	t.Cleanup(func() { taskIntegrationCompletionDeps = prev })

	out := shellCompNoDesc(t, "tasks", "unbind-worktree")
	assertShellCompContains(t, out, "set-bound", "set-other")
	assertShellCompDirective(t, out, cobra.ShellCompDirectiveNoFileComp)

	out = shellCompNoDescCompleting(t, "tasks", "unbind-worktree", "set-b")
	assertShellCompContains(t, out, "set-bound")
	assertShellCompOmitsExact(t, out, "set-other")
}

func queueShellCompletionDeps(t *testing.T, dataHome string) *tasks.Deps {
	t.Helper()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	return d
}

func TestTaskCompletionReadOnly(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	writeCompletionThoughts(t, cmdTasksDir(t, root), "fresh", nil)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_ = shellCompNoDesc(t, "tasks", "implement")

	statePath := filepath.Join(root, ".xdg", "pop", "workloads-state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("completion should not create task state")
	}
}

func TestTaskPathFlagsRequestDirectoryCompletion(t *testing.T) {
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

func assertShellCompDirective(t *testing.T, output string, want cobra.ShellCompDirective) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, ":") {
		t.Fatalf("no directive line in completion output:\n%s", output)
	}
	got, err := strconv.Atoi(strings.TrimPrefix(last, ":"))
	if err != nil {
		t.Fatalf("directive parse %q: %v", last, err)
	}
	if cobra.ShellCompDirective(got) != want {
		t.Fatalf("directive = %d, want %d\n%s", got, want, output)
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

// writeCompletionThoughts creates a valid Task set (no PRD pairing required)
// under the repository's Task storage tasks directory.
func writeCompletionThoughts(t *testing.T, tasksDir, stem string, taskIDs []string) {
	t.Helper()
	if len(taskIDs) == 0 {
		taskIDs = []string{"01-a"}
	}
	tasks := make([][2]string, len(taskIDs))
	for i, id := range taskIDs {
		tasks[i] = [2]string{id, "open"}
	}
	writeCompletionThoughtsStatuses(t, tasksDir, stem, tasks)
}

// writeCompletionThoughtsStatuses creates a valid Task set with explicit
// per-task statuses, each entry an {id, status} pair.
func writeCompletionThoughtsStatuses(t *testing.T, tasksDir, stem string, taskEntries [][2]string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var tasks strings.Builder
	tasks.WriteString(`{"tasks":[`)
	for i, entry := range taskEntries {
		id, status := entry[0], entry[1]
		if i > 0 {
			tasks.WriteByte(',')
		}
		file := id + ".md"
		tasks.WriteString(`{"id":"`)
		tasks.WriteString(id)
		tasks.WriteString(`","file":"`)
		tasks.WriteString(file)
		tasks.WriteString(`","title":"`)
		tasks.WriteString(id)
		tasks.WriteString(`","type":"AFK","status":"`)
		tasks.WriteString(status)
		tasks.WriteString(`"}`)
		if err := os.WriteFile(filepath.Join(taskDir, file), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tasks.WriteString(`]}`)
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(tasks.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
