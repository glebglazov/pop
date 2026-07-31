package tasks

import (
	"strings"
	"testing"
)

func TestAgentUsageCapabilityValidate(t *testing.T) {
	t.Run("unset is invalid", func(t *testing.T) {
		err := (AgentUsageCapability{}).validate("codex")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("error should name preset and capability, got %v", err)
		}
	})
	t.Run("supported requires Extract", func(t *testing.T) {
		err := (AgentUsageCapability{Kind: CapabilitySupported}).validate("claude")
		if err == nil || !strings.Contains(err.Error(), "Extract") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("supported with Extract passes", func(t *testing.T) {
		cap := AgentUsageCapability{
			Kind:    CapabilitySupported,
			Extract: func([]streamEventRecord) TokenUsage { return TokenUsage{} },
		}
		if err := cap.validate("claude"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("blind requires Reason", func(t *testing.T) {
		err := (AgentUsageCapability{Kind: CapabilityBlind}).validate("kimi")
		if err == nil || !strings.Contains(err.Error(), "Reason") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("blind with Reason passes", func(t *testing.T) {
		cap := AgentUsageCapability{Kind: CapabilityBlind, Reason: "stream carries no usage block"}
		if err := cap.validate("kimi"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAgentCostCapabilityValidate(t *testing.T) {
	t.Run("unset is invalid", func(t *testing.T) {
		err := (AgentCostCapability{}).validate("cursor")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "cursor") || !strings.Contains(err.Error(), "cost") {
			t.Fatalf("error should name preset and capability, got %v", err)
		}
	})
	t.Run("supported requires Extract", func(t *testing.T) {
		err := (AgentCostCapability{Kind: CapabilitySupported}).validate("pi")
		if err == nil || !strings.Contains(err.Error(), "Extract") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("supported with Extract passes", func(t *testing.T) {
		cap := AgentCostCapability{
			Kind:    CapabilitySupported,
			Extract: func([]streamEventRecord) PartialCost { return PartialCost{} },
		}
		if err := cap.validate("pi"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("blind requires Reason", func(t *testing.T) {
		err := (AgentCostCapability{Kind: CapabilityBlind}).validate("claude")
		if err == nil || !strings.Contains(err.Error(), "Reason") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("blind with Reason passes", func(t *testing.T) {
		cap := AgentCostCapability{Kind: CapabilityBlind, Reason: "stream reports no dollar cost"}
		if err := cap.validate("claude"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAgentToolTimingCapabilityValidate(t *testing.T) {
	t.Run("unset is invalid", func(t *testing.T) {
		err := (AgentToolTimingCapability{}).validate("codex")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "tool-timing") {
			t.Fatalf("error should name preset and capability, got %v", err)
		}
	})
	t.Run("supported requires Extract", func(t *testing.T) {
		err := (AgentToolTimingCapability{Kind: CapabilitySupported}).validate("claude")
		if err == nil || !strings.Contains(err.Error(), "Extract") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("supported with Extract passes", func(t *testing.T) {
		cap := AgentToolTimingCapability{
			Kind:    CapabilitySupported,
			Extract: func([]streamEventRecord) ([]ToolTiming, []toolWindow) { return nil, nil },
		}
		if err := cap.validate("claude"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("blind requires Reason", func(t *testing.T) {
		err := (AgentToolTimingCapability{Kind: CapabilityBlind}).validate("kimi")
		if err == nil || !strings.Contains(err.Error(), "Reason") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("blind with Reason passes", func(t *testing.T) {
		cap := AgentToolTimingCapability{Kind: CapabilityBlind, Reason: "stream carries no tool pairing"}
		if err := cap.validate("kimi"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAgentActualModelCapabilityValidate(t *testing.T) {
	t.Run("unset is invalid", func(t *testing.T) {
		err := (AgentActualModelCapability{}).validate("cursor")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "cursor") || !strings.Contains(err.Error(), "actual-model") {
			t.Fatalf("error should name preset and capability, got %v", err)
		}
	})
	t.Run("supported requires Extract", func(t *testing.T) {
		err := (AgentActualModelCapability{Kind: CapabilitySupported}).validate("claude")
		if err == nil || !strings.Contains(err.Error(), "Extract") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("supported with Extract passes", func(t *testing.T) {
		cap := AgentActualModelCapability{
			Kind:    CapabilitySupported,
			Extract: func([]streamEventRecord) string { return "" },
		}
		if err := cap.validate("claude"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("blind requires Reason", func(t *testing.T) {
		err := (AgentActualModelCapability{Kind: CapabilityBlind}).validate("codex")
		if err == nil || !strings.Contains(err.Error(), "Reason") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("blind with Reason passes", func(t *testing.T) {
		cap := AgentActualModelCapability{Kind: CapabilityBlind, Reason: "stream carries no actual-model field"}
		if err := cap.validate("codex"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPresetUsageAndCostCapabilitiesDeclared(t *testing.T) {
	// validate is not yet wired into construction; assert every preset's
	// declared stance would pass so the later switch-on slice is a one-liner.
	for _, preset := range agentCatalogOrder {
		adapter, err := ResolveAgentAdapter(preset)
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		if err := adapter.UsageCapability().validate(preset); err != nil {
			t.Fatalf("%s usage: %v", preset, err)
		}
		if err := adapter.CostCapability().validate(preset); err != nil {
			t.Fatalf("%s cost: %v", preset, err)
		}
		if err := adapter.ToolTimingCapability().validate(preset); err != nil {
			t.Fatalf("%s tool-timing: %v", preset, err)
		}
		if err := adapter.ActualModelCapability().validate(preset); err != nil {
			t.Fatalf("%s actual-model: %v", preset, err)
		}
	}
}
