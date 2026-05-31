package workload

import (
	"fmt"
	"strings"
)

// Agent presets map names to command argument prefixes (prompt appended as final arg).
var agentPresets = map[string][]string{
	"claude":   {"claude", "--dangerously-skip-permissions", "-p"},
	"opencode": {"opencode", "run"},
	"cursor":   {"cursor", "agent", "--print", "--force", "--trust"},
	"codex":    {"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check"},
	"pi":       {"pi", "-p"},
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
	if agentCmd != "" {
		return "sh", []string{"-c", agentCmd + ` "$@"`, "workload-agent", prompt}, nil
	}
	if preset == "" {
		preset = "claude"
	}
	prefix, ok := agentPresets[preset]
	if !ok {
		return "", nil, fmt.Errorf("unknown agent preset %q; valid: %s", preset, strings.Join(ValidAgentPresets(), ", "))
	}
	args = append([]string{}, prefix...)
	if preset == "cursor" {
		args = append(args, "--workspace", runtimePath)
	}
	args = append(args, prompt)
	return args[0], args[1:], nil
}
