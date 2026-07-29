package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// agentAvailabilityProbeTimeout bounds a single availability probe. Unknown on
// expiry so a slow CLI never blocks real work (ADR-0153). A variable so tests
// can shorten the wait.
var agentAvailabilityProbeTimeout = 5 * time.Second

// AgentAvailabilityProbeCapability describes a preset's optional read-only auth
// probe command and how to interpret its output. Presets with no status readout
// ship an empty capability.
type AgentAvailabilityProbeCapability struct {
	Command     *AgentCommand
	Interpret   func(exitCode int, output string) *AgentUnavailability
}

// Available reports whether this preset ships an availability probe.
func (c AgentAvailabilityProbeCapability) Available() bool {
	return c.Command != nil && c.Command.Name != "" && c.Interpret != nil
}

// AgentAuthenticationStatus is the doctor-facing outcome of an availability probe.
type AgentAuthenticationStatus int

const (
	AgentAuthAuthenticated AgentAuthenticationStatus = iota
	AgentAuthUnauthenticated
	AgentAuthUnknown
	AgentAuthCannotDetermine
)

// AgentAuthenticationProbe reports one preset's authentication status for doctor.
type AgentAuthenticationProbe struct {
	Status AgentAuthenticationStatus
	Detail string
}

// ProbeAgentAuthentication runs a preset's availability probe for doctor. It execs
// the probe command when the preset ships one; presets without a probe report
// cannot determine rather than a failure.
func ProbeAgentAuthentication(d *Deps, runtimePath, preset string) AgentAuthenticationProbe {
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return AgentAuthenticationProbe{
			Status: AgentAuthCannotDetermine,
			Detail: "pop cannot determine authentication (unrecognized preset)",
		}
	}
	capability := adapter.AvailabilityProbeCapability()
	if !capability.Available() {
		return AgentAuthenticationProbe{
			Status: AgentAuthCannotDetermine,
			Detail: "pop cannot determine authentication (no availability probe)",
		}
	}
	status, detail := evaluateAgentAvailabilityProbe(d, runtimePath, preset, capability)
	return AgentAuthenticationProbe{Status: status, Detail: detail}
}

// agentAvailabilityProbeMemo records one-way probe results for a single
// Implement or Verifier run: a preset marked unavailable stays skipped; every
// other preset is probed at most once.
type agentAvailabilityProbeMemo struct {
	skipped map[string]AgentUnavailability
	probed  map[string]struct{}
}

func newAgentAvailabilityProbeMemo() *agentAvailabilityProbeMemo {
	return &agentAvailabilityProbeMemo{
		skipped: make(map[string]AgentUnavailability),
		probed:  make(map[string]struct{}),
	}
}

// checkUnavailability returns a human-healing auth verdict when a prior probe or
// a fresh probe marks the preset unavailable. Nil means proceed (authenticated,
// unknown, or no probe).
func (m *agentAvailabilityProbeMemo) checkUnavailability(d *Deps, runtimePath, preset string) *AgentUnavailability {
	if m == nil {
		return nil
	}
	if u, ok := m.skipped[preset]; ok {
		cp := u
		return &cp
	}
	if _, ok := m.probed[preset]; ok {
		return nil
	}
	m.probed[preset] = struct{}{}

	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return nil
	}
	capability := adapter.AvailabilityProbeCapability()
	if !capability.Available() {
		return nil
	}
	if u := runAgentAvailabilityProbe(d, runtimePath, preset, capability); u != nil {
		m.skipped[preset] = *u
		return u
	}
	return nil
}

func runAgentAvailabilityProbe(d *Deps, runtimePath, preset string, capability AgentAvailabilityProbeCapability) *AgentUnavailability {
	status, detail := evaluateAgentAvailabilityProbe(d, runtimePath, preset, capability)
	if status != AgentAuthUnauthenticated {
		return nil
	}
	u := DetectedAuthFailure(detail).WithPreset(preset)
	return &u
}

func evaluateAgentAvailabilityProbe(d *Deps, runtimePath, preset string, capability AgentAvailabilityProbeCapability) (AgentAuthenticationStatus, string) {
	exitCode, combined, ok := execAgentAvailabilityProbe(d, runtimePath, capability)
	if !ok {
		return AgentAuthUnknown, "authentication status unknown"
	}
	if u := capability.Interpret(exitCode, combined); u != nil {
		return AgentAuthUnauthenticated, u.Reason
	}
	if probeReportsExplicitlyAuthenticated(preset, exitCode, combined) {
		return AgentAuthAuthenticated, "authenticated"
	}
	return AgentAuthUnknown, "authentication status unknown"
}

func execAgentAvailabilityProbe(d *Deps, runtimePath string, capability AgentAvailabilityProbeCapability) (exitCode int, combined string, ok bool) {
	if d == nil || d.Runner == nil || !capability.Available() {
		return 0, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentAvailabilityProbeTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	exitCode, err := d.Runner.Run(ctx, runtimePath, &stdout, &stderr, capability.Command.Name, capability.Command.Args...)
	if err != nil {
		return 0, "", false
	}
	combined = strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		if combined != "" {
			combined += "\n"
		}
		combined += strings.TrimSpace(stderr.String())
	}
	return exitCode, combined, true
}

func probeReportsExplicitlyAuthenticated(preset string, exitCode int, output string) bool {
	switch preset {
	case "cursor":
		if exitCode != 0 {
			return false
		}
		var status struct {
			IsAuthenticated *bool `json:"isAuthenticated"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
			return false
		}
		return status.IsAuthenticated != nil && *status.IsAuthenticated
	case "claude":
		if exitCode != 0 {
			return false
		}
		var status struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
			return false
		}
		return status.LoggedIn != nil && *status.LoggedIn
	case "codex":
		return exitCode == 0
	default:
		return false
	}
}

func interpretCursorAvailabilityProbe(exitCode int, output string) *AgentUnavailability {
	if exitCode != 0 {
		return nil
	}
	var status struct {
		IsAuthenticated *bool `json:"isAuthenticated"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
		return nil
	}
	if status.IsAuthenticated == nil {
		return nil
	}
	if *status.IsAuthenticated {
		return nil
	}
	reason := strings.TrimSpace(output)
	if reason == "" {
		reason = "cursor-agent status: not authenticated"
	}
	return DetectedAuthFailure(reason)
}

func interpretClaudeAvailabilityProbe(exitCode int, output string) *AgentUnavailability {
	if exitCode != 0 {
		return nil
	}
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
		return nil
	}
	if status.LoggedIn == nil {
		return nil
	}
	if *status.LoggedIn {
		return nil
	}
	reason := strings.TrimSpace(output)
	if reason == "" {
		reason = "claude auth status: not logged in"
	}
	return DetectedAuthFailure(reason)
}

func interpretCodexAvailabilityProbe(exitCode int, output string) *AgentUnavailability {
	// Only exit 0 is an explicit positive; non-zero reads as unknown (ADR-0153).
	_ = output
	if exitCode == 0 {
		return nil
	}
	return nil
}

// IsAgentAvailabilityProbeCommand reports whether name/args name a probe
// invocation rather than a headless agent attempt.
func IsAgentAvailabilityProbeCommand(name string, args []string) bool {
	switch name {
	case "claude":
		return len(args) >= 2 && args[0] == "auth" && args[1] == "status"
	case "cursor-agent":
		return len(args) >= 1 && args[0] == "status"
	case "codex":
		return len(args) >= 2 && args[0] == "login" && args[1] == "status"
	default:
		return false
	}
}
