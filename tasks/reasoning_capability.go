package tasks

import (
	"fmt"
	"os"
	"strings"
)

func (c AgentReasoningCapability) specTokens(reasoning string) []string {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" || c.Kind != CapabilitySupported || c.SpecTokens == nil {
		return nil
	}
	if c.EnvKey != "" {
		if _, handSet := os.LookupEnv(c.EnvKey); handSet {
			return nil
		}
	}
	return c.SpecTokens(reasoning)
}

func (c AgentReasoningCapability) argsContainReasoning(args []string) bool {
	if c.Contains != nil {
		return c.Contains(args)
	}
	return false
}

func claudeReasoningSpecTokens(reasoning string) []string {
	return []string{"--effort", reasoning}
}

func claudeArgsContainReasoning(args []string) bool {
	for _, arg := range args {
		if arg == "--effort" || strings.HasPrefix(arg, "--effort=") {
			return true
		}
	}
	return false
}

func codexReasoningSpecTokens(reasoning string) []string {
	return []string{"-c", fmt.Sprintf(`model_reasoning_effort="%s"`, reasoning)}
}

func codexArgsContainReasoning(args []string) bool {
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
	return false
}

func isCodexReasoningConfig(arg string) bool {
	key, _, found := strings.Cut(strings.TrimSpace(arg), "=")
	return found && strings.TrimSpace(key) == "model_reasoning_effort"
}

func cursorArgsContainReasoning(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "[") && strings.Contains(arg, "]") && strings.Contains(arg, "effort=") {
			return true
		}
	}
	return false
}

func piReasoningSpecTokens(reasoning string) []string {
	return []string{"--thinking", reasoning}
}

func piArgsContainReasoning(args []string) bool {
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
	return false
}

func piModelTokenContainsThinking(arg string) bool {
	model, thinking, found := strings.Cut(strings.TrimSpace(arg), ":")
	return found && strings.TrimSpace(model) != "" && strings.TrimSpace(thinking) != ""
}

func kimiReasoningSpecTokens(reasoning string) []string {
	return []string{"KIMI_MODEL_THINKING_EFFORT=" + reasoning}
}

func kimiArgsContainReasoning(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "KIMI_MODEL_THINKING_EFFORT=") {
			return true
		}
	}
	return false
}
