package tasks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

func spendLoadConfigWithOverrides(rates map[string]config.SpendModelRate) func(string) (*config.Config, error) {
	return func(string) (*config.Config, error) {
		return &config.Config{
			Spend: &config.SpendConfig{ModelRates: rates},
		}, nil
	}
}

func TestSpendRollupDeclaredRatePricesComposer(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-composer", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base, cursorPricedEvents(1000, 500, 0, 0, "composer-2.5"))

	loadConfig := spendLoadConfigWithOverrides(map[string]config.SpendModelRate{
		"composer-2.5": {Prompt: "0.001", Completion: "0.002"},
	})
	result, err := SpendRollupWith(env.deps(), nil, loadConfig, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if !row.Notional.HasCost {
		t.Fatalf("expected declared rate to price composer, got %+v", row.Notional)
	}
	want := 1000*0.001 + 500*0.002
	if diff := row.Notional.Dollars - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("notional = %v, want %v", row.Notional.Dollars, want)
	}
	if row.RateSource != RateSourceOverride {
		t.Fatalf("rate_source = %q, want override", row.RateSource)
	}

	var buf bytes.Buffer
	RenderSpendRollup(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "1500 (*~$") {
		t.Fatalf("expected declared-rate cell marker, got:\n%s", out)
	}
	if strings.Contains(out, "1500 (~$") {
		t.Fatalf("declared rate must not render as published table rate:\n%s", out)
	}
}

func TestSpendRollupDeclaredRateWinsOverTable(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-override", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "claude", base, claudePricedEvents(1000, 0, 0, 0, "claude-opus-5"))

	loadConfig := spendLoadConfigWithOverrides(map[string]config.SpendModelRate{
		"anthropic/claude-opus-5": {Prompt: "1", Completion: "1"},
	})
	result, err := SpendRollupWith(env.deps(), nil, loadConfig, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Sets[0]
	if row.Notional.Dollars != 1000 {
		t.Fatalf("notional = %v, want override prompt rate 1 * 1000", row.Notional.Dollars)
	}
	if row.RateSource != RateSourceOverride {
		t.Fatalf("rate_source = %q, want override", row.RateSource)
	}
}

func TestSpendRollupJSONDeclaredRateSource(t *testing.T) {
	env := spendFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	setDir := registerSpendSet(t, env, "2026-06-10-json", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeSpendRun(t, setDir, "01-a.md", "01-a", "cursor", base, cursorPricedEvents(100, 0, 0, 0, "composer-2.5-fast"))

	loadConfig := spendLoadConfigWithOverrides(map[string]config.SpendModelRate{
		"composer-2.5-fast": {Prompt: "0.00001", Completion: "0.00002"},
	})
	result, err := SpendRollupWith(env.deps(), nil, loadConfig, SpendOptions{
		ResolveInput: ResolveInput{CWD: env.root},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RenderSpendRollupJSON(&buf, result); err != nil {
		t.Fatal(err)
	}
	var decoded spendRollupJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Sets[0]
	if got.RateSource == nil || *got.RateSource != RateSourceOverride {
		t.Fatalf("rate_source = %v, want override", got.RateSource)
	}
	if got.NotionalCostUSD == nil || *got.NotionalCostUSD <= 0 {
		t.Fatalf("notional_cost_usd = %v", got.NotionalCostUSD)
	}
}

func TestValidateSpendRateModelKeyRefusesUnknownModel(t *testing.T) {
	if err := validateSpendRateModelKey("totally-unknown-model"); err == nil {
		t.Fatal("expected refusal")
	}
	if err := validateSpendRateModelKey("composer-2.5"); err != nil {
		t.Fatalf("composer-2.5 should be known: %v", err)
	}
}

func TestSpendRateModelKnownIncludesTableAndComposer(t *testing.T) {
	if !SpendRateModelKnown("anthropic/claude-opus-5") {
		t.Fatal("table model should be known")
	}
	if !SpendRateModelKnown("composer-2.5") {
		t.Fatal("composer should be known")
	}
	if SpendRateModelKnown("never-seen-model") {
		t.Fatal("unknown model must not be known")
	}
}
