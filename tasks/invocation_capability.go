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
