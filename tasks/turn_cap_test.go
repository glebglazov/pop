package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
)

// implementArgv resolves one implementation attempt's command and returns the
// whole argv, executable first, so a cap flag can be located (or its absence
// proven).
func implementArgv(t *testing.T, spec, agentCmd string, turnCap int) []string {
	t.Helper()
	invocation, err := ResolveImplementAgentInvocation(spec, agentCmd, "PROMPT", "/runtime", AgentOutputAuto, turnCap)
	if err != nil {
		t.Fatalf("resolve %q with cap %d: %v", spec, turnCap, err)
	}
	return append([]string{invocation.Name}, invocation.Args...)
}

func maxTurnsValues(argv []string) []string {
	var values []string
	for i, arg := range argv {
		if arg == "--max-turns" {
			if i+1 < len(argv) {
				values = append(values, argv[i+1])
				continue
			}
			values = append(values, "")
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--max-turns="); ok {
			values = append(values, value)
		}
	}
	return values
}

// TestImplementAttemptCarriesTurnCapOnlyWhereEnforceable pins ADR-0190 decisions
// 4 and 5 at the invocation seam: claude is told the repository's bound, the five
// presets that cannot be told are launched exactly as they would be without one,
// and a cap the human wrote into the spec himself is left alone.
func TestImplementAttemptCarriesTurnCapOnlyWhereEnforceable(t *testing.T) {
	t.Parallel()

	t.Run("claude carries the declared cap", func(t *testing.T) {
		argv := implementArgv(t, "claude", "", 7)
		if got := maxTurnsValues(argv); len(got) != 1 || got[0] != "7" {
			t.Fatalf("--max-turns in %v = %v, want exactly [7]", argv, got)
		}
	})

	t.Run("no declared cap carries no flag", func(t *testing.T) {
		argv := implementArgv(t, "claude", "", 0)
		if got := maxTurnsValues(argv); len(got) != 0 {
			t.Fatalf("uncapped claude argv %v carries --max-turns %v", argv, got)
		}
	})

	t.Run("a preset that cannot enforce a cap is launched unchanged", func(t *testing.T) {
		for _, preset := range []string{"opencode", "cursor", "codex", "pi", "kimi"} {
			capability, err := ResolveAgentAdapter(preset)
			if err != nil {
				t.Fatalf("%s: %v", preset, err)
			}
			if kind := capability.TurnCapEnforcementCapability().Kind; kind != CapabilityBlind {
				t.Fatalf("%s turn-cap enforcement = %v, want Blind", preset, kind)
			}
			uncapped := implementArgv(t, preset, "", 0)
			capped := implementArgv(t, preset, "", 7)
			if strings.Join(uncapped, "\x00") != strings.Join(capped, "\x00") {
				t.Fatalf("%s argv changed under a cap:\n uncapped %v\n capped   %v", preset, uncapped, capped)
			}
		}
	})

	t.Run("a custom agent command is launched unchanged", func(t *testing.T) {
		capability := customAgentAdapter{}.TurnCapEnforcementCapability()
		if err := capability.validate("custom"); err != nil {
			t.Fatalf("custom turn-cap enforcement: %v", err)
		}
		if capability.Kind != CapabilityBlind {
			t.Fatalf("custom turn-cap enforcement = %v, want Blind", capability.Kind)
		}
		uncapped := implementArgv(t, "", "my-agent --go", 0)
		capped := implementArgv(t, "", "my-agent --go", 7)
		if strings.Join(uncapped, "\x00") != strings.Join(capped, "\x00") {
			t.Fatalf("custom argv changed under a cap:\n uncapped %v\n capped   %v", uncapped, capped)
		}
	})

	t.Run("a hand-set cap wins and pop emits none of its own", func(t *testing.T) {
		argv := implementArgv(t, "claude --max-turns 5", "", 7)
		if got := maxTurnsValues(argv); len(got) != 1 || got[0] != "5" {
			t.Fatalf("--max-turns in %v = %v, want exactly [5] (the hand-set cap)", argv, got)
		}
	})
}

// TestVerificationInvocationCarriesNoTurnCap pins ADR-0190 decision 2 at the
// resolver every uncapped verb goes through: a Verifier is a claude headless run
// that would accept the flag, and it never gets one.
func TestVerificationInvocationCarriesNoTurnCap(t *testing.T) {
	t.Parallel()
	invocation, err := ResolveAgentInvocationWithMode("claude", "", "PROMPT", "/runtime", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve verifier invocation: %v", err)
	}
	argv := append([]string{invocation.Name}, invocation.Args...)
	if got := maxTurnsValues(argv); len(got) != 0 {
		t.Fatalf("verifier argv %v carries --max-turns %v", argv, got)
	}
}

// claudeArgvRecorder answers claude-preset invocations in process, recording the
// argv it is handed. It is where this slice's whole path ends: the drain resolves
// an invocation and the runner spawns it, so the flags pop chose are exactly what
// lands here. An implementation prompt is answered by ticking the task and
// reporting completion; anything else is a Verifier and is answered with a PASS.
type claudeArgvRecorder struct {
	mu        sync.Mutex
	implement [][]string
	verify    [][]string
}

// Run serves the claude availability probe (`claude auth status`). An empty
// answer reads as "authentication status unknown", which proceeds.
func (r *claudeArgvRecorder) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *claudeArgvRecorder) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	prompt := ""
	if len(args) > 0 {
		prompt = readSpilledPrompt(args[len(args)-1])
	}
	argv := append([]string{name}, args...)
	result := "VERDICT: PASS"
	r.mu.Lock()
	if taskPath := parseFakeAgentTaskPath(prompt); taskPath != "" {
		tickTaskFile(taskPath)
		result = "SUMMARY_START\ncap-bounded work\nSUMMARY_END\nTASK_COMPLETE"
		r.implement = append(r.implement, argv)
	} else {
		r.verify = append(r.verify, argv)
	}
	r.mu.Unlock()
	event, err := json.Marshal(map[string]string{"type": "result", "subtype": "success", "result": result})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(stdout, "%s\n", event)
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{exitCode: 0}
	return proc, nil
}

// TestDrainCarriesRepoTurnCapToImplementerNotVerifier drives a whole drain of a
// repository that declares a Turn cap in a central [repo."<path>"] block: the
// implementation attempt is launched with --max-turns set to it, and the Verifier
// that judges the same set is launched with no cap flag at all.
func TestDrainCarriesRepoTurnCapToImplementerNotVerifier(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	d := env.deps()
	runner := &claudeArgvRecorder{}
	d.Runner = runner
	d.LookPath = func(file string) (string, error) { return filepath.Join("/usr/bin", file), nil }

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	cfg := &config.Config{
		Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}},
		Repo: map[string]config.RepoOverrideConfig{
			runtimePath: {TurnCap: intPtr(9)},
		},
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.AgentPreset = "claude"

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) { return cfg, nil }, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v\n%s", err, buf.String())
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone\n%s", result, buf.String())
	}

	if len(runner.implement) == 0 {
		t.Fatalf("no implementation attempt was launched\n%s", buf.String())
	}
	for _, argv := range runner.implement {
		if got := maxTurnsValues(argv); len(got) != 1 || got[0] != "9" {
			t.Fatalf("implement argv %v carries --max-turns %v, want exactly [9]", argv, got)
		}
	}
	if len(runner.verify) == 0 {
		t.Fatalf("no Verifier was launched\n%s", buf.String())
	}
	for _, argv := range runner.verify {
		if got := maxTurnsValues(argv); len(got) != 0 {
			t.Fatalf("verifier argv %v carries --max-turns %v, want none", argv, got)
		}
	}
}

// TestDrainWithoutRepoTurnCapCarriesNoCapFlag is the same whole drain in a
// repository that declares nothing: the attempt is launched exactly as it was
// before a cap existed.
func TestDrainWithoutRepoTurnCapCarriesNoCapFlag(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	d := env.deps()
	runner := &claudeArgvRecorder{}
	d.Runner = runner
	d.LookPath = func(file string) (string, error) { return filepath.Join("/usr/bin", file), nil }

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.AgentPreset = "claude"

	if _, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) { return &config.Config{}, nil }, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v\n%s", err, buf.String())
	}
	if len(runner.implement) == 0 {
		t.Fatalf("no implementation attempt was launched\n%s", buf.String())
	}
	for _, argv := range runner.implement {
		if got := maxTurnsValues(argv); len(got) != 0 {
			t.Fatalf("uncapped implement argv %v carries --max-turns %v", argv, got)
		}
	}
}

// TestResolveRepoTurnCapReadsOneBoundPerRepository proves the read path the drain
// uses: a bound declared for one worktree of a repository resolves from every
// other worktree of it, because the block is matched by repository identity
// (ADR-0191).
func TestResolveRepoTurnCapReadsOneBoundPerRepository(t *testing.T) {
	t.Parallel()
	bareRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bareRoot, ".bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(bareRoot, "main")
	feature := filepath.Join(bareRoot, "feature")
	for _, dir := range []string{main, feature} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Repo: map[string]config.RepoOverrideConfig{
		main: {TurnCap: intPtr(12)},
	}}
	d := DefaultDeps()

	if got := resolveRepoTurnCap(d, cfg, feature); got != 12 {
		t.Fatalf("turn cap at the sibling worktree = %d, want 12", got)
	}
	if got := resolveRepoTurnCap(d, &config.Config{}, feature); got != 0 {
		t.Fatalf("turn cap with no declaration = %d, want 0", got)
	}
}
