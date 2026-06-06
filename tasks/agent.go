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

// Agent presets map names to command argument prefixes (prompt appended as final arg).
var agentPresets = map[string][]string{
	"claude":   {"claude", "--dangerously-skip-permissions", "-p"},
	"opencode": {"opencode", "run"},
	"cursor":   {"cursor-agent", "-p", "--force", "--trust"},
	"codex":    {"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
	"pi":       {"pi", "-p", "--no-extensions", "--no-skills"},
}

// ValidAgentPresets returns sorted preset names.
func ValidAgentPresets() []string {
	names := make([]string, 0, len(agentPresets))
	for name := range agentPresets {
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
		return &AgentInvocation{
			Name:         "sh",
			Args:         []string{"-c", agentCmd + ` "$@"`, "task-agent", prompt},
			OutputFormat: AgentOutputPlain,
		}, nil
	}
	if preset == "" {
		preset = "claude"
	}
	prefix, ok := agentPresets[preset]
	if !ok {
		return nil, fmt.Errorf("unknown agent preset %q; valid: %s", preset, strings.Join(ValidAgentPresets(), ", "))
	}
	args := append([]string{}, prefix...)
	format := AgentOutputPlain
	if mode == AgentOutputAuto {
		switch preset {
		case "claude":
			args = append(args, "--output-format", "stream-json", "--verbose")
			format = AgentOutputClaudeStreamJSON
		case "cursor":
			args = append(args, "--output-format", "stream-json")
			format = AgentOutputCursorStreamJSON
		case "codex":
			args = append(args, "--json")
			format = AgentOutputCodexJSONL
		case "opencode":
			args = append(args, "--format", "json")
			format = AgentOutputOpenCodeJSON
		case "pi":
			args = append(args, "--mode", "json")
			format = AgentOutputPiJSONL
		}
	}
	if preset == "cursor" {
		if mode == AgentOutputText {
			args = append(args, "--output-format", "text")
		}
		args = append(args, "--workspace", runtimePath)
	}
	args = append(args, prompt)
	return &AgentInvocation{Name: args[0], Args: args[1:], OutputFormat: format}, nil
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

// RenderAgentOutput writes normalized agent text without dumping structured events.
func RenderAgentOutput(w io.Writer, format AgentOutputFormat, raw string) {
	if format == AgentOutputPlain {
		_, _ = io.Copy(w, bytes.NewBufferString(raw))
		return
	}
	normalized := NormalizeAgentOutput(format, raw)
	if normalized.QuotaPause != nil {
		fmt.Fprintln(w, normalized.QuotaPause.Reason)
		return
	}
	if normalized.Output != "" {
		fmt.Fprint(w, normalized.Output)
	}
}
