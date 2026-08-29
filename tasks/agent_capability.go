package tasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
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

// AgentRateKeyCapability is a preset's declared rule for mapping a model
// identifier onto a Rate-table key (ADR-0218). Normalize may be nil: an
// adapter with no rule produces rate-blind runs rather than a build or run
// failure. This is not a stream-shape capability — it transforms a model
// string and needs no captured-stream fixture (ADR-0165).
type AgentRateKeyCapability struct {
	Normalize func(model string) string
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

// AgentTurnCapEnforcementCapability is a preset's declared stance on being told
// to cap the Turns one implementation attempt may spend (ADR-0190). Invocation
// shape, so no fixture backs it (ADR-0166). Emitting the flag and detecting a
// hand-written one are a matched pair on one struct, as with reasoning, so they
// cannot drift apart. Only claude is Supported; a Blind preset carries a sentence
// saying why its cap is out of argv's reach, and a bound pop cannot put on the
// command line is never pretended to be in effect.
type AgentTurnCapEnforcementCapability struct {
	Kind       CapabilityKind
	SpecTokens func(limit int) []string // required iff Supported
	Contains   func(args []string) bool // required iff Supported; optional for Blind
	Reason     string                   // required iff Blind
}

// validate reports whether this turn-cap enforcement stance is a complete declaration.
func (c AgentTurnCapEnforcementCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.SpecTokens == nil {
			return fmt.Errorf("agent preset %q: turn-cap enforcement capability is Supported but SpecTokens is nil", preset)
		}
		if c.Contains == nil {
			return fmt.Errorf("agent preset %q: turn-cap enforcement capability is Supported but Contains is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: turn-cap enforcement capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: turn-cap enforcement capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: turn-cap enforcement capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentTurnCapExhaustionCapability is a preset's declared stance on recognising
// that a run ended because it reached its Turn cap (ADR-0190). Stream-shape, so
// a captured run backs it (ADR-0165), and deliberately separate from
// AgentTurnCapEnforcementCapability: being able to set a cap and being able to
// see one hit are different answers per adapter — kimi would accept a cap whose
// exhaustion appears only on stderr, which is neither capability's seam.
//
// Exhausted reads the ending out of the agent's own stream and exit status. It
// is never given a turn count, because a count is pop's measurement of the run
// and not the agent's statement about why it stopped.
type AgentTurnCapExhaustionCapability struct {
	Kind      CapabilityKind
	Exhausted func(events []streamEventRecord, exitCode int) bool // required iff Supported
	Reason    string                                              // required iff Blind
}

// validate reports whether this turn-cap exhaustion stance is a complete declaration.
func (c AgentTurnCapExhaustionCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Exhausted == nil {
			return fmt.Errorf("agent preset %q: turn-cap exhaustion capability is Supported but Exhausted is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: turn-cap exhaustion capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: turn-cap exhaustion capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: turn-cap exhaustion capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentRefusalSignatureCapability is a preset's declared stance on recognising
// that a provider refused the request at all — the reading upstream of
// AgentQuotaResetCapability, which only dates a refusal somebody has already
// found (ADR-0234).
//
// Supported means the capture carries typed fields that state the refusal:
// Structured reads them, and Markers stay beneath as the reading for a provider
// that changes its event schema rather than its wording. Blind means this
// adapter has no structured channel to read — cursor's bare stderr lines,
// kimi's stderr diagnostics — and its own prose detector answers instead. That
// absence is stated so a later reader cannot mistake it for an unfinished job.
type AgentRefusalSignatureCapability struct {
	Kind CapabilityKind
	// Structured reports whether the typed fields of one whole capture state a
	// refusal, and which Quota window class they name. Required iff Supported.
	Structured func(raw string) (AgentQuotaWindowClass, bool)
	// Markers are the provider's own sentences, each paired with the window class
	// it announces. They detect a refusal whose typed fields are absent, and name
	// the class for one whose window field is.
	Markers []AgentRefusalMarker
	Reason  string // required iff Blind
}

// AgentRefusalMarker is one sentence a provider writes when it refuses, and the
// Quota window class that sentence names.
type AgentRefusalMarker struct {
	Sentence string
	Class    AgentQuotaWindowClass
}

// validate reports whether this refusal-signature stance is a complete declaration.
func (c AgentRefusalSignatureCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.Structured == nil {
			return fmt.Errorf("agent preset %q: refusal-signature capability is Supported but Structured is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: refusal-signature capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: refusal-signature capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: refusal-signature capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentQuotaResetCapability is a preset's declared stance on deriving PauseResetAt
// from quota diagnostics (ADR-0166).
type AgentQuotaResetCapability struct {
	Kind    CapabilityKind
	ResetAt func(reason string, now time.Time) time.Time // required iff Supported
	Reason  string                                        // required iff Blind
}

// validate reports whether this quota-reset stance is a complete declaration.
func (c AgentQuotaResetCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if c.ResetAt == nil {
			return fmt.Errorf("agent preset %q: quota-reset capability is Supported but ResetAt is nil", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: quota-reset capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: quota-reset capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: quota-reset capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentEffortLadderCapability is a preset's declared stance on Pop's built-in
// effort tier → model mapping when config does not override it (ADR-0049,
// ADR-0166).
type AgentEffortLadderCapability struct {
	Kind   CapabilityKind
	Ladder map[string][]config.EffortModel // required iff Supported
	Reason string                          // required iff Blind
}

// validate reports whether this effort-ladder stance is a complete declaration.
func (c AgentEffortLadderCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if len(c.Ladder) == 0 {
			return fmt.Errorf("agent preset %q: effort-ladder capability is Supported but Ladder is empty", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: effort-ladder capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: effort-ladder capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: effort-ladder capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentAttendedArgsCapability is a preset's declared stance on the arguments an
// attended session launches with — the auto-approval posture its headless prefix
// already asserts, spelled the way its interactive binary spells it (ADR-0187).
// Supported carries those arguments; Blind carries a sentence naming why the CLI
// has none, and a Blind preset launches bare rather than refusing to launch.
type AgentAttendedArgsCapability struct {
	Kind   CapabilityKind
	Args   []string // required iff Supported
	Reason string   // required iff Blind
}

// validate reports whether this attended-args stance is a complete declaration.
func (c AgentAttendedArgsCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if len(c.Args) == 0 {
			return fmt.Errorf("agent preset %q: attended-args capability is Supported but Args is empty", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: attended-args capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: attended-args capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: attended-args capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentReadOnlyPostureCapability is a preset's declared stance on being told to
// run without the ability to change the checkout it was pointed at (ADR-0221).
// Invocation shape, so no captured stream backs it (ADR-0166). Supported carries
// the arguments that constrain the CLI — a tool denial on one agent, a read-only
// sandbox on another — plus the headless-prefix flags they must displace, since
// a prefix that bypasses the sandbox outranks a later request for one. Blind
// carries a sentence naming why the CLI has no such argument, and a Blind preset
// runs exactly as it does today rather than refusing to run: the guarantee
// pop reports is the one it actually obtained.
type AgentReadOnlyPostureCapability struct {
	Kind CapabilityKind
	Args []string // required iff Supported
	// Withdraws are headless-prefix flag names this posture takes back out of
	// the command line, because leaving them in would silently outrank Args.
	Withdraws []string
	Reason    string // required iff Blind
}

// validate reports whether this read-only posture stance is a complete declaration.
func (c AgentReadOnlyPostureCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if len(c.Args) == 0 {
			return fmt.Errorf("agent preset %q: read-only posture capability is Supported but Args is empty", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: read-only posture capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: read-only posture capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: read-only posture capability has unknown kind %d", preset, c.Kind)
	}
}

// AgentExecutableCapability is a preset's declared CLI executable basename
// (ADR-0166).
type AgentExecutableCapability struct {
	Kind   CapabilityKind
	Name   string // required iff Supported
	Reason string // required iff Blind
}

// validate reports whether this executable-name stance is a complete declaration.
func (c AgentExecutableCapability) validate(preset string) error {
	switch c.Kind {
	case CapabilitySupported:
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("agent preset %q: executable capability is Supported but Name is empty", preset)
		}
		return nil
	case CapabilityBlind:
		if strings.TrimSpace(c.Reason) == "" {
			return fmt.Errorf("agent preset %q: executable capability is Blind but Reason is empty", preset)
		}
		return nil
	case capabilityUnset:
		return fmt.Errorf("agent preset %q: executable capability is unset", preset)
	default:
		return fmt.Errorf("agent preset %q: executable capability has unknown kind %d", preset, c.Kind)
	}
}
