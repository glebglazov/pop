package routine

import (
	"io"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestResolveRoutineAgentPresetsPrefersRoutinesConfig(t *testing.T) {
	cfg := &config.Config{
		Routines: &config.RoutinesConfig{Agents: []string{"codex", "claude"}},
		Task: &config.TasksConfig{
			Implement: &config.ImplementConfig{Agents: []string{"cursor"}},
		},
	}
	got := ResolveRoutineAgentPresets(nil, cfg)
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
		Task: &config.TasksConfig{
			Implement: &config.ImplementConfig{Agents: []string{"cursor", "claude"}},
		},
	}
	got := ResolveRoutineAgentPresets(nil, cfg)
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
			Routines: &config.RoutinesConfig{Agents: []string{"claude", "codex"}},
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
	result, preset, err := runRoutineWithAgentFallback(d, cfg, ResolveRoutineAgentPresets(nil, cfg), io.Discard, attempt)
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
			Routines: &config.RoutinesConfig{Agents: []string{"claude", "codex"}},
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
	_, preset, err := runRoutineWithAgentFallback(d, cfg, ResolveRoutineAgentPresets(nil, cfg), io.Discard, attempt)
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
		Routines: &config.RoutinesConfig{Agents: []string{"claude"}},
	}
	attempt := func(agentSpec string) (*tasks.RoutineAgentAttempt, error) {
		return &tasks.RoutineAgentAttempt{
			QuotaPaused:  true,
			QuotaPreset:  "claude",
			QuotaResetAt: time.Now().Add(time.Hour),
		}, nil
	}
	_, _, err := runRoutineWithAgentFallback(d, cfg, ResolveRoutineAgentPresets(nil, cfg), io.Discard, attempt)
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
