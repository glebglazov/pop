package tasks

import (
	"fmt"
	"io"
	"time"

	"github.com/glebglazov/pop/config"
)

// RoutineAgentAttempt is the outcome of one headless agent invocation for a Routine run.
type RoutineAgentAttempt struct {
	ExitCode     int
	Output       string
	QuotaPaused  bool
	QuotaPreset  string
	QuotaResetAt time.Time
	QuotaReason  string
	// QuotaCooldown is what the refusal behind this attempt asks the cooldown
	// store to record. It is kept beside QuotaResetAt rather than derived from
	// it because the two answer different questions: QuotaResetAt is when this
	// run may resume, while the request also says whether any provider stated
	// that instant (ADR-0235).
	QuotaCooldown AgentQuotaCooldownRequest
}

// RunRoutineAgentInvocation runs one headless agent invocation in runtimePath.
func RunRoutineAgentInvocation(d *Deps, runtimePath string, liveOut io.Writer, timeout time.Duration, agentSpec, prompt string) (*RoutineAgentAttempt, error) {
	invocation, err := ResolveAgentInvocation(agentSpec, "", prompt, runtimePath)
	if err != nil {
		return nil, fmt.Errorf("resolve agent %q: %w", agentSpec, err)
	}
	raw, outcome, err := runAgentAttempt(d, runtimePath, liveOut, timeout, invocation)
	if err != nil {
		return nil, err
	}
	result := &RoutineAgentAttempt{}
	if outcome != nil {
		result.ExitCode = outcome.exitCode
	}
	normalized := invocation.NormalizeOutput(raw)
	result.Output = normalized.Output
	if normalized.ProceedVerdict != nil {
		v := normalized.ProceedVerdict.WithPreset(invocation.AgentPreset())
		if _, ok := v.TimeHealing(); ok {
			v = resolveProceedResetAt(v, time.Now())
		}
		result.QuotaPaused = v.pausesUntilReset()
		result.QuotaPreset = v.Preset
		result.QuotaReason = v.Reason
		if th, ok := v.TimeHealing(); ok {
			result.QuotaResetAt = th.ResetAt
			result.QuotaCooldown = quotaCooldownRequest(v)
		}
	}
	return result, nil
}

// RecordAgentQuotaCooldown stores a machine-global cooldown for one agent preset
// and returns the expiry now in force, which is the standing row when this
// refusal was a guess against a cooldown that has not elapsed (ADR-0235).
func RecordAgentQuotaCooldown(d *Deps, cfg *config.Config, req AgentQuotaCooldownRequest) (time.Time, error) {
	unclassed, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return time.Time{}, err
	}
	return recordAgentQuotaCooldown(d, req, time.Now(), unclassed)
}
