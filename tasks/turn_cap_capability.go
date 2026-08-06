package tasks

import (
	"strconv"
	"strings"
)

// specTokens returns the argv tokens that tell this preset's CLI to stop after
// limit Turns, and nothing at all when the repository declares no cap or the
// adapter cannot be told about one — pop never claims a bound it cannot put on
// the command line (ADR-0190).
func (c AgentTurnCapEnforcementCapability) specTokens(limit int) []string {
	if limit <= 0 || c.Kind != CapabilitySupported || c.SpecTokens == nil {
		return nil
	}
	return c.SpecTokens(limit)
}

// argsContainTurnCap reports whether an augmented spec's own arguments already
// set this preset's cap, in which case the human's number wins and pop emits
// none of its own.
func (c AgentTurnCapEnforcementCapability) argsContainTurnCap(args []string) bool {
	if c.Contains != nil {
		return c.Contains(args)
	}
	return false
}

func claudeTurnCapSpecTokens(limit int) []string {
	return []string{"--max-turns", strconv.Itoa(limit)}
}

func claudeArgsContainTurnCap(args []string) bool {
	for _, arg := range args {
		if arg == "--max-turns" || strings.HasPrefix(arg, "--max-turns=") {
			return true
		}
	}
	return false
}
