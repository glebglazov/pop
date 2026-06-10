package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestTaskAgentsCatalogListsPresetsWithModelsColumn(t *testing.T) {
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
	}

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf); err != nil {
		t.Fatal(err)
	}

	rows := [][4]string{
		{"agent", "binary", "found", "models"},
		{"claude", "claude", "yes", "opus, sonnet, haiku, fable"},
		{"opencode", "opencode", "yes", "opencode/kimi-k2.6, opencode/gpt-5.5, opencode/claude-opus-4-8, opencode/claude-sonnet-4-6"},
		{"cursor", "cursor-agent", "no", "auto, composer-2.5, gpt-5.3-codex"},
		{"codex", "codex", "yes", "gpt-5.5, gpt-5.4-mini"},
		{"pi", "pi", "no", "opencode-go/kimi-k2.6, opencode-go/qwen3.7-max, opencode-go/minimax-m3, opencode-go/deepseek-v4-flash"},
	}
	var want strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&want, "%-9s %-14s %-5s %s\n", r[0], r[1], r[2], r[3])
	}
	if buf.String() != want.String() {
		t.Fatalf("catalog output mismatch\nwant:\n%sgot:\n%s", want.String(), buf.String())
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
