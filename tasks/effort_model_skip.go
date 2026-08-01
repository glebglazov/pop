package tasks

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

// effortModelSkips is the set of Effort ladder entries recorded as unrunnable,
// keyed by preset and then by the `--model` token the ladder resolves
// (ADR-0168). It is seeded from the durable skip store when a task's attempts
// begin and grows as model-scoped verdicts land, so a re-resolution inside one
// task sees the skips that task just discovered alongside the ones earlier runs
// recorded.
type effortModelSkips map[string]map[string]bool

// loadEffortModelSkips reads the skips still in force at now. A machine with no
// store yet has none, which is the steady state for most drains.
func loadEffortModelSkips(d *Deps, now time.Time) (effortModelSkips, error) {
	rows, err := ActiveAgentModelCooldownsWith(d, now)
	if err != nil {
		return nil, err
	}
	skips := effortModelSkips{}
	for _, row := range rows {
		skips.add(row.Preset, row.Model)
	}
	return skips, nil
}

func (s effortModelSkips) add(preset, model string) {
	preset, model = strings.TrimSpace(preset), strings.TrimSpace(model)
	if preset == "" || model == "" {
		return
	}
	if s[preset] == nil {
		s[preset] = map[string]bool{}
	}
	s[preset][model] = true
}

func (s effortModelSkips) blocked(preset, model string) bool {
	return s[preset][model]
}

// models lists one preset's skipped models in a stable order, for the diagnostic
// an exhausted tier reports.
func (s effortModelSkips) models(preset string) []string {
	out := make([]string, 0, len(s[preset]))
	for model := range s[preset] {
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

// effortModelResolution is what one Agent fallback entry's Effort tier resolves
// to once recorded skips are filtered out.
type effortModelResolution struct {
	// Spec is the agent preset spec to invoke.
	Spec string
	// Model is the Effort ladder token Spec pins. Empty when no ladder resolved
	// one — a hand-pinned `--model`, an agent with no ladder, a task whose effort
	// is not explicit — which is also what tells the walk there is no tier to
	// step through.
	Model string
	// Exhausted reports that the tier has entries and every one of them is
	// skipped, so this preset can run nothing at this Effort.
	Exhausted bool
}

// effortSpecResolver resolves one Agent fallback entry's base spec against the
// Effort model skips recorded so far. The drain holds one for the whole task, so
// every restart re-resolves through the same policy with a longer skip list.
type effortSpecResolver func(baseSpec string, skips effortModelSkips) effortModelResolution

// newEffortSpecResolver binds a task's Effort to the config that resolves it. A
// custom `--agent-cmd` run resolves nothing: the command is the invocation.
func newEffortSpecResolver(agentCmd, effort string, effortExplicit bool, cfg *config.Config) effortSpecResolver {
	return func(baseSpec string, skips effortModelSkips) effortModelResolution {
		if strings.TrimSpace(agentCmd) != "" {
			return effortModelResolution{Spec: baseSpec}
		}
		return resolveEffortModel(baseSpec, effort, effortExplicit, cfg, skips)
	}
}

// resolveEffortModel rewrites an agent preset spec so its model and reasoning are
// pinned to the first entry of the [effort.<agent>] tier that is not recorded as
// skipped. It is the single resolution path: a nil skip set is the plain
// head-of-tier resolution ADR-0049 shipped.
func resolveEffortModel(agentSpec, effort string, effortExplicit bool, cfg *config.Config, skips effortModelSkips) effortModelResolution {
	unresolved := effortModelResolution{Spec: agentSpec}
	if !effortExplicit {
		return unresolved
	}
	if effort == "" {
		effort = DefaultTaskEffort
	}
	name, extraArgs, err := parseAgentPresetSpec(agentSpec)
	if err != nil {
		return unresolved
	}
	if name == "" {
		name = DefaultAgentPreset
	}
	// A hand-pinned model steps outside the ladder, so it skips the whole bundle
	// (ADR-0049) and with it the Effort model skip: there is no tier to walk.
	if agentArgsContainModel(extraArgs) {
		return unresolved
	}
	bundles := effortModelsForAgent(cfg, name, effort)
	if len(bundles) == 0 || strings.TrimSpace(bundles[0].Model) == "" {
		return unresolved
	}
	adapter := agentAdapters[name]
	for _, bundle := range bundles {
		token := effortModelTokenForAgent(name, bundle, adapter, extraArgs)
		if token == "" || skips.blocked(name, token) {
			continue
		}
		args := append([]string{name}, extraArgs...)
		args = append(args, "--model", token)
		if adapter != nil {
			reasoning := adapter.ReasoningCapability()
			if !reasoning.argsContainReasoning(extraArgs) {
				args = append(args, reasoning.specTokens(bundle.Reasoning)...)
			}
		}
		for i, arg := range args {
			args[i] = shellQuote(arg)
		}
		return effortModelResolution{Spec: strings.Join(args, " "), Model: token}
	}
	return effortModelResolution{Exhausted: true}
}

// effortModelWalk carries one preset's Effort tier through a task's attempts. It
// hands out the invocation builder for the tier's first unskipped entry and,
// when a model-scoped verdict retires that entry, records the skip and
// re-resolves onto the next one — the attempt restarts there without spending a
// try, and the shortening candidate list is the loop guard (ADR-0168). A preset
// whose model no ladder resolved walks a single entry: a refusal there is a
// preset-scoped stop, because there is no tier to step through.
type effortModelWalk struct {
	d        *Deps
	preset   string
	baseSpec string
	resolve  effortSpecResolver
	skips    effortModelSkips
	build    func(agentSpec string) (func(prompt string) (*AgentInvocation, error), error)
	// model is the ladder token the entry currently in hand pins, empty when no
	// ladder resolved one.
	model string
}

// builder resolves the tier's surviving head into this attempt's invocation
// builder. It returns a preset-scoped verdict instead when every entry is
// already skipped — the escalation that hands the turn to Agent fallback without
// spawning the preset at all.
func (w *effortModelWalk) builder() (func(prompt string) (*AgentInvocation, error), *AgentProceedVerdict, error) {
	resolution := w.resolve(w.baseSpec, w.skips)
	if resolution.Exhausted {
		v := NewTierExhaustedVerdict(w.preset, fmt.Sprintf("every effort tier model is skipped: %s", strings.Join(w.skips.models(w.preset), ", ")))
		return nil, &v, nil
	}
	w.model = resolution.Model
	build, err := w.build(resolution.Spec)
	if err != nil {
		return nil, nil, err
	}
	return build, nil, nil
}

// nextModel names the ladder token the tier would resolve now, for the line the
// orchestrator prints after retiring an entry. Empty when nothing survives or no
// ladder resolved a model.
func (w *effortModelWalk) nextModel() string {
	return w.resolve(w.baseSpec, w.skips).Model
}

// retire records the entry a model-scoped verdict condemns as a durable Effort
// model skip, so this task's next resolution and every later run filter it out.
// It reports the preset-scoped escalation when that leaves the tier with nothing
// to restart on, and nil when another entry survives.
func (w *effortModelWalk) retire(v AgentProceedVerdict) (*AgentProceedVerdict, error) {
	if w.model == "" {
		escalated := v.escalateToPreset()
		return &escalated, nil
	}
	if err := updateAgentModelCooldown(w.d, w.preset, w.model, v.ResetAt, v.Recovery == ProceedRecoveryPermanent); err != nil {
		return nil, err
	}
	w.skips.add(w.preset, w.model)
	if w.resolve(w.baseSpec, w.skips).Exhausted {
		escalated := v.escalateToPreset()
		return &escalated, nil
	}
	return nil, nil
}
