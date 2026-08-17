package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSetSpendModelRateWritesOverrideLayer(t *testing.T) {
	fx := newPopRepoFixture(t)
	RegisterSpendModelRateModelValidator(func(string) error { return nil })
	t.Cleanup(func() { RegisterSpendModelRateModelValidator(nil) })

	rates := SpendModelRate{Prompt: "0.000001", Completion: "0.000002"}
	if err := SetSpendModelRateWith(fx.d, "composer-2.5", rates); err != nil {
		t.Fatalf("SetSpendModelRateWith: %v", err)
	}
	body, err := os.ReadFile(fx.overridePath)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"composer-2.5",
		`prompt = "0.000001"`,
		`completion = "0.000002"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("override layer missing %q:\n%s", want, text)
		}
	}
}

func TestSetSpendModelRateRefusesUnknownModel(t *testing.T) {
	fx := newPopRepoFixture(t)
	RegisterSpendModelRateModelValidator(func(model string) error {
		if model == "composer-2.5" {
			return nil
		}
		return fmt.Errorf("unknown model %q for a declared rate; pop has never run a model with that rate-table key", model)
	})
	t.Cleanup(func() { RegisterSpendModelRateModelValidator(nil) })

	err := SetSpendModelRateWith(fx.d, "never-seen-model", SpendModelRate{
		Prompt: "0.000001", Completion: "0.000002",
	})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if !strings.Contains(err.Error(), "never-seen-model") {
		t.Fatalf("error = %v, want the model named", err)
	}
	if _, statErr := os.Stat(fx.overridePath); !os.IsNotExist(statErr) {
		body, _ := os.ReadFile(fx.overridePath)
		t.Fatalf("refused write still created override layer:\n%s", body)
	}
}

func TestSetSpendModelRateRefusesMalformedRates(t *testing.T) {
	fx := newPopRepoFixture(t)
	RegisterSpendModelRateModelValidator(func(string) error { return nil })
	t.Cleanup(func() { RegisterSpendModelRateModelValidator(nil) })

	err := SetSpendModelRateWith(fx.d, "composer-2.5", SpendModelRate{
		Prompt: "nope", Completion: "0.000002",
	})
	if err == nil {
		t.Fatal("expected refusal")
	}
	if _, statErr := os.Stat(fx.overridePath); !os.IsNotExist(statErr) {
		body, _ := os.ReadFile(fx.overridePath)
		t.Fatalf("refused write still created override layer:\n%s", body)
	}
}
