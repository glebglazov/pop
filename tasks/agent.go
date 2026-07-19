package tasks

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	// ResetAt is the agent-reported absolute reset instant. A zero value means
	// unknown / unparseable; queue supervision must use its fixed fallback.
	ResetAt time.Time
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
	ReasoningArgs(reasoning string) []string
	ArgsContainReasoning(args []string) bool
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

var claudeEffortModels = map[string][]config.EffortModel{
	"heavy":    {{Model: "opus", Reasoning: "high"}},
	"standard": {{Model: "sonnet", Reasoning: "high"}},
	"light":    {{Model: "haiku", Reasoning: "high"}},
}

var codexEffortModels = map[string][]config.EffortModel{
	"heavy":    {{Model: "gpt-5.5", Reasoning: "high"}},
	"standard": {{Model: "gpt-5.5", Reasoning: "medium"}},
	"light":    {{Model: "gpt-5.4-mini", Reasoning: "low"}},
}

var cursorEffortModels = map[string][]config.EffortModel{
	"heavy":    {{Model: "composer-2.5"}},
	"standard": {{Model: "composer-2.5"}},
	"light":    {{Model: "composer-2.5-fast"}},
}

var piEffortModels = map[string][]config.EffortModel{
	"heavy":    {{Model: "opencode-go/qwen3.7-max", Reasoning: "high"}},
	"standard": {{Model: "opencode-go/kimi-k2.6", Reasoning: "medium"}},
	"light":    {{Model: "opencode-go/deepseek-v4-flash", Reasoning: "low"}},
}

var builtInEffortModels = map[string]map[string][]config.EffortModel{
	"claude": claudeEffortModels,
	"codex":  codexEffortModels,
	"cursor": cursorEffortModels,
	"pi":     piEffortModels,
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

func (a *presetAgentAdapter) ReasoningArgs(reasoning string) []string {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return nil
	}
	switch a.preset {
	case "claude":
		return []string{"--effort", reasoning}
	case "codex":
		return []string{"-c", fmt.Sprintf(`model_reasoning_effort="%s"`, reasoning)}
	case "pi":
		return []string{"--thinking", reasoning}
	default:
		return nil
	}
}

func (a *presetAgentAdapter) ArgsContainReasoning(args []string) bool {
	switch a.preset {
	case "claude":
		for _, arg := range args {
			if arg == "--effort" || strings.HasPrefix(arg, "--effort=") {
				return true
			}
		}
	case "codex":
		for i, arg := range args {
			if arg == "-c" {
				if i+1 < len(args) && isCodexReasoningConfig(args[i+1]) {
					return true
				}
				continue
			}
			if strings.HasPrefix(arg, "-c=") && isCodexReasoningConfig(strings.TrimPrefix(arg, "-c=")) {
				return true
			}
		}
	case "cursor":
		for _, arg := range args {
			if strings.Contains(arg, "[") && strings.Contains(arg, "]") && strings.Contains(arg, "effort=") {
				return true
			}
		}
	case "pi":
		for i, arg := range args {
			if arg == "--thinking" {
				return true
			}
			if strings.HasPrefix(arg, "--thinking=") {
				return true
			}
			if arg == "--model" {
				if i+1 < len(args) && piModelTokenContainsThinking(args[i+1]) {
					return true
				}
				continue
			}
			if strings.HasPrefix(arg, "--model=") && piModelTokenContainsThinking(strings.TrimPrefix(arg, "--model=")) {
				return true
			}
		}
	}
	return false
}

func isCodexReasoningConfig(arg string) bool {
	key, _, found := strings.Cut(strings.TrimSpace(arg), "=")
	return found && strings.TrimSpace(key) == "model_reasoning_effort"
}

func piModelTokenContainsThinking(arg string) bool {
	model, thinking, found := strings.Cut(strings.TrimSpace(arg), ":")
	return found && strings.TrimSpace(model) != "" && strings.TrimSpace(thinking) != ""
}

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

func (a customAgentAdapter) ReasoningArgs(reasoning string) []string { return nil }

func (a customAgentAdapter) ArgsContainReasoning(args []string) bool { return false }

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

// ResolveDefaultInteractiveAgentPreset returns the default Interactive agent
// preset for attended sessions (wayfinder work, HITL assistance, routine
// authoring). It follows [tasks.implement].agents when set, otherwise claude.
func ResolveDefaultInteractiveAgentPreset(cfg *config.Config) string {
	specs := ResolveDefaultAgentPresets(nil, "", false, cfg)
	if len(specs) == 0 {
		return DefaultAgentPreset
	}
	return specs[0]
}

// ResolveDefaultAgentPresets returns the ordered agent preset list for a run.
// Explicit CLI --agent flags win; otherwise [tasks.implement].agents applies;
// the final fallback is claude.
func ResolveDefaultAgentPresets(cliPresets []string, cliPreset string, agentExplicit bool, cfg *config.Config) []string {
	if agentExplicit {
		return nonEmptyAgentSpecs(cliPresets, cliPreset)
	}
	if cfg != nil && cfg.Task != nil && cfg.Task.Implement != nil && len(cfg.Task.Implement.Agents) > 0 {
		return nonEmptyAgentSpecs(cfg.Task.Implement.Agents, DefaultAgentPreset)
	}
	return nonEmptyAgentSpecs(nil, cliPreset)
}

func nonEmptyAgentSpecs(specs []string, fallback string) []string {
	var out []string
	for _, spec := range specs {
		if strings.TrimSpace(spec) != "" {
			out = append(out, spec)
		}
	}
	if len(out) == 0 {
		if strings.TrimSpace(fallback) != "" {
			return []string{fallback}
		}
		return []string{DefaultAgentPreset}
	}
	return out
}

func resolveTaskAgentSpecs(defaultSpecs []string, agentCmd, effort string, effortExplicit bool, cfg *config.Config) []string {
	specs := nonEmptyAgentSpecs(defaultSpecs, DefaultAgentPreset)
	if agentCmd != "" {
		return specs
	}
	resolved := make([]string, 0, len(specs))
	for _, spec := range specs {
		resolved = append(resolved, resolveTaskAgentSpecForEffortWithConfig(spec, effort, effortExplicit, cfg))
	}
	return resolved
}

func resolveTaskAgentSpecForEffort(agentSpec, effort string, effortExplicit bool) string {
	return resolveTaskAgentSpecForEffortWithConfig(agentSpec, effort, effortExplicit, nil)
}

// ResolveAgentSpecForEffort rewrites an agent preset spec so its model is pinned
// to the given effort tier via the [effort.<agent>] ladder. It is exported for
// Routine runs, which resolve effort outside the tasks package. An empty effort
// resolves to DefaultTaskEffort.
func ResolveAgentSpecForEffort(agentSpec, effort string, cfg *config.Config) string {
	return resolveTaskAgentSpecForEffortWithConfig(agentSpec, effort, true, cfg)
}

func resolveTaskAgentSpecForEffortWithConfig(agentSpec, effort string, effortExplicit bool, cfg *config.Config) string {
	if !effortExplicit {
		return agentSpec
	}
	if effort == "" {
		effort = DefaultTaskEffort
	}
	name, extraArgs, err := parseAgentPresetSpec(agentSpec)
	if err != nil {
		return agentSpec
	}
	if name == "" {
		name = DefaultAgentPreset
	}
	if agentArgsContainModel(extraArgs) {
		return agentSpec
	}
	bundles := effortModelsForAgent(cfg, name, effort)
	if len(bundles) == 0 || strings.TrimSpace(bundles[0].Model) == "" {
		return agentSpec
	}
	args := append([]string{name}, extraArgs...)
	adapter := agentAdapters[name]
	args = append(args, "--model", effortModelTokenForAgent(name, bundles[0], adapter, extraArgs))
	if adapter != nil && !adapter.ArgsContainReasoning(extraArgs) {
		args = append(args, adapter.ReasoningArgs(bundles[0].Reasoning)...)
	}
	for i, arg := range args {
		args[i] = shellQuote(arg)
	}
	return strings.Join(args, " ")
}

func effortModelTokenForAgent(agent string, bundle config.EffortModel, adapter AgentAdapter, extraArgs []string) string {
	return strings.TrimSpace(bundle.Model)
}

func effortModelsForAgent(cfg *config.Config, agent, effort string) []config.EffortModel {
	if cfg != nil && cfg.Effort != nil {
		if ladder, ok := cfg.Effort[agent]; ok {
			return effortModelsForTier(ladder, effort)
		}
	}
	if ladder, ok := builtInEffortModels[agent]; ok {
		return ladder[effort]
	}
	return nil
}

func effortModelsForTier(ladder config.EffortConfig, effort string) []config.EffortModel {
	switch effort {
	case "heavy":
		return ladder.Heavy
	case "standard":
		return ladder.Standard
	case "light":
		return ladder.Light
	default:
		return nil
	}
}

func agentArgsContainModel(args []string) bool {
	for _, arg := range args {
		if arg == "--model" || strings.HasPrefix(arg, "--model=") {
			return true
		}
	}
	return false
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
	if strings.ContainsAny(s, " \t\n'\"\\$`!&|;()<>[]") {
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

func loadConfigIfPresent(loadConfig func(string) (*config.Config, error)) (*config.Config, error) {
	if loadConfig == nil {
		return nil, nil
	}
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
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
	cfg, err := loadConfigIfPresent(loadConfig)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return AgentOutputAuto, nil
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
		return "", fmt.Errorf("[tasks.presets.%s] output: %w", name, err)
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
