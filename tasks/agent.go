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
	Name           string
	Args           []string
	OutputFormat   AgentOutputFormat
	RequestedAgent string
	adapter        AgentAdapter
}

// AgentPreset returns the owning adapter's preset name.
func (i *AgentInvocation) AgentPreset() string {
	if i == nil || i.adapter == nil {
		return ""
	}
	return i.adapter.Preset()
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
	// ExtraArgs are user-supplied arguments augmenting the preset (ADR-0017).
	// They precede pop's owned flags so owned flags stay authoritative.
	ExtraArgs []string
}

// AgentAssistanceRequest describes one attended HITL assistance invocation.
type AgentAssistanceRequest struct {
	Prompt      string
	RuntimePath string
	// ExtraArgs ride into attended assistance for the selected agent (ADR-0017).
	ExtraArgs []string
}

// AgentAssistanceMode describes how an adapter can offer attended HITL help.
type AgentAssistanceMode string

const (
	AgentAssistanceUnavailable AgentAssistanceMode = "unavailable"
	AgentAssistanceNative      AgentAssistanceMode = "native"
)

// AgentCommand is a resolved attended command owned by an Agent adapter.
type AgentCommand struct {
	Name string
	Args []string
}

// AgentAssistanceInvocation is a resolved attended command and human-facing
// command detail for a HITL assistance action.
type AgentAssistanceInvocation struct {
	AgentPreset string
	Mode        AgentAssistanceMode
	Command     AgentCommand
	Display     string
	Detail      string
}

// AgentAssistanceCapability reports whether attended assistance can be offered.
// Every supported preset launches its own interactive binary; an adapter reports
// Unavailable only when it has no usable attended command at all.
type AgentAssistanceCapability struct {
	Mode    AgentAssistanceMode
	Command *AgentCommand
}

// Available reports whether this capability can be offered to a human.
func (c AgentAssistanceCapability) Available() bool {
	return c.Mode == AgentAssistanceNative
}

// AgentAdapter owns one agent preset's headless command, output handling, and
// attended-assistance support decision.
type AgentAdapter interface {
	Preset() string
	HeadlessInvocation(AgentHeadlessRequest) (*AgentInvocation, error)
	NormalizeOutput(raw string, format AgentOutputFormat) AgentResult
	RenderOutput(w io.Writer, raw string, format AgentOutputFormat)
	AssistanceCapability() AgentAssistanceCapability
	AssistanceInvocation(AgentAssistanceRequest) (*AgentAssistanceInvocation, error)
	// Models returns the preset's curated, recommended-first model aliases that
	// Pop ships for display. Advisory only; never a validation gate (ADR-0019).
	Models() []string
}

// Agent adapters map preset names to per-agent behavior.
var agentAdapters = map[string]AgentAdapter{
	"claude": newPresetAgentAdapter("claude",
		[]string{"claude", "--dangerously-skip-permissions", "-p"},
		AgentOutputClaudeStreamJSON,
		[]string{"--output-format", "stream-json", "--verbose"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "claude"}},
		[]string{"opus", "sonnet", "haiku", "fable"},
	),
	"opencode": newPresetAgentAdapter("opencode",
		[]string{"opencode", "run"},
		AgentOutputOpenCodeJSON,
		[]string{"--format", "json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "opencode"}},
		[]string{"opencode/kimi-k2.6", "opencode/gpt-5.5", "opencode/claude-opus-4-8", "opencode/claude-sonnet-4-6"},
	),
	"cursor": newPresetAgentAdapter("cursor",
		[]string{"cursor-agent", "-p", "--force", "--trust"},
		AgentOutputCursorStreamJSON,
		[]string{"--output-format", "stream-json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "cursor-agent"}},
		[]string{"auto", "composer-2.5", "gpt-5.3-codex"},
	),
	"codex": newPresetAgentAdapter("codex",
		[]string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
		AgentOutputCodexJSONL,
		[]string{"--json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "codex"}},
		[]string{"gpt-5.5", "gpt-5.4-mini"},
	),
	"pi": newPresetAgentAdapter("pi",
		[]string{"pi", "-p", "--no-extensions", "--no-skills"},
		AgentOutputPiJSONL,
		[]string{"--mode", "json"},
		AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "pi"}},
		[]string{"opencode-go/kimi-k2.6", "opencode-go/qwen3.7-max", "opencode-go/minimax-m3", "opencode-go/deepseek-v4-flash"},
	),
}

type presetAgentAdapter struct {
	preset         string
	headlessPrefix []string
	autoFormat     AgentOutputFormat
	autoArgs       []string
	assistance     AgentAssistanceCapability
	models         []string
}

func newPresetAgentAdapter(preset string, headlessPrefix []string, autoFormat AgentOutputFormat, autoArgs []string, assistance AgentAssistanceCapability, models []string) AgentAdapter {
	return &presetAgentAdapter{
		preset:         preset,
		headlessPrefix: append([]string{}, headlessPrefix...),
		autoFormat:     autoFormat,
		autoArgs:       append([]string{}, autoArgs...),
		assistance:     assistance,
		models:         append([]string{}, models...),
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
	args := []string{a.headlessPrefix[0]}
	args = append(args, req.ExtraArgs...)
	args = append(args, a.headlessPrefix[1:]...)
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

func (a *presetAgentAdapter) Models() []string {
	return append([]string{}, a.models...)
}

func (a *presetAgentAdapter) AssistanceInvocation(req AgentAssistanceRequest) (*AgentAssistanceInvocation, error) {
	capability := a.AssistanceCapability()
	if !capability.Available() || capability.Command == nil || capability.Command.Name == "" {
		return nil, fmt.Errorf("agent preset %q does not support attended assistance", a.preset)
	}
	command := *capability.Command
	command.Args = []string{}
	command.Args = append(command.Args, req.ExtraArgs...)
	command.Args = append(command.Args, capability.Command.Args...)
	if req.Prompt != "" {
		command.Args = append(command.Args, req.Prompt)
	}
	invocation := &AgentAssistanceInvocation{
		AgentPreset: a.preset,
		Mode:        capability.Mode,
		Command:     command,
		Display:     displayAgentCommand(command, req.Prompt),
		Detail:      fmt.Sprintf("using %s native attended assistance", a.preset),
	}
	return invocation, nil
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

func (a customAgentAdapter) AssistanceInvocation(req AgentAssistanceRequest) (*AgentAssistanceInvocation, error) {
	return nil, fmt.Errorf("custom agent adapter does not support attended assistance")
}

func (a customAgentAdapter) Models() []string { return nil }

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

// presetAutoFormat returns the auto-mode output format for a preset, used to
// pick the line renderer that turns a stored stream's events back into the
// narrative they rendered live. An unknown preset (custom or absent) has no
// structured format, so its raw lines are the narrative as-is.
func presetAutoFormat(preset string) AgentOutputFormat {
	if a, ok := agentAdapters[preset].(*presetAgentAdapter); ok {
		return a.autoFormat
	}
	return AgentOutputPlain
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
			Name:           "sh",
			Args:           []string{"-c", agentCmd + ` "$@"`, "task-agent", prompt},
			OutputFormat:   AgentOutputPlain,
			RequestedAgent: requestedAgentSpec(preset, adapter.Preset()),
			adapter:        adapter,
		}, nil
	}
	_, extraArgs, err := parseAgentPresetSpec(preset)
	if err != nil {
		return nil, err
	}
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return nil, err
	}
	invocation, err := adapter.HeadlessInvocation(AgentHeadlessRequest{
		Prompt:      prompt,
		RuntimePath: runtimePath,
		OutputMode:  mode,
		ExtraArgs:   extraArgs,
	})
	if err != nil {
		return nil, err
	}
	invocation.RequestedAgent = requestedAgentSpec(preset, invocation.AgentPreset())
	return invocation, nil
}

func requestedAgentSpec(spec, fallback string) string {
	if strings.TrimSpace(spec) != "" {
		return spec
	}
	return fallback
}

// ResolveAgentAdapter returns the adapter for an --agent value. The value may
// carry extra invocation arguments after the preset name (ADR-0017); only the
// first token selects the adapter.
func ResolveAgentAdapter(preset string) (AgentAdapter, error) {
	name, _, err := parseAgentPresetSpec(preset)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = DefaultAgentPreset
	}
	adapter, ok := agentAdapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent preset %q; valid: %s", name, strings.Join(ValidAgentPresets(), ", "))
	}
	return adapter, nil
}

// AgentPresetName returns the normalized preset token from an Agent-preset-shaped
// value. It is exported for queue supervision, which stores cooldowns by
// subscription-level preset rather than by augmented CLI spec.
func AgentPresetName(spec string) (string, error) {
	name, _, err := parseAgentPresetSpec(spec)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = DefaultAgentPreset
	}
	return name, nil
}

func resolveDefaultAgentPreset(cliPreset, defaultPreset string, agentExplicit bool) string {
	if !agentExplicit && strings.TrimSpace(defaultPreset) != "" {
		return defaultPreset
	}
	return cliPreset
}

func taskPinMatchesPreset(taskAgent, preset string) bool {
	if strings.TrimSpace(taskAgent) == "" || strings.TrimSpace(preset) == "" {
		return false
	}
	name, err := AgentPresetName(taskAgent)
	return err == nil && name == preset
}

// resolveTaskAgentSpec applies ADR-0018 precedence for one task attempt:
// explicit --agent-cmd > explicitly-passed --agent > the task's `agent` key >
// a non-overriding default agent > the built-in default agent. agentExplicit
// must come from
// Flags().Changed("agent"), not the resolved value, so a bare defaulted
// --agent claude never stomps a planner's per-task choice.
func resolveTaskAgentSpec(cliPreset, defaultPreset string, agentExplicit bool, agentCmd, taskAgent string) string {
	if agentCmd != "" || agentExplicit || taskAgent == "" {
		if agentCmd == "" && !agentExplicit && strings.TrimSpace(defaultPreset) != "" {
			return defaultPreset
		}
		return cliPreset
	}
	return taskAgent
}

// validateManifestAgentSpec checks a Manifest `agent` key names a recognized
// Agent preset (ADR-0018). Opaque agent commands are not permitted: anything
// whose first token is not a known preset is a contract fault, surfaced as a
// Malformed Task set at discovery rather than mid-attempt. Extra args after
// the preset stay opaque/unvalidated, exactly as on the CLI.
func validateManifestAgentSpec(spec string) error {
	name, _, err := parseAgentPresetSpec(spec)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("empty agent value")
	}
	if _, ok := agentAdapters[name]; !ok {
		return fmt.Errorf("unknown agent preset %q in agent key (opaque agent commands are not permitted in a manifest); valid: %s", name, strings.Join(ValidAgentPresets(), ", "))
	}
	return nil
}

// parseAgentPresetSpec splits an --agent value into the preset name (first
// token) and the extra invocation arguments that augment it.
func parseAgentPresetSpec(spec string) (string, []string, error) {
	tokens, err := splitCommandWords(spec)
	if err != nil {
		return "", nil, fmt.Errorf("invalid agent value %q: %v", spec, err)
	}
	if len(tokens) == 0 {
		return "", nil, nil
	}
	return tokens[0], tokens[1:], nil
}

// splitCommandWords tokenizes on whitespace, honoring single and double
// quotes so a quoted argument survives as one token.
func splitCommandWords(s string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"':
			inWord = true
			i++
			for ; i < len(s) && s[i] != c; i++ {
				current.WriteByte(s[i])
			}
			if i == len(s) {
				return nil, fmt.Errorf("unterminated %c quote", c)
			}
		case ' ', '\t', '\n':
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			inWord = true
			current.WriteByte(c)
		}
	}
	if inWord {
		words = append(words, current.String())
	}
	return words, nil
}

// ResolveAgentAssistanceCapability returns attended-assistance support for the selected agent.
// agentCmd is intentionally ignored because custom --agent-cmd only applies to
// unattended issue attempts.
func ResolveAgentAssistanceCapability(preset, agentCmd string) (AgentAssistanceCapability, error) {
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return AgentAssistanceCapability{}, err
	}
	return adapter.AssistanceCapability(), nil
}

// ResolveAgentAssistanceInvocation returns the attended command owned by the selected adapter.
// agentCmd is accepted for call-site symmetry with headless invocation but is intentionally ignored:
// custom --agent-cmd only applies to unattended issue attempts.
func ResolveAgentAssistanceInvocation(preset, agentCmd, prompt, runtimePath string) (*AgentAssistanceInvocation, error) {
	_, extraArgs, err := parseAgentPresetSpec(preset)
	if err != nil {
		return nil, err
	}
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return nil, err
	}
	return adapter.AssistanceInvocation(AgentAssistanceRequest{
		Prompt:      prompt,
		RuntimePath: runtimePath,
		ExtraArgs:   extraArgs,
	})
}

func displayAgentCommand(command AgentCommand, prompt string) string {
	parts := []string{shellQuote(command.Name)}
	for _, arg := range command.Args {
		if prompt != "" && arg == prompt {
			parts = append(parts, "<HITL assistance prompt>")
			continue
		}
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.ContainsAny(s, " \t\n'\"\\$`!&|;()<>") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
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
	name, _, err := parseAgentPresetSpec(preset)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "claude"
	}
	mode := AgentOutputMode(cfg.TaskAgentOutput(name))
	if err := validateAgentOutputMode(mode); err != nil {
		return "", fmt.Errorf("[workload.agents.%s] output: %w", name, err)
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
