package tasks

import (
	"strings"
)

// RateKeySource names which model identifier supplied a run's Rate-table key
// (ADR-0218).
type RateKeySource string

const (
	// RateKeyFromActual means the key was derived from the run's Actual model.
	RateKeyFromActual RateKeySource = "actual"
	// RateKeyFromRequested means the adapter could not read an Actual model and
	// the key came from the model named in the Requested agent (or the run's
	// recorded pinned model).
	RateKeyFromRequested RateKeySource = "requested"
)

// RunRateKey is the Rate-table key resolved for one Captured run, and which of
// the two model routes supplied it. Key empty means the run is rate-blind —
// no normalisation rule, no model string, or no table entry (ADR-0218).
type RunRateKey struct {
	Key    string
	Source RateKeySource
}

// rateKeyEffortLevels is the closed set of effort/reasoning suffixes stripped
// from cursor model ids. Exact token match only — never a substring search
// against the Rate table.
var rateKeyEffortLevels = map[string]struct{}{
	"low": {}, "medium": {}, "high": {}, "max": {},
}

// claudeRateKey maps Claude's reported Actual model onto an OpenRouter id.
// Claude streams emit bare Anthropic names (e.g. claude-opus-5); the table is
// keyed with the anthropic/ vendor prefix.
func claudeRateKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Contains(model, "/") {
		return model
	}
	return "anthropic/" + model
}

// cursorRateKey maps cursor's model id onto an OpenRouter key.
//
// Stated transforms only (never a fuzzy or substring table match):
//  1. strip a trailing -thinking-<level> suffix
//  2. strip a leading cursor- vendor prefix
//  3. strip a trailing -<level> effort suffix
//  4. prefix anthropic/ for bare claude-* names; x-ai/ for bare grok-* names
//
// composer-* and any other unmatched id are returned unchanged so a missing
// table entry stays rate-blind rather than approximated.
func cursorRateKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	model = stripThinkingSuffix(model)
	model = strings.TrimPrefix(model, "cursor-")
	model = stripEffortSuffix(model)
	switch {
	case strings.HasPrefix(model, "claude-") && !strings.Contains(model, "/"):
		return "anthropic/" + model
	case strings.HasPrefix(model, "grok-") && !strings.Contains(model, "/"):
		return "x-ai/" + model
	default:
		return model
	}
}

// piRateKey maps pi's provider-namespaced model id onto the Rate table's
// vendor namespace. Only the opencode-go → table-vendor swaps confirmed against
// the live table are applied; everything else is returned unchanged.
func piRateKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	provider, name, ok := strings.Cut(model, "/")
	if !ok || provider != "opencode-go" {
		return model
	}
	switch {
	case strings.HasPrefix(name, "kimi-"):
		return "moonshotai/" + name
	case strings.HasPrefix(name, "deepseek-"):
		return "deepseek/" + name
	default:
		return model
	}
}

// codexRateKey maps a Codex model id onto an OpenRouter key. Codex streams
// carry no Actual model, so this only ever sees the invocation's pinned model;
// bare gpt-* names gain the openai/ vendor prefix.
func codexRateKey(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Contains(model, "/") {
		return model
	}
	return "openai/" + model
}

func stripThinkingSuffix(model string) string {
	const marker = "-thinking-"
	idx := strings.LastIndex(model, marker)
	if idx < 0 {
		return model
	}
	level := model[idx+len(marker):]
	if _, ok := rateKeyEffortLevels[level]; !ok {
		return model
	}
	return model[:idx]
}

func stripEffortSuffix(model string) string {
	idx := strings.LastIndex(model, "-")
	if idx <= 0 {
		return model
	}
	if _, ok := rateKeyEffortLevels[model[idx+1:]]; !ok {
		return model
	}
	return model[:idx]
}

// resolveRunRateKey derives the Rate-table key for one Captured run.
// Prefer the Actual model; when that is unreadable, fall back to the model
// named in the Requested agent (or the run's recorded pinned model). An
// adapter with no normalisation rule, or a model that matches no table entry,
// yields an empty key — rate-blind, never guessed (ADR-0218).
func resolveRunRateKey(run capturedRun, table *RateTable) RunRateKey {
	adapter, ok := agentAdapters[run.meta.Agent]
	if !ok {
		return RunRateKey{}
	}
	normalize := adapter.RateKeyCapability().Normalize
	if normalize == nil {
		return RunRateKey{}
	}

	if actual := extractActualModel(run.meta.Agent, run.events); actual != "" {
		key := normalize(actual)
		if _, found := table.lookup(key); found {
			return RunRateKey{Key: key, Source: RateKeyFromActual}
		}
		// Actual model was readable but not priceable — do not fall back to the
		// requested model and invent a different figure (ADR-0218).
		return RunRateKey{}
	}

	requested := requestedModelForRate(run)
	if requested == "" {
		return RunRateKey{}
	}
	key := normalize(requested)
	if _, found := table.lookup(key); found {
		return RunRateKey{Key: key, Source: RateKeyFromRequested}
	}
	return RunRateKey{}
}

func requestedModelForRate(run capturedRun) string {
	if m := AgentSpecModel(run.meta.RequestedAgent); m != "" {
		return m
	}
	return strings.TrimSpace(run.meta.Model)
}
