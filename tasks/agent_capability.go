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
