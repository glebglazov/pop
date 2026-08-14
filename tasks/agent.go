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
	AgentOutputKimiStreamJSON   AgentOutputFormat = "kimi-stream-json"

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
	Name string
	Args []string
	// Env carries KEY=VALUE entries layered over pop's own environment when the
	// process spawns — the one adapter capability arguments cannot express
	// (ADR-0164). Empty for every preset whose knobs are all flags.
	Env            []string
	OutputFormat   AgentOutputFormat
	RequestedAgent string
	adapter        AgentAdapter
	// promptArg is the index into Args of the generated prompt, so the run seam
	// can spill it to a file instead of handing execve a megabyte of argv
	// (see agent_prompt_spill.go). Zero means this invocation carries no
	// generated prompt — a prompt is never argv[0].
	promptArg int
	// promptFile is the spill file backing promptArg while an attempt runs, and
	// empty when the prompt is still inline.
	promptFile string
	// turnCap is the bound pop itself put on this command line, so an attempt
	// that ends at it can say which number it ended at (ADR-0190). Zero when the
	// repository declares no cap, when the adapter cannot be told about one, and
	// when the human wrote his own `--max-turns` — pop emitted nothing there, so
	// the number in force is his and not pop's to report.
	turnCap int
}

// AgentPreset returns the owning adapter's preset name.
func (i *AgentInvocation) AgentPreset() string {
	if i == nil || i.adapter == nil {
		return ""
	}
	return i.adapter.Preset()
}

// PinnedModel returns the model this command pins through pop's `--model` flag —
// whatever an Effort ladder resolved, or what the human typed in `--agent` args.
// Empty when nothing is pinned and the agent's own configuration picks the model,
// which is why a model refusal falls back to the name the provider reported.
func (i *AgentInvocation) PinnedModel() string {
	if i == nil {
		return ""
	}
	for idx, arg := range i.Args {
		if arg == "--model" {
			if idx+1 < len(i.Args) {
				return i.Args[idx+1]
			}
			return ""
		}
		if value, ok := strings.CutPrefix(arg, "--model="); ok {
			return value
		}
	}
	return ""
}

// AgentResult is the provider-neutral result of normalizing one invocation.
type AgentResult struct {
	Output string
	// ProceedVerdict is nil when the adapter can carry on, and otherwise says at
	// what scope it is stopped (ADR-0168).
	ProceedVerdict *AgentProceedVerdict
}

// AgentHeadlessRequest describes one unattended issue-attempt invocation.
type AgentHeadlessRequest struct {
	Prompt      string
	RuntimePath string
	OutputMode  AgentOutputMode
	// ExtraArgs are user-supplied arguments augmenting the preset (ADR-0017).
	// They precede pop's owned flags so owned flags stay authoritative.
	ExtraArgs []string
	// TurnCap is the repository's bound on how many Turns this attempt may spend,
	// and zero when it declares none. Only an implementation attempt sets it: a
	// Verifier reads and judges where an Implementer builds, and one number cannot
	// mean both (ADR-0190). An adapter that cannot be told about a cap ignores it.
	TurnCap int
}

// AgentAssistanceRequest describes one attended HITL assistance invocation.
type AgentAssistanceRequest struct {
	Prompt      string
	RuntimePath string
	// EntryArgs are the arguments of the attended entry's cmd, everything after
	// the token that named the preset. They are passed through as written: the
	// entry owns its whole invocation, model included (ADR-0195).
	EntryArgs []string
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
	// ClipboardPrompt is the generated briefing text for a preset whose
	// interactive binary takes no positional prompt (kimi), so it never rides
	// in Command.Args. The caller places it on the clipboard before launch and
	// tells the human to paste it (ADR-0164).
	ClipboardPrompt string
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
	AttendedArgsCapability() AgentAttendedArgsCapability
	AvailabilityProbeCapability() AgentAvailabilityProbeCapability
	UsageCapability() AgentUsageCapability
	CostCapability() AgentCostCapability
	ToolTimingCapability() AgentToolTimingCapability
	ActualModelCapability() AgentActualModelCapability
	StreamRenderCapability() AgentStreamRenderCapability
	TurnCapability() AgentTurnCapability
	PeakInputCapability() AgentPeakInputCapability
	ReasoningCapability() AgentReasoningCapability
	TurnCapEnforcementCapability() AgentTurnCapEnforcementCapability
	TurnCapExhaustionCapability() AgentTurnCapExhaustionCapability
	QuotaResetCapability() AgentQuotaResetCapability
	EffortLadderCapability() AgentEffortLadderCapability
	ExecutableCapability() AgentExecutableCapability
	AssistanceInvocation(AgentAssistanceRequest) (*AgentAssistanceInvocation, error)
	// Models returns the preset's curated, recommended-first model aliases that
	// Pop ships for display. Advisory only; never a validation gate (ADR-0019).
	Models() []string
}

// Agent adapters map preset names to per-agent behavior.
var agentAdapters = map[string]AgentAdapter{
	"claude": newPresetAgentAdapter(presetAgentSpec{
		preset:         "claude",
		headlessPrefix: []string{"claude", "--dangerously-skip-permissions", "-p"},
		autoFormat:     AgentOutputClaudeStreamJSON,
		autoArgs:       []string{"--output-format", "stream-json", "--verbose"},
		assistance:     AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "claude"}},
		attendedArgs:   AgentAttendedArgsCapability{Kind: CapabilitySupported, Args: []string{"--permission-mode", "auto"}},
		availability: AgentAvailabilityProbeCapability{
			Kind:                 CapabilitySupported,
			Command:              &AgentCommand{Name: "claude", Args: []string{"auth", "status"}},
			Interpret:            interpretClaudeAvailabilityProbe,
			ReportsAuthenticated: reportsClaudeAuthenticated,
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: claudeQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind: CapabilitySupported,
			Ladder: map[string][]config.EffortModel{
				"heavy":    {{Model: "opus", Reasoning: "high"}},
				"standard": {{Model: "sonnet", Reasoning: "high"}},
				"light":    {{Model: "sonnet", Reasoning: "low"}},
			},
		},
		executable: AgentExecutableCapability{Kind: CapabilitySupported, Name: "claude"},
		usage:       AgentUsageCapability{Kind: CapabilitySupported, Extract: claudeTokenUsage},
		cost:        AgentCostCapability{Kind: CapabilitySupported, Extract: claudePartialCost},
		toolTimings: AgentToolTimingCapability{Kind: CapabilitySupported, Extract: claudeToolTimings},
		actualModel: AgentActualModelCapability{Kind: CapabilitySupported, Extract: claudeActualModel},
		streamRender: AgentStreamRenderCapability{Kind: CapabilitySupported, Render: renderClaudeEvent},
		turns:        AgentTurnCapability{Kind: CapabilitySupported, Extract: claudeTurnCount},
		peakInput:    AgentPeakInputCapability{Kind: CapabilitySupported, Extract: claudePeakInput},
		reasoning: AgentReasoningCapability{
			Kind:       CapabilitySupported,
			SpecTokens: claudeReasoningSpecTokens,
			Contains:   claudeArgsContainReasoning,
		},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{
			Kind:       CapabilitySupported,
			SpecTokens: claudeTurnCapSpecTokens,
			Contains:   claudeArgsContainTurnCap,
		},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilitySupported, Exhausted: claudeTurnCapExhausted},
		models:      []string{"opus", "sonnet", "haiku", "fable"},
	}),
	"opencode": newPresetAgentAdapter(presetAgentSpec{
		preset:         "opencode",
		headlessPrefix: []string{"opencode", "run"},
		autoFormat:     AgentOutputOpenCodeJSON,
		autoArgs:       []string{"--format", "json"},
		assistance:     AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "opencode"}},
		attendedArgs:   AgentAttendedArgsCapability{Kind: CapabilityBlind, Reason: "opencode's CLI carries no auto-approval flag, so its attended session launches bare"},
		availability: AgentAvailabilityProbeCapability{
			Kind:   CapabilityBlind,
			Reason: "opencode ships no read-only auth status command",
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: piQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind:   CapabilityBlind,
			Reason: "opencode has no built-in effort ladder",
		},
		executable:     AgentExecutableCapability{Kind: CapabilitySupported, Name: "opencode"},
		usage:          AgentUsageCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no usage block"},
		cost:           AgentCostCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no dollar cost"},
		toolTimings:    AgentToolTimingCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no tool-use/tool-result pairing"},
		actualModel:    AgentActualModelCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no actual-model field"},
		streamRender:   AgentStreamRenderCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no renderable assistant/tool_result message shape"},
		turns:          AgentTurnCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no message boundary"},
		peakInput:      AgentPeakInputCapability{Kind: CapabilityBlind, Reason: "opencode's JSON parts carry no per-call usage block"},
		reasoning:      AgentReasoningCapability{Kind: CapabilityBlind, Reason: "opencode's CLI carries no reasoning or thinking level parameter"},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "opencode's turn cap is its per-agent `steps` key, reachable only from its own config file, which pop never writes"},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "opencode ends a step-capped run gracefully — a forced text-only summary and a success exit, indistinguishable from a run that finished"},
		models:         []string{"opencode/kimi-k2.6", "opencode/gpt-5.5", "opencode/claude-opus-4-8", "opencode/claude-sonnet-4-6"},
		modelsInstallDependent: true,
	}),
	"cursor": newPresetAgentAdapter(presetAgentSpec{
		preset:         "cursor",
		headlessPrefix: []string{"cursor-agent", "-p", "--force", "--trust"},
		autoFormat:     AgentOutputCursorStreamJSON,
		autoArgs:       []string{"--output-format", "stream-json"},
		assistance:     AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "cursor-agent"}},
		attendedArgs:   AgentAttendedArgsCapability{Kind: CapabilitySupported, Args: []string{"--force", "--trust"}},
		availability: AgentAvailabilityProbeCapability{
			Kind:                 CapabilitySupported,
			Command:              &AgentCommand{Name: "cursor-agent", Args: []string{"status", "--format", "json"}},
			IdentifyingArgs:        []string{"status"},
			Interpret:            interpretCursorAvailabilityProbe,
			ReportsAuthenticated: reportsCursorAuthenticated,
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: cursorQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind: CapabilitySupported,
			Ladder: map[string][]config.EffortModel{
				"heavy":    {{Model: "composer-2.5"}},
				"standard": {{Model: "composer-2.5"}},
				"light":    {{Model: "composer-2.5-fast"}},
			},
		},
		executable:             AgentExecutableCapability{Kind: CapabilitySupported, Name: "cursor-agent"},
		usage:                  AgentUsageCapability{Kind: CapabilitySupported, Extract: cursorTokenUsage},
		cost:                   AgentCostCapability{Kind: CapabilityBlind, Reason: "cursor reports token usage but no dollar cost"},
		toolTimings:            AgentToolTimingCapability{Kind: CapabilitySupported, Extract: cursorToolTimings},
		actualModel:            AgentActualModelCapability{Kind: CapabilitySupported, Extract: cursorActualModel},
		streamRender:           AgentStreamRenderCapability{Kind: CapabilitySupported, Render: renderCursorEvent},
		turns:                  AgentTurnCapability{Kind: CapabilitySupported, Extract: cursorTurnCount},
		peakInput:              AgentPeakInputCapability{Kind: CapabilityBlind, Reason: "cursor reports token usage only as a whole-run total on result"},
		reasoning: AgentReasoningCapability{
			Kind:     CapabilityBlind,
			Reason:   "cursor selects a full model name per effort tier instead of a separate reasoning parameter",
			Contains: cursorArgsContainReasoning,
		},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "cursor-agent has no turn cap of any kind"},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "cursor-agent has no turn cap, so there is no exhausted ending to recognise"},
		models:                 []string{"auto", "composer-2.5", "gpt-5.3-codex"},
		modelsInstallDependent: true,
	}),
	"codex": newPresetAgentAdapter(presetAgentSpec{
		preset:         "codex",
		headlessPrefix: []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
		autoFormat:     AgentOutputCodexJSONL,
		autoArgs:       []string{"--json"},
		assistance:     AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "codex"}},
		attendedArgs:   AgentAttendedArgsCapability{Kind: CapabilitySupported, Args: []string{"--dangerously-bypass-approvals-and-sandbox"}},
		availability: AgentAvailabilityProbeCapability{
			Kind:                 CapabilitySupported,
			Command:              &AgentCommand{Name: "codex", Args: []string{"login", "status"}},
			ReportsAuthenticated: reportsCodexAuthenticated,
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: codexQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind: CapabilitySupported,
			Ladder: map[string][]config.EffortModel{
				"heavy":    {{Model: "gpt-5.5", Reasoning: "high"}},
				"standard": {{Model: "gpt-5.5", Reasoning: "medium"}},
				"light":    {{Model: "gpt-5.4-mini", Reasoning: "low"}},
			},
		},
		executable:             AgentExecutableCapability{Kind: CapabilitySupported, Name: "codex"},
		usage:                  AgentUsageCapability{Kind: CapabilitySupported, Extract: codexTokenUsage},
		cost:                   AgentCostCapability{Kind: CapabilityBlind, Reason: "codex item streams carry no dollar cost"},
		toolTimings:            AgentToolTimingCapability{Kind: CapabilitySupported, Extract: codexToolTimings},
		actualModel:            AgentActualModelCapability{Kind: CapabilityBlind, Reason: "codex item streams carry no actual-model init event"},
		streamRender:           AgentStreamRenderCapability{Kind: CapabilitySupported, Render: renderCodexEvent},
		turns:                  AgentTurnCapability{Kind: CapabilitySupported, Extract: codexTurnCount},
		peakInput:              AgentPeakInputCapability{Kind: CapabilityBlind, Reason: "codex item streams carry no per-call usage block"},
		reasoning: AgentReasoningCapability{
			Kind:       CapabilitySupported,
			SpecTokens: codexReasoningSpecTokens,
			Contains:   codexArgsContainReasoning,
		},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "codex has no turn cap; a turn there is one HTTP request carrying many iterations"},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "codex has no turn cap, so there is no exhausted ending to recognise"},
		models:                 []string{"gpt-5.5", "gpt-5.4-mini"},
		modelsInstallDependent: true,
	}),
	"pi": newPresetAgentAdapter(presetAgentSpec{
		preset:         "pi",
		headlessPrefix: []string{"pi", "-p", "--no-extensions", "--no-skills"},
		autoFormat:     AgentOutputPiJSONL,
		autoArgs:       []string{"--mode", "json"},
		assistance:     AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "pi"}},
		attendedArgs:   AgentAttendedArgsCapability{Kind: CapabilityBlind, Reason: "pi's CLI carries no auto-approval flag — not even its headless drains pass one"},
		availability: AgentAvailabilityProbeCapability{
			Kind:   CapabilityBlind,
			Reason: "pi ships no read-only auth status command",
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: piQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind: CapabilitySupported,
			Ladder: map[string][]config.EffortModel{
				"heavy":    {{Model: "opencode-go/qwen3.7-max", Reasoning: "high"}},
				"standard": {{Model: "opencode-go/kimi-k2.6", Reasoning: "medium"}},
				"light":    {{Model: "opencode-go/deepseek-v4-flash", Reasoning: "low"}},
			},
		},
		executable:     AgentExecutableCapability{Kind: CapabilitySupported, Name: "pi"},
		usage:          AgentUsageCapability{Kind: CapabilitySupported, Extract: piTokenUsage},
		cost:           AgentCostCapability{Kind: CapabilitySupported, Extract: piPartialCost},
		toolTimings:    AgentToolTimingCapability{Kind: CapabilitySupported, Extract: piToolTimings},
		actualModel:    AgentActualModelCapability{Kind: CapabilitySupported, Extract: piActualModel},
		streamRender:   AgentStreamRenderCapability{Kind: CapabilitySupported, Render: renderPiEvent},
		turns:          AgentTurnCapability{Kind: CapabilitySupported, Extract: piTurnCount},
		peakInput:      AgentPeakInputCapability{Kind: CapabilitySupported, Extract: piPeakInput},
		reasoning: AgentReasoningCapability{
			Kind:       CapabilitySupported,
			SpecTokens: piReasoningSpecTokens,
			Contains:   piArgsContainReasoning,
		},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "pi has no turn cap, and no token or dollar budget either"},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "pi has no turn cap, so there is no exhausted ending to recognise"},
		models:         []string{"opencode-go/kimi-k2.6", "opencode-go/qwen3.7-max", "opencode-go/minimax-m3", "opencode-go/deepseek-v4-flash"},
		modelsInstallDependent: true,
	}),
	// kimi takes the prompt as its -p value and needs no permission flag: -p is
	// auto-permission by design and rejects --yolo/--auto (ADR-0164).
	"kimi": newPresetAgentAdapter(presetAgentSpec{
		preset:          "kimi",
		headlessPrefix:  []string{"kimi", "-p"},
		promptDelivery:  promptAsPrefixFlagValue,
		autoFormat:      AgentOutputKimiStreamJSON,
		autoArgs:        []string{"--output-format", "stream-json"},
		env:             []string{"KIMI_CODE_NO_AUTO_UPDATE=1"},
		assistance:      AgentAssistanceCapability{Mode: AgentAssistanceNative, Command: &AgentCommand{Name: "kimi"}},
		attendedArgs:    AgentAttendedArgsCapability{Kind: CapabilityBlind, Reason: "kimi's -p is its own auto-permission and it rejects --yolo/--auto, so its attended session launches bare (ADR-0164)"},
		availability: AgentAvailabilityProbeCapability{
			Kind:   CapabilityBlind,
			Reason: "kimi ships no read-only auth status command",
		},
		quotaReset: AgentQuotaResetCapability{Kind: CapabilitySupported, ResetAt: kimiQuotaResetAt},
		effortLadder: AgentEffortLadderCapability{
			Kind: CapabilitySupported,
			Ladder: map[string][]config.EffortModel{
				"heavy":    {{Model: "moonshot-ai/kimi-k3", Reasoning: "high"}},
				"standard": {{Model: "moonshot-ai/kimi-k3", Reasoning: "low"}},
				"light":    {{Model: "moonshot-ai/kimi-k2.7-code-highspeed"}},
			},
		},
		executable:      AgentExecutableCapability{Kind: CapabilitySupported, Name: "kimi"},
		usage:           AgentUsageCapability{Kind: CapabilityBlind, Reason: "kimi stream usage has not been verified against a captured run"},
		cost:            AgentCostCapability{Kind: CapabilityBlind, Reason: "kimi stream cost has not been verified against a captured run"},
		toolTimings:     AgentToolTimingCapability{Kind: CapabilityBlind, Reason: "kimi stream tool timings have not been verified against a captured run"},
		actualModel:     AgentActualModelCapability{Kind: CapabilityBlind, Reason: "kimi stream actual model has not been verified against a captured run"},
		streamRender:    AgentStreamRenderCapability{Kind: CapabilityBlind, Reason: "kimi stream rendering has not been verified against a captured run"},
		turns:           AgentTurnCapability{Kind: CapabilityBlind, Reason: "kimi stream turn count has not been verified against a captured run"},
		peakInput:       AgentPeakInputCapability{Kind: CapabilityBlind, Reason: "kimi stream peak input has not been verified against a captured run"},
		reasoning: AgentReasoningCapability{
			Kind:       CapabilitySupported,
			EnvKey:     "KIMI_MODEL_THINKING_EFFORT",
			SpecTokens: kimiReasoningSpecTokens,
			Contains:   kimiArgsContainReasoning,
		},
		turnCapEnforcement: AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "kimi's turn cap is loop_control.max_steps_per_turn, reachable only from its own config file, which pop never writes"},
		turnCapExhaustion: AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "kimi reports a step-capped ending as loop.max_steps_exceeded on stderr and puts nothing in its stream-json"},
		models:          []string{"moonshot-ai/kimi-k3", "moonshot-ai/kimi-k2.7-code", "moonshot-ai/kimi-k2.7-code-highspeed"},
		modelsInstallDependent: true,
	}),
}

// agentPromptDelivery names where a preset's generated prompt rides in the
// resolved command. Most CLIs take it as a positional argument; kimi has no
// positional prompt form and reads it as the value of the -p flag its headless
// prefix ends with (ADR-0164).
type agentPromptDelivery int

const (
	promptAsFinalArg agentPromptDelivery = iota
	promptAsPrefixFlagValue
)

// presetAgentSpec is one preset's declaration of everything the shared
// preset adapter needs: how it is invoked, how its output is read, and what it
// can offer a human.
type presetAgentSpec struct {
	preset         string
	headlessPrefix []string
	promptDelivery agentPromptDelivery
	autoFormat     AgentOutputFormat
	autoArgs       []string
	// env rides into every invocation of this preset as KEY=VALUE entries
	// layered over pop's own environment, for knobs the CLI exposes nowhere
	// else (ADR-0164).
	env []string
	assistance      AgentAssistanceCapability
	attendedArgs    AgentAttendedArgsCapability
	availability    AgentAvailabilityProbeCapability
	usage           AgentUsageCapability
	cost            AgentCostCapability
	toolTimings     AgentToolTimingCapability
	actualModel     AgentActualModelCapability
	streamRender    AgentStreamRenderCapability
	turns           AgentTurnCapability
	peakInput       AgentPeakInputCapability
	reasoning       AgentReasoningCapability
	// turnCapEnforcement says whether this preset can be told, in argv, to stop
	// after a number of Turns — the repository's bound on one implementation
	// attempt (ADR-0190).
	turnCapEnforcement AgentTurnCapEnforcementCapability
	// turnCapExhaustion says whether pop can recognise, from this preset's own
	// stream and exit status, that a run stopped because it reached that bound
	// (ADR-0190).
	turnCapExhaustion AgentTurnCapExhaustionCapability
	quotaReset      AgentQuotaResetCapability
	effortLadder    AgentEffortLadderCapability
	executable      AgentExecutableCapability
	models          []string
	// modelsInstallDependent marks a curated list whose aliases are resolved by
	// the local install's own provider config rather than being stable,
	// account-independent names, so the catalog can say so.
	modelsInstallDependent bool
}

type presetAgentAdapter struct {
	presetAgentSpec
}

func (s presetAgentSpec) validate() error {
	preset := s.preset
	for _, check := range []struct {
		err error
	}{
		{s.usage.validate(preset)},
		{s.cost.validate(preset)},
		{s.toolTimings.validate(preset)},
		{s.actualModel.validate(preset)},
		{s.streamRender.validate(preset)},
		{s.turns.validate(preset)},
		{s.peakInput.validate(preset)},
		{s.reasoning.validate(preset)},
		{s.turnCapEnforcement.validate(preset)},
		{s.turnCapExhaustion.validate(preset)},
		{s.quotaReset.validate(preset)},
		{s.effortLadder.validate(preset)},
		{s.executable.validate(preset)},
		{s.availability.validate(preset)},
		{s.attendedArgs.validate(preset)},
	} {
		if check.err != nil {
			return check.err
		}
	}
	return nil
}

func newPresetAgentAdapter(spec presetAgentSpec) AgentAdapter {
	if err := spec.validate(); err != nil {
		panic(err)
	}
	spec.headlessPrefix = append([]string{}, spec.headlessPrefix...)
	spec.autoArgs = append([]string{}, spec.autoArgs...)
	spec.env = append([]string{}, spec.env...)
	spec.availability = cloneAvailabilityProbeCapability(spec.availability)
	spec.attendedArgs.Args = append([]string{}, spec.attendedArgs.Args...)
	spec.models = append([]string{}, spec.models...)
	return &presetAgentAdapter{presetAgentSpec: spec}
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
	extraArgs, extraEnv := a.splitEnvAssignments(req.ExtraArgs)
	args := []string{a.headlessPrefix[0]}
	args = append(args, extraArgs...)
	args = append(args, a.headlessPrefix[1:]...)
	promptArg := 0
	// A flag-value prompt must stay adjacent to the flag it belongs to, so it
	// lands before pop's owned output flags instead of at the very end.
	if a.promptDelivery == promptAsPrefixFlagValue {
		promptArg = len(args)
		args = append(args, req.Prompt)
	}
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
	// The repository's Turn cap rides in argv where the adapter can enforce one —
	// pop's own flag, so it lands after the augmenting spec's arguments. A cap the
	// human already wrote into that spec wins and pop emits nothing of its own,
	// the same detect-and-defer a hand-set reasoning flag gets (ADR-0190).
	turnCap := 0
	if !a.turnCapEnforcement.argsContainTurnCap(extraArgs) {
		if tokens := a.turnCapEnforcement.specTokens(req.TurnCap); len(tokens) > 0 {
			args = append(args, tokens...)
			turnCap = req.TurnCap
		}
	}
	if a.promptDelivery == promptAsFinalArg {
		promptArg = len(args)
		args = append(args, req.Prompt)
	}
	return &AgentInvocation{
		Name:         args[0],
		Args:         args[1:],
		Env:          append(append([]string(nil), a.env...), extraEnv...),
		OutputFormat: format,
		adapter:      a,
		turnCap:      turnCap,
		// Args drops argv[0], so the recorded index shifts with it.
		promptArg: max(promptArg-1, 0),
	}, nil
}

// splitEnvAssignments separates the environment assignments a spec carries for
// this preset from its real arguments. The Effort ladder writes a reasoning
// level for a flagless preset as a KEY=VALUE spec token, and a spec may carry
// one hand-written; either way it belongs in the invocation environment, never
// in argv. Only keys this preset itself owns count, so an argument value that
// happens to contain "=" (codex's `-c model_reasoning_effort="high"`) stays an
// argument.
func (a *presetAgentAdapter) splitEnvAssignments(tokens []string) (args, env []string) {
	for _, token := range tokens {
		if key, _, found := strings.Cut(token, "="); found && a.ownsEnvKey(key) {
			env = append(env, token)
			continue
		}
		args = append(args, token)
	}
	return args, env
}

func (a *presetAgentAdapter) ownsEnvKey(key string) bool {
	if key == "" {
		return false
	}
	if key == a.reasoning.EnvKey {
		return true
	}
	for _, entry := range a.env {
		if name, _, found := strings.Cut(entry, "="); found && name == key {
			return true
		}
	}
	return false
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

func (a *presetAgentAdapter) AttendedArgsCapability() AgentAttendedArgsCapability {
	capability := a.attendedArgs
	capability.Args = append([]string{}, a.attendedArgs.Args...)
	return capability
}

func (a *presetAgentAdapter) AvailabilityProbeCapability() AgentAvailabilityProbeCapability {
	return cloneAvailabilityProbeCapability(a.availability)
}

func (a *presetAgentAdapter) UsageCapability() AgentUsageCapability {
	return a.usage
}

func (a *presetAgentAdapter) CostCapability() AgentCostCapability {
	return a.cost
}

func (a *presetAgentAdapter) ToolTimingCapability() AgentToolTimingCapability {
	return a.toolTimings
}

func (a *presetAgentAdapter) ActualModelCapability() AgentActualModelCapability {
	return a.actualModel
}

func (a *presetAgentAdapter) StreamRenderCapability() AgentStreamRenderCapability {
	return a.streamRender
}

func (a *presetAgentAdapter) TurnCapability() AgentTurnCapability {
	return a.turns
}

func (a *presetAgentAdapter) PeakInputCapability() AgentPeakInputCapability {
	return a.peakInput
}

func (a *presetAgentAdapter) ReasoningCapability() AgentReasoningCapability {
	return a.reasoning
}

func (a *presetAgentAdapter) TurnCapEnforcementCapability() AgentTurnCapEnforcementCapability {
	return a.turnCapEnforcement
}

func (a *presetAgentAdapter) TurnCapExhaustionCapability() AgentTurnCapExhaustionCapability {
	return a.turnCapExhaustion
}

func (a *presetAgentAdapter) QuotaResetCapability() AgentQuotaResetCapability {
	return a.quotaReset
}

func (a *presetAgentAdapter) EffortLadderCapability() AgentEffortLadderCapability {
	return a.effortLadder
}

func (a *presetAgentAdapter) ExecutableCapability() AgentExecutableCapability {
	return a.executable
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
	command.Args = append(command.Args, capability.Command.Args...)
	// The entry's own arguments come first and unread — its cmd is the invocation,
	// model and all (ADR-0195).
	command.Args = append(command.Args, req.EntryArgs...)
	// A session pop opens for a human to work in launches auto-approved, the same
	// posture the headless drains beside it have always had; the flag that says so
	// is per-preset, a preset with none launches bare, and an entry naming that
	// flag itself keeps its own value.
	command.Args = append(command.Args, a.attendedArgs.attendedArgsBeside(req.EntryArgs)...)
	// A preset with no positional prompt form (kimi) launches bare; its briefing
	// reaches the human another way (ADR-0164).
	clipboardPrompt := ""
	detail := fmt.Sprintf("using %s native attended assistance", a.preset)
	if req.Prompt != "" && a.promptDelivery == promptAsFinalArg {
		command.Args = append(command.Args, req.Prompt)
	} else if req.Prompt != "" {
		clipboardPrompt = req.Prompt
		detail = fmt.Sprintf("using %s native attended assistance; briefing delivered via clipboard", a.preset)
	}
	invocation := &AgentAssistanceInvocation{
		AgentPreset:     a.preset,
		Mode:            capability.Mode,
		Command:         command,
		Display:         displayAgentCommand(command, req.Prompt),
		Detail:          detail,
		ClipboardPrompt: clipboardPrompt,
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

func (a customAgentAdapter) AttendedArgsCapability() AgentAttendedArgsCapability {
	return AgentAttendedArgsCapability{Kind: CapabilityBlind, Reason: "a custom agent command is a headless command with no attended form"}
}

func (a customAgentAdapter) AvailabilityProbeCapability() AgentAvailabilityProbeCapability {
	return AgentAvailabilityProbeCapability{Kind: CapabilityBlind, Reason: "custom agent commands ship no availability probe"}
}

func (a customAgentAdapter) UsageCapability() AgentUsageCapability {
	return AgentUsageCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) CostCapability() AgentCostCapability {
	return AgentCostCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) ToolTimingCapability() AgentToolTimingCapability {
	return AgentToolTimingCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) ActualModelCapability() AgentActualModelCapability {
	return AgentActualModelCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) StreamRenderCapability() AgentStreamRenderCapability {
	return AgentStreamRenderCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) TurnCapability() AgentTurnCapability {
	return AgentTurnCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) PeakInputCapability() AgentPeakInputCapability {
	return AgentPeakInputCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) ReasoningCapability() AgentReasoningCapability {
	return AgentReasoningCapability{Kind: CapabilityBlind, Reason: "custom agent commands carry no reasoning parameter"}
}

func (a customAgentAdapter) TurnCapEnforcementCapability() AgentTurnCapEnforcementCapability {
	return AgentTurnCapEnforcementCapability{Kind: CapabilityBlind, Reason: "a custom agent command is an opaque shell command with no cap flag pop can know"}
}

func (a customAgentAdapter) TurnCapExhaustionCapability() AgentTurnCapExhaustionCapability {
	return AgentTurnCapExhaustionCapability{Kind: CapabilityBlind, Reason: "custom agent commands produce no structured stream"}
}

func (a customAgentAdapter) QuotaResetCapability() AgentQuotaResetCapability {
	return AgentQuotaResetCapability{Kind: CapabilityBlind, Reason: "custom agent commands carry no quota reset parsing"}
}

func (a customAgentAdapter) EffortLadderCapability() AgentEffortLadderCapability {
	return AgentEffortLadderCapability{Kind: CapabilityBlind, Reason: "custom agent commands carry no effort ladder"}
}

func (a customAgentAdapter) ExecutableCapability() AgentExecutableCapability {
	return AgentExecutableCapability{Kind: CapabilityBlind, Reason: "custom agent commands have no preset executable name"}
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

func cloneAvailabilityProbeCapability(capability AgentAvailabilityProbeCapability) AgentAvailabilityProbeCapability {
	if capability.Command == nil {
		if len(capability.IdentifyingArgs) > 0 {
			capability.IdentifyingArgs = append([]string{}, capability.IdentifyingArgs...)
		}
		return capability
	}
	clone := *capability.Command
	clone.Args = append([]string{}, capability.Command.Args...)
	capability.Command = &clone
	if len(capability.IdentifyingArgs) > 0 {
		capability.IdentifyingArgs = append([]string{}, capability.IdentifyingArgs...)
	}
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

// ResolveAgentInvocationWithMode applies an explicit output-mode override. The
// command it resolves carries no Turn cap: every uncapped verb (verification,
// Routine work) resolves through here, and the cap is implement-only (ADR-0190).
func ResolveAgentInvocationWithMode(preset, agentCmd, prompt, runtimePath string, mode AgentOutputMode) (*AgentInvocation, error) {
	return resolveAgentInvocation(preset, agentCmd, prompt, runtimePath, mode, 0)
}

// ResolveImplementAgentInvocation resolves the command for one implementation
// attempt, bounded by the repository's Turn cap. turnCap is zero when the
// repository declares none, and a preset that cannot be told to cap turns is
// invoked exactly as it would be without one (ADR-0190).
func ResolveImplementAgentInvocation(preset, agentCmd, prompt, runtimePath string, mode AgentOutputMode, turnCap int) (*AgentInvocation, error) {
	return resolveAgentInvocation(preset, agentCmd, prompt, runtimePath, mode, turnCap)
}

func resolveAgentInvocation(preset, agentCmd, prompt, runtimePath string, mode AgentOutputMode, turnCap int) (*AgentInvocation, error) {
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
			promptArg:      3,
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
		TurnCap:     turnCap,
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

// ResolveAttendedAgentSpec returns the command an attended session launches —
// gate assistance, an Assist session, Map assist, map grilling, a Routine
// refinement session. A human's session-lived override wins; otherwise it is the
// head of [work.attended].agents, and failing that the built-in claude. The
// implement list is deliberately not consulted: a list built for unattended
// drains is not what a human sits in front of (ADR-0194, ADR-0195).
//
// Cool-down and PATH awareness live one layer up in
// ResolveAgentAssistanceInvocation (ADR-0195 decision 6); this helper names the
// configured head only.
func ResolveAttendedAgentSpec(cfg *config.Config, override string) string {
	candidates := attendedLaunchCandidates(cfg, override)
	if len(candidates) == 0 {
		return DefaultAgentPreset
	}
	return candidates[0]
}

// attendedLaunchCandidates is the ordered attended-agent list a launch walks:
// a session-lived override promoted to the head with the configured remainder
// behind it, else [work.attended].agents, else the built-in default. It does
// not consult cooling or PATH — that is the pre-flight walk in
// ResolveAgentAssistanceInvocation.
func attendedLaunchCandidates(cfg *config.Config, override string) []string {
	var configured []string
	for _, entry := range cfg.AttendedAgents() {
		if spec := strings.TrimSpace(entry); spec != "" {
			configured = append(configured, spec)
		}
	}
	override = strings.TrimSpace(override)
	if override == "" {
		if len(configured) == 0 {
			return []string{DefaultAgentPreset}
		}
		return configured
	}
	out := []string{override}
	for _, spec := range configured {
		if spec == override {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// ResolveDefaultAgentPresets returns the ordered agent preset list for a run.
// Explicit CLI --agent flags win; otherwise [work.implement].agents applies;
// the final fallback is claude.
func ResolveDefaultAgentPresets(cliPresets []string, cliPreset string, agentExplicit bool, cfg *config.Config) []string {
	if agentExplicit {
		return nonEmptyAgentSpecs(cliPresets, cliPreset)
	}
	if agents := cfg.ImplementAgents(); len(agents) > 0 {
		return nonEmptyAgentSpecs(agents, DefaultAgentPreset)
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

// resolveTaskAgentSpecForEffortWithConfig resolves an agent spec at the head of
// its Effort tier, with no Effort model skip filtered out. Callers that walk the
// tier resolve through effortSpecResolver instead.
func resolveTaskAgentSpecForEffortWithConfig(agentSpec, effort string, effortExplicit bool, cfg *config.Config) string {
	if resolution := resolveEffortModel(agentSpec, effort, effortExplicit, cfg, nil); !resolution.Exhausted {
		return resolution.Spec
	}
	return agentSpec
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
	adapter, err := ResolveAgentAdapter(agent)
	if err != nil {
		return nil
	}
	return adapter.EffortLadderCapability().modelsForTier(effort)
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
//
// override is the attended agent a --agent flag named for this one invocation,
// empty when none was given; either way the spec it resolves to is an attended
// entry, and
// the entry's whole cmd is the invocation — arguments and model passed through
// as written, with the preset's declared posture arguments appended only where
// the entry did not name that flag (ADR-0195). No attended path reads a drain's
// agent list. It is the one chokepoint every attended call site passes through,
// which is why the policy lives here and not per call site.
//
// Before launching it walks the attended candidates and takes the first entry
// whose preset is not on a machine-global quota cooldown and whose binary is on
// PATH (ADR-0195 decision 6). Skipped entries are named in Detail with why —
// cooling until a time, or binary missing — and never with wording that implies
// a mid-session switch. A store it cannot read is treated as empty cooling
// rather than a refusal: the launch still proceeds on the first PATH-reachable
// entry. When every entry is unusable the session refuses with that same
// information.
func ResolveAgentAssistanceInvocation(d *Deps, cfg *config.Config, override, agentCmd, prompt, runtimePath string) (*AgentAssistanceInvocation, error) {
	candidates := attendedLaunchCandidates(cfg, override)

	// Cooling is a best-effort pre-flight read. A missing Deps/FS seam — or a
	// store that will not open — must not refuse the session (ADR-0195 decision 6):
	// treat cooling as empty and keep walking on PATH alone. Never fall back to
	// defaultDeps here: under `go test` that points at the real machine store.
	var cooldowns map[string]time.Time
	if d != nil && d.FS != nil {
		if loaded, err := ActiveAgentCooldownsWith(d, time.Now().UTC()); err == nil {
			cooldowns = loaded
		}
	}

	var skips []string
	for _, spec := range candidates {
		preset, err := AgentPresetName(spec)
		if err != nil {
			return nil, err
		}
		if cooldowns != nil {
			if until, cooling := cooldowns[preset]; cooling {
				skips = append(skips, formatAttendedSkipCooling(preset, until))
				continue
			}
		}
		if !AgentExecutableAvailableWith(d, preset) {
			skips = append(skips, formatAttendedSkipMissingBinary(preset))
			continue
		}
		_, entryArgs, err := parseAgentPresetSpec(spec)
		if err != nil {
			return nil, err
		}
		adapter, err := ResolveAgentAdapter(spec)
		if err != nil {
			return nil, err
		}
		invocation, err := adapter.AssistanceInvocation(AgentAssistanceRequest{
			Prompt:      prompt,
			RuntimePath: runtimePath,
			EntryArgs:   entryArgs,
		})
		if err != nil {
			return nil, err
		}
		if len(skips) > 0 {
			invocation.Detail = strings.Join(skips, "; ") + "; " + invocation.Detail
		}
		return invocation, nil
	}
	if len(skips) == 0 {
		return nil, fmt.Errorf("no usable attended agent")
	}
	return nil, fmt.Errorf("no usable attended agent: %s", strings.Join(skips, "; "))
}

func formatAttendedSkipCooling(preset string, until time.Time) string {
	return fmt.Sprintf("skipped %s (cooling until %s)", preset, until.Local().Format(time.RFC3339))
}

func formatAttendedSkipMissingBinary(preset string) string {
	return fmt.Sprintf("skipped %s (binary missing)", preset)
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
		return "", fmt.Errorf("[agents.%s] output: %w", name, err)
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
	case AgentOutputKimiStreamJSON:
		result = normalizeKimiStreamJSON(raw)
	default:
		return AgentResult{Output: raw}
	}
	if result.Output == "" && result.ProceedVerdict == nil {
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
	if normalized.ProceedVerdict != nil {
		fmt.Fprintln(w, normalized.ProceedVerdict.Reason)
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
