package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestTopicSteps_UnmarshalTOML(t *testing.T) {
	t.Run("bare string parses as agent step", func(t *testing.T) {
		var cfg struct {
			Steps TopicSteps `toml:"topic_agents"`
		}
		if _, err := toml.Decode(`topic_agents = ["claude", "ollama:llama3.2"]`, &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Steps) != 2 {
			t.Fatalf("len = %d, want 2", len(cfg.Steps))
		}
		if cfg.Steps[0].Type != TopicStepAgent || cfg.Steps[0].Command != "claude" || cfg.Steps[0].SetIf != TopicSetIfEmpty {
			t.Errorf("step[0] = %+v", cfg.Steps[0])
		}
		if cfg.Steps[1].Command != "ollama:llama3.2" {
			t.Errorf("step[1] = %+v", cfg.Steps[1])
		}
	})

	t.Run("typed tables parse", func(t *testing.T) {
		var cfg struct {
			Steps TopicSteps `toml:"topic_agents"`
		}
		raw := `
topic_agents = [
  { type = "truncate", set_if = "empty" },
  { type = "agent", command = "claude", set_if = "empty_or_seed" },
  { type = "agent", command = "ollama:llama3.2", set_if = "always" },
]
`
		if _, err := toml.Decode(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		want := []TopicStep{
			{Type: TopicStepTruncate, SetIf: TopicSetIfEmpty},
			{Type: TopicStepAgent, Command: "claude", SetIf: TopicSetIfEmptyOrSeed},
			{Type: TopicStepAgent, Command: "ollama:llama3.2", SetIf: TopicSetIfAlways},
		}
		for i, step := range want {
			if cfg.Steps[i] != step {
				t.Errorf("step[%d] = %+v, want %+v", i, cfg.Steps[i], step)
			}
		}
	})

	t.Run("mixed string and table", func(t *testing.T) {
		var cfg struct {
			Steps TopicSteps `toml:"topic_agents"`
		}
		raw := `topic_agents = ["claude", { type = "truncate", set_if = "empty" }]`
		if _, err := toml.Decode(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Steps) != 2 {
			t.Fatalf("len = %d, want 2", len(cfg.Steps))
		}
		if cfg.Steps[0].Type != TopicStepAgent || cfg.Steps[0].Command != "claude" {
			t.Errorf("step[0] = %+v", cfg.Steps[0])
		}
		if cfg.Steps[1].Type != TopicStepTruncate {
			t.Errorf("step[1] = %+v", cfg.Steps[1])
		}
	})

	t.Run("agent table defaults set_if to empty_or_seed", func(t *testing.T) {
		var cfg struct {
			Steps TopicSteps `toml:"topic_agents"`
		}
		if _, err := toml.Decode(`topic_agents = [{ type = "agent", command = "claude" }]`, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Steps[0].SetIf != TopicSetIfEmptyOrSeed {
			t.Errorf("set_if = %q, want %q", cfg.Steps[0].SetIf, TopicSetIfEmptyOrSeed)
		}
	})

	t.Run("truncate table defaults set_if to empty", func(t *testing.T) {
		var cfg struct {
			Steps TopicSteps `toml:"topic_agents"`
		}
		if _, err := toml.Decode(`topic_agents = [{ type = "truncate" }]`, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Steps[0].SetIf != TopicSetIfEmpty {
			t.Errorf("set_if = %q, want %q", cfg.Steps[0].SetIf, TopicSetIfEmpty)
		}
	})
}

func TestDefaultTopicSteps(t *testing.T) {
	steps := DefaultTopicSteps()
	if len(steps) != 1 {
		t.Fatalf("len = %d, want 1", len(steps))
	}
	if steps[0].Type != TopicStepTruncate || steps[0].SetIf != TopicSetIfEmpty {
		t.Errorf("default step = %+v", steps[0])
	}
}

func TestPaneMonitoringTopicSteps_Default(t *testing.T) {
	t.Run("nil config returns default truncate step", func(t *testing.T) {
		var c Config
		steps := c.PaneMonitoringTopicSteps()
		if len(steps) != 1 || steps[0].Type != TopicStepTruncate {
			t.Errorf("steps = %+v", steps)
		}
	})

	t.Run("unset topic_agents returns default", func(t *testing.T) {
		c := Config{PaneMonitoring: &PaneMonitoringConfig{}}
		steps := c.PaneMonitoringTopicSteps()
		if len(steps) != 1 || steps[0].Type != TopicStepTruncate {
			t.Errorf("steps = %+v", steps)
		}
	})

	t.Run("explicit empty array returns no steps", func(t *testing.T) {
		c := Config{PaneMonitoring: &PaneMonitoringConfig{TopicAgents: TopicSteps{}}}
		steps := c.PaneMonitoringTopicSteps()
		if len(steps) != 0 {
			t.Errorf("steps = %+v, want empty", steps)
		}
	})
}

func TestTopicSetIfAllows(t *testing.T) {
	cases := []struct {
		setIf TopicSetIf
		kind  string
		want  bool
	}{
		{TopicSetIfEmpty, "", true},
		{TopicSetIfEmpty, TopicKindSeed, false},
		{TopicSetIfEmpty, TopicKindFinal, false},
		{TopicSetIfEmptyOrSeed, "", true},
		{TopicSetIfEmptyOrSeed, TopicKindSeed, true},
		{TopicSetIfEmptyOrSeed, TopicKindFinal, false},
		{TopicSetIfAlways, "", true},
		{TopicSetIfAlways, TopicKindSeed, true},
		{TopicSetIfAlways, TopicKindFinal, true},
	}
	for _, tc := range cases {
		name := string(tc.setIf) + "/" + tc.kind
		t.Run(name, func(t *testing.T) {
			if got := TopicSetIfAllows(tc.setIf, tc.kind); got != tc.want {
				t.Errorf("TopicSetIfAllows(%q, %q) = %v, want %v", tc.setIf, tc.kind, got, tc.want)
			}
		})
	}
}
