package routine

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// ResolveRoutineAgentPresets returns the ordered agent preset list for a Routine run.
// [routines].agents wins when set; otherwise resolution falls through to
// [tasks.implement].agents and the built-in default agent.
func ResolveRoutineAgentPresets(cfg *config.Config) []string {
	if cfg != nil && cfg.Routines != nil {
		if agents := nonEmptyAgentSpecs(cfg.Routines.Agents); len(agents) > 0 {
			return agents
		}
	}
	return tasks.ResolveDefaultAgentPresets(nil, "", false, cfg)
}

func nonEmptyAgentSpecs(specs []string) []string {
	var out []string
	for _, spec := range specs {
		if strings.TrimSpace(spec) != "" {
			out = append(out, spec)
		}
	}
	return out
}

type routineAgentAttemptFunc func(agentSpec string) (*tasks.RoutineAgentAttempt, error)

// runRoutineWithAgentFallback walks the resolved agent list with implement-style
// quota fall-through, sharing the machine-global cooldown store.
func runRoutineWithAgentFallback(
	d *Deps,
	cfg *config.Config,
	runtimePath string,
	out io.Writer,
	prompt string,
	attempt routineAgentAttemptFunc,
) (*tasks.RoutineAgentAttempt, string, error) {
	specs := ResolveRoutineAgentPresets(cfg)
	taskDeps := d.taskDeps()
	cooldowns, err := tasks.ActiveAgentCooldownsWith(taskDeps, time.Now().UTC())
	if err != nil {
		return nil, "", fmt.Errorf("read agent cooldowns: %w", err)
	}
	var quotaAttempts []*tasks.RoutineAgentAttempt
	for i, agentSpec := range specs {
		preset, err := tasks.AgentPresetName(agentSpec)
		if err != nil {
			return nil, "", fmt.Errorf("resolve agent preset: %w", err)
		}
		if until, cooling := cooldowns[preset]; cooling {
			quotaAttempts = append(quotaAttempts, &tasks.RoutineAgentAttempt{
				QuotaPaused:  true,
				QuotaPreset:  preset,
				QuotaResetAt: until,
			})
			continue
		}
		result, err := attempt(agentSpec)
		if err != nil {
			return nil, preset, err
		}
		if result == nil {
			return nil, "", fmt.Errorf("agent %q returned no result", agentSpec)
		}
		if !result.QuotaPaused {
			return result, preset, nil
		}
		if err := tasks.RecordAgentQuotaCooldownFromReset(taskDeps, cfg, result.QuotaPreset, result.QuotaResetAt); err != nil {
			return nil, "", fmt.Errorf("record agent cooldown: %w", err)
		}
		cooldowns[result.QuotaPreset] = result.QuotaResetAt
		quotaAttempts = append(quotaAttempts, result)
		if out != nil && i+1 < len(specs) {
			fmt.Fprintf(out, "Agent %s quota-paused; trying next\n", result.QuotaPreset)
		}
	}
	if len(quotaAttempts) == 0 {
		return nil, "", fmt.Errorf("no agent attempts were run")
	}
	best := quotaAttempts[0]
	for _, attempt := range quotaAttempts[1:] {
		if attempt != nil && !attempt.QuotaResetAt.IsZero() &&
			(best.QuotaResetAt.IsZero() || attempt.QuotaResetAt.Before(best.QuotaResetAt)) {
			best = attempt
		}
	}
	return best, best.QuotaPreset, fmt.Errorf("all configured agents are quota-paused")
}
