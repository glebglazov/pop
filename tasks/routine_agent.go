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
		}
	}
	return result, nil
}

// RecordAgentQuotaCooldownFromReset stores a machine-global cooldown for one agent preset.
func RecordAgentQuotaCooldownFromReset(d *Deps, cfg *config.Config, preset string, resetAt time.Time) error {
	retryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return err
	}
	until := agentQuotaCooldownUntil(resetAt, time.Now(), retryAfter)
	return updateAgentCooldown(d, preset, until)
}
