package tasks

import (
	"errors"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestNewRunPlanSuccess exercises the shared run-plan constructor directly with
// an injected config-load func, proving the seam both RunTaskSetWith and
// RunTaskWith depend on resolves a usable bundle.
func TestNewRunPlanSuccess(t *testing.T) {
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{
			Task: &config.TasksConfig{
				Git: &config.TaskGitConfig{
					CommitConfigOverrides: []string{"user.name=Pop"},
				},
			},
		}, nil
	}

	plan, err := newRunPlan(loadConfig, runPlanInput{
		agentPresets: []string{"claude"},
		agentCmd:     "",
		allowDirty:   DirtyRuntimeContinue,
	})
	if err != nil {
		t.Fatalf("newRunPlan returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("newRunPlan returned nil plan")
	}
	if plan.cfg == nil {
		t.Fatal("plan.cfg is nil")
	}
	if plan.baseAgentPreset != "claude" {
		t.Fatalf("baseAgentPreset = %q, want %q", plan.baseAgentPreset, "claude")
	}
	if len(plan.baseAgentPresets) != 1 || plan.baseAgentPresets[0] != "claude" {
		t.Fatalf("baseAgentPresets = %v, want [claude]", plan.baseAgentPresets)
	}
	if plan.strategy != DirtyRuntimeContinue {
		t.Fatalf("strategy = %q, want %q", plan.strategy, DirtyRuntimeContinue)
	}
	if len(plan.commitOverrides) != 1 || plan.commitOverrides[0] != "user.name=Pop" {
		t.Fatalf("commitOverrides = %v, want [user.name=Pop]", plan.commitOverrides)
	}
	// Lazy resolvers stay callable off the plan.
	if _, err := plan.maxTries(false, 0); err != nil {
		t.Fatalf("plan.maxTries returned error: %v", err)
	}
	if _, err := plan.retryDelays(); err != nil {
		t.Fatalf("plan.retryDelays returned error: %v", err)
	}
}

// TestNewRunPlanCommitOverridesSetupError proves a malformed [tasks.git] entry
// fails the constructor as an ExitSetup error — the validation that must happen
// before any commit could run — with the underlying message preserved.
func TestNewRunPlanCommitOverridesSetupError(t *testing.T) {
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{
			Task: &config.TasksConfig{
				Git: &config.TaskGitConfig{
					CommitConfigOverrides: []string{"nokey"},
				},
			},
		}, nil
	}

	_, err := newRunPlan(loadConfig, runPlanInput{
		agentPresets: []string{"claude"},
		allowDirty:   DirtyRuntimeContinue,
	})
	if err == nil {
		t.Fatal("newRunPlan succeeded, want setup error")
	}
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error is not *ExitError: %T (%v)", err, err)
	}
	if exit.Code != ExitSetup {
		t.Fatalf("exit code = %d, want ExitSetup (%d)", exit.Code, ExitSetup)
	}
	if !strings.Contains(err.Error(), "commit_config_overrides") {
		t.Fatalf("error message %q does not mention commit_config_overrides", err.Error())
	}
}

// TestRunTaskWithConsumesSharedRunPlan proves the single-task entry point
// resolves its config bundle through the same newRunPlan seam RunTaskSetWith
// uses (decision 5): a malformed [tasks.git] commit-config override injected
// via loadConfig must fail RunTaskWith as an ExitSetup error before any agent
// runs or Drain is claimed — exactly the setup-error behavior
// TestNewRunPlanCommitOverridesSetupError proves for the constructor directly.
func TestRunTaskWithConsumesSharedRunPlan(t *testing.T) {
	env := setupExecutorFixture(t, false)
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{
			Task: &config.TasksConfig{
				Git: &config.TaskGitConfig{
					CommitConfigOverrides: []string{"nokey"},
				},
			},
		}, nil
	}

	opts := env.runOpts(true, "")
	_, err := RunTaskWith(env.deps(), nil, loadConfig, opts)
	if err == nil {
		t.Fatal("RunTaskWith succeeded, want setup error")
	}
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error is not *ExitError: %T (%v)", err, err)
	}
	if exit.Code != ExitSetup {
		t.Fatalf("exit code = %d, want ExitSetup (%d)", exit.Code, ExitSetup)
	}
	if !strings.Contains(err.Error(), "commit_config_overrides") {
		t.Fatalf("error message %q does not mention commit_config_overrides", err.Error())
	}
}
