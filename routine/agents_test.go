package routine

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func TestResolveRoutineAgentPresetsPrefersRoutinesConfig(t *testing.T) {
	cfg := &config.Config{
		Work: &config.WorkConfig{
			Routine:   &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("codex", "claude")},
			Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor")},
		},
	}
	got, err := ResolveRoutineAgentPresets(nil, cfg)
	if err != nil {
		t.Fatalf("ResolveRoutineAgentPresets: %v", err)
	}
	want := []string{"codex", "claude"}
	if len(got) != len(want) {
		t.Fatalf("agents = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agents[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveRoutineAgentPresetsFallsBackToImplementList(t *testing.T) {
	cfg := &config.Config{
		Work: &config.WorkConfig{
			Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor", "claude")},
		},
	}
	got, err := ResolveRoutineAgentPresets(nil, cfg)
	if err != nil {
		t.Fatalf("ResolveRoutineAgentPresets: %v", err)
	}
	want := []string{"cursor", "claude"}
	if len(got) != len(want) {
		t.Fatalf("agents = %#v, want %#v", got, want)
	}
}

func TestRunRoutineWithAgentFallbackQuotaFallthrough(t *testing.T) {
	root := t.TempDir()
	dataHome := root
	d := routineDeps(t, dataHome)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Work: &config.WorkConfig{Routine: &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("claude", "codex")}},
		}, nil
	}
	taskDeps := tasks.DefaultDeps()
	d.Tasks = taskDeps

	calls := 0
	attempt := func(agentSpec string) (*tasks.RoutineAgentAttempt, error) {
		calls++
		switch agentSpec {
		case "claude":
			return &tasks.RoutineAgentAttempt{
				QuotaPaused:  true,
				QuotaPreset:  "claude",
				QuotaResetAt: time.Now().Add(time.Hour),
			}, nil
		case "codex":
			return &tasks.RoutineAgentAttempt{ExitCode: 0}, nil
		default:
			t.Fatalf("unexpected agent %q", agentSpec)
			return nil, nil
		}
	}

	cfg := mustConfig(t, d.LoadConfig)
	result, preset, err := runRoutineWithAgentFallback(d, cfg, mustRoutinePresets(t, cfg), io.Discard, attempt)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if preset != "codex" {
		t.Fatalf("preset = %q, want codex", preset)
	}
	if result == nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRunRoutineWithAgentFallbackSkipsCooldownedPreset(t *testing.T) {
	root := t.TempDir()
	dataHome := root
	d := routineDeps(t, dataHome)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Work: &config.WorkConfig{Routine: &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("claude", "codex")}},
		}, nil
	}
	taskDeps := tasks.DefaultDeps()
	d.Tasks = taskDeps
	if err := tasks.RecordAgentQuotaCooldownFromReset(taskDeps, mustConfig(t, d.LoadConfig), "claude", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	calls := 0
	attempt := func(agentSpec string) (*tasks.RoutineAgentAttempt, error) {
		calls++
		if agentSpec != "codex" {
			t.Fatalf("unexpected agent %q", agentSpec)
		}
		return &tasks.RoutineAgentAttempt{ExitCode: 0}, nil
	}

	cfg := mustConfig(t, d.LoadConfig)
	_, preset, err := runRoutineWithAgentFallback(d, cfg, mustRoutinePresets(t, cfg), io.Discard, attempt)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if preset != "codex" {
		t.Fatalf("preset = %q, want codex", preset)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRunRoutineWithAgentFallbackAllQuotaPausedFails(t *testing.T) {
	d := routineDeps(t, t.TempDir())
	cfg := &config.Config{
		Work: &config.WorkConfig{Routine: &config.AgentGroupConfig{Agents: config.AgentEntriesFromCommands("claude")}},
	}
	attempt := func(agentSpec string) (*tasks.RoutineAgentAttempt, error) {
		return &tasks.RoutineAgentAttempt{
			QuotaPaused:  true,
			QuotaPreset:  "claude",
			QuotaResetAt: time.Now().Add(time.Hour),
		}, nil
	}
	_, _, err := runRoutineWithAgentFallback(d, cfg, mustRoutinePresets(t, cfg), io.Discard, attempt)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Fatal("empty error")
	}
}

func mustConfig(t *testing.T, load LoadConfigFunc) *config.Config {
	t.Helper()
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// mustRoutinePresets resolves a Routine's agent list for a test that is not
// about resolution failing.
func mustRoutinePresets(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	specs, err := ResolveRoutineAgentPresets(nil, cfg)
	if err != nil {
		t.Fatalf("ResolveRoutineAgentPresets: %v", err)
	}
	return specs
}

// routineFallthroughCfg is a config whose routine group states no list of its
// own and whose implement list is the fallthrough target. The named keys are
// those the override layer states as an empty list.
func routineFallthroughCfg(emptyOverrides ...string) *config.Config {
	return &config.Config{
		Work: &config.WorkConfig{
			Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor", "claude")},
		},
		EmptyAgentOverrides: emptyOverrides,
	}
}

// TestResolveRoutineAgentPresetsEmptyOverrideDisablesFallthrough pins the two
// empty states apart for Routines, the way verify and review pin them for
// themselves: an absent [work.routine].agents walks on to the implement list, an
// override of `agents = []` refuses (ADR-0202 decision 6).
func TestResolveRoutineAgentPresetsEmptyOverrideDisablesFallthrough(t *testing.T) {
	got, err := ResolveRoutineAgentPresets(nil, routineFallthroughCfg())
	if err != nil {
		t.Fatalf("absent list: %v", err)
	}
	if strings.Join(got, ",") != "cursor,claude" {
		t.Fatalf("agents = %v, want the implement list", got)
	}

	_, err = ResolveRoutineAgentPresets(nil, routineFallthroughCfg(config.KeyRoutineAgents))
	if err == nil {
		t.Fatal("resolved an explicit empty override; want a refusal")
	}
	if !strings.Contains(err.Error(), config.KeyRoutineAgents) {
		t.Fatalf("error = %q, want it to name %s", err, config.KeyRoutineAgents)
	}

	// A Routine's own manifest list outranks config, so it still runs.
	got, err = ResolveRoutineAgentPresets([]string{"pi"}, routineFallthroughCfg(config.KeyRoutineAgents))
	if err != nil {
		t.Fatalf("manifest list: %v", err)
	}
	if strings.Join(got, ",") != "pi" {
		t.Fatalf("agents = %v, want the manifest list", got)
	}
}

// TestFireRefusesEmptyRoutineAgentOverride carries the refusal through a whole
// fire: no agent is invoked, and the run is filed failed with the sentence that
// names the key.
func TestFireRefusesEmptyRoutineAgentOverride(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(root, "prompt-capture.txt")
	t.Setenv("FAKE_PROMPT_FILE", promptFile)
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)
	d.LoadConfig = func() (*config.Config, error) {
		return routineFallthroughCfg(config.KeyRoutineAgents), nil
	}

	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dataHome, "pop", "routines", "daily", "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Assess the service."), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := FireWith(d, "daily")
	if err == nil {
		t.Fatal("FireWith ran on an explicit empty agent override")
	}
	if !strings.Contains(err.Error(), config.KeyRoutineAgents) {
		t.Fatalf("error = %q, want it to name %s", err, config.KeyRoutineAgents)
	}
	if _, statErr := os.Stat(promptFile); statErr == nil {
		t.Fatal("an agent was invoked; the fallthrough should have been disabled")
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.LastRoutineRun("daily")
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.Outcome != store.RoutineRunFailed {
		t.Fatalf("row = %+v, want a failed run", row)
	}
	if !strings.Contains(row.FailReason, config.KeyRoutineAgents) {
		t.Fatalf("fail reason = %q, want it to name %s", row.FailReason, config.KeyRoutineAgents)
	}
}
