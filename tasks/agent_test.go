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
	invocation, err := ResolveAgentAssistanceInvocation("claude", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "claude" || invocation.Mode != AgentAssistanceNative {
		t.Fatalf("invocation = %#v, want claude native", invocation)
	}
	if invocation.Command.Name != "claude" {
		t.Fatalf("command name = %q, want claude", invocation.Command.Name)
	}
	if !reflect.DeepEqual(invocation.Command.Args, []string{"assist prompt"}) {
		t.Fatalf("command args = %#v", invocation.Command.Args)
	}
	if invocation.Display != "claude <HITL assistance prompt>" {
		t.Fatalf("display = %q", invocation.Display)
	}
	if !strings.Contains(invocation.Detail, "native") || strings.Contains(invocation.Detail, "fallback") {
		t.Fatalf("detail = %q, want native detail", invocation.Detail)
	}
}

func TestResolveAgentAssistanceInvocationCursorLaunchesOwnBinary(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation("cursor", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.AgentPreset != "cursor" || invocation.Mode != AgentAssistanceNative {
		t.Fatalf("invocation = %#v, want cursor native", invocation)
	}
	if invocation.Command.Name != "cursor-agent" {
		t.Fatalf("command name = %q, want cursor-agent", invocation.Command.Name)
	}
	if !reflect.DeepEqual(invocation.Command.Args, []string{"assist prompt"}) {
		t.Fatalf("command args = %#v", invocation.Command.Args)
	}
	if invocation.Display != "cursor-agent <HITL assistance prompt>" {
		t.Fatalf("display = %q", invocation.Display)
	}
	if !strings.Contains(invocation.Detail, "native") || strings.Contains(invocation.Detail, "fallback") {
		t.Fatalf("detail = %q, want native detail", invocation.Detail)
	}
}

func TestResolveAgentAssistanceInvocationCarriesExtraArgsNative(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation("claude --model opus4.8", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Mode != AgentAssistanceNative || invocation.Command.Name != "claude" {
		t.Fatalf("invocation = %#v, want claude native", invocation)
	}
	want := []string{"--model", "opus4.8", "assist prompt"}
	if !reflect.DeepEqual(invocation.Command.Args, want) {
		t.Fatalf("command args = %#v, want %#v", invocation.Command.Args, want)
	}
}

func TestResolveAgentAssistanceInvocationCarriesExtraArgsForNonClaudePreset(t *testing.T) {
	invocation, err := ResolveAgentAssistanceInvocation("cursor --model gpt-5", "", "assist prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Mode != AgentAssistanceNative || invocation.Command.Name != "cursor-agent" {
		t.Fatalf("invocation = %#v, want cursor-agent native", invocation)
	}
	want := []string{"--model", "gpt-5", "assist prompt"}
	if !reflect.DeepEqual(invocation.Command.Args, want) {
		t.Fatalf("command args = %#v, want %#v", invocation.Command.Args, want)
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

	invocation, err := ResolveAgentAssistanceInvocation("cursor", "fake-agent --verbose", "assist prompt", "/tmp/runtime")
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
		return &config.Config{Task: &config.TaskConfig{
			Agents: map[string]config.TaskAgentConfig{"claude": {Output: "text"}},
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
		return &config.Config{Task: &config.TaskConfig{
			Agents: map[string]config.TaskAgentConfig{"claude": {Output: "structured-ish"}},
		}}, nil
	}
	_, err := resolveAgentOutputMode(loadInvalid, "claude", "")
	if err == nil || !strings.Contains(err.Error(), "[workload.agents.claude] output") {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeClaudeStreamJSONExtractsResult(t *testing.T) {
	raw := "{\"type\":\"system\",\"subtype\":\"init\"}\n" +
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"working\"}]}}\n" +
		"{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"SUMMARY_START\\ndone\\nSUMMARY_END\\nTASK_COMPLETE\"}\n"
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.QuotaPause != nil {
		t.Fatalf("unexpected quota pause: %#v", result.QuotaPause)
	}
	if !strings.Contains(result.Output, "SUMMARY_START\ndone\nSUMMARY_END\nTASK_COMPLETE") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestNormalizeClaudeStreamJSONDetectsQuotaPause(t *testing.T) {
	raw := "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You've hit your weekly limit · resets Mon 12:00am\"}\n"
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.QuotaPause == nil {
		t.Fatal("missing quota pause")
	}
	if !strings.Contains(result.QuotaPause.Reason, "weekly limit") {
		t.Fatalf("reason = %q", result.QuotaPause.Reason)
	}
	var out bytes.Buffer
	RenderAgentOutput(&out, AgentOutputClaudeStreamJSON, raw)
	if strings.Contains(out.String(), "{\"type\"") {
		t.Fatalf("rendered raw JSONL: %q", out.String())
	}
}

func TestInvocationNormalizesStructuredOutputThroughAdapter(t *testing.T) {
	invocation, err := ResolveAgentInvocation("claude", "", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	raw := "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You've hit your weekly limit · resets Mon 12:00am\"}\n"
	result := invocation.NormalizeOutput(raw)
	if result.QuotaPause == nil {
		t.Fatal("missing quota pause")
	}
	if !strings.Contains(result.QuotaPause.Reason, "weekly limit") {
		t.Fatalf("reason = %q", result.QuotaPause.Reason)
	}
}

func TestNormalizePlainOutputDoesNotDetectClaudeQuotaPause(t *testing.T) {
	raw := "You've hit your weekly limit · resets Mon 12:00am\n"
	result := NormalizeAgentOutput(AgentOutputPlain, raw)
	if result.QuotaPause != nil {
		t.Fatalf("plain output detected quota pause: %#v", result.QuotaPause)
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

func writePromptTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTaskAgentSpecPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		cliPreset     string
		defaultPreset string
		agentExplicit bool
		agentCmd      string
		taskAgent     string
		want          string
	}{
		{
			name:      "task key wins over bare defaulted agent",
			cliPreset: "claude", agentExplicit: false,
			taskAgent: "codex --model gpt-5-codex",
			want:      "codex --model gpt-5-codex",
		},
		{
			name:          "task key wins over supplied default agent",
			cliPreset:     "claude",
			defaultPreset: "opencode",
			agentExplicit: false,
			taskAgent:     "codex --model gpt-5-codex",
			want:          "codex --model gpt-5-codex",
		},
		{
			name:          "supplied default agent wins for unpinned task",
			cliPreset:     "claude",
			defaultPreset: "opencode",
			agentExplicit: false,
			taskAgent:     "",
			want:          "opencode",
		},
		{
			name:          "explicit agent wins over supplied default agent",
			cliPreset:     "codex",
			defaultPreset: "opencode",
			agentExplicit: true,
			taskAgent:     "",
			want:          "codex",
		},
		{
			name:      "explicit agent wins over task key",
			cliPreset: "claude", agentExplicit: true,
			taskAgent: "codex --model gpt-5-codex",
			want:      "claude",
		},
		{
			name:      "explicit bare claude still wins over task key",
			cliPreset: "claude", agentExplicit: true,
			taskAgent: "opencode",
			want:      "claude",
		},
		{
			name:      "agent-cmd wins over task key without explicit agent",
			cliPreset: "claude", agentExplicit: false,
			agentCmd:  "./my-agent.sh",
			taskAgent: "codex",
			want:      "claude",
		},
		{
			name:      "agent-cmd wins over explicit agent and task key",
			cliPreset: "opencode", agentExplicit: true,
			agentCmd:  "./my-agent.sh",
			taskAgent: "codex",
			want:      "opencode",
		},
		{
			name:      "no task key falls through to resolved CLI agent",
			cliPreset: "claude", agentExplicit: false,
			taskAgent: "",
			want:      "claude",
		},
		{
			name:      "no task key with explicit augmented agent",
			cliPreset: "claude --model opus4.8", agentExplicit: true,
			taskAgent: "",
			want:      "claude --model opus4.8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskAgentSpec(tt.cliPreset, tt.defaultPreset, tt.agentExplicit, tt.agentCmd, tt.taskAgent)
			if got != tt.want {
				t.Fatalf("resolveTaskAgentSpec = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateManifestAgentSpec(t *testing.T) {
	if err := validateManifestAgentSpec("claude --model opus4.8"); err != nil {
		t.Fatalf("recognized preset rejected: %v", err)
	}
	if err := validateManifestAgentSpec("codex"); err != nil {
		t.Fatalf("bare preset rejected: %v", err)
	}
	if err := validateManifestAgentSpec("./run-agent.sh --opaque"); err == nil || !strings.Contains(err.Error(), "unknown agent preset") {
		t.Fatalf("opaque command not rejected: %v", err)
	}
	if err := validateManifestAgentSpec("claude 'unterminated"); err == nil || !strings.Contains(err.Error(), "invalid agent value") {
		t.Fatalf("unterminated quote not rejected: %v", err)
	}
	if err := validateManifestAgentSpec(" \t"); err == nil || !strings.Contains(err.Error(), "empty agent value") {
		t.Fatalf("whitespace-only value not rejected: %v", err)
	}
}

func TestCuratedModelAliasesPerPreset(t *testing.T) {
	want := map[string][]string{
		"claude":   {"opus", "sonnet", "haiku", "fable"},
		"opencode": {"opencode/kimi-k2.6", "opencode/gpt-5.5", "opencode/claude-opus-4-8", "opencode/claude-sonnet-4-6"},
		"cursor":   {"auto", "composer-2.5", "gpt-5.3-codex"},
		"codex":    {"gpt-5.5", "gpt-5.4-mini"},
		"pi":       {"opencode-go/kimi-k2.6", "opencode-go/qwen3.7-max", "opencode-go/minimax-m3", "opencode-go/deepseek-v4-flash"},
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
