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
	if err := runTaskAgentsWith(d, &buf); err != nil {
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
