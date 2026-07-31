package tasks

import (
	"fmt"
	"strings"
)

// CapabilityKind is the supported/blind stance for a stream-shape adapter
// capability (ADR-0165). The zero value is never valid: an unset stance is
// indistinguishable from a forgotten decision, which is the hole this kind
// exists to close.
type CapabilityKind int

const (
	capabilityUnset CapabilityKind = iota // zero value — never valid
	CapabilitySupported
	CapabilityBlind
)

// AgentUsageCapability is a preset's declared stance on extracting TokenUsage
// from a Captured run's events. Supported carries the extraction rule; Blind
// carries a sentence naming what the stream lacks.
type AgentUsageCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) TokenUsage // required iff Supported
	Reason  string                               // required iff Blind
}

// validate reports whether this usage stance is a complete declaration.
// Construction does not call it yet — that waits until every capability is
// declared (ADR-0165).
func (c AgentUsageCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: usage capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: usage capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: usage capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: usage capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentCostCapability is a preset's declared stance on extracting PartialCost
// from a Captured run's events. Cost is partial by construction — only adapters
// whose stream reports dollars are Supported (ADR-0160).
type AgentCostCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) PartialCost // required iff Supported
	Reason  string                                // required iff Blind
}

// validate reports whether this cost stance is a complete declaration.
// Construction does not call it yet — that waits until every capability is
// declared (ADR-0165).
func (c AgentCostCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: cost capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: cost capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: cost capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: cost capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentToolTimingCapability is a preset's declared stance on extracting per-tool
// durations from a Captured run's events (ADR 0016, ADR-0165).
type AgentToolTimingCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) ([]ToolTiming, []toolWindow) // required iff Supported
	Reason  string                                               // required iff Blind
}

// validate reports whether this tool-timing stance is a complete declaration.
func (c AgentToolTimingCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: tool-timing capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: tool-timing capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: tool-timing capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: tool-timing capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentActualModelCapability is a preset's declared stance on reading the
// attempt's actual model from a Captured run's events (ADR-0165).
type AgentActualModelCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) string // required iff Supported
	Reason  string                           // required iff Blind
}

// validate reports whether this actual-model stance is a complete declaration.
func (c AgentActualModelCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: actual-model capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: actual-model capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: actual-model capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: actual-model capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentStreamRenderCapability is a preset's declared stance on rendering a
// Captured run's events into a readable stream replay (ADR-0165).
type AgentStreamRenderCapability struct {
	Kind   CapabilityKind
	Render func(streamEventRecord) []StreamEvent // required iff Supported
	Reason string                                // required iff Blind
}

// validate reports whether this stream-render stance is a complete declaration.
func (c AgentStreamRenderCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Render == nil {
			return fmt.Errorf("agent preset %q: stream-render capability is Supported but Render is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: stream-render capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: stream-render capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: stream-render capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentTurnCapability is a preset's declared stance on counting model calls in
// a Captured run (ADR-0165).
type AgentTurnCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) TurnCount // required iff Supported
	Reason  string                              // required iff Blind
}

// validate reports whether this turn stance is a complete declaration.
func (c AgentTurnCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: turn capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: turn capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: turn capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: turn capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentPeakInputCapability is a preset's declared stance on deriving the
// largest per-call context size from a Captured run (ADR-0165).
type AgentPeakInputCapability struct {
	Kind    CapabilityKind
	Extract func([]streamEventRecord) PeakInput // required iff Supported
	Reason  string                              // required iff Blind
}

// validate reports whether this peak-input stance is a complete declaration.
func (c AgentPeakInputCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Extract == nil {
			return fmt.Errorf("agent preset %q: peak-input capability is Supported but Extract is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: peak-input capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: peak-input capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: peak-input capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentReasoningCapability is a preset's declared stance on emitting and
// detecting a reasoning/thinking level for the Effort ladder (ADR-0049,
// ADR-0164, ADR-0166). Emitting and detecting are a matched pair: one
// declaration per preset so they cannot drift apart. A preset with no
// separate reasoning parameter is Blind; kimi's env channel is EnvKey on the
// same struct, not a parallel field.
type AgentReasoningCapability struct {
	Kind       CapabilityKind
	SpecTokens func(reasoning string) []string // required iff Supported
	Contains   func(args []string) bool        // required iff Supported; optional for Blind
	EnvKey     string                          // iff Supported and env-channel (kimi)
	Reason     string                          // required iff Blind
}

// validate reports whether this reasoning stance is a complete declaration.
func (c AgentReasoningCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.SpecTokens == nil {
			return fmt.Errorf("agent preset %q: reasoning capability is Supported but SpecTokens is nil", preset)
		}
		if c.Contains == nil {
			return fmt.Errorf("agent preset %q: reasoning capability is Supported but Contains is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: reasoning capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: reasoning capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: reasoning capability has unknown kind %d", preset, c.Kind)
	}
}
