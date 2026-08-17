package tasks

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/glebglazov/pop/config"
)

func init() {
	config.RegisterSpendModelRateModelValidator(validateSpendRateModelKey)
}

// validateSpendRateModelKey refuses a model name pop has never run.
func validateSpendRateModelKey(model string) error {
	if _, ok := knownSpendRateModelKeys()[model]; ok {
		return nil
	}
	return unknownSpendRateModelError(model)
}

func unknownSpendRateModelError(model string) error {
	return fmt.Errorf("unknown model %q for a declared rate; pop has never run a model with that rate-table key", model)
}

var (
	knownSpendRateModelKeysOnce sync.Once
	knownSpendRateModelKeysMap  map[string]struct{}
)

func knownSpendRateModelKeys() map[string]struct{} {
	knownSpendRateModelKeysOnce.Do(buildKnownSpendRateModelKeys)
	return knownSpendRateModelKeysMap
}

func buildKnownSpendRateModelKeys() {
	keys := map[string]struct{}{}
	table, err := loadVendoredRateTable()
	if err == nil {
		for model := range table.Models {
			keys[model] = struct{}{}
		}
	}
	for preset, adapter := range agentAdapters {
		cap := adapter.RateKeyCapability()
		if cap.Normalize == nil {
			continue
		}
		normalize := cap.Normalize
		for _, raw := range adapter.Models() {
			recordKnownSpendRateModel(keys, normalize, raw)
		}
		ladder := adapter.EffortLadderCapability()
		for _, tier := range ladder.modelsForTier("heavy") {
			recordKnownSpendRateModel(keys, normalize, tier.Model)
		}
		for _, tier := range ladder.modelsForTier("standard") {
			recordKnownSpendRateModel(keys, normalize, tier.Model)
		}
		for _, tier := range ladder.modelsForTier("light") {
			recordKnownSpendRateModel(keys, normalize, tier.Model)
		}
		_ = preset
	}
	knownSpendRateModelKeysMap = keys
}

func recordKnownSpendRateModel(keys map[string]struct{}, normalize func(string) string, raw string) {
	if key := normalize(raw); key != "" {
		keys[key] = struct{}{}
	}
}

// loadDeclaredSpendRates reads merged [spend.model_rates] from config.
func loadDeclaredSpendRates(loadConfig func(string) (*config.Config, error)) map[string]ModelRates {
	if loadConfig == nil {
		loadConfig = config.Load
	}
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil || cfg == nil || cfg.Spend == nil || len(cfg.Spend.ModelRates) == 0 {
		return nil
	}
	out := make(map[string]ModelRates, len(cfg.Spend.ModelRates))
	for model, raw := range cfg.Spend.ModelRates {
		rates, err := modelRatesFromDeclared(raw)
		if err != nil {
			continue
		}
		out[model] = rates
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func modelRatesFromDeclared(raw config.SpendModelRate) (ModelRates, error) {
	return modelRatesFromSpendConfig(raw)
}

func modelRatesFromSpendConfig(raw config.SpendModelRate) (ModelRates, error) {
	jsonRates := rateTableRatesJSON{
		Prompt:     raw.Prompt,
		Completion: raw.Completion,
		CacheRead:  raw.CacheRead,
		CacheWrite: raw.CacheWrite,
	}
	return parseModelRates(jsonRates)
}

// KnownSpendRateModelKeys returns the sorted rate-table keys pop recognizes for
// declared overrides. Tests and completion may use it; the setter refuses any
// other name.
func KnownSpendRateModelKeys() []string {
	keys := make([]string, 0, len(knownSpendRateModelKeys()))
	for model := range knownSpendRateModelKeys() {
		keys = append(keys, model)
	}
	sort.Strings(keys)
	return keys
}

// SpendRateModelKnown reports whether model is a rate-table key pop recognizes.
func SpendRateModelKnown(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	_, ok := knownSpendRateModelKeys()[model]
	return ok
}
