package tasks

import (
	"os/exec"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

func (c AgentQuotaResetCapability) resetAt(reason string, now time.Time) time.Time {
	if c.Kind != CapabilitySupported || c.ResetAt == nil {
		return time.Time{}
	}
	return c.ResetAt(reason, now)
}

func (c AgentEffortLadderCapability) modelsForTier(effort string) []config.EffortModel {
	if c.Kind != CapabilitySupported || c.Ladder == nil {
		return nil
	}
	return c.Ladder[effort]
}

// attendedArgs returns what this preset adds to an attended launch. A Blind
// preset has no auto-approval flag of its own, so it contributes nothing and its
// interactive command stays byte-identical to the bare binary (ADR-0187).
func (c AgentAttendedArgsCapability) attendedArgs() []string {
	if c.Kind != CapabilitySupported {
		return nil
	}
	return append([]string{}, c.Args...)
}

// attendedArgsBeside returns the declared posture arguments an attended launch
// still needs beside the entry's own: every declared flag the entry does not
// already name, with the values that belong to it. A flag the entry names wins
// outright, which is how the human sitting at the terminal keeps the last word
// on their permission posture without a config key for it (ADR-0195). The match
// is by exact flag name — no preset declares more than two flags, so there is
// nothing here to guess at.
func (c AgentAttendedArgsCapability) attendedArgsBeside(entryArgs []string) []string {
	declared := c.attendedArgs()
	if len(declared) == 0 {
		return nil
	}
	named := namedFlags(entryArgs)
	var out []string
	keep := false
	for _, arg := range declared {
		if flag, isFlag := flagName(arg); isFlag {
			keep = !named[flag]
		}
		if keep {
			out = append(out, arg)
		}
	}
	return out
}

// namedFlags is the set of flag names an argument list names, in either the
// `--flag value` or the `--flag=value` spelling.
func namedFlags(args []string) map[string]bool {
	named := make(map[string]bool, len(args))
	for _, arg := range args {
		if flag, ok := flagName(arg); ok {
			named[flag] = true
		}
	}
	return named
}

// flagName reports the flag one argument names, stripping any `=value` tail. A
// non-flag argument (a value, a positional) reports false.
func flagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return "", false
	}
	name, _, _ := strings.Cut(arg, "=")
	return name, true
}

func (c AgentExecutableCapability) executableName() string {
	if c.Kind != CapabilitySupported {
		return ""
	}
	return strings.TrimSpace(c.Name)
}

// AgentExecutableAvailable reports whether a preset's CLI executable is on PATH.
func AgentExecutableAvailable(preset string) bool {
	adapter, err := ResolveAgentAdapter(strings.ToLower(preset))
	if err != nil {
		return false
	}
	name := adapter.ExecutableCapability().executableName()
	if name == "" {
		return false
	}
	_, err = exec.LookPath(name)
	return err == nil
}

func (c AgentAvailabilityProbeCapability) matchesProbeInvocation(name string, args []string) bool {
	if !c.Available() || c.Command == nil {
		return false
	}
	if c.Command.Name != name {
		return false
	}
	id := c.IdentifyingArgs
	if len(id) == 0 {
		id = c.Command.Args
	}
	if len(args) < len(id) {
		return false
	}
	for i, want := range id {
		if args[i] != want {
			return false
		}
	}
	return true
}
