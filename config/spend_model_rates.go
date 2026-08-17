package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is `pop config spend model-rate set` and its read-back: the declared
// notional rates a human states for models the published Rate table cannot
// cover (ADR-0218). Stating one is a human stating intent, so it lands in the
// override layer rather than in a gap-filler pop wrote for itself (ADR-0212
// decision 5).

const spendModelRatesPrefix = "spend.model_rates."

// spendModelRateModelValidator is registered from tasks at init so config can
// refuse a model pop has never run without importing tasks.
var spendModelRateModelValidator func(string) error

// RegisterSpendModelRateModelValidator records the Spend lens's model-name
// validator. tasks registers at init; tests may replace it temporarily.
func RegisterSpendModelRateModelValidator(fn func(string) error) {
	spendModelRateModelValidator = fn
}

// SetSpendModelRate states the four per-token rates for one model's declared
// override. It lands in config.override.toml under spend.model_rates.<model>
// and beats any hand-authored value for that model at the next config load.
func SetSpendModelRate(model string, rates SpendModelRate) error {
	return SetSpendModelRateWith(defaultDeps, model, rates)
}

// SetSpendModelRateWith is the injectable variant.
func SetSpendModelRateWith(d *Deps, model string, rates SpendModelRate) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model must not be empty")
	}
	if spendModelRateModelValidator != nil {
		if err := spendModelRateModelValidator(model); err != nil {
			return err
		}
	}
	if problem := spendModelRateProblem(model, rates); problem != "" {
		return overrideRefusal(problem)
	}
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	key := spendModelRatesPrefix + model
	if err := setDocumentValue(doc, key, spendModelRateDocument(rates)); err != nil {
		return err
	}
	return file.save(d, doc)
}

// DeleteSpendModelRate removes one model's declared override so the Spend lens
// falls back to the published Rate table alone for that model.
func DeleteSpendModelRate(model string) error {
	return DeleteSpendModelRateWith(defaultDeps, model)
}

// DeleteSpendModelRateWith is the injectable variant.
func DeleteSpendModelRateWith(d *Deps, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model must not be empty")
	}
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	if !deleteDocumentValue(doc, spendModelRatesPrefix+model) {
		return nil
	}
	return file.save(d, doc)
}

func spendModelRateDocument(rates SpendModelRate) map[string]any {
	doc := map[string]any{
		"prompt":     rates.Prompt,
		"completion": rates.Completion,
	}
	if rates.CacheRead != "" {
		doc["cache_read"] = rates.CacheRead
	}
	if rates.CacheWrite != "" {
		doc["cache_write"] = rates.CacheWrite
	}
	return doc
}

// spendModelRateProblem reports why rates cannot be stored for model, in the
// words a load of that value would use, and "" when it can.
func spendModelRateProblem(model string, rates SpendModelRate) string {
	if _, err := parseSpendModelRate(rates); err != nil {
		return fmt.Sprintf("spend.model_rates.%s: %v", model, err)
	}
	var cfg Config
	buffer := renderKeyTOML(spendModelRatesPrefix+model, spendModelRateDocument(rates))
	md, err := toml.Decode(buffer, &cfg)
	if err != nil {
		return fmt.Sprintf("spend.model_rates.%s cannot hold this value: %v", model, err)
	}
	for _, finding := range spendModelRateFindings(&cfg, md) {
		if !findingSpeaksAbout(finding, spendModelRatesPrefix+model) {
			continue
		}
		return "Loading this value would report: " + finding.Message
	}
	return ""
}

func spendModelRateFindings(cfg *Config, _ toml.MetaData) []Finding {
	return spendModelRateEntryFindings(overrideBufferLabel, cfg.Spend)
}

func spendModelRateEntryFindings(path string, spend *SpendConfig) []Finding {
	if spend == nil || len(spend.ModelRates) == 0 {
		return nil
	}
	var findings []Finding
	for model, raw := range spend.ModelRates {
		if _, err := parseSpendModelRate(raw); err != nil {
			findings = append(findings, Finding{
				Path:    spendModelRatesPrefix + model,
				Message: fmt.Sprintf("%s: %v", path, err),
			})
		}
	}
	return findings
}

// parseSpendModelRate reads the four rate strings a declared override stores.
func parseSpendModelRate(raw SpendModelRate) (SpendModelRate, error) {
	prompt, err := parseSpendRateString(raw.Prompt, "prompt")
	if err != nil {
		return SpendModelRate{}, err
	}
	completion, err := parseSpendRateString(raw.Completion, "completion")
	if err != nil {
		return SpendModelRate{}, err
	}
	out := SpendModelRate{Prompt: prompt, Completion: completion}
	if raw.CacheRead != "" {
		v, err := parseSpendRateString(raw.CacheRead, "cache_read")
		if err != nil {
			return SpendModelRate{}, err
		}
		out.CacheRead = v
	}
	if raw.CacheWrite != "" {
		v, err := parseSpendRateString(raw.CacheWrite, "cache_write")
		if err != nil {
			return SpendModelRate{}, err
		}
		out.CacheWrite = v
	}
	return out, nil
}

func parseSpendRateString(s, field string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%s: empty rate", field)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("%s: want a per-token rate, got %q", field, s)
	}
	if v < 0 {
		return "", fmt.Errorf("%s: want a non-negative rate, got %g", field, v)
	}
	return strconv.FormatFloat(v, 'f', -1, 64), nil
}

// ParseSpendModelRateCLI reads prompt and completion plus optional cache rates
// off the command line for `pop config spend model-rate set`.
func ParseSpendModelRateCLI(args []string) (SpendModelRate, error) {
	if len(args) < 2 || len(args) > 4 {
		return SpendModelRate{}, fmt.Errorf("want prompt and completion rates, with optional cache_read and cache_write")
	}
	rates := SpendModelRate{Prompt: args[0], Completion: args[1]}
	if len(args) > 2 {
		rates.CacheRead = args[2]
	}
	if len(args) > 3 {
		rates.CacheWrite = args[3]
	}
	return parseSpendModelRate(rates)
}
