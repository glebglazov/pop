package tasks

import (
	"time"

	"github.com/glebglazov/pop/config"
)

// runPlan is the resolved-config bundle shared by both Implement entry points
// (RunTaskSetWith and RunTaskWith). The implementing agent list and first preset,
// agent output mode, dirty-runtime strategy, commit-config overrides, quota
// retry-after, and effort validation are frozen when newRunPlan runs. A whole-set
// Drain re-reads only cfg each turn, so phase switches, Refiner and Verifier
// settings, gate rendering, and the next task's max-tries and retry delays can
// change without letting a malformed commit rule appear after commits begin.
// RunTaskWith has no Drain turn and keeps the snapshot it loaded at start.
//
// The cfg boundary belongs here because every live reader already reads through
// the plan, while every frozen value is stored beside it. The default attempt
// timeout remains a per-call resolution (resolveAttemptTimeout), since it
// depends on the caller's Timeout option, not on config.
type runPlan struct {
	cfg                  *config.Config
	baseAgentPresets     []string
	baseAgentPreset      string
	agentOutput          AgentOutputMode
	strategy             DirtyRuntimeStrategy
	commitOverrides      []string
	agentQuotaRetryAfter time.Duration
}

// reloadConfig re-reads the merged configuration for one Drain turn and touches
// nothing else on the plan, which is what separates it from reloading the plan:
// every field beside cfg is frozen for the whole Drain. A failed read keeps the
// previous snapshot, matching the gate menu's reload contract.
func (p *runPlan) reloadConfig(loadConfig func(string) (*config.Config, error)) error {
	cfg, err := loadConfigIfPresent(loadConfig)
	if err != nil {
		return err
	}
	p.cfg = cfg
	return nil
}

// runPlanInput carries the per-call agent/dirty options the constructor needs.
// It is decoupled from RunTaskSetOptions/RunTaskOptions so both entry points can
// feed the same fields without the plan depending on either options type.
type runPlanInput struct {
	agentPresets  []string
	agentPreset   string
	agentExplicit bool
	agentCmd      string
	agentOutput   AgentOutputMode
	allowDirty    DirtyRuntimeStrategy
}

// newRunPlan resolves the shared config bundle. Every failure is an ExitSetup
// error with the same message text and in the same order both entry points used
// inline, so a malformed [work.implement.git] entry (commit overrides) still fails before
// any commit could happen and setup-failure behavior stays byte-identical.
func newRunPlan(loadConfig func(string) (*config.Config, error), in runPlanInput) (*runPlan, error) {
	cfg, err := loadConfigIfPresent(loadConfig)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	baseAgentPresets := ResolveDefaultAgentPresets(in.agentPresets, in.agentPreset, in.agentExplicit, cfg)
	baseAgentPreset := baseAgentPresets[0]
	agentOutput := AgentOutputAuto
	if in.agentCmd == "" {
		agentOutput, err = resolveAgentOutputMode(loadConfig, baseAgentPreset, in.agentOutput)
		if err != nil {
			return nil, exitErr(ExitSetup, "%v", err)
		}
	}
	if err := validateDirtyRuntimeStrategy(in.allowDirty); err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	strategy := resolveDirtyRuntimeStrategy(in.allowDirty)

	// Resolve commit-config overrides up front (the lazy validation point) so a
	// malformed [work.implement.git] entry fails the drain hard before any commit —
	// including the per-task dirty-runtime checkpoint, which commits earliest.
	commitOverrides, err := resolveCommitConfigOverrides(loadConfig)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	agentQuotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	if _, err := cfg.EffortFor(baseAgentPreset); err != nil {
		return nil, exitErr(ExitSetup, "config: %v", err)
	}

	return &runPlan{
		cfg:                  cfg,
		baseAgentPresets:     baseAgentPresets,
		baseAgentPreset:      baseAgentPreset,
		agentOutput:          agentOutput,
		strategy:             strategy,
		commitOverrides:      commitOverrides,
		agentQuotaRetryAfter: agentQuotaRetryAfter,
	}, nil
}

// maxTries resolves the per-task attempt ceiling from the current config and the
// caller's --max-tries flag. Whole-set drains call it for the selected task;
// RunTaskWith calls it once after its pre-run confirmation.
func (p *runPlan) maxTries(explicit bool, flagValue int) (int, error) {
	return resolveImplementMaxTries(p.cfg, explicit, flagValue)
}

// retryDelays resolves the between-attempt delay schedule from config. Lazy for
// the same reason as maxTries.
func (p *runPlan) retryDelays() ([]time.Duration, error) {
	return resolveImplementAttemptRetryDelays(p.cfg)
}

// resolveAttemptTimeout applies the default attempt timeout when the caller did
// not set one. A non-positive option means "unset".
func resolveAttemptTimeout(optTimeout time.Duration) time.Duration {
	if optTimeout <= 0 {
		return DefaultAttemptTimeout
	}
	return optTimeout
}

func resolveAgentQuotaRetryAfter(cfg *config.Config) (time.Duration, error) {
	resolved, err := cfg.ResolveWorkDaemon()
	if err != nil {
		return 0, err
	}
	return resolved.AgentQuotaRetryAfter, nil
}

// resolveCommitConfigOverrides loads config and validates the commit-config
// overrides for the drain path. A nil loadConfig (or a load that fails to find
// a config file) yields no overrides — commits behave exactly as today. A
// malformed entry is returned as a hard error so the caller fails the drain.
func resolveCommitConfigOverrides(loadConfig func(string) (*config.Config, error)) ([]string, error) {
	if loadConfig == nil {
		return nil, nil
	}
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		// A missing/unreadable config is not a drain-stopping error here; the
		// rest of the run already tolerates it. Only a present-but-malformed
		// override entry must fail hard, which ResolveCommitConfigOverrides does.
		return nil, nil
	}
	return cfg.ResolveCommitConfigOverrides()
}
