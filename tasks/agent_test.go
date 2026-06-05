package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

func TestAgentAssistanceCapabilitySupportsFallbacks(t *testing.T) {
	capability, err := ResolveAgentAssistanceCapability("cursor", "")
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Available() || capability.Mode != AgentAssistanceFallback {
		t.Fatalf("capability = %#v, want available fallback", capability)
	}
	if capability.Command == nil || capability.Command.Name == "" {
		t.Fatalf("fallback command = %#v", capability.Command)
	}
}

func TestCustomAgentCmdHasNoAssistanceCapability(t *testing.T) {
	capability, err := ResolveAgentAssistanceCapability("claude", "fake-agent --verbose")
	if err != nil {
		t.Fatal(err)
	}
	if capability.Available() || capability.Mode != AgentAssistanceUnavailable {
		t.Fatalf("capability = %#v, want unavailable", capability)
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
	for _, want := range []string{"/abs/tasks/01-a.md", "/abs/runtime", "index.json", "Do NOT make git commits", "optional context references"} {
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
	dir := filepath.Join(root, "thoughts/issues/demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-afk.md"), "## AFK\n\n## Acceptance criteria\n\n- [x] done\n")
	writePromptTestFile(t, filepath.Join(dir, "02-hitl.md"), "## Review\n\nCheck the AFK result.\n\n## Acceptance criteria\n\n- [ ] approved\n")
	writePromptTestFile(t, filepath.Join(dir, "progress.txt"), "2026-06-05T10:00:00Z [01-afk.md] DONE\nimplemented storage\nverified tests\n---\n")

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Issues: []Issue{
			{ID: "01-afk", File: "01-afk.md", Title: "Build storage", Type: "AFK", Status: "done"},
			{ID: "02-hitl", File: "02-hitl.md", Title: "Review storage", Type: "HITL", Status: "open", BlockedBy: []string{"01-afk"}},
		},
	}

	prompt := BuildHITLAssistancePrompt(DefaultDeps(), "demo", m, m.Issues[1], "/runtime")
	for _, want := range []string{
		"Issue set: demo",
		"Blocking HITL issue: 02-hitl - Review storage",
		"Human-facing issue path: " + filepath.Join(dir, "02-hitl.md"),
		"Check the AFK result.",
		"- 01-afk [AFK done] Build storage",
		"blocked_by: 01-afk",
		"- 01-afk (01-afk.md, DONE at 2026-06-05T10:00:00Z)",
		"implemented storage",
		"verified tests",
		"complete: the human marks the HITL issue done",
		"defer: the human skips the HITL issue",
		"edit and rerun",
		"exit without changing issue state",
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
	dir := filepath.Join(root, "thoughts/issues/demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptTestFile(t, filepath.Join(dir, "01-hitl.md"), "## Decide\n\nHuman choice.\n\n## Acceptance criteria\n\n- [ ] decided\n")

	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Issues: []Issue{
			{ID: "01-hitl", File: "01-hitl.md", Title: "Decide", Type: "HITL", Status: "open"},
		},
	}

	prompt := BuildHITLAssistancePrompt(DefaultDeps(), "demo", m, m.Issues[0], "")
	for _, want := range []string{
		"Issue set: demo",
		"Human choice.",
		"No completed AFK work summary is available in progress.txt.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}

func TestBuildHITLAssistancePromptWithUnreadableHITLIssueFile(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		ReadFileFunc: func(path string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	}}
	m := &Manifest{
		Stem: "demo",
		Dir:  "/issues/demo",
		Issues: []Issue{
			{ID: "01-afk", File: "01-afk.md", Title: "Done", Type: "AFK", Status: "done"},
			{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
		},
	}

	prompt := BuildHITLAssistancePrompt(d, "demo", m, m.Issues[1], "/runtime")
	for _, want := range []string{
		"Human-facing issue path: /issues/demo/02-hitl.md",
		"Could not read /issues/demo/02-hitl.md",
		"Proceed by inspecting the issue path manually",
		"No completed AFK work summary is available in progress.txt.",
		"complete",
		"defer",
		"edit and rerun",
		"exit without changing issue state",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}

func writePromptTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
