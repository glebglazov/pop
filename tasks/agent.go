package tasks

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
)

// AgentOutputFormat controls how a preset's output is normalized.
type AgentOutputFormat string

const (
	AgentOutputPlain            AgentOutputFormat = "plain"
	AgentOutputClaudeStreamJSON AgentOutputFormat = "claude-stream-json"
	AgentOutputCursorStreamJSON AgentOutputFormat = "cursor-stream-json"
	AgentOutputCodexJSONL       AgentOutputFormat = "codex-jsonl"
	AgentOutputOpenCodeJSON     AgentOutputFormat = "opencode-json"
	AgentOutputPiJSONL          AgentOutputFormat = "pi-jsonl"

	AgentOutputAuto AgentOutputMode = "auto"
	AgentOutputText AgentOutputMode = "text"
)

// AgentOutputMode controls whether presets use adapter defaults or plain text.
type AgentOutputMode string

// Set validates and assigns an agent-output mode for Cobra flag parsing.
func (m *AgentOutputMode) Set(value string) error {
	switch AgentOutputMode(value) {
	case AgentOutputAuto, AgentOutputText:
		*m = AgentOutputMode(value)
		return nil
	default:
		return fmt.Errorf("invalid agent-output mode %q; valid candidates: %s", value, strings.Join(ValidAgentOutputModes(), ", "))
	}
}

func (m AgentOutputMode) String() string { return string(m) }

func (m AgentOutputMode) Type() string { return "agent-output-mode" }

// ValidAgentOutputModes returns the accepted --agent-output values.
func ValidAgentOutputModes() []string {
	return []string{string(AgentOutputAuto), string(AgentOutputText)}
}

// AgentInvocation is one resolved headless-agent command.
type AgentInvocation struct {
	Name         string
	Args         []string
	OutputFormat AgentOutputFormat
	adapter      AgentAdapter
}

// AgentResult is the provider-neutral result of normalizing one invocation.
type AgentResult struct {
	Output     string
	QuotaPause *AgentQuotaPause
}

// AgentQuotaPause reports that execution stopped because the agent allowance ran out.
type AgentQuotaPause struct {
	Reason string
}

// AgentHeadlessRequest describes one unattended issue-attempt invocation.
type AgentHeadlessRequest struct {
	Prompt      string
	RuntimePath string
	OutputMode  AgentOutputMode
}

// AgentAssistanceMode describes how an adapter can offer attended HITL help.
type AgentAssistanceMode string

const (
	AgentAssistanceUnavailable AgentAssistanceMode = "unavailable"
	AgentAssistanceNative      AgentAssistanceMode = "native"
	AgentAssistanceFallback    AgentAssistanceMode = "fallback"
)

// AgentCommand is a resolved attended command owned by an Agent adapter.
type AgentCommand struct {
	Name string
	Args []string
}

// AgentAssistanceCapability reports whether attended assistance can be offered.
// Fallback allows an adapter to support HITL help even when the selected agent
// does not have a native interactive command.
type AgentAssistanceCapability struct {
	Mode    AgentAssistanceMode
	Command *AgentCommand
}

// Available reports whether this capability can be offered to a human.
func (c AgentAssistanceCapability) Available() bool {
	return c.Mode == AgentAssistanceNative || c.Mode == AgentAssistanceFallback
}

// AgentAdapter owns one agent preset's headless command, output handling, and
// attended-assistance support decision.
type AgentAdapter interface {
	Preset() string
	HeadlessInvocation(AgentHeadlessRequest) (*AgentInvocation, error)
	NormalizeOutput(raw string, format AgentOutputFormat) AgentResult
	RenderOutput(w io.Writer, raw string, format AgentOutputFormat)
	AssistanceCapability() AgentAssistanceCapability
}

// Agent adapters map preset names to per-agent behavior.
var agentAdapters = map[string]AgentAdapter{
	"claude": newPresetAgentAdapter("claude",
		[]string{"claude", "--dangerously-skip-permissions", "-p"},
		AgentOutputClaudeStreamJSON,
		[]string{"--output-format", "stream-json", "--verbose"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "claude"}},
	),
	"opencode": newPresetAgentAdapter("opencode",
		[]string{"opencode", "run"},
		AgentOutputOpenCodeJSON,
		[]string{"--format", "json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "opencode"}},
	),
	"cursor": newPresetAgentAdapter("cursor",
		[]string{"cursor-agent", "-p", "--force", "--trust"},
		AgentOutputCursorStreamJSON,
		[]string{"--output-format", "stream-json"},
		AgentAssistanceCapability{Mode: AgentAssistanceFallback, Command: &AgentCommand{Name: "claude"}},
	),
	"codex": newPresetAgentAdapter("codex",
		[]string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
		AgentOutputCodexJSONL,
		[]string{"--json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "codex"}},
	),
	"pi": newPresetAgentAdapter("pi",
		[]string{"pi", "-p", "--no-extensions", "--no-skills"},
		AgentOutputPiJSONL,
		[]string{"--mode", "json"},
		AgentAssistanceCapability{Mode: AgentAssistanceFallback, Command: &AgentCommand{Name: "claude"}},
	),
}

type presetAgentAdapter struct {
	preset         string
	headlessPrefix []string
	autoFormat     AgentOutputFormat
	autoArgs       []string
	assistance     AgentAssistanceCapability
}

func newPresetAgentAdapter(preset string, headlessPrefix []string, autoFormat AgentOutputFormat, autoArgs []string, assistance AgentAssistanceCapability) AgentAdapter {
	return &presetAgentAdapter{
		preset:         preset,
		headlessPrefix: append([]string{}, headlessPrefix...),
		autoFormat:     autoFormat,
		autoArgs:       append([]string{}, autoArgs...),
		assistance:     assistance,
	}
}

func (a *presetAgentAdapter) Preset() string { return a.preset }

func (a *presetAgentAdapter) HeadlessInvocation(req AgentHeadlessRequest) (*AgentInvocation, error) {
	if err := validateAgentOutputMode(req.OutputMode); err != nil {
		return nil, err
	}
	mode := req.OutputMode
	if mode == "" {
		mode = AgentOutputAuto
	}
	args := append([]string{}, a.headlessPrefix...)
	format := AgentOutputPlain
	if mode == AgentOutputAuto {
		args = append(args, a.autoArgs...)
		format = a.autoFormat
	}
	if a.preset == "cursor" {
		if mode == AgentOutputText {
			args = append(args, "--output-format", "text")
		}
		args = append(args, "--workspace", req.RuntimePath)
	}
	args = append(args, req.Prompt)
	return &AgentInvocation{Name: args[0], Args: args[1:], OutputFormat: format, adapter: a}, nil
}

func (a *presetAgentAdapter) NormalizeOutput(raw string, format AgentOutputFormat) AgentResult {
	return normalizeAgentOutput(format, raw)
}

func (a *presetAgentAdapter) RenderOutput(w io.Writer, raw string, format AgentOutputFormat) {
	renderAgentOutput(w, format, raw)
}

func (a *presetAgentAdapter) AssistanceCapability() AgentAssistanceCapability {
	return cloneAssistanceCapability(a.assistance)
}

type customAgentAdapter struct{}

func (customAgentAdapter) Preset() string { return "custom" }

func (a customAgentAdapter) HeadlessInvocation(req AgentHeadlessRequest) (*AgentInvocation, error) {
	return nil, fmt.Errorf("custom agent adapter requires ResolveCustomAgentInvocation")
}

func (a customAgentAdapter) NormalizeOutput(raw string, format AgentOutputFormat) AgentResult {
	return normalizeAgentOutput(format, raw)
}

func (a customAgentAdapter) RenderOutput(w io.Writer, raw string, format AgentOutputFormat) {
	renderAgentOutput(w, format, raw)
}

func (a customAgentAdapter) AssistanceCapability() AgentAssistanceCapability {
	return AgentAssistanceCapability{Mode: AgentAssistanceUnavailable}
}

func cloneAssistanceCapability(capability AgentAssistanceCapability) AgentAssistanceCapability {
	if capability.Command == nil {
		return capability
	}
	clone := *capability.Command
	clone.Args = append([]string{}, capability.Command.Args...)
	capability.Command = &clone
	return capability
}

// ValidAgentPresets returns sorted preset names.
func ValidAgentPresets() []string {
	names := make([]string, 0, len(agentAdapters))
	for name := range agentAdapters {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// ResolveAgentCommand returns the executable name and args for an agent invocation.
func ResolveAgentCommand(preset, agentCmd, prompt, runtimePath string) (name string, args []string, err error) {
	invocation, err := ResolveAgentInvocation(preset, agentCmd, prompt, runtimePath)
	if err != nil {
		return "", nil, err
	}
	return invocation.Name, invocation.Args, nil
}

// ResolveAgentInvocation returns an agent command together with its output protocol.
func ResolveAgentInvocation(preset, agentCmd, prompt, runtimePath string) (*AgentInvocation, error) {
	return ResolveAgentInvocationWithMode(preset, agentCmd, prompt, runtimePath, AgentOutputAuto)
}

// ResolveAgentInvocationWithMode applies an explicit output-mode override.
func ResolveAgentInvocationWithMode(preset, agentCmd, prompt, runtimePath string, mode AgentOutputMode) (*AgentInvocation, error) {
	if err := validateAgentOutputMode(mode); err != nil {
		return nil, err
	}
	if mode == "" {
		mode = AgentOutputAuto
	}
	if agentCmd != "" {
		adapter := customAgentAdapter{}
		return &AgentInvocation{
			Name:         "sh",
			Args:         []string{"-c", agentCmd + ` "$@"`, "task-agent", prompt},
			OutputFormat: AgentOutputPlain,
			adapter:      adapter,
		}, nil
	}
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return nil, err
	}
	return adapter.HeadlessInvocation(AgentHeadlessRequest{
		Prompt:      prompt,
		RuntimePath: runtimePath,
		OutputMode:  mode,
	})
}

// ResolveAgentAdapter returns the adapter for a named preset.
func ResolveAgentAdapter(preset string) (AgentAdapter, error) {
	if preset == "" {
		preset = "claude"
	}
	adapter, ok := agentAdapters[preset]
	if !ok {
		return nil, fmt.Errorf("unknown agent preset %q; valid: %s", preset, strings.Join(ValidAgentPresets(), ", "))
	}
	return adapter, nil
}

// ResolveAgentAssistanceCapability returns attended-assistance support for the selected agent.
// Custom headless commands are intentionally not treated as attended commands.
func ResolveAgentAssistanceCapability(preset, agentCmd string) (AgentAssistanceCapability, error) {
	if agentCmd != "" {
		return AgentAssistanceCapability{Mode: AgentAssistanceUnavailable}, nil
	}
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return AgentAssistanceCapability{}, err
	}
	return adapter.AssistanceCapability(), nil
}

func validateAgentOutputMode(mode AgentOutputMode) error {
	switch mode {
	case "", AgentOutputAuto, AgentOutputText:
		return nil
	default:
		return fmt.Errorf("invalid agent-output mode %q; valid candidates: %s", mode, strings.Join(ValidAgentOutputModes(), ", "))
	}
}

func resolveAgentOutputMode(loadConfig func(string) (*config.Config, error), preset string, override AgentOutputMode) (AgentOutputMode, error) {
	if override != "" {
		if err := validateAgentOutputMode(override); err != nil {
			return "", err
		}
		return override, nil
	}
	if loadConfig == nil {
		return AgentOutputAuto, nil
	}
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return AgentOutputAuto, nil
		}
		return "", fmt.Errorf("load config: %w", err)
	}
	if preset == "" {
		preset = "claude"
	}
	mode := AgentOutputMode(cfg.TaskAgentOutput(preset))
	if err := validateAgentOutputMode(mode); err != nil {
		return "", fmt.Errorf("[workload.agents.%s] output: %w", preset, err)
	}
	return mode, nil
}

// NormalizeAgentOutput converts provider output into the completion-contract text.
func NormalizeAgentOutput(format AgentOutputFormat, raw string) AgentResult {
	return normalizeAgentOutput(format, raw)
}

func normalizeAgentOutput(format AgentOutputFormat, raw string) AgentResult {
	var result AgentResult
	switch format {
	case AgentOutputClaudeStreamJSON:
		result = normalizeClaudeStreamJSON(raw)
	case AgentOutputCursorStreamJSON:
		result = normalizeCursorStreamJSON(raw)
	case AgentOutputCodexJSONL:
		result = normalizeCodexJSONL(raw)
	case AgentOutputOpenCodeJSON:
		result = normalizeOpenCodeJSON(raw)
	case AgentOutputPiJSONL:
		result = normalizePiJSONL(raw)
	default:
		return AgentResult{Output: raw}
	}
	if result.Output == "" && result.QuotaPause == nil {
		result.Output = raw
	}
	return result
}

// NormalizeOutput converts this invocation's raw output into completion-contract text.
func (i *AgentInvocation) NormalizeOutput(raw string) AgentResult {
	if i != nil && i.adapter != nil {
		return i.adapter.NormalizeOutput(raw, i.OutputFormat)
	}
	if i == nil {
		return AgentResult{}
	}
	return normalizeAgentOutput(i.OutputFormat, raw)
}

// RenderAgentOutput writes normalized agent text without dumping structured events.
func RenderAgentOutput(w io.Writer, format AgentOutputFormat, raw string) {
	renderAgentOutput(w, format, raw)
}

func renderAgentOutput(w io.Writer, format AgentOutputFormat, raw string) {
	if format == AgentOutputPlain {
		_, _ = io.Copy(w, bytes.NewBufferString(raw))
		return
	}
	normalized := normalizeAgentOutput(format, raw)
	if normalized.QuotaPause != nil {
		fmt.Fprintln(w, normalized.QuotaPause.Reason)
		return
	}
	if normalized.Output != "" {
		fmt.Fprint(w, normalized.Output)
	}
}

// RenderOutput writes this invocation's normalized agent text.
func (i *AgentInvocation) RenderOutput(w io.Writer, raw string) {
	if i != nil && i.adapter != nil {
		i.adapter.RenderOutput(w, raw, i.OutputFormat)
		return
	}
	if i != nil {
		renderAgentOutput(w, i.OutputFormat, raw)
	}
}
