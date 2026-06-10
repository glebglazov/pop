package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestTaskAgentsCatalogListsPresetsWithInjectedPathLookup(t *testing.T) {
	found := map[string]bool{
		"claude":   true,
		"opencode": true,
		"codex":    true,
	}
	var looked []string
	d := &tasks.Deps{
		LookPath: func(file string) (string, error) {
			looked = append(looked, file)
			if found[file] {
				return "/mock/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
		Runner: failRunner{t: t},
	}

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, ""); err != nil {
		t.Fatal(err)
	}

	want := "" +
		"agent     binary         found notes\n" +
		"claude    claude         yes   default; accepts extra args, e.g. --model <alias>\n" +
		"opencode  opencode       yes   accepts extra args\n" +
		"cursor    cursor-agent   no    HITL assistance falls back to claude\n" +
		"codex     codex          yes   accepts extra args\n" +
		"pi        pi             no    HITL assistance falls back to claude\n"
	if buf.String() != want {
		t.Fatalf("catalog output mismatch\nwant:\n%sgot:\n%s", want, buf.String())
	}

	wantLookups := []string{"claude", "opencode", "cursor-agent", "codex", "pi"}
	if strings.Join(looked, ",") != strings.Join(wantLookups, ",") {
		t.Fatalf("lookups = %v, want %v", looked, wantLookups)
	}
}

func TestTaskAgentsModelsOpenCodeUsesLiveListing(t *testing.T) {
	runner := &captureModelsRunner{
		stdout: "openai/gpt-5\nanthropic/claude-sonnet-4\n\n",
	}
	d := &tasks.Deps{Runner: runner}

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, "opencode"); err != nil {
		t.Fatal(err)
	}

	if runner.name != "opencode" || strings.Join(runner.args, " ") != "models" {
		t.Fatalf("command = %s %v, want opencode models", runner.name, runner.args)
	}
	want := "" +
		"agent: opencode\n" +
		"model source: live\n" +
		"notes: live listing from opencode models\n" +
		"models:\n" +
		"  openai/gpt-5\n" +
		"  anthropic/claude-sonnet-4\n"
	if buf.String() != want {
		t.Fatalf("models output mismatch\nwant:\n%sgot:\n%s", want, buf.String())
	}
}

func TestTaskAgentsModelsClaudeUsesKnownAliasesWithoutExec(t *testing.T) {
	d := &tasks.Deps{Runner: failRunner{t: t}}

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, "claude"); err != nil {
		t.Fatal(err)
	}

	want := "" +
		"agent: claude\n" +
		"model source: known aliases\n" +
		"notes: known stable aliases shipped with Pop\n" +
		"models:\n" +
		"  opus\n" +
		"  sonnet\n" +
		"  haiku\n" +
		"  fable\n"
	if buf.String() != want {
		t.Fatalf("models output mismatch\nwant:\n%sgot:\n%s", want, buf.String())
	}
}

func TestTaskAgentsModelsEmptyForPresetsWithoutCatalog(t *testing.T) {
	for _, preset := range []string{"codex", "cursor", "pi"} {
		t.Run(preset, func(t *testing.T) {
			d := &tasks.Deps{Runner: failRunner{t: t}}

			var buf bytes.Buffer
			if err := runTaskAgentsWith(d, &buf, preset); err != nil {
				t.Fatal(err)
			}

			want := "" +
				"agent: " + preset + "\n" +
				"model source: empty\n" +
				"notes: Pop has no model catalog for this preset; pass --model only when you know a valid value.\n"
			if buf.String() != want {
				t.Fatalf("models output mismatch\nwant:\n%sgot:\n%s", want, buf.String())
			}
		})
	}
}

func TestTaskAgentsModelsLiveFailureDegradesToMessage(t *testing.T) {
	d := &tasks.Deps{Runner: &captureModelsRunner{
		exitCode: 127,
		stderr:   "opencode: not found\n",
	}}

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf, "opencode"); err != nil {
		t.Fatal(err)
	}

	want := "" +
		"agent: opencode\n" +
		"model source: live\n" +
		"notes: live model listing failed: opencode: not found\n"
	if buf.String() != want {
		t.Fatalf("models output mismatch\nwant:\n%sgot:\n%s", want, buf.String())
	}
}

func TestTaskAgentsCommandRegisteredAndHelpVisible(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"tasks", "agents"})
	if err != nil {
		t.Fatal(err)
	}
	if got != taskAgentsCmd {
		t.Fatalf("Find(tasks agents) = %q, want %q", got.CommandPath(), taskAgentsCmd.CommandPath())
	}

	var out bytes.Buffer
	taskCmd.SetOut(&out)
	taskCmd.SetErr(&out)
	t.Cleanup(func() {
		taskCmd.SetOut(nil)
		taskCmd.SetErr(nil)
	})
	if err := taskCmd.Help(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\n  agents ") {
		t.Fatalf("tasks help missing agents command:\n%s", out.String())
	}
}

type failRunner struct {
	t *testing.T
}

func (r failRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.t.Fatalf("tasks agents must not run command %s %v", name, args)
	return 1, nil
}

func (r failRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	r.t.Fatalf("tasks agents must not start command %s %v", name, args)
	return nil, nil
}

type captureModelsRunner struct {
	name     string
	args     []string
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (r *captureModelsRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.name = name
	r.args = append([]string{}, args...)
	_, _ = io.WriteString(stdout, r.stdout)
	_, _ = io.WriteString(stderr, r.stderr)
	return r.exitCode, r.err
}

func (r *captureModelsRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return nil, errors.New("unexpected start")
}
