package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func TestResolveAgentCommandPresets(t *testing.T) {
	presets := []string{"claude", "opencode", "cursor", "codex", "pi"}
	for _, preset := range presets {
		name, args, err := ResolveAgentCommand(preset, "", "prompt text", "/tmp/runtime")
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		if name == "" || len(args) == 0 {
			t.Fatalf("%s: empty command", preset)
		}
		last := args[len(args)-1]
		if last != "prompt text" {
			t.Fatalf("%s: last arg = %q", preset, last)
		}
	}
}

func TestResolveAgentInvocationPreservesRepresentativePresetCommands(t *testing.T) {
	tests := []struct {
		preset string
		name   string
		args   []string
		format AgentOutputFormat
	}{
		{
			preset: "claude",
			name:   "claude",
			args:   []string{"--dangerously-skip-permissions", "-p", "--output-format", "stream-json", "--verbose", "prompt text"},
			format: AgentOutputClaudeStreamJSON,
		},
		{
			preset: "cursor",
			name:   "cursor-agent",
			args:   []string{"-p", "--force", "--trust", "--output-format", "stream-json", "--workspace", "/tmp/runtime", "prompt text"},
			format: AgentOutputCursorStreamJSON,
		},
		{
			preset: "codex",
			name:   "codex",
			args:   []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "--json", "prompt text"},
			format: AgentOutputCodexJSONL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			invocation, err := ResolveAgentInvocation(tt.preset, "", "prompt text", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if invocation.Name != tt.name {
				t.Fatalf("name = %q, want %q", invocation.Name, tt.name)
			}
			if !reflect.DeepEqual(invocation.Args, tt.args) {
				t.Fatalf("args = %#v, want %#v", invocation.Args, tt.args)
			}
			if invocation.OutputFormat != tt.format {
				t.Fatalf("format = %q, want %q", invocation.OutputFormat, tt.format)
			}
		})
	}
}

func TestResolveAgentCommandDefaultClaude(t *testing.T) {
	name, args, err := ResolveAgentCommand("", "", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "claude" {
		t.Fatalf("name = %q", name)
	}
	if args[0] != "--dangerously-skip-permissions" && len(args) < 2 {
		t.Fatalf("args = %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--output-format stream-json") || !strings.Contains(joined, "--verbose") {
		t.Fatalf("claude args = %v", args)
	}
}

func TestResolveAgentCommandCursor(t *testing.T) {
	name, args, err := ResolveAgentCommand("cursor", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "cursor-agent" {
		t.Fatalf("name = %q, want cursor-agent", name)
	}
	wantPrefix := []string{"-p", "--force", "--trust", "--output-format", "stream-json", "--workspace", "/tmp/runtime"}
	if len(args) < len(wantPrefix)+1 {
		t.Fatalf("args = %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, args[i], want, args)
		}
	}
	if args[len(args)-1] != "prompt text" {
		t.Fatalf("last arg = %q", args[len(args)-1])
	}
}

func TestResolveAgentCommandPiHermetic(t *testing.T) {
	name, args, err := ResolveAgentCommand("pi", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "pi" {
		t.Fatalf("name = %q, want pi", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-extensions", "--no-skills", "--mode json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("pi args missing %q: %v", want, args)
		}
	}
	if args[len(args)-1] != "prompt text" {
		t.Fatalf("last arg = %q", args[len(args)-1])
	}
}

func TestResolveAgentInvocationKimiPromptRidesAsFlagValue(t *testing.T) {
	invocation, err := ResolveAgentInvocation("kimi", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Name != "kimi" {
		t.Fatalf("name = %q, want kimi", invocation.Name)
	}
	// kimi has no positional prompt form: the prompt is -p's value, so it sits
	// next to the flag rather than at the end, and -p is auto-permission so no
	// permission flag is owned at all.
	want := []string{"-p", "prompt text", "--output-format", "stream-json"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
	if invocation.OutputFormat != AgentOutputKimiStreamJSON {
		t.Fatalf("format = %q, want %q", invocation.OutputFormat, AgentOutputKimiStreamJSON)
	}
	if !reflect.DeepEqual(invocation.Env, []string{"KIMI_CODE_NO_AUTO_UPDATE=1"}) {
		t.Fatalf("env = %#v, want KIMI_CODE_NO_AUTO_UPDATE=1", invocation.Env)
	}
}

func TestResolveAgentInvocationKimiExtraArgsPrecedeOwnedFlags(t *testing.T) {
	invocation, err := ResolveAgentInvocation("kimi --model moonshot-ai/kimi-k3", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "moonshot-ai/kimi-k3", "-p", "prompt text", "--output-format", "stream-json"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
}

func TestResolveAgentInvocationKimiTextModeDropsOwnedOutputFlags(t *testing.T) {
	invocation, err := ResolveAgentInvocationWithMode("kimi", "", "prompt text", "/tmp/runtime", AgentOutputText)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "prompt text"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
	if invocation.OutputFormat != AgentOutputPlain {
		t.Fatalf("format = %q, want plain", invocation.OutputFormat)
	}
}

func TestPresetInvocationsWithoutEnvNeedsStayEmpty(t *testing.T) {
	for _, preset := range []string{"claude", "opencode", "cursor", "codex", "pi"} {
		t.Run(preset, func(t *testing.T) {
			invocation, err := ResolveAgentInvocation(preset, "", "p", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if len(invocation.Env) != 0 {
				t.Fatalf("env = %#v, want empty", invocation.Env)
			}
		})
	}
}

func TestKimiIsOptInOnlyAndNeverADefault(t *testing.T) {
	if DefaultAgentPreset == "kimi" {
		t.Fatal("built-in default must stay claude")
	}
	for _, specs := range [][]string{
		ResolveDefaultAgentPresets(nil, "", false, nil),
		ResolveDefaultAgentPresets(nil, "", false, &config.Config{}),
		{ResolveDefaultInteractiveAgentPreset(nil)},
	} {
		for _, spec := range specs {
			if spec == "kimi" {
				t.Fatalf("kimi appeared in a default agent list: %v", specs)
			}
		}
	}
}

func TestKimiAssistanceLaunchesBareInteractiveBinary(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation(nil, "kimi --model moonshot-ai/kimi-k3", "", "briefing text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Mode != AgentAssistanceNative {
		t.Fatalf("mode = %q, want native", invocation.Mode)
	}
	if invocation.Command.Name != "kimi" {
		t.Fatalf("command = %q, want kimi", invocation.Command.Name)
	}
	// kimi's interactive mode accepts no initial prompt, so the briefing is
	// never an argv item (ADR-0164); it declares no auto-approval flag and the
	// spec's model is not an attended concern (ADR-0187), so argv is empty.
	if len(invocation.Command.Args) != 0 {
		t.Fatalf("args = %#v, want none", invocation.Command.Args)
	}
	if invocation.ClipboardPrompt != "briefing text" {
		t.Fatalf("ClipboardPrompt = %q, want the briefing (delivered via clipboard, ADR-0164)", invocation.ClipboardPrompt)
	}
	if !strings.Contains(invocation.Detail, "clipboard") {
		t.Fatalf("Detail = %q, want it to mention clipboard delivery", invocation.Detail)
	}
}

// TestClaudeAssistanceCarriesNoClipboardPrompt contrasts kimi's clipboard path
// with every preset whose interactive binary takes the briefing positionally:
// the prompt rides in argv, so ClipboardPrompt must stay empty.
func TestClaudeAssistanceCarriesNoClipboardPrompt(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation(nil, "claude", "", "briefing text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.ClipboardPrompt != "" {
		t.Fatalf("ClipboardPrompt = %q, want empty when the prompt rides in argv", invocation.ClipboardPrompt)
	}
	found := false
	for _, arg := range invocation.Command.Args {
		if arg == "briefing text" {
			found = true
		}
	}
	if !found {
		t.Fatalf("args = %#v, want the briefing as a positional arg", invocation.Command.Args)
	}
}

func TestResolveAgentCommandCustom(t *testing.T) {
	name, args, err := ResolveAgentCommand("", "fake-agent --verbose", "prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "sh" {
		t.Fatalf("name = %q", name)
	}
	if !strings.Contains(args[1], "fake-agent") {
		t.Fatalf("args = %v", args)
	}
	if args[len(args)-1] != "prompt" {
		t.Fatalf("prompt arg = %q", args[len(args)-1])
	}
}

func TestResolveAgentCommandUnknownPreset(t *testing.T) {
	_, _, err := ResolveAgentCommand("unknown", "", "p", "/tmp/runtime")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAgentInvocationAugmentedPreset(t *testing.T) {
	invocation, err := ResolveAgentInvocation("claude --model opus4.8", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Name != "claude" {
		t.Fatalf("name = %q, want claude", invocation.Name)
	}
	want := []string{"--model", "opus4.8", "--dangerously-skip-permissions", "-p", "--output-format", "stream-json", "--verbose", "prompt text"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
	if invocation.OutputFormat != AgentOutputClaudeStreamJSON {
		t.Fatalf("format = %q, want structured", invocation.OutputFormat)
	}
	if invocation.AgentPreset() != "claude" {
		t.Fatalf("preset = %q, want claude", invocation.AgentPreset())
	}
}

func TestResolveTaskAgentSpecForEffortClaudeModels(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "heavy", agentSpec: "claude", effort: "heavy", want: "claude --model opus --effort high"},
		{name: "standard", agentSpec: "claude", effort: "standard", want: "claude --model sonnet --effort high"},
		{name: "light", agentSpec: "claude", effort: "light", want: "claude --model haiku --effort high"},
		{name: "preserves explicit model", agentSpec: "claude --model custom", effort: "heavy", want: "claude --model custom"},
		{name: "preserves quoted extra arg", agentSpec: `claude --append-system-prompt "be nice"`, effort: "heavy", want: "claude --append-system-prompt 'be nice' --model opus --effort high"},
		{name: "preserves explicit reasoning", agentSpec: "claude --effort low", effort: "heavy", want: "claude --effort low --model opus"},
		{name: "absent effort unchanged", agentSpec: "claude", effort: "standard", want: "claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explicit := tt.name != "absent effort unchanged"
			got := resolveTaskAgentSpecForEffort(tt.agentSpec, tt.effort, explicit)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskAgentSpecForEffortCodexModels(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "heavy", agentSpec: "codex", effort: "heavy", want: `codex --model gpt-5.5 -c 'model_reasoning_effort="high"'`},
		{name: "standard", agentSpec: "codex", effort: "standard", want: `codex --model gpt-5.5 -c 'model_reasoning_effort="medium"'`},
		{name: "light", agentSpec: "codex", effort: "light", want: `codex --model gpt-5.4-mini -c 'model_reasoning_effort="low"'`},
		{name: "preserves explicit model and skips reasoning", agentSpec: "codex --model custom", effort: "heavy", want: "codex --model custom"},
		{name: "preserves explicit reasoning", agentSpec: "codex -c model_reasoning_effort=low", effort: "heavy", want: "codex -c model_reasoning_effort=low --model gpt-5.5"},
		{name: "preserves explicit reasoning via equals form", agentSpec: "codex -c=model_reasoning_effort=low", effort: "heavy", want: "codex -c=model_reasoning_effort=low --model gpt-5.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskAgentSpecForEffort(tt.agentSpec, tt.effort, true)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskAgentSpecForEffortCursorModels(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "heavy", agentSpec: "cursor", effort: "heavy", want: `cursor --model composer-2.5`},
		{name: "standard", agentSpec: "cursor", effort: "standard", want: `cursor --model composer-2.5`},
		{name: "light", agentSpec: "cursor", effort: "light", want: `cursor --model composer-2.5-fast`},
		{name: "preserves explicit model", agentSpec: "cursor --model custom", effort: "heavy", want: "cursor --model custom"},
		{name: "preserves explicit bracketed model", agentSpec: `cursor --model "composer-2.5[effort=low]"`, effort: "heavy", want: `cursor --model "composer-2.5[effort=low]"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskAgentSpecForEffort(tt.agentSpec, tt.effort, true)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskAgentSpecForEffortPiModels(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "heavy", agentSpec: "pi", effort: "heavy", want: "pi --model opencode-go/qwen3.7-max --thinking high"},
		{name: "standard", agentSpec: "pi", effort: "standard", want: "pi --model opencode-go/kimi-k2.6 --thinking medium"},
		{name: "light", agentSpec: "pi", effort: "light", want: "pi --model opencode-go/deepseek-v4-flash --thinking low"},
		{name: "preserves explicit model and skips thinking", agentSpec: "pi --model custom", effort: "heavy", want: "pi --model custom"},
		{name: "preserves explicit thinking", agentSpec: "pi --thinking low", effort: "heavy", want: "pi --thinking low --model opencode-go/qwen3.7-max"},
		{name: "preserves explicit thinking via equals form", agentSpec: "pi --thinking=low", effort: "heavy", want: "pi --thinking=low --model opencode-go/qwen3.7-max"},
		{name: "preserves model thinking shorthand", agentSpec: "pi --model opencode-go/kimi-k2.6:low", effort: "heavy", want: "pi --model opencode-go/kimi-k2.6:low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskAgentSpecForEffort(tt.agentSpec, tt.effort, true)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

// unsetEnvForTest clears a variable for one test and restores whatever the
// ambient environment had, so a level exported into the dev's own shell cannot
// silently flip a ladder expectation.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTaskAgentSpecForEffortKimiModels(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "heavy", agentSpec: "kimi", effort: "heavy", want: "kimi --model moonshot-ai/kimi-k3 KIMI_MODEL_THINKING_EFFORT=high"},
		{name: "standard", agentSpec: "kimi", effort: "standard", want: "kimi --model moonshot-ai/kimi-k3 KIMI_MODEL_THINKING_EFFORT=low"},
		{name: "light is model only", agentSpec: "kimi", effort: "light", want: "kimi --model moonshot-ai/kimi-k2.7-code-highspeed"},
		{name: "preserves explicit model and skips reasoning", agentSpec: "kimi --model kimi-code/k3", effort: "heavy", want: "kimi --model kimi-code/k3"},
		{name: "preserves explicit reasoning assignment", agentSpec: "kimi KIMI_MODEL_THINKING_EFFORT=max", effort: "heavy", want: "kimi KIMI_MODEL_THINKING_EFFORT=max --model moonshot-ai/kimi-k3"},
		{name: "absent effort unchanged", agentSpec: "kimi", effort: "standard", want: "kimi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvForTest(t, "KIMI_MODEL_THINKING_EFFORT")
			explicit := tt.name != "absent effort unchanged"
			got := resolveTaskAgentSpecForEffort(tt.agentSpec, tt.effort, explicit)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

// The kimi ladder's reasoning has no flag to ride, so this walks the whole path
// an effort tier takes to the spawned process: tier to spec, spec to invocation
// environment (ADR-0164).
func TestKimiEffortReasoningReachesTheInvocationEnvironment(t *testing.T) {
	tests := []struct {
		effort   string
		wantArgs []string
		wantEnv  []string
	}{
		{
			effort:   "heavy",
			wantArgs: []string{"--model", "moonshot-ai/kimi-k3", "-p", "prompt text", "--output-format", "stream-json"},
			wantEnv:  []string{"KIMI_CODE_NO_AUTO_UPDATE=1", "KIMI_MODEL_THINKING_EFFORT=high"},
		},
		{
			effort:   "standard",
			wantArgs: []string{"--model", "moonshot-ai/kimi-k3", "-p", "prompt text", "--output-format", "stream-json"},
			wantEnv:  []string{"KIMI_CODE_NO_AUTO_UPDATE=1", "KIMI_MODEL_THINKING_EFFORT=low"},
		},
		{
			effort:   "light",
			wantArgs: []string{"--model", "moonshot-ai/kimi-k2.7-code-highspeed", "-p", "prompt text", "--output-format", "stream-json"},
			wantEnv:  []string{"KIMI_CODE_NO_AUTO_UPDATE=1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.effort, func(t *testing.T) {
			unsetEnvForTest(t, "KIMI_MODEL_THINKING_EFFORT")
			spec := resolveTaskAgentSpecForEffort("kimi", tt.effort, true)
			invocation, err := ResolveAgentInvocation(spec, "", "prompt text", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(invocation.Args, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", invocation.Args, tt.wantArgs)
			}
			if !reflect.DeepEqual(invocation.Env, tt.wantEnv) {
				t.Fatalf("env = %#v, want %#v", invocation.Env, tt.wantEnv)
			}
		})
	}
}

func TestKimiHandSetThinkingEffortEnvWinsOverLadder(t *testing.T) {
	t.Setenv("KIMI_MODEL_THINKING_EFFORT", "max")
	spec := resolveTaskAgentSpecForEffort("kimi", "heavy", true)
	// The model still applies; only the reasoning half yields.
	if want := "kimi --model moonshot-ai/kimi-k3"; spec != want {
		t.Fatalf("spec = %q, want %q", spec, want)
	}
	invocation, err := ResolveAgentInvocation(spec, "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	// Pop's own KIMI_MODEL_THINKING_EFFORT reaches the child through inheritance,
	// so the invocation carries only the baked entry.
	if !reflect.DeepEqual(invocation.Env, []string{"KIMI_CODE_NO_AUTO_UPDATE=1"}) {
		t.Fatalf("env = %#v, want only the baked entry", invocation.Env)
	}
}

func TestKimiPinnedModelSuppressesModelAndReasoning(t *testing.T) {
	unsetEnvForTest(t, "KIMI_MODEL_THINKING_EFFORT")
	spec := resolveTaskAgentSpecForEffort("kimi --model kimi-code/k3", "heavy", true)
	if want := "kimi --model kimi-code/k3"; spec != want {
		t.Fatalf("spec = %q, want %q", spec, want)
	}
	invocation, err := ResolveAgentInvocation(spec, "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(invocation.Env, []string{"KIMI_CODE_NO_AUTO_UPDATE=1"}) {
		t.Fatalf("env = %#v, want only the baked entry", invocation.Env)
	}
}

func TestResolveTaskAgentSpecForEffortKimiConfiguredLadderReplacesBuiltIn(t *testing.T) {
	// An install whose provider config names aliases differently overrides the
	// baked moonshot-ai names wholesale (ADR-0164).
	cfg := &config.Config{Effort: map[string]config.EffortConfig{
		"kimi": {
			Heavy: []config.EffortModel{{Model: "kimi-code/k3", Reasoning: "max"}},
			Light: []config.EffortModel{{Model: "kimi-code/k2.7-code"}},
		},
	}}
	tests := []struct {
		name   string
		effort string
		want   string
	}{
		{name: "configured heavy", effort: "heavy", want: "kimi --model kimi-code/k3 KIMI_MODEL_THINKING_EFFORT=max"},
		{name: "configured light", effort: "light", want: "kimi --model kimi-code/k2.7-code"},
		{name: "unconfigured tier resolves nothing", effort: "standard", want: "kimi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvForTest(t, "KIMI_MODEL_THINKING_EFFORT")
			got := resolveTaskAgentSpecForEffortWithConfig("kimi", tt.effort, true, cfg)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

// The env-assignment channel is per-preset: an argument that merely contains
// "=" must stay in argv for a preset that has no env keys of its own.
func TestArgChannelPresetKeepsEqualsArgsInArgv(t *testing.T) {
	spec := resolveTaskAgentSpecForEffort("codex", "heavy", true)
	invocation, err := ResolveAgentInvocation(spec, "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if len(invocation.Env) != 0 {
		t.Fatalf("env = %#v, want empty", invocation.Env)
	}
	joined := strings.Join(invocation.Args, " ")
	if !strings.Contains(joined, `-c model_reasoning_effort="high"`) {
		t.Fatalf("args lost the reasoning config: %#v", invocation.Args)
	}
}

func TestResolveTaskAgentSpecForEffortCursorConfiguredModelOnly(t *testing.T) {
	cfg := &config.Config{Effort: map[string]config.EffortConfig{
		"cursor": {
			Heavy: []config.EffortModel{{Model: "composer-2.5"}},
		},
	}}
	got := resolveTaskAgentSpecForEffortWithConfig("cursor", "heavy", true, cfg)
	want := "cursor --model composer-2.5"
	if got != want {
		t.Fatalf("spec = %q, want %q", got, want)
	}
}

func TestCursorAdapterDetectsBracketedReasoning(t *testing.T) {
	adapter, err := ResolveAgentAdapter("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.ReasoningCapability().argsContainReasoning([]string{"--model", "composer-2.5[effort=high]"}) {
		t.Fatal("cursor adapter did not detect bracketed effort")
	}
	if adapter.ReasoningCapability().argsContainReasoning([]string{"--model", "composer-2.5"}) {
		t.Fatal("cursor adapter detected reasoning in a plain model token")
	}
}

func TestPiAdapterDetectsThinkingReasoning(t *testing.T) {
	adapter, err := ResolveAgentAdapter("pi")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "flag form", args: []string{"--thinking", "high"}, want: true},
		{name: "equals flag form", args: []string{"--thinking=medium"}, want: true},
		{name: "model shorthand separate arg", args: []string{"--model", "opencode-go/kimi-k2.6:low"}, want: true},
		{name: "model shorthand equals arg", args: []string{"--model=opencode-go/kimi-k2.6:low"}, want: true},
		{name: "plain model", args: []string{"--model", "opencode-go/kimi-k2.6"}, want: false},
		{name: "bare thinking token", args: []string{"--thinking"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.ReasoningCapability().argsContainReasoning(tt.args)
			if got != tt.want {
				t.Fatalf("argsContainReasoning(%#v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestResolveTaskAgentSpecForConfiguredEffortModels(t *testing.T) {
	cfg := &config.Config{Effort: map[string]config.EffortConfig{
		"opencode": {
			Heavy:    []config.EffortModel{{Model: "opencode/claude-opus-4-8", Reasoning: "high"}, {Model: "opencode/kimi-k2.6"}},
			Standard: []config.EffortModel{{Model: "opencode/claude-sonnet-4-6"}},
			Light:    []config.EffortModel{{Model: "opencode/kimi-k2.6"}},
		},
		"claude": {
			Heavy: []config.EffortModel{{Model: "custom-opus", Reasoning: "max"}},
		},
	}}
	tests := []struct {
		name      string
		agentSpec string
		effort    string
		want      string
	}{
		{name: "configured opencode", agentSpec: "opencode", effort: "heavy", want: "opencode --model opencode/claude-opus-4-8"},
		{name: "configured claude replaces built in", agentSpec: "claude", effort: "standard", want: "claude"},
		{name: "configured claude uses configured tier", agentSpec: "claude", effort: "heavy", want: "claude --model custom-opus --effort max"},
		{name: "codex uses built in when unconfigured", agentSpec: "codex", effort: "heavy", want: `codex --model gpt-5.5 -c 'model_reasoning_effort="high"'`},
		{name: "explicit model still wins", agentSpec: "opencode --model already", effort: "heavy", want: "opencode --model already"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskAgentSpecForEffortWithConfig(tt.agentSpec, tt.effort, true, cfg)
			if got != tt.want {
				t.Fatalf("spec = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskAgentSpecEffortModelPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		agentCmd       string
		defaultSpecs   []string
		effort         string
		effortExplicit bool
		wantSpecs      []string
	}{
		{
			name:           "agent model pin wins over effort",
			defaultSpecs:   []string{"claude --model sonnet"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{"claude --model sonnet"},
		},
		{
			name:           "bare claude composes with effort",
			defaultSpecs:   []string{"claude"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{"claude --model opus --effort high"},
		},
		{
			name:           "codex composes with effort",
			defaultSpecs:   []string{"codex"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{`codex --model gpt-5.5 -c 'model_reasoning_effort="high"'`},
		},
		{
			name:           "cursor composes with explicit model",
			defaultSpecs:   []string{"cursor"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{`cursor --model composer-2.5`},
		},
		{
			name:           "fallback list entries each resolve effort",
			defaultSpecs:   []string{"claude", "cursor", "codex"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{"claude --model opus --effort high", `cursor --model composer-2.5`, `codex --model gpt-5.5 -c 'model_reasoning_effort="high"'`},
		},
		{
			name:           "agent-cmd leaves fallback list untouched",
			agentCmd:       "./my-agent.sh",
			defaultSpecs:   []string{"claude", "codex"},
			effort:         "heavy",
			effortExplicit: true,
			wantSpecs:      []string{"claude", "codex"},
		},
		{
			name:         "absent explicit effort leaves bare claude alone",
			defaultSpecs: []string{"claude"},
			effort:       "standard",
			wantSpecs:    []string{"claude"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolve := newEffortSpecResolver(tt.agentCmd, tt.effort, tt.effortExplicit, nil)
			var got []string
			for _, spec := range nonEmptyAgentSpecs(tt.defaultSpecs, DefaultAgentPreset) {
				got = append(got, resolve(spec, nil).Spec)
			}
			if !reflect.DeepEqual(got, tt.wantSpecs) {
				t.Fatalf("resolved specs = %#v, want %#v", got, tt.wantSpecs)
			}
		})
	}
}

func TestResolveAgentInvocationAugmentedOwnedFlagsAppendedLast(t *testing.T) {
	invocation, err := ResolveAgentInvocation("claude --output-format text", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--output-format", "text", "--dangerously-skip-permissions", "-p", "--output-format", "stream-json", "--verbose", "prompt text"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
	if invocation.OutputFormat != AgentOutputClaudeStreamJSON {
		t.Fatalf("format = %q, want structured despite user --output-format", invocation.OutputFormat)
	}
}

func TestResolveAgentInvocationAugmentedQuotedArgs(t *testing.T) {
	invocation, err := ResolveAgentInvocation(`claude --append-system-prompt "be nice"`, "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--append-system-prompt", "be nice", "--dangerously-skip-permissions", "-p", "--output-format", "stream-json", "--verbose", "prompt text"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
}

func TestResolveAgentInvocationAugmentedUnknownPreset(t *testing.T) {
	_, err := ResolveAgentInvocation("nope --model opus4.8", "", "p", "/tmp/runtime")
	if err == nil || !strings.Contains(err.Error(), `unknown agent preset "nope"`) {
		t.Fatalf("err = %v, want unknown agent preset", err)
	}
}

func TestResolveAgentInvocationAgentCmdWinsOverAugmentedPreset(t *testing.T) {
	invocation, err := ResolveAgentInvocation("claude --model opus4.8", "fake-agent --verbose", "prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Name != "sh" {
		t.Fatalf("name = %q, want sh", invocation.Name)
	}
	if invocation.OutputFormat != AgentOutputPlain {
		t.Fatalf("format = %q, want plain", invocation.OutputFormat)
	}
	if strings.Contains(strings.Join(invocation.Args, " "), "opus4.8") {
		t.Fatalf("augmented preset leaked into --agent-cmd invocation: %#v", invocation.Args)
	}
}

func TestResolveAgentInvocationOutputFormats(t *testing.T) {
	claude, err := ResolveAgentInvocation("claude", "", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if claude.OutputFormat != AgentOutputClaudeStreamJSON {
		t.Fatalf("claude format = %q", claude.OutputFormat)
	}

	formats := map[string]AgentOutputFormat{
		"opencode": AgentOutputOpenCodeJSON,
		"cursor":   AgentOutputCursorStreamJSON,
		"codex":    AgentOutputCodexJSONL,
		"pi":       AgentOutputPiJSONL,
		"kimi":     AgentOutputKimiStreamJSON,
	}
	for preset, want := range formats {
		invocation, err := ResolveAgentInvocation(preset, "", "p", "/tmp/runtime")
		if err != nil {
			t.Fatal(err)
		}
		if invocation.OutputFormat != want {
			t.Fatalf("%s format = %q, want %q", preset, invocation.OutputFormat, want)
		}
	}

	custom, err := ResolveAgentInvocation("", "fake-agent", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if custom.OutputFormat != AgentOutputPlain {
		t.Fatalf("custom format = %q, want plain", custom.OutputFormat)
	}
}

func TestResolveAgentInvocationStructuredFlags(t *testing.T) {
	tests := []struct {
		preset string
		flag   string
	}{
		{preset: "claude", flag: "--output-format stream-json"},
		{preset: "cursor", flag: "--output-format stream-json"},
		{preset: "codex", flag: "--json"},
		{preset: "opencode", flag: "--format json"},
		{preset: "pi", flag: "--mode json"},
		{preset: "kimi", flag: "--output-format stream-json"},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			invocation, err := ResolveAgentInvocation(tt.preset, "", "p", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(invocation.Args, " "), tt.flag) {
				t.Fatalf("%s args = %v", tt.preset, invocation.Args)
			}
		})
	}
}

func TestResolveAgentInvocationTextFallbacks(t *testing.T) {
	for _, preset := range []string{"claude", "cursor", "codex", "opencode", "pi"} {
		t.Run(preset, func(t *testing.T) {
			invocation, err := ResolveAgentInvocationWithMode(preset, "", "p", "/tmp/runtime", AgentOutputText)
			if err != nil {
				t.Fatal(err)
			}
			if invocation.OutputFormat != AgentOutputPlain {
				t.Fatalf("format = %q, want plain", invocation.OutputFormat)
			}
			args := strings.Join(invocation.Args, " ")
			for _, structured := range []string{"stream-json", "--json", "--format json", "--mode json"} {
				if strings.Contains(args, structured) {
					t.Fatalf("%s text fallback args = %v", preset, invocation.Args)
				}
			}
			if preset == "cursor" && !strings.Contains(args, "--output-format text") {
				t.Fatalf("cursor text fallback args = %v", invocation.Args)
			}
		})
	}
}

func TestResolveAgentInvocationTextModePreservesHeadlessCommands(t *testing.T) {
	tests := []struct {
		preset string
		name   string
		args   []string
	}{
		{
			preset: "claude",
			name:   "claude",
			args:   []string{"--dangerously-skip-permissions", "-p", "prompt text"},
		},
		{
			preset: "cursor",
			name:   "cursor-agent",
			args:   []string{"-p", "--force", "--trust", "--output-format", "text", "--workspace", "/tmp/runtime", "prompt text"},
		},
		{
			preset: "pi",
			name:   "pi",
			args:   []string{"-p", "--no-extensions", "--no-skills", "prompt text"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			invocation, err := ResolveAgentInvocationWithMode(tt.preset, "", "prompt text", "/tmp/runtime", AgentOutputText)
			if err != nil {
				t.Fatal(err)
			}
			if invocation.Name != tt.name {
				t.Fatalf("name = %q, want %q", invocation.Name, tt.name)
			}
			if !reflect.DeepEqual(invocation.Args, tt.args) {
				t.Fatalf("args = %#v, want %#v", invocation.Args, tt.args)
			}
			if invocation.OutputFormat != AgentOutputPlain {
				t.Fatalf("format = %q, want plain", invocation.OutputFormat)
			}
		})
	}
}

func TestAgentAssistanceCapabilityNativeForEveryPreset(t *testing.T) {
	wantBinary := map[string]string{
		"claude":   "claude",
		"opencode": "opencode",
		"cursor":   "cursor-agent",
		"codex":    "codex",
		"pi":       "pi",
	}
	for preset, binary := range wantBinary {
		t.Run(preset, func(t *testing.T) {
			capability, err := ResolveAgentAssistanceCapability(preset, "")
			if err != nil {
				t.Fatal(err)
			}
			if !capability.Available() || capability.Mode != AgentAssistanceNative {
				t.Fatalf("capability = %#v, want available native", capability)
			}
			if capability.Command == nil || capability.Command.Name != binary {
				t.Fatalf("native command = %#v, want %q", capability.Command, binary)
			}
		})
	}
}

func TestResolveAgentAssistanceInvocationNative(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation(nil, "claude", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "claude" || invocation.Mode != AgentAssistanceNative {
		t.Fatalf("invocation = %#v, want claude native", invocation)
	}
	if invocation.Command.Name != "claude" {
		t.Fatalf("command name = %q, want claude", invocation.Command.Name)
	}
	if !reflect.DeepEqual(invocation.Command.Args, []string{"--dangerously-skip-permissions", "assist prompt"}) {
		t.Fatalf("command args = %#v", invocation.Command.Args)
	}
	if invocation.Display != "claude --dangerously-skip-permissions <HITL assistance prompt>" {
		t.Fatalf("display = %q", invocation.Display)
	}
	if !strings.Contains(invocation.Detail, "native") || strings.Contains(invocation.Detail, "fallback") {
		t.Fatalf("detail = %q, want native detail", invocation.Detail)
	}
}

func TestResolveAgentAssistanceInvocationCursorLaunchesOwnBinary(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation(nil, "cursor", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "cursor" || invocation.Mode != AgentAssistanceNative {
		t.Fatalf("invocation = %#v, want cursor native", invocation)
	}
	if invocation.Command.Name != "cursor-agent" {
		t.Fatalf("command name = %q, want cursor-agent", invocation.Command.Name)
	}
	if !reflect.DeepEqual(invocation.Command.Args, []string{"--force", "--trust", "assist prompt"}) {
		t.Fatalf("command args = %#v", invocation.Command.Args)
	}
	if invocation.Display != "cursor-agent --force --trust <HITL assistance prompt>" {
		t.Fatalf("display = %q", invocation.Display)
	}
	if !strings.Contains(invocation.Detail, "native") || strings.Contains(invocation.Detail, "fallback") {
		t.Fatalf("detail = %q, want native detail", invocation.Detail)
	}
}

// TestAttendedAssistanceIgnoresImplementListExtraArgs pins decision 4 of
// ADR-0187: an attended session takes the preset name from the implement agent
// list and nothing else, so a --model tuned for unattended drains no longer
// steers the interactive sessions pop opens.
func TestAttendedAssistanceIgnoresImplementListExtraArgs(t *testing.T) {
	for _, tc := range []struct {
		spec string
		name string
		want []string
	}{
		{"claude --model opus4.8", "claude", []string{"--dangerously-skip-permissions", "assist prompt"}},
		{"cursor --model gpt-5", "cursor-agent", []string{"--force", "--trust", "assist prompt"}},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			invocation, err := ResolveAgentAssistanceInvocation(nil, tc.spec, "", "assist prompt", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if invocation.Mode != AgentAssistanceNative || invocation.Command.Name != tc.name {
				t.Fatalf("invocation = %#v, want %s native", invocation, tc.name)
			}
			if !reflect.DeepEqual(invocation.Command.Args, tc.want) {
				t.Fatalf("command args = %#v, want %#v", invocation.Command.Args, tc.want)
			}
		})
	}
}

// TestAttendedArgsConfigReplacesDeclaredDefaults pins decision 2 of ADR-0187:
// [agents.<preset>].attended_args replaces the adapter's declared list wholesale
// rather than appending to it — an empty list launches the bare binary — and
// attended_model is the only way a model reaches an attended command.
func TestAttendedArgsConfigReplacesDeclaredDefaults(t *testing.T) {
	bare := []string{}
	for _, tc := range []struct {
		name  string
		block config.AgentConfig
		want  []string
	}{
		{
			name:  "replaces",
			block: config.AgentConfig{AttendedArgs: &[]string{"--permission-mode", "acceptEdits"}},
			want:  []string{"--permission-mode", "acceptEdits", "assist prompt"},
		},
		{
			name:  "empty list launches bare",
			block: config.AgentConfig{AttendedArgs: &bare},
			want:  []string{"assist prompt"},
		},
		{
			name:  "model only",
			block: config.AgentConfig{AttendedModel: "opus"},
			want:  []string{"--dangerously-skip-permissions", "--model", "opus", "assist prompt"},
		},
		{
			name:  "args and model together",
			block: config.AgentConfig{AttendedArgs: &[]string{"--permission-mode", "plan"}, AttendedModel: "sonnet"},
			want:  []string{"--permission-mode", "plan", "--model", "sonnet", "assist prompt"},
		},
		{
			name:  "unset keeps the declared default and names no model",
			block: config.AgentConfig{},
			want:  []string{"--dangerously-skip-permissions", "assist prompt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Agents: map[string]config.AgentConfig{"claude": tc.block}}
			// The preset arrives as the implement list writes it; only its name may
			// select the [agents.<preset>] block.
			invocation, err := ResolveAgentAssistanceInvocation(cfg, "claude --model from-implement-list", "", "assist prompt", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(invocation.Command.Args, tc.want) {
				t.Fatalf("command args = %#v, want %#v", invocation.Command.Args, tc.want)
			}
		})
	}

	// A block for another preset must not reach this one.
	cfg := &config.Config{Agents: map[string]config.AgentConfig{"cursor": {AttendedModel: "composer-2.5"}}}
	invocation, err := ResolveAgentAssistanceInvocation(cfg, "claude", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--dangerously-skip-permissions", "assist prompt"}; !reflect.DeepEqual(invocation.Command.Args, want) {
		t.Fatalf("command args = %#v, want %#v", invocation.Command.Args, want)
	}
}

// TestAttendedAssistanceLaunchesAutoApproved pins the per-preset auto-approval
// posture of ADR-0187: the flag differs per agent, a preset with none launches
// exactly as it did before, and a custom --agent-cmd gains no attended form.
func TestAttendedAssistanceLaunchesAutoApproved(t *testing.T) {
	for _, tc := range []struct {
		preset string
		want   []string
	}{
		{"claude", []string{"--dangerously-skip-permissions", "assist prompt"}},
		{"cursor", []string{"--force", "--trust", "assist prompt"}},
		{"codex", []string{"--dangerously-bypass-approvals-and-sandbox", "assist prompt"}},
		{"opencode", []string{"assist prompt"}},
		{"pi", []string{"assist prompt"}},
		// kimi takes no positional prompt and declares no flag, so it launches bare.
		{"kimi", nil},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			invocation, err := ResolveAgentAssistanceInvocation(nil, tc.preset, "", "assist prompt", "/tmp/runtime")
			if err != nil {
				t.Fatal(err)
			}
			if len(invocation.Command.Args) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(invocation.Command.Args, tc.want) {
				t.Fatalf("attended args = %#v, want %#v", invocation.Command.Args, tc.want)
			}
		})
	}

	if _, err := (customAgentAdapter{}).AssistanceInvocation(AgentAssistanceRequest{Prompt: "assist prompt"}); err == nil {
		t.Fatal("a custom agent command must report attended assistance unavailable, not gain an attended form")
	}
	if capability := (customAgentAdapter{}).AttendedArgsCapability(); capability.Kind != CapabilityBlind {
		t.Fatalf("custom attended-args capability = %#v, want Blind", capability)
	}
}

func TestAgentCmdIgnoredForAttendedAssistance(t *testing.T) {
	capability, err := ResolveAgentAssistanceCapability("claude", "fake-agent --verbose")
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Available() || capability.Mode != AgentAssistanceNative {
		t.Fatalf("capability = %#v, want native despite --agent-cmd", capability)
	}

	invocation, err := ResolveAgentAssistanceInvocation(nil, "cursor", "fake-agent --verbose", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Command.Name != "cursor-agent" {
		t.Fatalf("command name = %q, want adapter-owned cursor-agent", invocation.Command.Name)
	}
	if strings.Contains(invocation.Display, "fake-agent") || strings.Contains(strings.Join(invocation.Command.Args, " "), "fake-agent") {
		t.Fatalf("--agent-cmd leaked into attended assistance: %#v", invocation)
	}
}

func TestResolveAgentOutputModePrecedence(t *testing.T) {
	loadText := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{
			Presets: map[string]config.TaskAgentConfig{"claude": {Output: "text"}},
		}}, nil
	}
	mode, err := resolveAgentOutputMode(loadText, "claude", "")
	if err != nil || mode != AgentOutputText {
		t.Fatalf("configured mode = %q, err = %v", mode, err)
	}
	mode, err = resolveAgentOutputMode(loadText, "claude", AgentOutputAuto)
	if err != nil || mode != AgentOutputAuto {
		t.Fatalf("override mode = %q, err = %v", mode, err)
	}
	mode, err = resolveAgentOutputMode(loadText, "cursor", "")
	if err != nil || mode != AgentOutputAuto {
		t.Fatalf("other agent mode = %q, err = %v", mode, err)
	}
	mode, err = resolveAgentOutputMode(loadText, "claude --model opus4.8", "")
	if err != nil || mode != AgentOutputText {
		t.Fatalf("augmented preset mode = %q, err = %v", mode, err)
	}
}

func TestResolveAgentOutputModeRejectsInvalidConfig(t *testing.T) {
	loadInvalid := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{
			Presets: map[string]config.TaskAgentConfig{"claude": {Output: "structured-ish"}},
		}}, nil
	}
	_, err := resolveAgentOutputMode(loadInvalid, "claude", "")
	if err == nil || !strings.Contains(err.Error(), "[tasks.presets.claude] output") {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeClaudeStreamJSONExtractsResult(t *testing.T) {
	raw := "{\"type\":\"system\",\"subtype\":\"init\"}\n" +
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"working\"}]}}\n" +
		"{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"SUMMARY_START\\ndone\\nSUMMARY_END\\nTASK_COMPLETE\"}\n"
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict != nil {
		t.Fatalf("unexpected quota pause: %#v", result.ProceedVerdict)
	}
	if !strings.Contains(result.Output, "SUMMARY_START\ndone\nSUMMARY_END\nTASK_COMPLETE") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizeClaudeStreamJSONDetectsQuotaPause(t *testing.T) {
	raw := "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You've hit your weekly limit · resets Mon 12:00am\"}\n"
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("missing quota pause")
	}
	if !strings.Contains(result.ProceedVerdict.Reason, "weekly limit") {
		t.Fatalf("reason = %q", result.ProceedVerdict.Reason)
	}
	var out bytes.Buffer
	RenderAgentOutput(&out, AgentOutputClaudeStreamJSON, raw)
	if strings.Contains(out.String(), "{\"type\"") {
		t.Fatalf("rendered raw JSONL: %q", out.String())
	}
}

func TestClaudeQuotaResetAtParsesCapturedWeeklyLimitString(t *testing.T) {
	loc := time.FixedZone("local", -5*60*60)
	reason := "You've hit your weekly limit · resets Mon 12:00am"
	now := time.Date(2026, 6, 11, 15, 0, 0, 0, loc) // Thu
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, loc) // next Mon
	got := claudeQuotaResetAt(reason, now)
	if !got.Equal(want) {
		t.Fatalf("reset = %s, want %s", got, want)
	}
	if got.Sub(now) <= 24*time.Hour {
		t.Fatalf("weekly reset should remain multi-day, got %s after now", got.Sub(now))
	}
	if viaPreset := agentQuotaResetAt("claude", reason, now); !viaPreset.Equal(want) {
		t.Fatalf("preset reset = %s, want %s", viaPreset, want)
	}
}

func TestClaudeQuotaResetAtParsesBareTimeAndFailures(t *testing.T) {
	loc := time.FixedZone("local", 2*60*60)
	now := time.Date(2026, 6, 15, 23, 0, 0, 0, loc)
	want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if got := claudeQuotaResetAt("You've hit your session limit · resets 12:00am", now); !got.Equal(want) {
		t.Fatalf("bare reset = %s, want %s", got, want)
	}
	for _, reason := range []string{
		"You've hit your weekly limit · reset Mon 12:00am",
		"You've hit your Opus limit · resets 13:00pm",
		"You've hit your session limit · resets noon",
	} {
		if got := claudeQuotaResetAt(reason, now); !got.IsZero() {
			t.Fatalf("reset for %q = %s, want zero", reason, got)
		}
	}
}

// TestNormalizeCodexJSONLDetectsQuotaPause uses the stream captured verbatim
// from a live codex exec --json limit-hit: the turn aborts (exit 1) and emits an
// `error` event plus a `turn.failed` event, both carrying the usage-limit
// message. Detection must fire and preserve the reset time in the reason.
func TestNormalizeCodexJSONLDetectsQuotaPause(t *testing.T) {
	raw := `{"type":"thread.started","thread_id":"t"}
{"type":"turn.started"}
{"type":"error","message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:28 AM."}
{"type":"turn.failed","error":{"message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:28 AM."}}
`
	result := NormalizeAgentOutput(AgentOutputCodexJSONL, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("missing quota pause")
	}
	if !strings.Contains(result.ProceedVerdict.Reason, "usage limit") {
		t.Fatalf("reason = %q", result.ProceedVerdict.Reason)
	}
	if !strings.Contains(result.ProceedVerdict.Reason, "2:28 AM") {
		t.Fatalf("reset time not preserved in reason = %q", result.ProceedVerdict.Reason)
	}
	var out bytes.Buffer
	RenderAgentOutput(&out, AgentOutputCodexJSONL, raw)
	if strings.Contains(out.String(), "{\"type\"") {
		t.Fatalf("rendered raw JSONL: %q", out.String())
	}
}

func TestCodexQuotaResetAtParsesCapturedLimitString(t *testing.T) {
	loc := time.FixedZone("local", -5*60*60)
	reason := "You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:28 AM."
	now := time.Date(2026, 6, 15, 1, 30, 0, 0, loc)
	want := time.Date(2026, 6, 15, 2, 28, 0, 0, loc)
	if got := codexQuotaResetAt(reason, now); !got.Equal(want) {
		t.Fatalf("reset = %s, want %s", got, want)
	}
}

func TestCodexQuotaResetAtNextOccurrenceAndFailures(t *testing.T) {
	loc := time.FixedZone("local", 2*60*60)
	now := time.Date(2026, 6, 15, 3, 0, 0, 0, loc)
	want := time.Date(2026, 6, 16, 2, 28, 0, 0, loc)
	if got := codexQuotaResetAt("try again at 2:28 AM.", now); !got.Equal(want) {
		t.Fatalf("next reset = %s, want %s", got, want)
	}
	for _, reason := range []string{
		"try again later",
		"try again at 13:28 AM.",
		"try again at 2:99 AM.",
	} {
		if got := codexQuotaResetAt(reason, now); !got.IsZero() {
			t.Fatalf("reset for %q = %s, want zero", reason, got)
		}
	}
}

// TestNormalizeCodexJSONLNonLimitErrorIsNotQuotaPause guards against false
// positives: an ordinary codex error or a completed run must not be read as a
// quota pause.
func TestNormalizeCodexJSONLNonLimitErrorIsNotQuotaPause(t *testing.T) {
	for _, raw := range []string{
		`{"type":"turn.failed","error":{"message":"sandbox denied write to /etc/hosts"}}` + "\n",
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n",
	} {
		if pause := NormalizeAgentOutput(AgentOutputCodexJSONL, raw).ProceedVerdict; pause != nil {
			t.Fatalf("unexpected quota pause for %q: %q", raw, pause.Reason)
		}
	}
}

func TestInvocationNormalizesStructuredOutputThroughAdapter(t *testing.T) {
	invocation, err := ResolveAgentInvocation("claude", "", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	raw := "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You've hit your weekly limit · resets Mon 12:00am\"}\n"
	result := invocation.NormalizeOutput(raw)
	if result.ProceedVerdict == nil {
		t.Fatal("missing quota pause")
	}
	if !strings.Contains(result.ProceedVerdict.Reason, "weekly limit") {
		t.Fatalf("reason = %q", result.ProceedVerdict.Reason)
	}
}

func TestNormalizePlainOutputDoesNotDetectClaudeQuotaPause(t *testing.T) {
	raw := "You've hit your weekly limit · resets Mon 12:00am\n"
	result := NormalizeAgentOutput(AgentOutputPlain, raw)
	if result.ProceedVerdict != nil {
		t.Fatalf("plain output detected quota pause: %#v", result.ProceedVerdict)
	}
	if result.Output != raw {
		t.Fatalf("output = %q, want %q", result.Output, raw)
	}
}

func TestNormalizeCursorStreamJSONExtractsResult(t *testing.T) {
	raw := "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"working\"}]}}\n" +
		"{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"SUMMARY_START\\ncursor\\nSUMMARY_END\\nTASK_COMPLETE\"}\n"
	result := NormalizeAgentOutput(AgentOutputCursorStreamJSON, raw)
	if !strings.Contains(result.Output, "SUMMARY_START\ncursor\nSUMMARY_END\nTASK_COMPLETE") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizeCursorStreamJSONDetectsAuthFailure(t *testing.T) {
	authLine := "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."
	raw := authLine + "\n"
	result := NormalizeAgentOutput(AgentOutputCursorStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("missing auth failure unavailability")
	}
	if result.ProceedVerdict.Kind != ProceedAuthFailure {
		t.Fatalf("kind = %q, want %q", result.ProceedVerdict.Kind, ProceedAuthFailure)
	}
	if result.ProceedVerdict.Reason != authLine {
		t.Fatalf("reason = %q, want %q", result.ProceedVerdict.Reason, authLine)
	}
	if _, ok := result.ProceedVerdict.TimeHealing(); ok {
		t.Fatal("auth failure must be human-healing")
	}
}

func TestNormalizeCursorStreamJSONAuthFailureNotDetectedOnOtherFormats(t *testing.T) {
	authLine := "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.\n"
	for _, format := range []AgentOutputFormat{AgentOutputClaudeStreamJSON, AgentOutputCodexJSONL, AgentOutputPlain} {
		if result := NormalizeAgentOutput(format, authLine); result.ProceedVerdict != nil {
			t.Fatalf("format %q detected auth failure: %#v", format, result.ProceedVerdict)
		}
	}
}

func TestNormalizeCodexJSONLExtractsLastAgentMessage(t *testing.T) {
	raw := "{\"type\":\"thread.started\",\"thread_id\":\"1\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"working\"}}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"SUMMARY_START\\ncodex\\nSUMMARY_END\\nTASK_COMPLETE\"}}\n"
	result := NormalizeAgentOutput(AgentOutputCodexJSONL, raw)
	if result.Output != "SUMMARY_START\ncodex\nSUMMARY_END\nTASK_COMPLETE\n" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizeOpenCodeJSONExtractsTextParts(t *testing.T) {
	raw := "{\"type\":\"step_start\",\"sessionID\":\"1\",\"part\":{}}\n" +
		"{\"type\":\"text\",\"sessionID\":\"1\",\"part\":{\"text\":\"SUMMARY_START\\nopencode\\nSUMMARY_END\\nTASK_COMPLETE\"}}\n"
	result := NormalizeAgentOutput(AgentOutputOpenCodeJSON, raw)
	if result.Output != "SUMMARY_START\nopencode\nSUMMARY_END\nTASK_COMPLETE\n" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizePiJSONLExtractsLastAssistantMessage(t *testing.T) {
	raw := "{\"type\":\"session\",\"version\":3}\n" +
		"{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"working\"}]}}\n" +
		"{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"SUMMARY_START\\npi\\nSUMMARY_END\\nTASK_COMPLETE\"}]}}\n"
	result := NormalizeAgentOutput(AgentOutputPiJSONL, raw)
	if result.Output != "SUMMARY_START\npi\nSUMMARY_END\nTASK_COMPLETE\n" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizeStructuredOutputPreservesDiagnosticsWithoutTranscript(t *testing.T) {
	tests := []struct {
		name   string
		format AgentOutputFormat
		raw    string
		want   string
	}{
		{name: "claude", format: AgentOutputClaudeStreamJSON, raw: "claude stderr\n", want: "claude stderr\n"},
		{name: "cursor", format: AgentOutputCursorStreamJSON, raw: "cursor stderr\n", want: "cursor stderr\n"},
		{name: "codex", format: AgentOutputCodexJSONL, raw: "{\"type\":\"error\",\"message\":\"codex failed\"}\n", want: "codex failed\n"},
		{name: "opencode", format: AgentOutputOpenCodeJSON, raw: "{\"type\":\"error\",\"error\":{\"message\":\"opencode failed\"}}\n", want: "opencode failed\n"},
		{name: "pi", format: AgentOutputPiJSONL, raw: "{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"errorMessage\":\"pi failed\"}}\n", want: "pi failed\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeAgentOutput(tt.format, tt.raw)
			if result.Output != tt.want {
				t.Fatalf("output = %q, want %q", result.Output, tt.want)
			}
		})
	}
}

func TestNormalizeStructuredOutputFallsBackToRawForUnknownSchema(t *testing.T) {
	raw := "  {\"type\":\"future_event\",\"payload\":{\"text\":\"opaque\"}}\n"
	result := NormalizeAgentOutput(AgentOutputCodexJSONL, raw)
	if result.Output != raw {
		t.Fatalf("output = %q, want raw %q", result.Output, raw)
	}

	var out bytes.Buffer
	RenderAgentOutput(&out, AgentOutputCodexJSONL, raw)
	if out.String() != raw {
		t.Fatalf("rendered = %q, want raw %q", out.String(), raw)
	}
}

func TestNormalizeStructuredOutputRawFallbackUsesCompletionContract(t *testing.T) {
	raw := "{\"type\":\"future_event\"}\n" +
		"SUMMARY_START\nfallback text\nSUMMARY_END\nTASK_COMPLETE\n"
	result := NormalizeAgentOutput(AgentOutputCodexJSONL, raw)
	if result.Output != raw {
		t.Fatalf("output = %q, want raw %q", result.Output, raw)
	}
	assessment := AssessCompletion(result.Output, []byte("## Acceptance criteria\n\n- [x] ok\n"))
	if !assessment.Complete || assessment.Summary != "fallback text" {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestBuildAgentPromptAbsolutePaths(t *testing.T) {
	prompt := BuildAgentPrompt("/abs/tasks/01-a.md", "/abs/runtime")
	for _, want := range []string{
		"/abs/tasks/01-a.md", "/abs/runtime", "index.json", "Do NOT make git commits", "optional context references",
		"single non-interactive session",
		"later turn",
		"completion sentinel (TASK_COMPLETE or TASK_FAILED) is recorded as a",
		"keep polling it across successive",
		"tool timeout",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Parent PRD") {
		t.Fatalf("prompt must not synthesize a PRD path:\n%s", prompt)
	}
}

func TestBuildHITLAssistancePromptWithCompletedAFKWork(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks/demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-afk.md"), "## AFK\n\n## Acceptance criteria\n\n- [x] done\n")
	writePromptTestFile(t, filepath.Join(dir, "02-hitl.md"), "## Review\n\nCheck the AFK result.\n\n## Acceptance criteria\n\n- [ ] approved\n")
	writePromptTestFile(t, filepath.Join(dir, "progress.txt"), "2026-06-05T10:00:00Z [01-afk.md] DONE\nimplemented storage\nverified tests\n---\n")

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Title: "Build storage", Type: "AFK", Status: "done"},
			{ID: "02-hitl", File: "02-hitl.md", Title: "Review storage", Type: "HITL", Status: "open", BlockedBy: []string{"01-afk"}},
		},
	}

	prompt := BuildHITLAssistancePrompt(DefaultDeps(), "demo", m, m.Tasks[1], "/runtime")
	for _, want := range []string{
		"Task set: demo",
		"Blocking HITL task: 02-hitl - Review storage",
		"Human-facing task path: " + filepath.Join(dir, "02-hitl.md"),
		"Check the AFK result.",
		"- 01-afk [AFK done] Build storage",
		"blocked_by: 01-afk",
		"- 01-afk (01-afk.md, DONE at 2026-06-05T10:00:00Z)",
		"implemented storage",
		"verified tests",
		"complete: the human marks the HITL task done",
		"defer: the human skips the HITL task",
		"edit and rerun",
		"exit without changing task state",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "raw agent transcript") {
		t.Fatalf("prompt should not request raw transcripts:\n%s", prompt)
	}
}

func TestBuildHITLAssistancePromptWithNoCompletedAFKWork(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks/demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-hitl.md"), "## Decide\n\nHuman choice.\n\n## Acceptance criteria\n\n- [ ] decided\n")

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-hitl", File: "01-hitl.md", Title: "Decide", Type: "HITL", Status: "open"},
		},
	}

	prompt := BuildHITLAssistancePrompt(DefaultDeps(), "demo", m, m.Tasks[0], "")
	for _, want := range []string{
		"Task set: demo",
		"Human choice.",
		"No completed AFK work summary is available in progress.txt.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}

func TestBuildHITLAssistancePromptWithUnreadableHITLTaskFile(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		ReadFileFunc: func(path string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	}}
	m := &Manifest{
		Stem: "demo",
		Dir:  "/tasks/demo",
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Title: "Done", Type: "AFK", Status: "done"},
			{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
		},
	}

	prompt := BuildHITLAssistancePrompt(d, "demo", m, m.Tasks[1], "/runtime")
	for _, want := range []string{
		"Human-facing task path: /tasks/demo/02-hitl.md",
		"Could not read /tasks/demo/02-hitl.md",
		"Proceed by inspecting the task path manually",
		"No completed AFK work summary is available in progress.txt.",
		"complete",
		"defer",
		"edit and rerun",
		"exit without changing task state",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}

func TestBuildFailedAssistancePromptIncludesBodyAndFailureReason(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-a.md"),
		"## Build storage\n\nWire up the cache layer.\n\n## Acceptance criteria\n\n- [ ] cache writes\n")

	// A persisted attempt footer is the durable source of the failure reason.
	streamDir := taskStreamDir(dir, "01-a.md")
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)},
		[]streamEventRecord{{Type: "event", AtMS: 5, Raw: `{"type":"system"}`}},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeFailed, DurationMS: 1_000, Reason: "unchecked acceptance criteria", ExitCode: 0})

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "Build storage", Type: "AFK", Status: "failed"},
			{ID: "02-b", File: "02-b.md", Title: "Use storage", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
		},
	}

	prompt := BuildFailedAssistancePrompt(realFSDeps(), "demo", m, m.Tasks[0], "/runtime")
	for _, want := range []string{
		"Task set: demo",
		"Failed task: 01-a - Build storage",
		"Task path: " + filepath.Join(dir, "01-a.md"),
		"Runtime checkout: /runtime",
		"Why the last attempt failed:",
		"unchecked acceptance criteria",
		"Wire up the cache layer.",
		"re-run:",
		"complete by hand:",
		"- 02-b [AFK open] Use storage",
		"blocked_by: 01-a",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
	// The Failed gate offers only re-run and complete; defer is not framed, and
	// the prompt never points the agent at the raw captured stream.
	for _, unwanted := range []string{"defer", "raw", "stream", "transcript"} {
		if strings.Contains(strings.ToLower(prompt), unwanted) {
			t.Fatalf("prompt should not mention %q:\n%s", unwanted, prompt)
		}
	}
}

func TestBuildFailedAssistancePromptWithoutRecordedReason(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-a.md"),
		"## Build storage\n\nDo the work.\n\n## Acceptance criteria\n\n- [ ] done\n")

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "Build storage", Type: "AFK", Status: "failed"},
		},
	}

	prompt := BuildFailedAssistancePrompt(realFSDeps(), "demo", m, m.Tasks[0], "")
	if !strings.Contains(prompt, "no structured failure reason was recorded") {
		t.Fatalf("missing fallback reason line:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Do the work.") {
		t.Fatalf("missing task body:\n%s", prompt)
	}
}

func TestBuildVerifyFailedAssistancePromptIncludesFindingsAndDiffStat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "Build storage", Type: "AFK", Status: "done"},
		},
	}

	git := stubGit("shaHEAD\n", "shaHEAD\nshaEARLY\n", " foo.go | 1 +\n")
	d := realFSDeps()
	d.Git = git

	prompt := BuildVerifyFailedAssistancePrompt(d, "demo", m, "shaHEAD", "the retry looks flaky", "/runtime")
	for _, want := range []string{
		"Task set: demo",
		"Task set path: " + dir,
		"Work SHA: shaHEAD",
		"Runtime checkout: /runtime",
		"Recorded Verifier findings:",
		"the retry looks flaky",
		"Accumulated work diff (at shaHEAD)",
		"Commit range: shaEARLY^..HEAD",
		"foo.go | 1 +",
		"git diff shaEARLY^..HEAD -- <path>",
		"- 01-a [AFK done] Build storage",
		"accept:",
		"remediate:",
		"Re-running the Verifier is not offered",
		"advisory only",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}

func TestBuildVerifyFailedAssistancePromptWithoutFindingsOrDiff(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		},
	}

	d := realFSDeps()
	d.Git = stubGit("", "", "")

	prompt := BuildVerifyFailedAssistancePrompt(d, "demo", m, "", "", "")
	if !strings.Contains(prompt, "none were recorded for this verdict") {
		t.Fatalf("missing findings fallback:\n%s", prompt)
	}
	if !strings.Contains(prompt, "(no committed changes for this set)") {
		t.Fatalf("missing empty diff fallback:\n%s", prompt)
	}
}

func writePromptTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCuratedModelAliasesPerPreset(t *testing.T) {
	want := map[string][]string{
		"claude":   {"opus", "sonnet", "haiku", "fable"},
		"opencode": {"opencode/kimi-k2.6", "opencode/gpt-5.5", "opencode/claude-opus-4-8", "opencode/claude-sonnet-4-6"},
		"cursor":   {"auto", "composer-2.5", "gpt-5.3-codex"},
		"codex":    {"gpt-5.5", "gpt-5.4-mini"},
		"pi":       {"opencode-go/kimi-k2.6", "opencode-go/qwen3.7-max", "opencode-go/minimax-m3", "opencode-go/deepseek-v4-flash"},
		"kimi":     {"moonshot-ai/kimi-k3", "moonshot-ai/kimi-k2.7-code", "moonshot-ai/kimi-k2.7-code-highspeed"},
	}
	for preset, models := range want {
		adapter, err := ResolveAgentAdapter(preset)
		if err != nil {
			t.Fatalf("resolve %s: %v", preset, err)
		}
		if got := adapter.Models(); !reflect.DeepEqual(got, models) {
			t.Fatalf("%s models = %#v, want %#v", preset, got, models)
		}
	}
}

func TestCuratedModelsAreDefensiveCopies(t *testing.T) {
	adapter, err := ResolveAgentAdapter("claude")
	if err != nil {
		t.Fatal(err)
	}
	got := adapter.Models()
	got[0] = "mutated"
	if again := adapter.Models(); again[0] != "opus" {
		t.Fatalf("Models() leaked internal slice; second call got %q", again[0])
	}
}
