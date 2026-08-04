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

// AttendedAgentSettings is one preset's user-owned attended overrides, read from
// [agents.<preset>] (ADR-0187). It is what an attended launch consults instead of
// the extra arguments of an --agent spec: an attended session takes the preset
// name from the implement agent list and nothing else.
type AttendedAgentSettings struct {
	// Args replaces the adapter's declared attended arguments wholesale when
	// ArgsSet is true — including replacing them with nothing, which is how a user
	// asks for a bare interactive binary.
	Args    []string
	ArgsSet bool
	// Model is named on the attended command as `--model <value>`. Empty means pop
	// names no model and the agent's own configuration decides.
	Model string
}

// attendedAgentSettingsFor reads one preset's [agents.<preset>] block.
func attendedAgentSettingsFor(cfg *config.Config, preset string) AttendedAgentSettings {
	block := cfg.AgentSettingsFor(preset)
	settings := AttendedAgentSettings{Model: strings.TrimSpace(block.AttendedModel)}
	if block.AttendedArgs != nil {
		settings.Args = append([]string{}, (*block.AttendedArgs)...)
		settings.ArgsSet = true
	}
	return settings
}

// attendedArgsWith resolves the argument list an attended launch carries: the
// user's list when they set one, otherwise the adapter's declared default. The
// user's list replaces rather than appends, the deliberate attended exception to
// ADR-0017's flags-come-last rule (ADR-0187).
func (s AttendedAgentSettings) attendedArgsWith(declared AgentAttendedArgsCapability) []string {
	if s.ArgsSet {
		return append([]string{}, s.Args...)
	}
	return declared.attendedArgs()
}

// modelArgs returns the model flag an attended command carries, or nothing when
// the user named no model.
func (s AttendedAgentSettings) modelArgs() []string {
	if strings.TrimSpace(s.Model) == "" {
		return nil
	}
	return []string{"--model", strings.TrimSpace(s.Model)}
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
