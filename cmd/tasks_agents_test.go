package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestTaskAgentsCatalogListsPresetsWithEffortLadders(t *testing.T) {
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
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Effort: map[string]config.EffortConfig{
			"opencode": {
				Heavy:    []config.EffortModel{{Model: "opencode/claude-opus-4-8", Reasoning: "high"}, {Model: "opencode/kimi-k2.6"}},
				Standard: []config.EffortModel{{Model: "opencode/claude-sonnet-4-6", Reasoning: "medium"}},
				Light:    []config.EffortModel{{Model: "opencode/kimi-k2.6"}},
			},
		}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf); err != nil {
		t.Fatal(err)
	}

	rows := [][4]string{
		{"agent", "binary", "found", "effort ladder"},
		{"claude", "claude", "yes", "heavy: opus[reasoning=high] (built-in); standard: sonnet[reasoning=high] (built-in); light: haiku[reasoning=high] (built-in)"},
		{"opencode", "opencode", "yes", "heavy: opencode/claude-opus-4-8[reasoning=high], opencode/kimi-k2.6 (configured); standard: opencode/claude-sonnet-4-6[reasoning=medium] (configured); light: opencode/kimi-k2.6 (configured)"},
		{"cursor", "cursor-agent", "no", "heavy: composer-2.5[reasoning=high] (built-in); standard: composer-2.5[reasoning=medium] (built-in); light: composer-2.5[reasoning=low] (built-in)"},
		{"codex", "codex", "yes", "heavy: gpt-5.5[reasoning=high] (built-in); standard: gpt-5.5[reasoning=medium] (built-in); light: gpt-5.4-mini[reasoning=low] (built-in)"},
		{"pi", "pi", "no", "heavy: opencode-go/qwen3.7-max[reasoning=high] (built-in); standard: opencode-go/kimi-k2.6[reasoning=medium] (built-in); light: opencode-go/deepseek-v4-flash[reasoning=low] (built-in)"},
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

func TestTaskAgentsCatalogListsConfigOnlyEffortAgents(t *testing.T) {
	d := &tasks.Deps{
		LookPath: func(file string) (string, error) {
			if file == "custom-agent" {
				return "/mock/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
	}
	oldLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Effort: map[string]config.EffortConfig{
			"custom-agent": {
				Heavy: []config.EffortModel{{Model: "custom-large", Reasoning: "high"}},
			},
		}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = oldLoad })

	var buf bytes.Buffer
	if err := runTaskAgentsWith(d, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "custom-agent custom-agent   yes   heavy: custom-large[reasoning=high] (configured); standard: none (configured); light: none (configured)\n"
	if !strings.Contains(got, want) {
		t.Fatalf("config-only agent row missing\nwant contains:\n%sgot:\n%s", want, got)
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
